package drive

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"
)

const maxMultipartUploadConcurrency = 4

type UploadExecutionProgress struct {
	Bytes                int64
	FileID               string
	IsServerDeduplicated bool
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

func (d *Drive) executeUploadPlanV2(
	ctx context.Context,
	files []FinalUploadFile,
	plan UploadPlan,
	concurrency int,
	onProgress func(UploadExecutionProgress),
) error {
	if d == nil || d.http == nil {
		return errDriveHTTPUnconfigured
	}
	rules, err := d.UploadRules(ctx)
	if err != nil {
		return err
	}
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
	for _, item := range plan.Items {
		if item.BundleID != "" {
			bundleByClientID[item.ClientID] = item.BundleID
		}
	}

	pendingByIntent := make(map[string][]FinalUploadFile)
	intentOrder := make([]string, 0)
	returned := make(map[string]struct{}, len(plan.Items))
	rejected := make([]UploadPlanItem, 0)
	staged := make(map[string]struct{})
	bundleCredits := make(map[string]int64)
	failedBundles := make(map[string]struct{})
	failures := make([]error, 0)
	var stateMu sync.Mutex
	var progressMu sync.Mutex
	bundleCancels := make(map[string]context.CancelFunc, len(plan.Bundles))
	bundleContexts := make(map[string]context.Context, len(plan.Bundles))
	for bundleID := range plan.Bundles {
		bundleContexts[bundleID], bundleCancels[bundleID] = context.WithCancel(ctx)
	}
	defer func() {
		for _, cancel := range bundleCancels {
			cancel()
		}
	}()

	emitProgress := func(progress UploadExecutionProgress) {
		if onProgress == nil {
			return
		}
		progressMu.Lock()
		onProgress(progress)
		progressMu.Unlock()
	}
	addFailure := func(failure error) {
		stateMu.Lock()
		failures = append(failures, failure)
		stateMu.Unlock()
	}
	isBundleFailed := func(bundleID string) bool {
		stateMu.Lock()
		_, failed := failedBundles[bundleID]
		stateMu.Unlock()
		return failed
	}

	report := func(file FinalUploadFile, bytes int64, deduplicated bool) {
		if bundleID := bundleByClientID[file.FID]; bundleID != "" {
			stateMu.Lock()
			bundleCredits[bundleID] += bytes
			stateMu.Unlock()
		}
		emitProgress(UploadExecutionProgress{Bytes: bytes, IsServerDeduplicated: deduplicated})
	}
	markReady := func(file FinalUploadFile, bytes int64, deduplicated bool) {
		if bundleByClientID[file.FID] != "" {
			stateMu.Lock()
			staged[file.FID] = struct{}{}
			stateMu.Unlock()
			report(file, bytes, deduplicated)
			return
		}
		emitProgress(UploadExecutionProgress{Bytes: bytes, FileID: file.FID, IsServerDeduplicated: deduplicated})
	}
	markIntentReady := func(source FinalUploadFile, copies []FinalUploadFile) {
		markReady(source, 0, false)
		for _, file := range copies {
			markReady(file, file.Size, true)
		}
	}
	abortBundle := func(bundleID string, cause error) {
		bundle, ok := plan.Bundles[bundleID]
		if !ok {
			return
		}
		abortCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		if err := d.abortNTEBundle(abortCtx, bundle); err != nil && d.log != nil {
			d.log.Error(errors.Join(cause, err), "Drive:UploadV2:bundle-abort:"+bundleID)
		}
	}
	failTargets := func(failure error, targets []FinalUploadFile) {
		bundleIDs := make(map[string]struct{})
		hasNonBundle := false
		for _, file := range targets {
			bundleID := bundleByClientID[file.FID]
			if bundleID == "" {
				hasNonBundle = true
				continue
			}
			bundleIDs[bundleID] = struct{}{}
		}
		newlyFailed := make([]string, 0, len(bundleIDs))
		stateMu.Lock()
		if hasNonBundle || len(bundleIDs) == 0 {
			failures = append(failures, failure)
		}
		for bundleID := range bundleIDs {
			if _, failed := failedBundles[bundleID]; failed {
				continue
			}
			failedBundles[bundleID] = struct{}{}
			failures = append(failures, failure)
			newlyFailed = append(newlyFailed, bundleID)
		}
		stateMu.Unlock()
		for _, bundleID := range newlyFailed {
			if cancel := bundleCancels[bundleID]; cancel != nil {
				cancel()
			}
			abortBundle(bundleID, failure)
		}
	}
	targetContext := func(targets []FinalUploadFile) context.Context {
		bundleID := ""
		for _, target := range targets {
			current := bundleByClientID[target.FID]
			if current == "" || (bundleID != "" && current != bundleID) {
				return ctx
			}
			bundleID = current
		}
		if bundleCtx := bundleContexts[bundleID]; bundleCtx != nil {
			return bundleCtx
		}
		return ctx
	}

	for _, item := range plan.Items {
		returned[item.ClientID] = struct{}{}
		file, ok := filesByID[item.ClientID]
		if !ok {
			continue
		}
		switch {
		case item.Status == "created" || item.Status == "exists":
			markReady(file, file.Size, true)
		case item.Status == "pending" && item.IntentID != "":
			if _, exists := pendingByIntent[item.IntentID]; !exists {
				intentOrder = append(intentOrder, item.IntentID)
			}
			pendingByIntent[item.IntentID] = append(pendingByIntent[item.IntentID], file)
		default:
			rejected = append(rejected, item)
		}
	}
	for _, file := range files {
		if _, ok := returned[file.FID]; !ok {
			rejected = append(rejected, UploadPlanItem{ClientID: file.FID, Status: "error", Reason: "upload_plan_item_missing"})
		}
	}
	for _, item := range rejected {
		file, ok := filesByID[item.ClientID]
		if !ok {
			continue
		}
		reason := item.Reason
		if reason == "" {
			reason = item.Status
		}
		failTargets(&UploadV2Error{Code: reason, Message: file.Name + ": " + reason}, []FinalUploadFile{file})
	}

	packed := make([]preparedUpload, 0, max(1, rules.Pack.MaxFiles))
	var packedBytes int64
	taskPool := newUploadTaskPool(ctx, concurrency)
	defer func() { _ = taskPool.Close() }()
	multipartSlots := make(chan struct{}, maxMultipartUploadConcurrency)
	queueTask := func(task func()) error {
		if err := taskPool.Submit(task); err != nil {
			d.abortAllNTEBundles(ctx, plan.Bundles)
			return err
		}
		return nil
	}
	flushPacked := func() error {
		groups := partitionPackedUploads(packed, rules.Pack)
		packed = make([]preparedUpload, 0, max(1, rules.Pack.MaxFiles))
		packedBytes = 0
		for _, group := range groups {
			if len(group.members) == 1 {
				member := group.members[0]
				if err := queueTask(func() {
					err := d.uploadPreparedDirect(ctx, member.upload, member.source, member.data, member.compression, func(bytes int64) {
						report(member.source, bytes, false)
					})
					if err != nil {
						addFailure(err)
						return
					}
					markIntentReady(member.source, member.copies)
				}); err != nil {
					return err
				}
				continue
			}
			members := slices.Clone(group.members)
			if err := queueTask(func() {
				if err := d.uploadPack(ctx, members, func(bytes int64) {
					emitProgress(UploadExecutionProgress{Bytes: bytes})
				}, markIntentReady); err != nil {
					addFailure(err)
				}
			}); err != nil {
				return err
			}
		}
		return nil
	}
	for _, intentID := range intentOrder {
		if err := ctx.Err(); err != nil {
			d.abortAllNTEBundles(ctx, plan.Bundles)
			return err
		}
		targets := pendingByIntent[intentID]
		allFailedBundles := len(targets) > 0
		allBundled := len(targets) > 0
		for _, target := range targets {
			bundleID := bundleByClientID[target.FID]
			if bundleID == "" {
				allBundled = false
				allFailedBundles = false
				continue
			}
			if !isBundleFailed(bundleID) {
				allFailedBundles = false
			}
		}
		if allFailedBundles {
			continue
		}
		upload, ok := plan.Uploads[intentID]
		if !ok {
			if allBundled {
				for _, file := range targets {
					markReady(file, file.Size, true)
				}
				continue
			}
			failTargets(fmt.Errorf("upload intent missing for %s", targets[0].Name), targets)
			continue
		}
		source := targets[0]
		if source.Size >= rules.DirectThreshold() {
			taskCtx := targetContext(targets)
			if err := queueTask(func() {
				select {
				case multipartSlots <- struct{}{}:
				case <-taskCtx.Done():
					return
				}
				defer func() { <-multipartSlots }()
				err := d.uploadParts(taskCtx, upload, source, rules, func(bytes int64) { report(source, bytes, false) })
				if err != nil {
					if taskCtx.Err() == nil || ctx.Err() != nil {
						failTargets(err, targets)
					}
					return
				}
				markIntentReady(source, targets[1:])
			}); err != nil {
				return err
			}
			continue
		}
		data, compression, err := prepareDirectUpload(source)
		if err != nil {
			failTargets(err, targets)
			continue
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
			taskCtx := targetContext(targets)
			if err := queueTask(func() {
				err := d.uploadPreparedDirect(taskCtx, upload, source, data, compression, func(bytes int64) { report(source, bytes, false) })
				if err != nil {
					if taskCtx.Err() == nil || ctx.Err() != nil {
						failTargets(err, targets)
					}
					return
				}
				markIntentReady(source, targets[1:])
			}); err != nil {
				return err
			}
			continue
		}
		packed = append(packed, member)
		packedBytes += member.payloadBytes
		if shouldFlushUploadPack(len(packed), packedBytes, rules.Pack) {
			if err := flushPacked(); err != nil {
				return err
			}
		}
	}

	if err := flushPacked(); err != nil {
		return err
	}
	if err := taskPool.Close(); err != nil {
		d.abortAllNTEBundles(ctx, plan.Bundles)
		return err
	}
	rolledBackBundles := make(map[string]struct{}, len(plan.Bundles))
	rollbackFailedBundles := func() {
		for bundleID := range failedBundles {
			if _, rolledBack := rolledBackBundles[bundleID]; rolledBack {
				continue
			}
			rolledBackBundles[bundleID] = struct{}{}
			if credited := bundleCredits[bundleID]; credited > 0 {
				emitProgress(UploadExecutionProgress{Bytes: -credited})
			}
		}
	}
	rollbackFailedBundles()

	for bundleID, bundle := range plan.Bundles {
		if _, failed := failedBundles[bundleID]; failed {
			continue
		}
		members := make([]FinalUploadFile, 0, len(bundle.MemberClientIDs))
		complete := true
		for _, clientID := range bundle.MemberClientIDs {
			file, exists := filesByID[clientID]
			_, isStaged := staged[clientID]
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
			failTargets(&UploadV2Error{Code: "nte_bundle_incomplete", Message: name + ": nte_bundle_incomplete"}, members)
			continue
		}
		if err := d.completeNTEBundle(ctx, bundle); err != nil {
			failTargets(err, members)
			continue
		}
		for _, file := range members {
			emitProgress(UploadExecutionProgress{FileID: file.FID})
		}
	}
	rollbackFailedBundles()
	if err := ctx.Err(); err != nil {
		d.abortAllNTEBundles(ctx, plan.Bundles)
		return err
	}
	if len(failures) > 0 {
		return errors.Join(failures...)
	}
	return nil
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
		if err != nil && d.log != nil {
			d.log.Error(err, "Drive:UploadV2:bundle-abort:"+strings.TrimSpace(id))
		}
	}
}
