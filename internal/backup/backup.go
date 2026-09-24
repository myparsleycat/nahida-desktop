// Package backup takes automatic backups of the registered games' mod folders
// and the folders the user adds, and restores them. Files travel through the
// drive's upload pipeline, filtered by the server's upload rules; the server
// keeps each run as a snapshot.
package backup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/wailsapp/wails/v3/pkg/application"

	"nahida.live/desktop/internal/db"
	"nahida.live/desktop/internal/drive"
	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/setting"
)

// shutdownWait bounds how long shutdown waits for a cancelled run to record
// its result.
const shutdownWait = 15 * time.Second

// abortTimeout bounds the request that abandons a snapshot after a failed run.
const abortTimeout = 10 * time.Second

// The events the service emits.
const (
	EventStatus = "backup:status"
	EventResult = "backup:result"
)

// The app state keys the service keeps.
const (
	stateClientDeviceID = "backup.clientDeviceId"
	stateServerDeviceID = "backup.serverDeviceId"
	stateDeviceName     = "backup.deviceName"
	stateLastRun        = "backup.lastRun"
	stateLastSuccessAt  = "backup.lastSuccessAt"
	// stateBaseSnapshotID names the snapshot the backup_committed rows hold.
	// It is emptied while those rows change, so a run cut short in between
	// takes the files from the server again instead of trusting them.
	stateBaseSnapshotID = "backup.baseSnapshotId"
)

// Errors a caller can tell apart.
var (
	ErrRunning        = errors.New("BACKUP_RUNNING")
	ErrNotLoggedIn    = errors.New("BACKUP_NOT_LOGGED_IN")
	ErrNoTargets      = errors.New("BACKUP_NO_TARGETS")
	ErrMissingTarget  = errors.New("BACKUP_TARGET_MISSING")
	ErrUnreadable     = errors.New("BACKUP_UNREADABLE_FILE")
	ErrNotDirectory   = errors.New("BACKUP_NOT_DIRECTORY")
	ErrDestination    = errors.New("RESTORE_DESTINATION_NOT_EMPTY")
	ErrNotConfigured  = errors.New("backup service is not configured")
	errTooManyMissing = errors.New("BACKUP_UPLOAD_FAILED")
)

// Remote is the drive surface a backup travels through.
type Remote interface {
	BackupJSON(ctx context.Context, method, route string, body, out any) error
	BackupFilter(ctx context.Context) (func(name string, size int64) string, error)
	UploadBackupFiles(
		ctx context.Context,
		snapshotID string,
		files []drive.BackupUploadFile,
		deleted []drive.BackupDeletedFile,
		onProgress func(int64),
	) ([]string, error)
	DownloadBackupFile(
		ctx context.Context,
		snapshotID string,
		file drive.BackupDownload,
		destination string,
		onProgress func(int64),
	) error
}

// Settings is the slice of the settings service the backup reads and writes.
type Settings interface {
	Get(ctx context.Context, key string) (any, error)
	Set(ctx context.Context, key string, value any) error
}

// Session answers whether a user is signed in.
type Session interface {
	IsLoggedIn(ctx context.Context) (bool, error)
}

// Options wire the service.
type Options struct {
	Remote    Remote
	Settings  Settings
	Session   Session
	Log       *infra.Log
	EventEmit func(name string, data ...any)
}

// Status is what the service is doing right now.
type Status struct {
	State      string `json:"state"`
	Trigger    string `json:"trigger,omitempty"`
	Processed  int    `json:"processed"`
	Total      int    `json:"total"`
	Bytes      int64  `json:"bytes"`
	TotalBytes int64  `json:"totalBytes"`
}

// The states of Status.
const (
	StateIdle       = "idle"
	StateScanning   = "scanning"
	StateHashing    = "hashing"
	StateUploading  = "uploading"
	StateCommitting = "committing"
	StateRestoring  = "restoring"
)

// RunResult is how the last backup run ended.
type RunResult struct {
	At         time.Time        `json:"at"`
	Trigger    string           `json:"trigger"`
	Outcome    string           `json:"outcome"`
	SnapshotID string           `json:"snapshotId,omitempty"`
	FileCount  int              `json:"fileCount"`
	TotalSize  int64            `json:"totalSize"`
	Skipped    map[string]int32 `json:"skipped"`
	Error      string           `json:"error,omitempty"`
}

// The outcomes of RunResult.
const (
	OutcomeCompleted = "completed"
	OutcomeUnchanged = "unchanged"
	OutcomeFailed    = "failed"
	OutcomeCancelled = "cancelled"
)

// Overview is everything the backup page shows about this device.
type Overview struct {
	Status     Status     `json:"status"`
	Targets    []Target   `json:"targets"`
	DeviceName string     `json:"deviceName"`
	LastRun    *RunResult `json:"lastRun"`
	NextRunAt  *time.Time `json:"nextRunAt"`
}

// Backup is the Wails service behind the backup page.
type Backup struct {
	opts Options

	mu     sync.Mutex
	client *db.Client
	status Status
	cancel context.CancelFunc
	// runs counts the backup or restore goroutine, which shutdown waits for so
	// it does not write its result into a closed database.
	runs    sync.WaitGroup
	ctx     context.Context
	stop    context.CancelFunc
	done    chan struct{}
	changed time.Time
	// startedAt is when the scheduler started, which is where the interval
	// counts from when the startup catch-up is off.
	startedAt time.Time

	watchMu    sync.Mutex
	watchRoots []string
	watch      interface{ Close() error }
	// watchRetryAt is when a watch of watchRoots that failed to open is tried
	// again.
	watchRetryAt time.Time

	now func() time.Time
}

// New builds the service. The database arrives later through UseClient.
func New(opts Options) *Backup {
	return &Backup{opts: opts, status: Status{State: StateIdle}, now: time.Now}
}

// UseClient binds the local database.
//
//wails:ignore
func (b *Backup) UseClient(client *db.Client) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.client = client
}

// ServiceStartup starts the scheduler.
func (b *Backup) ServiceStartup(ctx context.Context, _ application.ServiceOptions) error {
	b.mu.Lock()
	if b.stop != nil {
		b.mu.Unlock()
		return nil
	}
	runCtx, stop := context.WithCancel(context.WithoutCancel(ctx))
	b.ctx, b.stop, b.done = runCtx, stop, make(chan struct{})
	b.startedAt = b.now()
	done := b.done
	b.mu.Unlock()

	go func() {
		defer close(done)
		b.schedule(runCtx)
	}()
	return nil
}

// ServiceShutdown stops the scheduler, cancels a running backup and closes the
// folder watch.
func (b *Backup) ServiceShutdown() error {
	b.mu.Lock()
	stop, done, cancel := b.stop, b.done, b.cancel
	b.stop, b.done = nil, nil
	b.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if stop != nil {
		stop()
		<-done
	}
	finished := make(chan struct{})
	go func() {
		b.runs.Wait()
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(shutdownWait):
		b.report(errors.New("backup run did not stop before shutdown"), "shutdown", "wait", nil)
	}
	return b.closeWatch()
}

// GetOverview answers the state of the backup on this device.
func (b *Backup) GetOverview(ctx context.Context) (Overview, error) {
	client, err := b.requireClient()
	if err != nil {
		return Overview{}, err
	}
	targets, err := b.targets(ctx, client)
	if err != nil {
		return Overview{}, err
	}
	name, err := b.deviceName(ctx, client)
	if err != nil {
		return Overview{}, err
	}
	lastRun, err := b.lastRun(ctx, client)
	if err != nil {
		return Overview{}, err
	}
	next, err := b.nextRunAt(ctx, client)
	if err != nil {
		return Overview{}, err
	}

	b.mu.Lock()
	status := b.status
	b.mu.Unlock()
	return Overview{Status: status, Targets: targets, DeviceName: name, LastRun: lastRun, NextRunAt: next}, nil
}

// SetGameExcluded includes a registered game in the backup or leaves it out.
func (b *Backup) SetGameExcluded(ctx context.Context, game string, excluded bool) error {
	if b.opts.Settings == nil {
		return ErrNotConfigured
	}
	current, err := b.excludedGames(ctx)
	if err != nil {
		return err
	}
	next := make([]string, 0, len(current)+1)
	for _, item := range current {
		if item != game {
			next = append(next, item)
		}
	}
	if excluded {
		next = append(next, game)
	}
	return b.opts.Settings.Set(ctx, setting.KeyBackupExcludedGames, next)
}

// AddCustomPath adds a folder of this PC to the backup.
func (b *Backup) AddCustomPath(ctx context.Context, path string) error {
	client, err := b.requireClient()
	if err != nil {
		return err
	}
	path = filepath.Clean(strings.TrimSpace(path))
	if !isDirectory(path) {
		return ErrNotDirectory
	}
	label := filepath.Base(path)
	if label == "." || label == string(filepath.Separator) || strings.HasSuffix(label, ":") {
		label = path
	}
	return client.BackupCustomPaths.Insert(ctx, db.BackupCustomPathRow{ID: uuid.NewString(), Path: path, Label: label})
}

// RemoveCustomPath takes a folder the user added out of the backup.
func (b *Backup) RemoveCustomPath(ctx context.Context, id string) error {
	client, err := b.requireClient()
	if err != nil {
		return err
	}
	return client.BackupCustomPaths.Delete(ctx, id)
}

// RenameDevice changes the name this PC is listed under.
func (b *Backup) RenameDevice(ctx context.Context, name string) error {
	client, err := b.requireClient()
	if err != nil {
		return err
	}
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 80 {
		return errors.New("BACKUP_INVALID_DEVICE_NAME")
	}
	if err := b.putState(ctx, client, stateDeviceName, name); err != nil {
		return err
	}
	if loggedIn, _ := b.loggedIn(ctx); !loggedIn {
		return nil
	}
	_, err = b.registerDevice(ctx, client)
	return err
}

// RunNow starts a backup in the background.
func (b *Backup) RunNow(ctx context.Context) error {
	return b.start(ctx, "manual")
}

// Cancel stops the running backup or restore.
func (b *Backup) Cancel() {
	b.mu.Lock()
	cancel := b.cancel
	b.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (b *Backup) requireClient() (*db.Client, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.client == nil || b.opts.Remote == nil || b.opts.Settings == nil {
		return nil, ErrNotConfigured
	}
	return b.client, nil
}

func (b *Backup) targets(ctx context.Context, client *db.Client) ([]Target, error) {
	games, err := client.GamePaths.List(ctx)
	if err != nil {
		return nil, err
	}
	custom, err := client.BackupCustomPaths.List(ctx)
	if err != nil {
		return nil, err
	}
	excluded, err := b.excludedGames(ctx)
	if err != nil {
		return nil, err
	}
	return listTargets(games, custom, excluded)
}

func (b *Backup) excludedGames(ctx context.Context) ([]string, error) {
	value, err := b.opts.Settings.Get(ctx, setting.KeyBackupExcludedGames)
	if err != nil {
		return nil, err
	}
	games, _ := value.([]string)
	return games, nil
}

func (b *Backup) settingBool(ctx context.Context, key string) bool {
	value, err := b.opts.Settings.Get(ctx, key)
	enabled, _ := value.(bool)
	return err == nil && enabled
}

func (b *Backup) keepCount(ctx context.Context) int {
	value, err := b.opts.Settings.Get(ctx, setting.KeyBackupKeepCount)
	count, ok := value.(int)
	if err != nil || !ok || count < 1 {
		return 5
	}
	return count
}

func (b *Backup) interval(ctx context.Context) (time.Duration, error) {
	value, err := b.opts.Settings.Get(ctx, setting.KeyBackupInterval)
	if err != nil {
		return 0, fmt.Errorf("read backup interval: %w", err)
	}
	name, ok := value.(string)
	if !ok {
		return 0, fmt.Errorf("backup interval has invalid type %T", value)
	}
	duration, ok := setting.BackupIntervalDuration(name)
	if !ok {
		return 0, fmt.Errorf("unsupported backup interval %q", name)
	}
	return duration, nil
}

func (b *Backup) loggedIn(ctx context.Context) (bool, error) {
	if b.opts.Session == nil {
		return false, nil
	}
	return b.opts.Session.IsLoggedIn(ctx)
}

func (b *Backup) getState(ctx context.Context, client *db.Client, key string) (string, error) {
	value, err := client.AppState.GetValue(ctx, key)
	if err != nil || value == nil {
		return "", err
	}
	return *value, nil
}

func (b *Backup) putState(ctx context.Context, client *db.Client, key, value string) error {
	return client.AppState.Upsert(ctx, key, value, b.now().UTC().Format(time.RFC3339Nano))
}

func (b *Backup) deviceName(ctx context.Context, client *db.Client) (string, error) {
	name, err := b.getState(ctx, client, stateDeviceName)
	if err != nil || name != "" {
		return name, err
	}
	return hostName(), nil
}

// hostName is the name a PC is listed under until the user renames it.
func hostName() string {
	host, err := os.Hostname()
	if err != nil || strings.TrimSpace(host) == "" {
		return "PC"
	}
	return host
}

func (b *Backup) lastRun(ctx context.Context, client *db.Client) (*RunResult, error) {
	raw, err := b.getState(ctx, client, stateLastRun)
	if err != nil || raw == "" {
		return nil, err
	}
	// A record an older build wrote in another shape reads as no record.
	var result RunResult
	if decodeErr := json.Unmarshal([]byte(raw), &result); decodeErr != nil {
		b.report(decodeErr, "state", "decode-last-run", nil)
		return nil, nil
	}
	return &result, nil
}

func (b *Backup) lastSuccessAt(ctx context.Context, client *db.Client) (time.Time, error) {
	raw, err := b.getState(ctx, client, stateLastSuccessAt)
	if err != nil || raw == "" {
		return time.Time{}, err
	}
	at, parseErr := time.Parse(time.RFC3339Nano, raw)
	if parseErr != nil {
		b.report(parseErr, "state", "decode-last-success", nil)
		return time.Time{}, nil
	}
	return at, nil
}

func (b *Backup) setStatus(status Status) {
	b.mu.Lock()
	b.status = status
	b.mu.Unlock()
	if b.opts.EventEmit != nil {
		b.opts.EventEmit(EventStatus, status)
	}
}

func (b *Backup) report(err error, operation, stage string, fields map[string]any) {
	if err == nil || b.opts.Log == nil {
		return
	}
	_ = infra.ReportError(
		b.opts.Log,
		err,
		"Backup",
		infra.Diagnostic{Operation: operation, Stage: stage, Fields: fields},
	)
}
