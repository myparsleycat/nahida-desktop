package backup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/samber/lo"

	"nahida.live/desktop/internal/db"
	"nahida.live/desktop/internal/drive"
)

// missingToleranceFiles and missingToleranceShare bound how many files a run
// may give up on (unreadable, or not uploaded), the larger of a count and a
// share of the whole, before it fails instead of committing a snapshot with
// holes.
const (
	missingToleranceFiles = 20
	missingToleranceShare = 0.05
)

// statusInterval bounds how often progress is emitted.
const statusInterval = 250 * time.Millisecond

// Snapshot is one backup run the server keeps.
type Snapshot struct {
	ID          string           `json:"id"`
	DeviceID    string           `json:"deviceId"`
	State       string           `json:"state"`
	Trigger     string           `json:"trigger"`
	FileCount   int32            `json:"fileCount"`
	TotalSize   int64            `json:"totalSize"`
	Skipped     map[string]int32 `json:"skipped"`
	CreatedAt   time.Time        `json:"createdAt"`
	CompletedAt *time.Time       `json:"completedAt"`
}

// ManifestTarget is one folder of a snapshot.
type ManifestTarget struct {
	ID         string  `json:"id"`
	Kind       string  `json:"kind"`
	Game       *string `json:"game"`
	Label      string  `json:"label"`
	SourcePath string  `json:"sourcePath"`
}

// ManifestFile is one file of a snapshot.
type ManifestFile struct {
	ID         string    `json:"id"`
	TargetID   string    `json:"targetId"`
	Path       string    `json:"path"`
	SHA256     string    `json:"sha256"`
	Size       int64     `json:"size"`
	ModifiedAt time.Time `json:"modifiedAt"`
}

type manifestSnapshot struct {
	Snapshot
	Targets []ManifestTarget `json:"targets"`
}

type manifest struct {
	Snapshot manifestSnapshot `json:"snapshot"`
	Files    []ManifestFile   `json:"files"`
}

type serverDevice struct {
	ID string `json:"id"`
}

// ListSnapshots answers the snapshots the server keeps for this PC, the newest
// first.
func (b *Backup) ListSnapshots(ctx context.Context) ([]Snapshot, error) {
	client, err := b.requireClient()
	if err != nil {
		return nil, err
	}
	deviceID, err := b.getState(ctx, client, stateServerDeviceID)
	if err != nil {
		return nil, err
	}
	if deviceID == "" {
		return []Snapshot{}, nil
	}
	snapshots := []Snapshot{}
	err = b.opts.Remote.BackupJSON(
		ctx,
		http.MethodGet,
		"/backup/devices/"+url.PathEscape(deviceID)+"/snapshots",
		nil,
		&snapshots,
	)
	var apiErr *drive.BackupAPIError
	if errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound {
		return []Snapshot{}, nil
	}
	return snapshots, err
}

// DeleteSnapshot deletes one snapshot of this PC from the server.
func (b *Backup) DeleteSnapshot(ctx context.Context, snapshotID string) error {
	if _, err := b.requireClient(); err != nil {
		return err
	}
	return b.opts.Remote.BackupJSON(ctx, http.MethodDelete, "/backup/snapshots/"+url.PathEscape(snapshotID), nil, nil)
}

// GetSnapshotTargets answers the folders a snapshot holds, which is what the
// restore dialog offers.
func (b *Backup) GetSnapshotTargets(ctx context.Context, snapshotID string) ([]ManifestTarget, error) {
	if _, err := b.requireClient(); err != nil {
		return nil, err
	}
	loaded, err := b.manifest(ctx, snapshotID)
	if err != nil {
		return nil, err
	}
	return loaded.Snapshot.Targets, nil
}

func (b *Backup) manifest(ctx context.Context, snapshotID string) (manifest, error) {
	var loaded manifest
	err := b.opts.Remote.BackupJSON(
		ctx,
		http.MethodGet,
		"/backup/snapshots/"+url.PathEscape(snapshotID)+"/manifest",
		nil,
		&loaded,
	)
	return loaded, err
}

// start begins a run unless one is already going. A manual run reports why it
// cannot start; an automatic one just does not.
func (b *Backup) start(ctx context.Context, trigger string) error {
	client, err := b.requireClient()
	if err != nil {
		return err
	}
	loggedIn, err := b.loggedIn(ctx)
	if err != nil {
		return err
	}
	if !loggedIn {
		return ErrNotLoggedIn
	}

	b.mu.Lock()
	if b.cancel != nil {
		b.mu.Unlock()
		return ErrRunning
	}
	parent := b.ctx
	if parent == nil {
		parent = context.Background()
	}
	runCtx, cancel := context.WithCancel(parent)
	b.cancel = cancel
	b.runs.Add(1)
	b.mu.Unlock()

	go func() {
		defer b.runs.Done()
		defer func() {
			cancel()
			b.mu.Lock()
			b.cancel = nil
			b.mu.Unlock()
			b.setStatus(Status{State: StateIdle})
		}()
		result := b.run(runCtx, client, trigger)
		b.finish(context.WithoutCancel(runCtx), client, result)
	}()
	return nil
}

// finish records how a run ended and tells the page.
func (b *Backup) finish(ctx context.Context, client *db.Client, result RunResult) {
	// A run is dated by when it ended: the next interval and the backoff after
	// a failure both count from there.
	result.At = b.now().UTC()
	raw, err := json.Marshal(result)
	if err == nil {
		err = b.putState(ctx, client, stateLastRun, string(raw))
	}
	if err == nil && (result.Outcome == OutcomeCompleted || result.Outcome == OutcomeUnchanged) {
		err = b.putState(ctx, client, stateLastSuccessAt, result.At.UTC().Format(time.RFC3339Nano))
	}
	b.report(err, "run", "record-result", nil)
	if b.opts.EventEmit != nil {
		b.opts.EventEmit(EventResult, result)
	}
}

// run takes one backup: scan, hash, announce, upload and commit.
func (b *Backup) run(ctx context.Context, client *db.Client, trigger string) RunResult {
	result := RunResult{At: b.now().UTC(), Trigger: trigger, Skipped: map[string]int32{}}
	fail := func(err error, stage string) RunResult {
		result.Outcome = OutcomeFailed
		if ctx.Err() != nil {
			result.Outcome = OutcomeCancelled
		}
		result.Error = err.Error()
		if result.Outcome == OutcomeFailed {
			b.report(err, "run", stage, map[string]any{"trigger": trigger, "snapshotId": result.SnapshotID})
		}
		return result
	}

	b.setStatus(Status{State: StateScanning, Trigger: trigger})
	allTargets, err := b.targets(ctx, client)
	if err != nil {
		return fail(err, "targets")
	}
	for _, target := range allTargets {
		if !target.Excluded && target.Missing {
			return fail(fmt.Errorf("%w: %s", ErrMissingTarget, target.Path), "targets")
		}
	}
	targets := includedTargets(allTargets)
	if len(targets) == 0 {
		return fail(ErrNoTargets, "targets")
	}
	rules, err := b.opts.Remote.BackupRules(ctx)
	if err != nil {
		return fail(err, "rules")
	}
	rulesVersion := rules.Version
	scan, err := scanTargets(ctx, targets, rules.Filter)
	if err != nil {
		return fail(err, "scan")
	}
	result.Skipped = scan.skipped
	tolerance := missingTolerance(len(scan.files))

	totalBytes := lo.SumBy(scan.files, func(file scannedFile) int64 { return file.size })
	throttle := newStatusThrottle(
		b,
		Status{State: StateHashing, Trigger: trigger, Total: len(scan.files), TotalBytes: totalBytes},
	)
	unreadable, err := hashFiles(ctx, client.BackupFileCache, scan.files, max(2, runtime.NumCPU()/2), func(done int) {
		throttle.update(func(status *Status) { status.Processed = done })
	})
	if err != nil {
		return fail(err, "hash")
	}
	throttle.flush()

	// A file that is locked or vanished since the scan keeps what the base
	// snapshot has, so one open log does not cost the backup; many of them
	// fail the run.
	setAside := map[int]struct{}{}
	if len(unreadable) > 0 {
		unreadableErr := fmt.Errorf("%w: %d file(s)", ErrUnreadable, len(unreadable))
		if len(unreadable) > tolerance {
			return fail(unreadableErr, "hash")
		}
		b.report(unreadableErr, "run", "hash-skip", map[string]any{
			"trigger": trigger,
			"paths":   lo.Map(unreadable, func(index int, _ int) string { return scan.files[index].fullPath }),
		})
		for _, index := range unreadable {
			setAside[index] = struct{}{}
		}
		scan.skipped["unreadable"] += int32(len(unreadable))
	}

	// Content the server refused for good under the current upload rules is
	// not offered again; like an unreadable file, it keeps what the base
	// snapshot has until the content or the rules change.
	rejectedRows, err := client.BackupRejected.Current(ctx, rulesVersion)
	if err != nil {
		return fail(err, "rejected")
	}
	rejected := make(map[rejectedKey]string, len(rejectedRows))
	for _, row := range rejectedRows {
		rejected[rejectedKey{row.SHA256, row.Ext}] = row.Reason
	}
	for index, file := range scan.files {
		if _, gone := setAside[index]; gone {
			continue
		}
		if reason, ok := rejected[fileRejectedKey(file.sha256, file.relPath)]; ok {
			setAside[index] = struct{}{}
			scan.skipped[reason]++
		}
	}

	var held []committedKey
	if len(setAside) > 0 {
		for index := range setAside {
			held = append(held, committedKey{targetKey(targets[scan.files[index].target]), scan.files[index].relPath})
		}
		scan.files = lo.Reject(scan.files, func(_ scannedFile, index int) bool {
			_, gone := setAside[index]
			return gone
		})
		totalBytes = lo.SumBy(scan.files, func(file scannedFile) int64 { return file.size })
	}

	deviceID, err := b.registerDevice(ctx, client)
	if err != nil {
		return fail(err, "register-device")
	}

	base, err := b.getState(ctx, client, stateBaseSnapshotID)
	if err != nil {
		return fail(err, "base")
	}
	digest := manifestDigest(targets, scan.files)
	created, err := b.createSnapshot(ctx, deviceID, trigger, digest, base, targets)
	var apiErr *drive.BackupAPIError
	if errors.As(err, &apiErr) && apiErr.Code == "base_mismatch" {
		// The files this PC remembers are not the server's newest snapshot:
		// take that snapshot's list as the base and try once more.
		base = apiErr.LatestSnapshotID
		if err := b.rebase(ctx, client, base); err != nil {
			return fail(err, "rebase")
		}
		created, err = b.createSnapshot(ctx, deviceID, trigger, digest, base, targets)
	}
	if err != nil {
		return fail(err, "create-snapshot")
	}
	if created.Unchanged {
		result.Outcome = OutcomeUnchanged
		result.FileCount = len(scan.files)
		result.TotalSize = totalBytes
		return result
	}
	if len(created.TargetIDs) != len(targets) {
		return fail(errors.New("backup snapshot answered a different number of targets"), "create-snapshot")
	}
	result.SnapshotID = created.SnapshotID

	committed, err := client.BackupCommitted.All(ctx)
	if err != nil {
		return fail(err, "base")
	}
	changes := diffCommitted(targets, scan.files, committed, held)
	snapshot, kept, commitErr := b.upload(
		ctx, client, rulesVersion, created.SnapshotID, created.TargetIDs, targets, scan, changes, trigger, tolerance,
		len(unreadable),
	)
	if commitErr != nil {
		abortCtx, cancelAbort := context.WithTimeout(context.WithoutCancel(ctx), abortTimeout)
		defer cancelAbort()
		abortErr := b.opts.Remote.BackupJSON(
			abortCtx,
			http.MethodPost,
			"/backup/snapshots/"+url.PathEscape(created.SnapshotID)+"/abort",
			nil,
			nil,
		)
		b.report(abortErr, "run", "abort", map[string]any{"snapshotId": created.SnapshotID})
		return fail(commitErr, "upload")
	}

	if err := b.recordCommitted(ctx, client, snapshot.ID, targets, scan.files, kept, changes.deleted); err != nil {
		b.report(err, "run", "record-base", map[string]any{"snapshotId": snapshot.ID})
	}
	result.Outcome = OutcomeCompleted
	result.FileCount = int(snapshot.FileCount)
	result.TotalSize = snapshot.TotalSize
	result.Skipped = snapshot.Skipped
	return result
}

// createdSnapshot is how the server answers a new snapshot.
type createdSnapshot struct {
	Unchanged  bool     `json:"unchanged"`
	SnapshotID string   `json:"snapshotId"`
	TargetIDs  []string `json:"targetIds"`
}

// createSnapshot opens a snapshot taken against base, the snapshot whose files
// this PC remembers ("" for none).
func (b *Backup) createSnapshot(
	ctx context.Context,
	deviceID, trigger, digest, base string,
	targets []Target,
) (createdSnapshot, error) {
	type snapshotTarget struct {
		Key        string  `json:"key"`
		Kind       string  `json:"kind"`
		Game       *string `json:"game"`
		Label      string  `json:"label"`
		SourcePath string  `json:"sourcePath"`
	}
	var created createdSnapshot
	err := b.opts.Remote.BackupJSON(ctx, http.MethodPost, "/backup/snapshots", map[string]any{
		"deviceId":       deviceID,
		"trigger":        trigger,
		"manifestDigest": digest,
		"baseSnapshotId": lo.EmptyableToPtr(base),
		"targets": lo.Map(targets, func(target Target, _ int) snapshotTarget {
			return snapshotTarget{
				Key:        targetKey(target),
				Kind:       target.Kind,
				Game:       target.Game,
				Label:      target.Label,
				SourcePath: target.Path,
			}
		}),
	}, &created)
	return created, err
}

// rebase makes the files of the server's snapshot latest the base this PC
// compares against ("" for a device with no snapshot yet).
func (b *Backup) rebase(ctx context.Context, client *db.Client, latest string) error {
	if err := b.putState(ctx, client, stateBaseSnapshotID, ""); err != nil {
		return err
	}
	rows := []db.BackupCommittedRow{}
	if latest != "" {
		var loaded struct {
			Snapshot struct {
				Targets []struct {
					ID  string `json:"id"`
					Key string `json:"key"`
				} `json:"targets"`
			} `json:"snapshot"`
			Files []ManifestFile `json:"files"`
		}
		route := "/backup/snapshots/" + url.PathEscape(latest) + "/manifest"
		if err := b.opts.Remote.BackupJSON(ctx, http.MethodGet, route, nil, &loaded); err != nil {
			return err
		}
		keys := make(map[string]string, len(loaded.Snapshot.Targets))
		for _, target := range loaded.Snapshot.Targets {
			keys[target.ID] = target.Key
		}
		for _, file := range loaded.Files {
			if key := keys[file.TargetID]; key != "" {
				size, mtime := file.Size, file.ModifiedAt.UnixNano()
				rows = append(rows, db.BackupCommittedRow{
					TargetKey: key, RelPath: file.Path, SHA256: file.SHA256, Size: &size, Mtime: &mtime,
				})
			}
		}
	}
	if err := client.BackupCommitted.Replace(ctx, rows); err != nil {
		return err
	}
	return b.putState(ctx, client, stateBaseSnapshotID, latest)
}

// recordCommitted remembers the files of a committed snapshot: the scanned
// files it kept take their new content, the removed ones go, and so do the
// targets it no longer covers. A file the server refused or never received
// keeps what the base snapshot had, as the snapshot does.
func (b *Backup) recordCommitted(
	ctx context.Context,
	client *db.Client,
	snapshotID string,
	targets []Target,
	files []scannedFile,
	kept []int,
	deleted []db.BackupCommittedRow,
) error {
	if err := b.putState(ctx, client, stateBaseSnapshotID, ""); err != nil {
		return err
	}
	upserts := lo.Map(kept, func(index int, _ int) db.BackupCommittedRow {
		file := files[index]
		size, mtime := file.size, file.modified.UnixNano()
		return db.BackupCommittedRow{
			TargetKey: targetKey(targets[file.target]),
			RelPath:   file.relPath,
			SHA256:    file.sha256,
			Size:      &size,
			Mtime:     &mtime,
		}
	})
	keys := lo.Map(targets, func(target Target, _ int) string { return targetKey(target) })
	if err := client.BackupCommitted.Apply(ctx, keys, upserts, deleted); err != nil {
		return err
	}
	return b.putState(ctx, client, stateBaseSnapshotID, snapshotID)
}

// committedChanges is what a scan changes against the base snapshot: the
// indexes of the scanned files that are new or changed, and the remembered
// files of the scanned targets that are gone.
type committedChanges struct {
	changed []int
	deleted []db.BackupCommittedRow
}

// committedKey names one remembered file by its target key and path.
type committedKey struct{ target, path string }

// diffCommitted compares a scan with the remembered files. The held files were
// found but could not be read, so they are neither changed nor removed. The
// modification time is compared to the microsecond, which is what the server
// keeps: a base taken from the server's list matches the files it came from.
func diffCommitted(
	targets []Target,
	files []scannedFile,
	committed []db.BackupCommittedRow,
	held []committedKey,
) committedChanges {
	remembered := make(map[committedKey]db.BackupCommittedRow, len(committed))
	for _, row := range committed {
		remembered[committedKey{row.TargetKey, row.RelPath}] = row
	}

	var changes committedChanges
	seen := lo.Keyify(held)
	for index, file := range files {
		key := committedKey{targetKey(targets[file.target]), file.relPath}
		seen[key] = struct{}{}
		if row, ok := remembered[key]; !ok || row.SHA256 != file.sha256 ||
			row.Size == nil || *row.Size != file.size || row.Mtime == nil ||
			*row.Mtime/int64(time.Microsecond) != file.modified.UnixNano()/int64(time.Microsecond) {
			changes.changed = append(changes.changed, index)
		}
	}

	scanned := lo.Keyify(lo.Map(targets, func(target Target, _ int) string { return targetKey(target) }))
	for _, row := range committed {
		key := committedKey{row.TargetKey, row.RelPath}
		if _, covered := scanned[row.TargetKey]; !covered {
			continue
		}
		if _, ok := seen[key]; !ok {
			changes.deleted = append(changes.deleted, row)
		}
	}
	return changes
}

// upload sends what a snapshot changes against its base and commits it, and
// answers the indexes of the changed files the snapshot kept. Content the
// server still misses after the upload is dropped from the snapshot when it is
// a small share of the whole, so one unreadable file does not cost the backup;
// beyond that the run fails.
func (b *Backup) upload(
	ctx context.Context,
	client *db.Client,
	rulesVersion string,
	snapshotID string,
	targetIDs []string,
	targets []Target,
	scan scanResult,
	changes committedChanges,
	trigger string,
	tolerance, unreadable int,
) (Snapshot, []int, error) {
	files := make([]drive.BackupUploadFile, len(changes.changed))
	for position, index := range changes.changed {
		file := scan.files[index]
		files[position] = drive.BackupUploadFile{
			ClientID:   strconv.Itoa(index),
			TargetID:   targetIDs[file.target],
			RelPath:    file.relPath,
			FullPath:   file.fullPath,
			Size:       file.size,
			SHA256:     file.sha256,
			ModifiedAt: file.modified,
		}
	}
	targetIDByKey := make(map[string]string, len(targets))
	for index, target := range targets {
		targetIDByKey[targetKey(target)] = targetIDs[index]
	}
	deleted := lo.Map(changes.deleted, func(row db.BackupCommittedRow, _ int) drive.BackupDeletedFile {
		return drive.BackupDeletedFile{TargetID: targetIDByKey[row.TargetKey], RelPath: row.RelPath}
	})
	totalBytes := lo.SumBy(files, func(file drive.BackupUploadFile) int64 { return file.Size })

	var sent int64
	var sentMu sync.Mutex
	throttle := newStatusThrottle(
		b,
		Status{State: StateUploading, Trigger: trigger, Total: len(files), TotalBytes: totalBytes},
	)
	rejections, uploadErr := b.opts.Remote.UploadBackupFiles(ctx, snapshotID, files, deleted, func(bytes int64) {
		sentMu.Lock()
		sent += bytes
		current := sent
		sentMu.Unlock()
		throttle.update(func(status *Status) { status.Bytes = current })
	})
	throttle.flush()

	// The refusals are remembered before the commit and beside an upload
	// error, so a run that fails still does not offer the same content again.
	b.rememberRejections(ctx, client, rulesVersion, snapshotID, files, rejections)
	if err := ctx.Err(); err != nil {
		return Snapshot{}, nil, err
	}
	if errors.Is(uploadErr, drive.ErrBackupPlanning) {
		return Snapshot{}, nil, uploadErr
	}
	if errors.Is(uploadErr, drive.ErrBackupSourceRead) {
		return Snapshot{}, nil, uploadErr
	}
	dropped := lo.Keyify(lo.FilterMap(rejections, func(rejection drive.BackupRejection, _ int) (string, bool) {
		return rejection.ClientID, rejection.Denied
	}))
	keep := func() []int {
		return lo.FilterMap(files, func(file drive.BackupUploadFile, _ int) (int, bool) {
			index, _ := strconv.Atoi(file.ClientID)
			_, gone := dropped[file.ClientID]
			return index, !gone
		})
	}

	b.setStatus(Status{State: StateCommitting, Trigger: trigger, Total: len(files), TotalBytes: totalBytes})
	route := "/backup/snapshots/" + url.PathEscape(snapshotID) + "/commit"
	body := map[string]any{"skipped": scan.skipped}
	tolerance -= unreadable

	// A refused commit names at most a page of the missing hashes, so the
	// failed files add up over rounds until the commit goes through. Every
	// round adds at least one, and the tolerance bounds how many there are.
	var failed []drive.BackupUploadFile
	failedIDs := map[string]struct{}{}
	for {
		var snapshot Snapshot
		err := b.opts.Remote.BackupJSON(ctx, http.MethodPost, route, body, &snapshot)
		if err == nil {
			if len(failed) > 0 {
				b.report(
					uploadErr,
					"run",
					"partial-upload",
					map[string]any{"snapshotId": snapshotID, "dropped": len(failed)},
				)
			}
			for _, file := range failed {
				dropped[file.ClientID] = struct{}{}
			}
			return snapshot, keep(), nil
		}
		var apiErr *drive.BackupAPIError
		if !errors.As(err, &apiErr) || apiErr.Code != "backup_incomplete" {
			return Snapshot{}, nil, errors.Join(err, uploadErr)
		}

		missing := lo.Keyify(apiErr.Missing)
		added := 0
		for _, file := range files {
			if _, ok := missing[file.SHA256]; !ok {
				continue
			}
			if _, ok := failedIDs[file.ClientID]; ok {
				continue
			}
			failedIDs[file.ClientID] = struct{}{}
			failed = append(failed, file)
			added++
		}
		if added == 0 || len(failed) > tolerance {
			return Snapshot{}, nil, errors.Join(errTooManyMissing, uploadErr, err)
		}
		body["failed"] = lo.Map(failed, func(file drive.BackupUploadFile, _ int) map[string]string {
			return map[string]string{"targetId": file.TargetID, "path": file.RelPath, "reason": "upload_failed"}
		})
	}
}

// rejectedKey names refused content by its hash and file extension, since the
// server judges a file by both.
type rejectedKey struct{ sha256, ext string }

func fileRejectedKey(sha256, relPath string) rejectedKey {
	return rejectedKey{sha256, strings.ToLower(path.Ext(relPath))}
}

// rememberRejections records the content the server refused for good; other
// refusals are offered again next run. A failure to record costs only a retry
// next run, so it is logged, not raised.
func (b *Backup) rememberRejections(
	ctx context.Context,
	client *db.Client,
	rulesVersion, snapshotID string,
	files []drive.BackupUploadFile,
	rejections []drive.BackupRejection,
) {
	rejections = lo.Filter(rejections, func(rejection drive.BackupRejection, _ int) bool {
		return rejection.Permanent
	})
	if len(rejections) == 0 {
		return
	}
	byClientID := lo.KeyBy(files, func(file drive.BackupUploadFile) string { return file.ClientID })
	rows := lo.FilterMap(rejections, func(rejection drive.BackupRejection, _ int) (db.BackupRejectedRow, bool) {
		file, ok := byClientID[rejection.ClientID]
		if !ok || file.SHA256 == "" || rejection.Reason == "" {
			return db.BackupRejectedRow{}, false
		}
		key := fileRejectedKey(file.SHA256, file.RelPath)
		return db.BackupRejectedRow{
			SHA256: key.sha256, Ext: key.ext, Reason: rejection.Reason, RulesVersion: rulesVersion,
		}, true
	})
	err := client.BackupRejected.UpsertMany(context.WithoutCancel(ctx), rows)
	b.report(err, "run", "record-rejected", map[string]any{
		"snapshotId":   snapshotID,
		"rulesVersion": rulesVersion,
		"paths": lo.FilterMap(rejections, func(rejection drive.BackupRejection, _ int) (string, bool) {
			file, ok := byClientID[rejection.ClientID]
			return file.FullPath, ok
		}),
	})
}

// missingTolerance is how many of total files a run may leave out.
func missingTolerance(total int) int {
	return max(missingToleranceFiles, int(float64(total)*missingToleranceShare))
}

// registerDevice registers this PC with the server, or updates its name and
// retention, and answers its server id.
func (b *Backup) registerDevice(ctx context.Context, client *db.Client) (string, error) {
	clientID, err := b.getState(ctx, client, stateClientDeviceID)
	if err != nil {
		return "", err
	}
	if clientID == "" {
		clientID = uuid.NewString()
		if err := b.putState(ctx, client, stateClientDeviceID, clientID); err != nil {
			return "", err
		}
	}
	name, err := b.deviceName(ctx, client)
	if err != nil {
		return "", err
	}

	var device serverDevice
	if err := b.opts.Remote.BackupJSON(ctx, http.MethodPut, "/backup/devices/"+url.PathEscape(clientID), map[string]any{
		"name":      name,
		"keepCount": b.keepCount(ctx),
	}, &device); err != nil {
		return "", err
	}
	if err := b.putState(ctx, client, stateServerDeviceID, device.ID); err != nil {
		return "", err
	}
	return device.ID, nil
}

// statusThrottle emits progress at most every statusInterval.
type statusThrottle struct {
	backup  *Backup
	mu      sync.Mutex
	status  Status
	emitted time.Time
}

func newStatusThrottle(backup *Backup, status Status) *statusThrottle {
	backup.setStatus(status)
	return &statusThrottle{backup: backup, status: status, emitted: time.Now()}
}

func (t *statusThrottle) update(apply func(*Status)) {
	t.mu.Lock()
	apply(&t.status)
	if time.Since(t.emitted) < statusInterval {
		t.mu.Unlock()
		return
	}
	t.emitted = time.Now()
	status := t.status
	t.mu.Unlock()
	t.backup.setStatus(status)
}

func (t *statusThrottle) flush() {
	t.mu.Lock()
	status := t.status
	t.mu.Unlock()
	t.backup.setStatus(status)
}
