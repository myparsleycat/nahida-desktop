package backup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"runtime"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/samber/lo"

	"nahida.live/desktop/internal/db"
	"nahida.live/desktop/internal/drive"
)

// missingTolerance is how many files a run may give up on, beyond a share of
// the whole, before it fails instead of committing a snapshot with holes.
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
	filter, err := b.opts.Remote.BackupFilter(ctx)
	if err != nil {
		return fail(err, "rules")
	}
	scan, err := scanTargets(ctx, targets, filter)
	if err != nil {
		return fail(err, "scan")
	}
	result.Skipped = scan.skipped

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
	if len(unreadable) > 0 {
		return fail(fmt.Errorf("%w: %d file(s)", ErrUnreadable, len(unreadable)), "hash")
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
	changes := diffCommitted(targets, scan.files, committed)
	snapshot, kept, commitErr := b.upload(ctx, created.SnapshotID, created.TargetIDs, targets, scan, changes, trigger)
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

func diffCommitted(targets []Target, files []scannedFile, committed []db.BackupCommittedRow) committedChanges {
	type fileKey struct{ target, path string }
	remembered := make(map[fileKey]db.BackupCommittedRow, len(committed))
	for _, row := range committed {
		remembered[fileKey{row.TargetKey, row.RelPath}] = row
	}

	var changes committedChanges
	seen := make(map[fileKey]struct{}, len(files))
	for index, file := range files {
		key := fileKey{targetKey(targets[file.target]), file.relPath}
		seen[key] = struct{}{}
		if row, ok := remembered[key]; !ok || row.SHA256 != file.sha256 ||
			row.Size == nil || *row.Size != file.size || row.Mtime == nil || *row.Mtime != file.modified.UnixNano() {
			changes.changed = append(changes.changed, index)
		}
	}

	scanned := lo.Keyify(lo.Map(targets, func(target Target, _ int) string { return targetKey(target) }))
	for _, row := range committed {
		key := fileKey{row.TargetKey, row.RelPath}
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
	snapshotID string,
	targetIDs []string,
	targets []Target,
	scan scanResult,
	changes committedChanges,
	trigger string,
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
	denied, uploadErr := b.opts.Remote.UploadBackupFiles(ctx, snapshotID, files, deleted, func(bytes int64) {
		sentMu.Lock()
		sent += bytes
		current := sent
		sentMu.Unlock()
		throttle.update(func(status *Status) { status.Bytes = current })
	})
	throttle.flush()
	if err := ctx.Err(); err != nil {
		return Snapshot{}, nil, err
	}
	if errors.Is(uploadErr, drive.ErrBackupPlanning) {
		return Snapshot{}, nil, uploadErr
	}
	dropped := lo.Keyify(denied)
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
	var snapshot Snapshot
	err := b.opts.Remote.BackupJSON(ctx, http.MethodPost, route, body, &snapshot)

	if err == nil {
		return snapshot, keep(), nil
	}
	var apiErr *drive.BackupAPIError
	if !errors.As(err, &apiErr) || apiErr.Code != "backup_incomplete" {
		return Snapshot{}, nil, errors.Join(err, uploadErr)
	}
	missing := lo.Filter(files, func(file drive.BackupUploadFile, _ int) bool {
		return lo.Contains(apiErr.Missing, file.SHA256)
	})
	tolerance := max(missingToleranceFiles, int(float64(len(scan.files))*missingToleranceShare))
	if len(missing) == 0 || len(missing) > tolerance {
		return Snapshot{}, nil, errors.Join(errTooManyMissing, uploadErr, err)
	}

	body["failed"] = lo.Map(missing, func(file drive.BackupUploadFile, _ int) map[string]string {
		return map[string]string{"targetId": file.TargetID, "path": file.RelPath, "reason": "upload_failed"}
	})
	b.report(uploadErr, "run", "partial-upload", map[string]any{"snapshotId": snapshotID, "dropped": len(missing)})
	if err := b.opts.Remote.BackupJSON(ctx, http.MethodPost, route, body, &snapshot); err != nil {
		return Snapshot{}, nil, err
	}
	for _, file := range missing {
		dropped[file.ClientID] = struct{}{}
	}
	return snapshot, keep(), nil
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
