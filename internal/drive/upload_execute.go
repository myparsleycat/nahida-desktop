package drive

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path"
	"slices"
	"strings"
	"sync"
	"time"

	"nahida.live/desktop/internal/infra"
)

const maxMultipartUploadConcurrency = 4

type UploadExecutionProgress struct {
	Bytes                int64
	FileID               string
	IsServerDeduplicated bool
}

type uploadRun struct {
	drive            *Drive
	ctx              context.Context
	files            []FinalUploadFile
	plan             UploadPlan
	rules            UploadRules
	concurrency      int
	onProgress       func(UploadExecutionProgress)
	filesByID        map[string]FinalUploadFile
	bundleByClientID map[string]string

	taskPool          *uploadTaskPool
	multipartSlots    chan struct{}
	packed            []preparedUpload
	packedBytes       int64
	pendingByIntent   map[string][]FinalUploadFile
	intentOrder       []string
	returned          map[string]struct{}
	rejected          []UploadPlanItem
	staged            map[string]struct{}
	bundleCredits     map[string]int64
	bundleContexts    map[string]context.Context
	bundleCancels     map[string]context.CancelFunc
	failedBundles     map[string]struct{}
	rolledBackBundles map[string]struct{}
	failures          []error
	rejections        map[string]string
	stateMu           sync.Mutex
	progressMu        sync.Mutex
}

// executeUploadPlanV2 uploads what a plan still needs. Beside the joined
// failures it answers the client ids of the files whose content the server
// refused for good, with the refusal code.
func (d *Drive) executeUploadPlanV2(
	ctx context.Context,
	files []FinalUploadFile,
	plan UploadPlan,
	rules UploadRules,
	concurrency int,
	onProgress func(UploadExecutionProgress),
) (map[string]string, error) {
	if d == nil || d.http == nil {
		return nil, errDriveHTTPUnconfigured
	}
	run, err := d.newUploadRun(ctx, files, plan, rules, concurrency, onProgress)
	if err != nil {
		return nil, err
	}
	defer run.close()

	run.indexPlan()
	if err := run.dispatchIntents(); err != nil {
		return run.rejections, err
	}
	run.finalizeBundles()
	if err := ctx.Err(); err != nil {
		d.abortAllNTEBundles(ctx, plan.Bundles)
		return run.rejections, err
	}
	return run.rejections, errors.Join(run.failures...)
}

func (d *Drive) newUploadRun(
	ctx context.Context,
	files []FinalUploadFile,
	plan UploadPlan,
	rules UploadRules,
	concurrency int,
	onProgress func(UploadExecutionProgress),
) (*uploadRun, error) {
	filesByID := make(map[string]FinalUploadFile, len(files))
	for _, file := range files {
		filesByID[file.FID] = file
	}

	bundleByClientID := make(map[string]string)
	for bundleID, bundle := range plan.Bundles {
		for _, clientID := range bundle.MemberClientIDs {
			bundleByClientID[clientID] = bundleID
		}
	}

	// Plan items override bundle membership for their client id.
	for _, item := range plan.Items {
		if item.BundleID != "" {
			bundleByClientID[item.ClientID] = item.BundleID
		}
	}

	run := &uploadRun{
		drive:             d,
		ctx:               ctx,
		files:             files,
		plan:              plan,
		rules:             rules,
		concurrency:       concurrency,
		onProgress:        onProgress,
		filesByID:         filesByID,
		bundleByClientID:  bundleByClientID,
		pendingByIntent:   make(map[string][]FinalUploadFile),
		intentOrder:       make([]string, 0),
		returned:          make(map[string]struct{}, len(plan.Items)),
		rejected:          make([]UploadPlanItem, 0),
		staged:            make(map[string]struct{}),
		bundleCredits:     make(map[string]int64),
		bundleContexts:    make(map[string]context.Context, len(plan.Bundles)),
		bundleCancels:     make(map[string]context.CancelFunc, len(plan.Bundles)),
		failedBundles:     make(map[string]struct{}),
		rolledBackBundles: make(map[string]struct{}, len(plan.Bundles)),
		failures:          make([]error, 0),
		rejections:        make(map[string]string),
	}
	for bundleID := range plan.Bundles {
		run.bundleContexts[bundleID], run.bundleCancels[bundleID] = context.WithCancel(ctx)
	}
	return run, nil
}

func (r *uploadRun) close() {
	if r.taskPool != nil {
		_ = r.taskPool.Close()
	}
	for _, cancel := range r.bundleCancels {
		cancel()
	}
}

func (r *uploadRun) indexPlan() {
	for _, item := range r.plan.Items {
		r.returned[item.ClientID] = struct{}{}
		file, ok := r.filesByID[item.ClientID]
		if !ok {
			continue
		}
		switch {
		case item.Status == "created" || item.Status == "exists":
			r.markReady(file, file.Size, true)
		case item.Status == "pending" && item.IntentID != "":
			if _, exists := r.pendingByIntent[item.IntentID]; !exists {
				r.intentOrder = append(r.intentOrder, item.IntentID)
			}
			r.pendingByIntent[item.IntentID] = append(r.pendingByIntent[item.IntentID], file)
		default:
			r.rejected = append(r.rejected, item)
		}
	}

	for _, file := range r.files {
		if _, ok := r.returned[file.FID]; !ok {
			r.rejected = append(
				r.rejected,
				UploadPlanItem{ClientID: file.FID, Status: "error", Reason: "upload_plan_item_missing"},
			)
		}
	}

	for _, item := range r.rejected {
		file, ok := r.filesByID[item.ClientID]
		if !ok {
			continue
		}
		reason := item.Reason
		if reason == "" {
			reason = item.Status
		}
		r.failTargets(&UploadV2Error{Code: reason, Message: file.Name + ": " + reason}, []FinalUploadFile{file})
	}
}

func (r *uploadRun) dispatchIntents() error {
	r.packed = make([]preparedUpload, 0, max(1, r.rules.Pack.MaxFiles))
	r.taskPool = newUploadTaskPool(r.ctx, r.concurrency)
	r.multipartSlots = make(chan struct{}, maxMultipartUploadConcurrency)

	for _, intentID := range r.intentOrder {
		if err := r.dispatchIntent(intentID); err != nil {
			return err
		}
	}
	if err := r.flushPacked(); err != nil {
		return err
	}
	if err := r.taskPool.Close(); err != nil {
		r.drive.abortAllNTEBundles(r.ctx, r.plan.Bundles)
		return err
	}
	return nil
}

func (r *uploadRun) dispatchIntent(intentID string) error {
	if err := r.ctx.Err(); err != nil {
		r.drive.abortAllNTEBundles(r.ctx, r.plan.Bundles)
		return err
	}

	targets := r.pendingByIntent[intentID]
	allFailedBundles := len(targets) > 0
	allBundled := len(targets) > 0
	for _, target := range targets {
		bundleID := r.bundleByClientID[target.FID]
		if bundleID == "" {
			allBundled = false
			allFailedBundles = false
			continue
		}
		if !r.isBundleFailed(bundleID) {
			allFailedBundles = false
		}
	}
	if allFailedBundles {
		return nil
	}

	upload, ok := r.plan.Uploads[intentID]
	if !ok {
		if allBundled {
			for _, file := range targets {
				r.markReady(file, file.Size, true)
			}
			return nil
		}
		r.failTargets(fmt.Errorf("upload intent missing for %s", targets[0].Name), targets)
		return nil
	}

	source := targets[0]
	if source.Size >= r.rules.DirectUploadMaxLogicalBytes {
		return r.queuePartsIntent(upload, source, targets)
	}
	data, compression, useParts, err := prepareUploadRoute(
		source,
		upload,
		r.rules.Compression,
		r.rules.MaxUploadBodyBytes,
	)
	if err != nil {
		r.failTargets(err, targets)
		return nil
	}
	if useParts {
		return r.queuePartsIntent(upload, source, targets)
	}

	member := preparedUpload{
		upload:       upload,
		source:       source,
		copies:       slices.Clone(targets[1:]),
		data:         data,
		compression:  compression,
		payloadBytes: int64(len(data)),
		logicalSize:  source.Size,
	}
	if allBundled {
		return r.queueBundledDirectIntent(member, targets)
	}

	r.packed = append(r.packed, member)
	r.packedBytes += member.payloadBytes
	if shouldFlushUploadPack(len(r.packed), r.packedBytes, r.rules.Pack) {
		return r.flushPacked()
	}
	return nil
}

func (r *uploadRun) queuePartsIntent(upload UploadPlanEntry, source FinalUploadFile, targets []FinalUploadFile) error {
	taskCtx := r.targetContext(targets)
	return r.queueTask(func() {
		select {
		case r.multipartSlots <- struct{}{}:
		case <-taskCtx.Done():
			return
		}
		defer func() { <-r.multipartSlots }()

		err := r.drive.uploadParts(
			taskCtx,
			upload,
			source,
			r.rules,
			func(bytes int64) { r.report(source, bytes, false) },
		)
		if err != nil {
			if taskCtx.Err() == nil || r.ctx.Err() != nil {
				r.failTargets(err, targets)
			}
			return
		}
		r.markIntentReady(source, targets[1:])
	})
}

func (r *uploadRun) queueBundledDirectIntent(member preparedUpload, targets []FinalUploadFile) error {
	taskCtx := r.targetContext(targets)
	return r.queueTask(func() {
		err := r.drive.uploadPreparedDirect(
			taskCtx,
			member.upload,
			member.source,
			member.data,
			member.compression,
			func(bytes int64) { r.report(member.source, bytes, false) },
		)
		if err != nil {
			if taskCtx.Err() == nil || r.ctx.Err() != nil {
				r.failTargets(err, targets)
			}
			return
		}
		r.markIntentReady(member.source, targets[1:])
	})
}

func (r *uploadRun) flushPacked() error {
	groups := partitionPackedUploads(r.packed, r.rules.Pack)
	r.packed = make([]preparedUpload, 0, max(1, r.rules.Pack.MaxFiles))
	r.packedBytes = 0
	for _, group := range groups {
		if len(group.members) == 1 {
			member := group.members[0]
			if err := r.queueTask(func() {
				err := r.drive.uploadPreparedDirect(
					r.ctx,
					member.upload,
					member.source,
					member.data,
					member.compression,
					func(bytes int64) {
						r.report(member.source, bytes, false)
					},
				)
				if err != nil {
					r.failTargets(err, append([]FinalUploadFile{member.source}, member.copies...))
					return
				}
				r.markIntentReady(member.source, member.copies)
			}); err != nil {
				return err
			}
			continue
		}
		members := slices.Clone(group.members)
		if err := r.queueTask(func() {
			if err := r.drive.uploadPack(r.ctx, members, func(bytes int64) {
				r.emitProgress(UploadExecutionProgress{Bytes: bytes})
			}, r.markIntentReady); err != nil {
				r.failPack(err)
			}
		}); err != nil {
			return err
		}
	}
	return nil
}

func (r *uploadRun) queueTask(task func()) error {
	if err := r.taskPool.Submit(task); err != nil {
		r.drive.abortAllNTEBundles(r.ctx, r.plan.Bundles)
		return err
	}
	return nil
}

func (r *uploadRun) finalizeBundles() {
	r.rollbackFailedBundles()
	for bundleID, bundle := range r.plan.Bundles {
		if _, failed := r.failedBundles[bundleID]; failed {
			continue
		}
		members := make([]FinalUploadFile, 0, len(bundle.MemberClientIDs))
		complete := true
		for _, clientID := range bundle.MemberClientIDs {
			file, exists := r.filesByID[clientID]
			_, isStaged := r.staged[clientID]
			if !exists || !isStaged {
				complete = false
			}
			if exists {
				members = append(members, file)
			}
		}
		if !complete || len(members) != len(bundle.MemberClientIDs) {
			name := bundleID
			if len(members) > 0 {
				name = members[0].Name
			}
			r.failTargets(
				&UploadV2Error{Code: "nte_bundle_incomplete", Message: name + ": nte_bundle_incomplete"},
				members,
			)
			continue
		}
		if err := r.drive.completeNTEBundle(r.ctx, bundle); err != nil {
			r.failTargets(err, members)
			continue
		}
		for _, file := range members {
			r.emitProgress(UploadExecutionProgress{FileID: file.FID})
		}
	}
	r.rollbackFailedBundles()
}

func (r *uploadRun) rollbackFailedBundles() {
	for bundleID := range r.failedBundles {
		if _, rolledBack := r.rolledBackBundles[bundleID]; rolledBack {
			continue
		}
		r.rolledBackBundles[bundleID] = struct{}{}
		if credited := r.bundleCredits[bundleID]; credited > 0 {
			r.emitProgress(UploadExecutionProgress{Bytes: -credited})
		}
	}
}

func (r *uploadRun) emitProgress(progress UploadExecutionProgress) {
	if r.onProgress == nil {
		return
	}
	r.progressMu.Lock()
	r.onProgress(progress)
	r.progressMu.Unlock()
}

func (r *uploadRun) report(file FinalUploadFile, bytes int64, deduplicated bool) {
	if bundleID := r.bundleByClientID[file.FID]; bundleID != "" {
		r.stateMu.Lock()
		r.bundleCredits[bundleID] += bytes
		r.stateMu.Unlock()
	}
	r.emitProgress(UploadExecutionProgress{Bytes: bytes, IsServerDeduplicated: deduplicated})
}

func (r *uploadRun) markReady(file FinalUploadFile, bytes int64, deduplicated bool) {
	if r.bundleByClientID[file.FID] != "" {
		r.stateMu.Lock()
		r.staged[file.FID] = struct{}{}
		r.stateMu.Unlock()
		r.report(file, bytes, deduplicated)
		return
	}
	r.emitProgress(UploadExecutionProgress{Bytes: bytes, FileID: file.FID, IsServerDeduplicated: deduplicated})
}

func (r *uploadRun) markIntentReady(source FinalUploadFile, copies []FinalUploadFile) {
	r.markReady(source, 0, false)
	for _, file := range copies {
		r.markReady(file, file.Size, true)
	}
}

// failPack records the failures of a pack upload: a member's own failure fails
// that member's files, and a failure of the whole pack is recorded as is.
func (r *uploadRun) failPack(failure error) {
	failures := []error{failure}
	if joined, ok := failure.(interface{ Unwrap() []error }); ok {
		failures = joined.Unwrap()
	}
	for _, item := range failures {
		var member *packMemberError
		if errors.As(item, &member) {
			r.failTargets(item, member.files)
			continue
		}
		r.stateMu.Lock()
		r.failures = append(r.failures, item)
		r.stateMu.Unlock()
	}
}

func (r *uploadRun) isBundleFailed(bundleID string) bool {
	r.stateMu.Lock()
	_, failed := r.failedBundles[bundleID]
	r.stateMu.Unlock()
	return failed
}

func (r *uploadRun) failTargets(failure error, targets []FinalUploadFile) {
	bundleIDs := make(map[string]struct{})
	hasNonBundle := false
	for _, file := range targets {
		bundleID := r.bundleByClientID[file.FID]
		if bundleID == "" {
			hasNonBundle = true
			continue
		}
		bundleIDs[bundleID] = struct{}{}
	}
	code, rejected := uploadRejectionCode(failure)
	newlyFailed := make([]string, 0, len(bundleIDs))
	r.stateMu.Lock()
	if rejected {
		for _, file := range refusedContent(targets) {
			r.rejections[file.FID] = code
		}
	}
	if hasNonBundle || len(bundleIDs) == 0 {
		r.failures = append(r.failures, failure)
	}
	for bundleID := range bundleIDs {
		if _, failed := r.failedBundles[bundleID]; failed {
			continue
		}
		r.failedBundles[bundleID] = struct{}{}
		r.failures = append(r.failures, failure)
		newlyFailed = append(newlyFailed, bundleID)
	}
	r.stateMu.Unlock()
	for _, bundleID := range newlyFailed {
		if cancel := r.bundleCancels[bundleID]; cancel != nil {
			cancel()
		}
		r.abortBundle(bundleID, failure)
	}
}

// uploadRejectionCode answers the code of a failure by which the server refused
// a file's content for good.
func uploadRejectionCode(failure error) (string, bool) {
	var uploadErr *UploadV2Error
	if !errors.As(failure, &uploadErr) || !permanentUploadRejection(uploadErr.Code) {
		return "", false
	}
	return uploadErr.Code, true
}

// permanentUploadRejection reports whether a refusal code judges the content
// and name of a file against the upload rules, so sending the same content
// under the same name and rules is refused again.
func permanentUploadRejection(code string) bool {
	switch code {
	case "unsupported_file_type", string(uploadFileDenialExtension), string(uploadFileDenialSize):
		return true
	}
	return false
}

// refusedContent answers the targets a refusal of their upload is about. The
// copies of an intent share its content but not always its extension, and the
// server judged the source only; a failure shared by different contents, such
// as a bundle's, names none of them.
func refusedContent(targets []FinalUploadFile) []FinalUploadFile {
	if len(targets) == 0 {
		return nil
	}
	source := targets[0]
	if slices.ContainsFunc(targets, func(file FinalUploadFile) bool { return file.SHA256 != source.SHA256 }) {
		return nil
	}
	ext := strings.ToLower(path.Ext(source.Name))
	return slices.DeleteFunc(slices.Clone(targets), func(file FinalUploadFile) bool {
		return strings.ToLower(path.Ext(file.Name)) != ext
	})
}

func (r *uploadRun) abortBundle(bundleID string, cause error) {
	bundle, ok := r.plan.Bundles[bundleID]
	if !ok {
		return
	}
	abortCtx, cancel := context.WithTimeout(context.WithoutCancel(r.ctx), 30*time.Second)
	defer cancel()
	if err := r.drive.abortNTEBundle(abortCtx, bundle); err != nil {
		_ = infra.ReportError(r.drive.log, err, "Drive", infra.Diagnostic{
			Severity: infra.DiagnosticError, Operation: "upload", Stage: "bundle-abort",
			Fields: map[string]any{"bundleId": bundleID, "primaryError": cause.Error()},
		})
	}
}

func (r *uploadRun) targetContext(targets []FinalUploadFile) context.Context {
	bundleID := ""
	for _, target := range targets {
		current := r.bundleByClientID[target.FID]
		if current == "" || (bundleID != "" && current != bundleID) {
			return r.ctx
		}
		bundleID = current
	}
	if bundleCtx := r.bundleContexts[bundleID]; bundleCtx != nil {
		return bundleCtx
	}
	return r.ctx
}

func redistributeUploadFiles(files []FinalUploadFile) []FinalUploadFile {
	const largeThreshold = 50 * 1024 * 1024
	large := make([]FinalUploadFile, 0)
	small := make([]FinalUploadFile, 0)
	for _, file := range files {
		if file.Size >= largeThreshold {
			large = append(large, file)
		} else {
			small = append(small, file)
		}
	}
	if len(large) == 0 || len(small) == 0 {
		return slices.Clone(files)
	}
	interval := max(1, len(small)/len(large))
	out := make([]FinalUploadFile, 0, len(files))
	for len(small) > 0 || len(large) > 0 {
		count := min(interval, len(small))
		out = append(out, small[:count]...)
		small = small[count:]
		if len(large) > 0 {
			out = append(out, large[0])
			large = large[1:]
		}
	}
	return out
}

func shouldFlushUploadPack(files int, payloadBytes int64, pack UploadPackRules) bool {
	return files >= pack.MaxFiles || payloadBytes >= pack.PayloadBudget
}

func runUploadTasks(ctx context.Context, concurrency int, tasks []func()) error {
	pool := newUploadTaskPool(ctx, min(max(1, concurrency), max(1, len(tasks))))
	for _, task := range tasks {
		if err := pool.Submit(task); err != nil {
			_ = pool.Close()
			return err
		}
	}
	return pool.Close()
}

type uploadTaskPool struct {
	ctx   context.Context
	jobs  chan func()
	wg    sync.WaitGroup
	close sync.Once
}

func newUploadTaskPool(ctx context.Context, concurrency int) *uploadTaskPool {
	concurrency = max(1, concurrency)
	p := &uploadTaskPool{ctx: ctx, jobs: make(chan func(), concurrency)}
	p.wg.Add(concurrency)
	for range concurrency {
		go func() {
			defer p.wg.Done()
			for task := range p.jobs {
				task()
			}
		}()
	}
	return p
}

func (p *uploadTaskPool) Submit(task func()) error {
	select {
	case p.jobs <- task:
		return nil
	case <-p.ctx.Done():
		return p.ctx.Err()
	}
}

func (p *uploadTaskPool) Close() error {
	p.close.Do(func() { close(p.jobs) })
	p.wg.Wait()
	return p.ctx.Err()
}

func (d *Drive) completeNTEBundle(ctx context.Context, bundle NTEBundle) error {
	started := d.now()
	for attempt := 0; d.now().Sub(started) < uploadCompleteLimit; attempt++ {
		result, err := d.sendJSON(ctx, bundle.CompleteURL, map[string]any{"token": bundle.Form.Token})
		if err != nil {
			if ctx.Err() != nil {
				return err
			}
			result = uploadHTTPResult{reason: err.Error()}
		}
		if result.status >= 200 && result.status < 300 && result.status != http.StatusAccepted {
			return nil
		}
		if !retryableUploadResult(result) {
			return uploadResultError(result)
		}
		if err := d.sleep(ctx, retryDelay(min(attempt, 4), 30*time.Second)); err != nil {
			return err
		}
	}
	return &UploadV2Error{Code: "nte_bundle_incomplete"}
}

func (d *Drive) abortNTEBundle(ctx context.Context, bundle NTEBundle) error {
	result, err := d.sendJSON(ctx, bundle.AbortURL, map[string]any{"token": bundle.Form.Token})
	if err != nil {
		return err
	}
	if result.status < 200 || result.status >= 300 {
		return uploadResultError(result)
	}
	return nil
}

func (d *Drive) abortAllNTEBundles(ctx context.Context, bundles map[string]NTEBundle) {
	for id, bundle := range bundles {
		abortCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		err := d.abortNTEBundle(abortCtx, bundle)
		cancel()
		if err != nil {
			_ = infra.ReportError(d.log, err, "Drive", infra.Diagnostic{
				Severity: infra.DiagnosticError, Operation: "upload", Stage: "bundle-abort",
				Fields: map[string]any{"bundleId": strings.TrimSpace(id)},
			})
		}
	}
}
