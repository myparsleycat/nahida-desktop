package transfer

import (
	"context"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
)

const (
	updateEventName = "transfer:update"
	emitInterval    = 500 * time.Millisecond
	speedWindow     = 5 * time.Second
	mib             = 1024 * 1024
)

type Status string

const (
	StatusPending   Status = "pending"
	StatusPreparing Status = "preparing"
	StatusProgress  Status = "progress"
	StatusCompleted Status = "completed"
	StatusPaused    Status = "paused"
	StatusCanceled  Status = "canceled"
	StatusError     Status = "error"
)

type WindowProgressMode string

const (
	WindowProgressNormal        WindowProgressMode = "normal"
	WindowProgressIndeterminate WindowProgressMode = "indeterminate"
	WindowProgressError         WindowProgressMode = "error"
	WindowProgressPaused        WindowProgressMode = "paused"
)

type WindowProgress struct {
	Value float64
	Mode  WindowProgressMode
}

type PlanPhase string

const (
	PlanPermissionCheck PlanPhase = "permission_check"
	PlanParentLookup    PlanPhase = "parent_lookup"
	PlanFileValidation  PlanPhase = "file_validation"
	PlanDedupLookup     PlanPhase = "dedup_lookup"
	PlanProcessing      PlanPhase = "processing"
)

type DownloadFile struct {
	ID         string  `json:"id" cbor:"id"`
	FileID     string  `json:"fileId" cbor:"fileId"`
	ParentID   *string `json:"parentId" cbor:"parentId"`
	Name       string  `json:"name" cbor:"name"`
	Size       int64   `json:"size" cbor:"size"`
	UncompSize *int64  `json:"uncompSize,omitempty" cbor:"uncompSize,omitempty"`
	CompAlg    *string `json:"compAlg" cbor:"compAlg"`
	URL        string  `json:"url" cbor:"url"`
	URLOrigin  string  `json:"urlOrigin,omitempty" cbor:"urlOrigin,omitempty"`
}

func LogicalFileBytes(file DownloadFile) int64 {
	if file.UncompSize != nil {
		return *file.UncompSize
	}
	return file.Size
}

type Root struct {
	ID       string  `json:"id" cbor:"id"`
	ParentID *string `json:"parentId" cbor:"parentId"`
	Name     string  `json:"name" cbor:"name"`
}

type Directory struct {
	ID       string  `json:"id" cbor:"id"`
	ParentID *string `json:"parentId" cbor:"parentId"`
	Name     string  `json:"name" cbor:"name"`
}

type DestinationKind string

const (
	DestinationFile      DestinationKind = "file"
	DestinationDirectory DestinationKind = "directory"
)

type DestinationTarget struct {
	Path string          `json:"path"`
	Kind DestinationKind `json:"kind"`
}

type Data struct {
	Root  *Root          `json:"root,omitempty"`
	Files []DownloadFile `json:"files"`
	Dirs  []Directory    `json:"dirs"`
}

// Snapshot is the renderer-safe transfer shape. The payload Data and runner
// state are deliberately absent, matching Electron's getDisplayTransfers.
type Snapshot struct {
	PID                string              `json:"pid"`
	Type               string              `json:"type"`
	QueueGroupID       *uint64             `json:"queueGroupId,omitempty"`
	CurrentID          string              `json:"currentId,omitempty"`
	Status             Status              `json:"status"`
	TotalSize          int64               `json:"totalSize"`
	TransferredSize    int64               `json:"transferedSize"`
	Progress           float64             `json:"progress"`
	Speed              float64             `json:"speed"`
	ETA                float64             `json:"eta"`
	StartTime          int64               `json:"startTime"`
	Name               string              `json:"name"`
	TotalFiles         int                 `json:"totalFiles"`
	TransferredFiles   int                 `json:"transferedFiles"`
	FailedFiles        int                 `json:"failedFiles"`
	Path               string              `json:"path,omitempty"`
	DestinationPaths   []string            `json:"destinationPaths,omitempty"`
	DestinationTargets []DestinationTarget `json:"destinationTargets,omitempty"`
	Error              string              `json:"error,omitempty"`
	ErrorCode          string              `json:"errorCode,omitempty"`
	PlanPhase          *PlanPhase          `json:"planPhase,omitempty"`
	PlanProgress       *float64            `json:"planProgress,omitempty"`
}

type Record struct {
	Snapshot
	Data Data `json:"data"`
}

type CreateParams struct {
	PID                string
	Type               string
	Name               string
	Path               string
	DestinationPaths   []string
	DestinationTargets []DestinationTarget
	CurrentID          string
	InitialStatus      Status
	Data               Data
	QueueGroupID       *uint64
	ManualStart        bool
	RestartData        any
}

// Runner performs one transfer attempt. Implementations must stop promptly
// when ctx is cancelled so pause, cancel, and shutdown preserve queue order.
type Runner func(ctx context.Context, transfer *Transfer, pid string) error

type Settings interface {
	GetPowerSaveBlockInTransfer(context.Context) (bool, error)
	GetDownloadBandwidthLimitMibps(context.Context) (int, error)
}

type startNotificationSettings interface {
	GetMoveTransferPageWhenStartTransfer(context.Context) (bool, error)
}

type Logger interface {
	Error(msg any, where string)
}

type Options struct {
	Settings           Settings
	Log                Logger
	EventEmit          func(name string, data ...any)
	PreventSuspension  func(bool) error
	SyncWindowProgress func(*WindowProgress)
	Now                func() time.Time
}

type Updates struct {
	Status             *Status
	CurrentID          *string
	TotalSize          *int64
	TotalFiles         *int
	TransferredSize    *int64
	Progress           *float64
	TransferredFiles   *int
	FailedFiles        *int
	Path               *string
	DestinationPaths   []string
	DestinationTargets []DestinationTarget
	Error              *string
	ErrorCode          *string
	PlanPhase          *PlanPhase
	PlanProgress       *float64
	ClearCurrentID     bool
	ClearError         bool
	ClearErrorCode     bool
	ClearPlanPhase     bool
	ClearPlanProgress  bool
}

type speedSample struct {
	at   time.Time
	size int64
}

type orderedSnapshot struct {
	snapshot Snapshot
	order    uint64
}

type entry struct {
	record       Record
	runner       Runner
	restartData  any
	cancel       context.CancelFunc
	completedIDs map[string]struct{}
	samples      []speedSample
	createdOrder uint64
	queueOrder   uint64
}

type Transfer struct {
	mu                 sync.RWMutex
	destinationMu      sync.RWMutex
	emitMu             sync.Mutex
	powerMu            sync.Mutex
	entries            map[string]*entry
	queueGroupSequence uint64
	creationSequence   uint64
	queueSequence      uint64
	settings           Settings
	log                Logger
	eventEmit          func(string, ...any)
	preventSuspension  func(bool) error
	syncWindowProgress func(*WindowProgress)
	powerBlocked       bool
	now                func() time.Time
	emitEvery          time.Duration
	lastProgressEmit   time.Time
	progressEmitTimer  *time.Timer
	progressEmitGen    uint64
	emitStopped        bool
	app                *application.App
	lifecycleCtx       context.Context
	lifecycleCancel    context.CancelFunc
	wake               chan struct{}
	queueDone          chan struct{}
	downloadBandwidth  *BandwidthLimiter
	slowChunks         *SlowChunkMonitor
}

func New() *Transfer {
	return NewWithOptions(Options{})
}

func NewWithOptions(opts Options) *Transfer {
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	return &Transfer{
		entries:            make(map[string]*entry),
		settings:           opts.Settings,
		log:                opts.Log,
		eventEmit:          opts.EventEmit,
		preventSuspension:  opts.PreventSuspension,
		syncWindowProgress: opts.SyncWindowProgress,
		now:                now,
		emitEvery:          emitInterval,
		wake:               make(chan struct{}, 1),
		downloadBandwidth:  NewBandwidthLimiter(),
		slowChunks:         NewSlowChunkMonitor(),
	}
}

// ServiceStartup starts the single sequential queue worker.
func (t *Transfer) ServiceStartup(ctx context.Context, _ application.ServiceOptions) error {
	if ctx == nil {
		ctx = context.Background()
	}
	t.mu.Lock()
	if t.lifecycleCancel != nil {
		t.mu.Unlock()
		return nil
	}
	t.cancelProgressEmitLocked()
	t.lastProgressEmit = time.Time{}
	t.emitStopped = false
	t.app = application.Get()
	t.lifecycleCtx, t.lifecycleCancel = context.WithCancel(ctx)
	workerCtx := t.lifecycleCtx
	t.queueDone = make(chan struct{})
	done := t.queueDone
	t.mu.Unlock()

	go func() {
		defer close(done)
		for {
			if err := t.ProcessQueue(workerCtx); err != nil && !errors.Is(err, context.Canceled) {
				t.logError(err, "transfer.queue")
			}
			select {
			case <-workerCtx.Done():
				return
			case <-t.wake:
			}
		}
	}()
	t.signalQueue()
	return nil
}

func (t *Transfer) ServiceShutdown() error {
	t.mu.Lock()
	t.emitStopped = true
	t.cancelProgressEmitLocked()
	cancelLifecycle := t.lifecycleCancel
	t.lifecycleCancel = nil
	for _, item := range t.entries {
		if item.cancel != nil {
			item.cancel()
		}
	}
	done := t.queueDone
	t.mu.Unlock()
	if cancelLifecycle != nil {
		cancelLifecycle()
	}
	if done != nil {
		<-done
	}
	if t.downloadBandwidth != nil {
		t.downloadBandwidth.Close()
	}
	if t.slowChunks != nil {
		t.slowChunks.Close()
	}
	t.emitMu.Lock()
	if t.syncWindowProgress != nil {
		t.syncWindowProgress(nil)
	}
	t.emitMu.Unlock()
	return t.setPowerBlocked(false)
}

// List returns newest-first renderer-safe snapshots.
func (t *Transfer) List() []Snapshot {
	t.mu.RLock()
	items := t.snapshotEntriesLocked()
	t.mu.RUnlock()
	return orderSnapshots(items)
}

func (t *Transfer) snapshotEntriesLocked() []orderedSnapshot {
	items := make([]orderedSnapshot, 0, len(t.entries))
	for _, item := range t.entries {
		items = append(items, orderedSnapshot{snapshot: cloneSnapshot(item.record.Snapshot), order: item.createdOrder})
	}
	return items
}

func orderSnapshots(items []orderedSnapshot) []Snapshot {
	slices.SortFunc(items, func(a, b orderedSnapshot) int {
		switch {
		case a.order > b.order:
			return -1
		case a.order < b.order:
			return 1
		default:
			return 0
		}
	})
	out := make([]Snapshot, len(items))
	for i := range items {
		out[i] = items[i].snapshot
	}
	return out
}

func (t *Transfer) Cancel(pid string) error {
	t.destinationMu.Lock()
	t.mu.Lock()
	item, ok := t.entries[pid]
	if !ok {
		t.mu.Unlock()
		t.destinationMu.Unlock()
		return fmt.Errorf("transfer %q not found", pid)
	}
	if isTerminal(item.record.Status) && item.cancel == nil {
		delete(t.entries, pid)
		shouldEmit := t.scheduleEmitLocked(true, t.now())
		t.mu.Unlock()
		t.destinationMu.Unlock()
		if shouldEmit {
			t.emit()
		}
		return t.RefreshPowerSaveBlock(context.Background())
	}
	if item.cancel != nil {
		item.cancel()
	}
	item.record.Status = StatusCanceled
	shouldEmit := t.scheduleEmitLocked(true, t.now())
	t.mu.Unlock()
	t.destinationMu.Unlock()
	if shouldEmit {
		t.emit()
	}
	t.signalQueue()
	return t.RefreshPowerSaveBlock(context.Background())
}

func (t *Transfer) Pause(pid string) error {
	t.destinationMu.Lock()
	t.mu.Lock()
	item, ok := t.entries[pid]
	if !ok {
		t.mu.Unlock()
		t.destinationMu.Unlock()
		return fmt.Errorf("transfer %q not found", pid)
	}
	if item.record.Status != StatusPending && item.record.Status != StatusPreparing && item.record.Status != StatusProgress {
		t.mu.Unlock()
		t.destinationMu.Unlock()
		return nil
	}
	item.record.Status = StatusPaused
	if item.cancel != nil {
		item.cancel()
	}
	shouldEmit := t.scheduleEmitLocked(true, t.now())
	t.mu.Unlock()
	t.destinationMu.Unlock()
	if shouldEmit {
		t.emit()
	}
	return t.RefreshPowerSaveBlock(context.Background())
}

func (t *Transfer) Resume(pid string) error {
	restartable, err := t.canRestart(pid)
	if err != nil || !restartable {
		return err
	}
	return t.ManualStart(pid)
}

func (t *Transfer) Retry(pid string) error {
	restartable, err := t.canRestart(pid)
	if err != nil || !restartable {
		return err
	}
	if err := t.ResetTransfer(pid); err != nil {
		return err
	}
	return t.ManualStart(pid)
}

func (t *Transfer) canRestart(pid string) (bool, error) {
	t.mu.RLock()
	item, ok := t.entries[pid]
	if !ok {
		t.mu.RUnlock()
		return false, fmt.Errorf("transfer %q not found", pid)
	}
	restartable := item.restartData != nil
	t.mu.RUnlock()
	return restartable, nil
}

func (t *Transfer) PauseAll() error {
	for _, item := range t.List() {
		if item.Status == StatusProgress {
			if err := t.Pause(item.PID); err != nil {
				return err
			}
		}
	}
	return nil
}

func (t *Transfer) ResumeAll() error {
	for _, item := range t.List() {
		if item.Status == StatusPaused {
			if err := t.Resume(item.PID); err != nil {
				return err
			}
		}
	}
	return nil
}

func (t *Transfer) Clear() error {
	t.destinationMu.Lock()
	t.mu.Lock()
	changed := false
	for pid, item := range t.entries {
		if isTerminal(item.record.Status) && item.cancel == nil {
			delete(t.entries, pid)
			changed = true
		}
	}
	shouldEmit := changed && t.scheduleEmitLocked(true, t.now())
	t.mu.Unlock()
	t.destinationMu.Unlock()
	if shouldEmit {
		t.emit()
	}
	return t.RefreshPowerSaveBlock(context.Background())
}

//wails:ignore
func (t *Transfer) UseSettings(settings Settings) {
	t.mu.Lock()
	t.settings = settings
	t.mu.Unlock()
}

//wails:ignore
func (t *Transfer) UsePreventSuspension(fn func(bool) error) {
	t.powerMu.Lock()
	t.preventSuspension = fn
	t.powerMu.Unlock()
}

//wails:ignore
func (t *Transfer) Create(params CreateParams) (Record, error) {
	if params.PID == "" {
		return Record{}, errors.New("transfer pid is required")
	}
	if params.Type != "upload" && params.Type != "download" {
		return Record{}, fmt.Errorf("unsupported transfer type %q", params.Type)
	}
	initialStatus := params.InitialStatus
	if initialStatus == "" {
		initialStatus = StatusPreparing
	}
	if !isValidStatus(initialStatus) {
		return Record{}, fmt.Errorf("unsupported transfer status %q", initialStatus)
	}
	totalSize := int64(0)
	for _, file := range params.Data.Files {
		if size := LogicalFileBytes(file); size > 0 {
			totalSize += size
		}
	}

	t.destinationMu.Lock()
	t.mu.Lock()
	if _, exists := t.entries[params.PID]; exists {
		t.mu.Unlock()
		t.destinationMu.Unlock()
		return Record{}, fmt.Errorf("transfer %q already exists", params.PID)
	}
	queueGroupID := params.QueueGroupID
	if queueGroupID == nil {
		hasOpen := false
		for _, item := range t.entries {
			if isOpen(item.record.Status) {
				hasOpen = true
				break
			}
		}
		if !hasOpen || t.queueGroupSequence == 0 {
			t.queueGroupSequence++
		}
		id := t.queueGroupSequence
		queueGroupID = &id
	}
	t.creationSequence++
	t.queueSequence++
	destinationPaths := slices.Clone(params.DestinationPaths)
	if params.DestinationTargets != nil {
		destinationPaths = destinationTargetPaths(params.DestinationTargets)
	}
	record := Record{
		Snapshot: Snapshot{
			PID:                params.PID,
			Type:               params.Type,
			QueueGroupID:       queueGroupID,
			CurrentID:          params.CurrentID,
			Status:             initialStatus,
			TotalSize:          totalSize,
			StartTime:          t.now().UnixMilli(),
			Name:               params.Name,
			TotalFiles:         len(params.Data.Files),
			Path:               params.Path,
			DestinationPaths:   destinationPaths,
			DestinationTargets: slices.Clone(params.DestinationTargets),
		},
		Data: cloneData(params.Data),
	}
	t.entries[params.PID] = &entry{
		record:       record,
		restartData:  params.RestartData,
		completedIDs: make(map[string]struct{}),
		createdOrder: t.creationSequence,
		queueOrder:   t.queueSequence,
	}
	shouldEmit := t.scheduleEmitLocked(true, t.now())
	t.mu.Unlock()
	t.destinationMu.Unlock()
	if shouldEmit {
		t.emit()
	}
	if initialStatus == StatusPending && !params.ManualStart {
		t.signalQueue()
	}
	err := t.RefreshPowerSaveBlock(context.Background())
	t.notifyStarted(context.Background(), params.Name)
	return cloneRecord(record), err
}

// AttachCancel connects a transfer entry to work managed outside the queue.
// It is used by server-side Drive imports so Transfer.Cancel stops the active
// HTTP stream through the same cancellation path as other transfers.
//
//wails:ignore
func (t *Transfer) AttachCancel(pid string, cancel context.CancelFunc) error {
	if cancel == nil {
		return errors.New("transfer cancel function is required")
	}
	t.destinationMu.Lock()
	t.mu.Lock()
	item, ok := t.entries[pid]
	if ok {
		item.cancel = cancel
	}
	t.mu.Unlock()
	t.destinationMu.Unlock()
	if !ok {
		return fmt.Errorf("transfer %q not found", pid)
	}
	return nil
}

// ClearCancel detaches externally managed work after it finishes.
//
//wails:ignore
func (t *Transfer) ClearCancel(pid string) {
	t.destinationMu.Lock()
	t.mu.Lock()
	if item, ok := t.entries[pid]; ok {
		item.cancel = nil
	}
	t.mu.Unlock()
	t.destinationMu.Unlock()
}

func (t *Transfer) notifyStarted(ctx context.Context, name string) {
	if t.eventEmit == nil {
		return
	}
	t.eventEmit("fn:toast", "전송이 시작되었습니다", map[string]any{"description": name})
	settings, ok := t.settings.(startNotificationSettings)
	if !ok {
		return
	}
	move, err := settings.GetMoveTransferPageWhenStartTransfer(ctx)
	if err != nil {
		t.logError(err, "transfer.startNotification")
		return
	}
	if move {
		t.eventEmit("fn:navi", "/transfer")
	}
}

//wails:ignore
func (t *Transfer) RegisterRunner(pid string, runner Runner) error {
	if runner == nil {
		return errors.New("transfer runner is required")
	}
	t.mu.Lock()
	item, ok := t.entries[pid]
	if ok {
		item.runner = runner
	}
	t.mu.Unlock()
	if !ok {
		return fmt.Errorf("transfer %q not found", pid)
	}
	t.signalQueue()
	return nil
}

//wails:ignore
func (t *Transfer) ProcessQueue(ctx context.Context) error {
	for {
		pid, runner, runCtx, ok := t.next(ctx)
		if !ok {
			return ctx.Err()
		}
		if runner == nil {
			return nil
		}
		err := runner(runCtx, t, pid)
		t.finishRun(pid, err)
	}
}

func (t *Transfer) next(parent context.Context) (string, Runner, context.Context, bool) {
	if err := parent.Err(); err != nil {
		return "", nil, nil, false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	var selected *entry
	for _, item := range t.entries {
		if item.record.Status != StatusPending {
			continue
		}
		if item.runner == nil {
			continue
		}
		if selected == nil || item.queueOrder < selected.queueOrder {
			selected = item
		}
	}
	if selected == nil {
		return "", nil, nil, true
	}
	runCtx, cancel := context.WithCancel(parent)
	selected.cancel = cancel
	return selected.record.PID, selected.runner, runCtx, true
}

func (t *Transfer) finishRun(pid string, runErr error) {
	t.destinationMu.Lock()
	t.mu.Lock()
	item, ok := t.entries[pid]
	if !ok {
		t.mu.Unlock()
		t.destinationMu.Unlock()
		return
	}
	item.cancel = nil
	if item.record.Status == StatusPaused || item.record.Status == StatusCanceled {
		shouldEmit := t.scheduleEmitLocked(true, t.now())
		t.mu.Unlock()
		t.destinationMu.Unlock()
		if shouldEmit {
			t.emit()
		}
		_ = t.RefreshPowerSaveBlock(context.Background())
		return
	}
	if runErr != nil && !isTerminal(item.record.Status) {
		item.record.Status = StatusError
		item.record.Error = runErr.Error()
	}
	shouldEmit := t.scheduleEmitLocked(true, t.now())
	t.mu.Unlock()
	t.destinationMu.Unlock()
	if shouldEmit {
		t.emit()
	}
	_ = t.RefreshPowerSaveBlock(context.Background())
}

//wails:ignore
func (t *Transfer) Update(pid string, updates Updates) error {
	now := t.now()
	reservationChange := updates.Status != nil || updates.DestinationPaths != nil || updates.DestinationTargets != nil
	if reservationChange {
		t.destinationMu.Lock()
	}
	t.mu.Lock()
	item, ok := t.entries[pid]
	if !ok {
		t.mu.Unlock()
		if reservationChange {
			t.destinationMu.Unlock()
		}
		return fmt.Errorf("transfer %q not found", pid)
	}
	applyUpdates(&item.record.Snapshot, updates)
	if updates.TransferredSize != nil && item.record.Status == StatusProgress {
		lastIsDuplicate := len(item.samples) > 0 && item.samples[len(item.samples)-1].size == item.record.TransferredSize
		if !lastIsDuplicate {
			item.samples = append(item.samples, speedSample{at: now, size: item.record.TransferredSize})
		}
		cutoff := now.Add(-speedWindow)
		first := 0
		for first < len(item.samples) && item.samples[first].at.Before(cutoff) {
			first++
		}
		item.samples = slices.Clone(item.samples[first:])
		if len(item.samples) > 1 {
			oldest := item.samples[0]
			newest := item.samples[len(item.samples)-1]
			elapsed := newest.at.Sub(oldest.at).Seconds()
			if elapsed > 0 {
				item.record.Speed = float64(newest.size-oldest.size) / elapsed
			}
		} else {
			item.record.Speed = 0
			item.record.ETA = 0
		}
		if item.record.Speed > 0 && item.record.TotalSize > item.record.TransferredSize {
			item.record.ETA = math.Ceil(float64(item.record.TotalSize-item.record.TransferredSize) / item.record.Speed)
		}
		if item.record.TotalSize > 0 {
			item.record.Progress = clamp(float64(item.record.TransferredSize)/float64(item.record.TotalSize)*100, 0, 100)
		} else if item.record.TransferredSize > 0 {
			item.record.Progress = 100
		}
	}
	shouldEmit := t.scheduleEmitLocked(updates.Status != nil, now)
	t.mu.Unlock()
	if reservationChange {
		t.destinationMu.Unlock()
	}
	if shouldEmit {
		t.emit()
	}
	if updates.Status != nil {
		return t.RefreshPowerSaveBlock(context.Background())
	}
	return nil
}

//wails:ignore
func (t *Transfer) Get(pid string) (Record, bool) {
	t.mu.RLock()
	item, ok := t.entries[pid]
	if !ok {
		t.mu.RUnlock()
		return Record{}, false
	}
	record := cloneRecord(item.record)
	t.mu.RUnlock()
	return record, true
}

//wails:ignore
func (t *Transfer) IsActiveDownloadDestination(path string) bool {
	t.destinationMu.RLock()
	defer t.destinationMu.RUnlock()
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.isActiveDownloadDestinationLocked(path)
}

// GuardDownloadDestinations takes a stable snapshot of active destination
// conflicts and prevents transfer registration or state changes until release.
// Callers must invoke release after the protected filesystem operations finish.
//
//wails:ignore
func (t *Transfer) GuardDownloadDestinations(paths []string) (blocked []bool, release func()) {
	if t == nil {
		return make([]bool, len(paths)), func() {}
	}
	t.destinationMu.RLock()
	t.mu.RLock()
	blocked = make([]bool, len(paths))
	for index, path := range paths {
		blocked[index] = t.isActiveDownloadDestinationLocked(path)
	}
	t.mu.RUnlock()
	return blocked, t.destinationMu.RUnlock
}

func (t *Transfer) isActiveDownloadDestinationLocked(path string) bool {
	for _, item := range t.entries {
		if item.record.Type != "download" ||
			(!isOpen(item.record.Status) && item.record.Status != StatusPaused && item.cancel == nil) {
			continue
		}
		if slices.ContainsFunc(item.record.DestinationPaths, func(destinationPath string) bool {
			return transferPathsOverlap(path, destinationPath)
		}) {
			return true
		}
	}
	return false
}

//wails:ignore
func (t *Transfer) SetData(pid string, data Data, totalSize int64, name string, destinationTargets []DestinationTarget) error {
	t.destinationMu.Lock()
	t.mu.Lock()
	item, ok := t.entries[pid]
	if !ok {
		t.mu.Unlock()
		t.destinationMu.Unlock()
		return fmt.Errorf("transfer %q not found", pid)
	}
	item.record.Data = cloneData(data)
	item.record.TotalSize = max(0, totalSize)
	item.record.TotalFiles = len(data.Files)
	item.record.DestinationPaths = destinationTargetPaths(destinationTargets)
	item.record.DestinationTargets = slices.Clone(destinationTargets)
	if name != "" {
		item.record.Name = name
	}
	shouldEmit := t.scheduleEmitLocked(true, t.now())
	t.mu.Unlock()
	t.destinationMu.Unlock()
	if shouldEmit {
		t.emit()
	}
	return nil
}

//wails:ignore
func (t *Transfer) All() []Record {
	t.mu.RLock()
	out := make([]Record, 0, len(t.entries))
	for _, item := range t.entries {
		out = append(out, cloneRecord(item.record))
	}
	t.mu.RUnlock()
	return out
}

//wails:ignore
func (t *Transfer) RestartData(pid string) (any, bool) {
	t.mu.RLock()
	item, ok := t.entries[pid]
	if !ok {
		t.mu.RUnlock()
		return nil, false
	}
	data := item.restartData
	t.mu.RUnlock()
	return data, true
}

//wails:ignore
func (t *Transfer) MarkFileCompleted(pid, fileID string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	item, ok := t.entries[pid]
	if !ok {
		return fmt.Errorf("transfer %q not found", pid)
	}
	item.completedIDs[fileID] = struct{}{}
	return nil
}

//wails:ignore
func (t *Transfer) CompletedFilesCount(pid string) int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	item, ok := t.entries[pid]
	if !ok {
		return 0
	}
	return len(item.completedIDs)
}

//wails:ignore
func (t *Transfer) MarkFileFailed(pid, message string) error {
	t.mu.Lock()
	item, ok := t.entries[pid]
	if !ok {
		t.mu.Unlock()
		return fmt.Errorf("transfer %q not found", pid)
	}
	item.record.FailedFiles++
	if message != "" && item.record.Error == "" {
		item.record.Error = message
	}
	shouldEmit := t.scheduleEmitLocked(true, t.now())
	t.mu.Unlock()
	if shouldEmit {
		t.emit()
	}
	return nil
}

//wails:ignore
func (t *Transfer) IsFileCompleted(pid, fileID string) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	item, ok := t.entries[pid]
	if !ok {
		return false
	}
	_, ok = item.completedIDs[fileID]
	return ok
}

//wails:ignore
func (t *Transfer) RefreshPowerSaveBlock(ctx context.Context) error {
	t.mu.RLock()
	settings := t.settings
	hasActive := false
	for _, item := range t.entries {
		if item.record.Status == StatusPreparing || item.record.Status == StatusProgress {
			hasActive = true
			break
		}
	}
	t.mu.RUnlock()
	enabled := false
	if hasActive && settings != nil {
		var err error
		enabled, err = settings.GetPowerSaveBlockInTransfer(ctx)
		if err != nil {
			return err
		}
	}
	return t.setPowerBlocked(hasActive && enabled)
}

//wails:ignore
func (t *Transfer) SetDownloadBandwidthLimitMibps(mibps int) {
	if t == nil || t.downloadBandwidth == nil {
		return
	}
	if mibps <= 0 {
		t.downloadBandwidth.SetRateBPS(0)
		return
	}
	t.downloadBandwidth.SetRateBPS(float64(mibps * mib))
}

//wails:ignore
func (t *Transfer) ApplyBandwidthLimitsFromSettings(ctx context.Context) error {
	t.mu.RLock()
	settings := t.settings
	t.mu.RUnlock()
	if settings == nil {
		return nil
	}
	mibps, err := settings.GetDownloadBandwidthLimitMibps(ctx)
	if err != nil {
		return err
	}
	t.SetDownloadBandwidthLimitMibps(mibps)
	return nil
}

//wails:ignore
func (t *Transfer) TakeDownloadBandwidth(ctx context.Context, bytes int64, onWait func()) error {
	if t == nil || t.downloadBandwidth == nil {
		return nil
	}
	return t.downloadBandwidth.Take(ctx, bytes, onWait)
}

// Take implements infra.DownloadLimiter without making the infrastructure
// package depend on this service package.
//
//wails:ignore
func (t *Transfer) Take(ctx context.Context, bytes int64, onWait func()) error {
	return t.TakeDownloadBandwidth(ctx, bytes, onWait)
}

//wails:ignore
func (t *Transfer) SlowChunks() *SlowChunkMonitor {
	if t == nil {
		return nil
	}
	return t.slowChunks
}

func (t *Transfer) setPowerBlocked(block bool) error {
	t.powerMu.Lock()
	defer t.powerMu.Unlock()
	if t.powerBlocked == block {
		return nil
	}
	if t.preventSuspension != nil {
		if err := t.preventSuspension(block); err != nil {
			return err
		}
	}
	t.powerBlocked = block
	return nil
}

//wails:ignore
func (t *Transfer) ManualStart(pid string) error {
	t.mu.RLock()
	item, ok := t.entries[pid]
	if !ok {
		t.mu.RUnlock()
		return fmt.Errorf("transfer %q not found", pid)
	}
	if item.record.Status == StatusProgress {
		t.mu.RUnlock()
		return nil
	}
	runningPID := ""
	for otherPID, other := range t.entries {
		if otherPID != pid && (other.record.Status == StatusProgress || other.record.Status == StatusPreparing) {
			runningPID = otherPID
			break
		}
	}
	t.mu.RUnlock()
	if runningPID != "" {
		if err := t.Pause(runningPID); err != nil {
			return err
		}
	}

	t.destinationMu.Lock()
	t.mu.Lock()
	item, ok = t.entries[pid]
	if !ok {
		t.mu.Unlock()
		t.destinationMu.Unlock()
		return fmt.Errorf("transfer %q not found", pid)
	}
	if item.runner == nil {
		t.mu.Unlock()
		t.destinationMu.Unlock()
		return fmt.Errorf("transfer %q has no registered runner", pid)
	}
	if !isOpen(item.record.Status) || item.record.QueueGroupID == nil {
		hasOpen := false
		for otherPID, other := range t.entries {
			if otherPID != pid && isOpen(other.record.Status) {
				hasOpen = true
				break
			}
		}
		if !hasOpen || t.queueGroupSequence == 0 {
			t.queueGroupSequence++
		}
		id := t.queueGroupSequence
		item.record.QueueGroupID = &id
	}
	if item.record.Status != StatusPreparing {
		item.record.Status = StatusPending
	}
	for _, other := range t.entries {
		if other != item {
			other.queueOrder++
		}
	}
	item.queueOrder = 0
	item.record.FailedFiles = 0
	item.record.Error = ""
	item.record.ErrorCode = ""
	shouldEmit := t.scheduleEmitLocked(true, t.now())
	t.mu.Unlock()
	t.destinationMu.Unlock()
	if shouldEmit {
		t.emit()
	}
	t.signalQueue()
	return nil
}

//wails:ignore
func (t *Transfer) ResetStartTime(pid string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	item, ok := t.entries[pid]
	if !ok {
		return fmt.Errorf("transfer %q not found", pid)
	}
	item.record.StartTime = t.now().UnixMilli()
	item.samples = nil
	return nil
}

//wails:ignore
func (t *Transfer) ResetTransfer(pid string) error {
	t.mu.Lock()
	item, ok := t.entries[pid]
	if !ok {
		t.mu.Unlock()
		return fmt.Errorf("transfer %q not found", pid)
	}
	item.record.TransferredSize = 0
	item.record.Progress = 0
	item.record.Speed = 0
	item.record.ETA = 0
	item.record.StartTime = t.now().UnixMilli()
	item.record.TransferredFiles = 0
	item.record.FailedFiles = 0
	item.record.Error = ""
	item.record.ErrorCode = ""
	item.samples = nil
	item.completedIDs = make(map[string]struct{})
	shouldEmit := t.scheduleEmitLocked(true, t.now())
	t.mu.Unlock()
	if shouldEmit {
		t.emit()
	}
	return nil
}

func (t *Transfer) signalQueue() {
	select {
	case t.wake <- struct{}{}:
	default:
	}
}

func (t *Transfer) emit() {
	t.emitMu.Lock()
	defer t.emitMu.Unlock()

	t.mu.RLock()
	if t.emitStopped {
		t.mu.RUnlock()
		return
	}
	app := t.app
	syncWindowProgress := t.syncWindowProgress
	items := t.snapshotEntriesLocked()
	t.mu.RUnlock()
	t.dispatchSnapshots(app, syncWindowProgress, items)
}

func (t *Transfer) dispatchSnapshots(
	app *application.App,
	syncWindowProgress func(*WindowProgress),
	items []orderedSnapshot,
) {
	snapshots := orderSnapshots(items)
	if syncWindowProgress != nil {
		syncWindowProgress(CalculateWindowProgress(snapshots))
	}
	if app != nil {
		app.Event.Emit(updateEventName, snapshots)
	}
}

// scheduleEmitLocked plans a renderer update while t.mu is held. Immediate
// state changes cancel a pending progress update, while ordinary progress uses
// a leading-and-trailing throttle so the latest snapshot is never stranded.
func (t *Transfer) scheduleEmitLocked(immediate bool, now time.Time) bool {
	if t.emitStopped {
		return false
	}
	if immediate {
		t.cancelProgressEmitLocked()
		return true
	}

	interval := t.emitEvery
	if interval <= 0 {
		interval = emitInterval
	}
	elapsed := now.Sub(t.lastProgressEmit)
	if t.lastProgressEmit.IsZero() || elapsed < 0 || elapsed >= interval {
		t.cancelProgressEmitLocked()
		t.lastProgressEmit = now
		return true
	}
	if t.progressEmitTimer != nil {
		return false
	}

	t.progressEmitGen++
	generation := t.progressEmitGen
	t.progressEmitTimer = time.AfterFunc(interval-elapsed, func() {
		t.flushProgressEmit(generation)
	})
	return false
}

func (t *Transfer) cancelProgressEmitLocked() {
	t.progressEmitGen++
	if t.progressEmitTimer != nil {
		t.progressEmitTimer.Stop()
		t.progressEmitTimer = nil
	}
}

func (t *Transfer) flushProgressEmit(generation uint64) {
	t.emitMu.Lock()
	defer t.emitMu.Unlock()

	t.mu.Lock()
	if t.emitStopped || generation != t.progressEmitGen || t.progressEmitTimer == nil {
		t.mu.Unlock()
		return
	}
	t.progressEmitTimer = nil
	t.lastProgressEmit = t.now()
	app := t.app
	syncWindowProgress := t.syncWindowProgress
	items := t.snapshotEntriesLocked()
	t.mu.Unlock()
	t.dispatchSnapshots(app, syncWindowProgress, items)
}

func (t *Transfer) logError(err error, where string) {
	if err == nil || t.log == nil {
		return
	}
	t.log.Error(err.Error(), where)
}

func applyUpdates(record *Snapshot, updates Updates) {
	if updates.Status != nil {
		record.Status = *updates.Status
	}
	if updates.CurrentID != nil {
		record.CurrentID = *updates.CurrentID
	}
	if updates.TotalSize != nil {
		record.TotalSize = max(0, *updates.TotalSize)
		if record.TotalSize > 0 {
			record.TransferredSize = min(record.TransferredSize, record.TotalSize)
		}
	}
	if updates.TotalFiles != nil {
		record.TotalFiles = max(0, *updates.TotalFiles)
	}
	if updates.TransferredSize != nil {
		record.TransferredSize = max(0, *updates.TransferredSize)
		if record.TotalSize > 0 {
			record.TransferredSize = min(record.TransferredSize, record.TotalSize)
		}
	}
	if updates.Progress != nil {
		record.Progress = clamp(*updates.Progress, 0, 100)
	}
	if updates.TransferredFiles != nil {
		record.TransferredFiles = max(0, *updates.TransferredFiles)
	}
	if updates.FailedFiles != nil {
		record.FailedFiles = max(0, *updates.FailedFiles)
	}
	if updates.Path != nil {
		record.Path = *updates.Path
	}
	if updates.DestinationPaths != nil {
		record.DestinationPaths = slices.Clone(updates.DestinationPaths)
		record.DestinationTargets = nil
	}
	if updates.DestinationTargets != nil {
		record.DestinationTargets = slices.Clone(updates.DestinationTargets)
		record.DestinationPaths = destinationTargetPaths(updates.DestinationTargets)
	}
	if updates.Error != nil {
		record.Error = *updates.Error
	}
	if updates.ErrorCode != nil {
		record.ErrorCode = *updates.ErrorCode
	}
	if updates.PlanPhase != nil {
		phase := *updates.PlanPhase
		record.PlanPhase = &phase
	}
	if updates.PlanProgress != nil {
		progress := clamp(*updates.PlanProgress, 0, 100)
		record.PlanProgress = &progress
	}
	if updates.ClearCurrentID {
		record.CurrentID = ""
	}
	if updates.ClearError {
		record.Error = ""
	}
	if updates.ClearErrorCode {
		record.ErrorCode = ""
	}
	if updates.ClearPlanPhase {
		record.PlanPhase = nil
	}
	if updates.ClearPlanProgress {
		record.PlanProgress = nil
	}
}

func AggregateProgress(transfers []Snapshot) *float64 {
	openGroups := make(map[uint64]struct{})
	for _, item := range transfers {
		if isOpen(item.Status) && item.QueueGroupID != nil {
			openGroups[*item.QueueGroupID] = struct{}{}
		}
	}
	scoped := make([]Snapshot, 0, len(transfers))
	for _, item := range transfers {
		if !isAggregate(item.Status) {
			continue
		}
		if len(openGroups) == 0 {
			if isOpen(item.Status) {
				scoped = append(scoped, item)
			}
			continue
		}
		if item.QueueGroupID != nil {
			if _, ok := openGroups[*item.QueueGroupID]; ok {
				scoped = append(scoped, item)
			}
		}
	}
	if len(scoped) == 0 {
		return nil
	}
	totalSize := int64(0)
	for _, item := range scoped {
		totalSize += max(0, item.TotalSize)
	}
	var value float64
	if totalSize > 0 {
		transferred := int64(0)
		for _, item := range scoped {
			if item.Status == StatusCompleted {
				transferred += max(0, item.TotalSize)
			} else {
				transferred += max(0, min(item.TransferredSize, item.TotalSize))
			}
		}
		value = float64(transferred) / float64(totalSize) * 100
	} else {
		for _, item := range scoped {
			if item.Status == StatusCompleted {
				value += 100
			} else {
				value += clamp(item.Progress, 0, 100)
			}
		}
		value /= float64(len(scoped))
	}
	value = clamp(value, 0, 100)
	return &value
}

func CalculateWindowProgress(transfers []Snapshot) *WindowProgress {
	remaining := make([]Snapshot, 0, len(transfers))
	for _, item := range transfers {
		if item.Status != StatusCompleted && item.Status != StatusCanceled {
			remaining = append(remaining, item)
		}
	}
	if len(remaining) == 0 {
		return nil
	}

	mode := WindowProgressNormal
	switch {
	case slices.ContainsFunc(remaining, func(item Snapshot) bool {
		return item.Status == StatusProgress
	}):
		mode = WindowProgressNormal
	case slices.ContainsFunc(remaining, func(item Snapshot) bool {
		return item.Status == StatusPreparing || item.Status == StatusPending
	}):
		mode = WindowProgressIndeterminate
	case slices.ContainsFunc(remaining, func(item Snapshot) bool {
		return item.Status == StatusPaused
	}):
		mode = WindowProgressPaused
	case slices.ContainsFunc(remaining, func(item Snapshot) bool {
		return item.Status == StatusError
	}):
		mode = WindowProgressError
	}

	progress := AggregateProgress(transfers)
	if progress == nil {
		progress = fallbackWindowProgress(remaining)
	}
	if progress == nil {
		return nil
	}
	return &WindowProgress{Value: *progress, Mode: mode}
}

func fallbackWindowProgress(transfers []Snapshot) *float64 {
	if len(transfers) == 0 {
		return nil
	}
	totalSize := int64(0)
	for _, item := range transfers {
		totalSize += max(0, item.TotalSize)
	}
	var value float64
	if totalSize > 0 {
		transferred := int64(0)
		for _, item := range transfers {
			transferred += max(0, min(item.TransferredSize, item.TotalSize))
		}
		value = float64(transferred) / float64(totalSize) * 100
	} else {
		for _, item := range transfers {
			value += clamp(item.Progress, 0, 100)
		}
		value /= float64(len(transfers))
	}
	value = clamp(value, 0, 100)
	return &value
}

func isOpen(status Status) bool {
	return status == StatusPreparing || status == StatusPending || status == StatusProgress
}

func isAggregate(status Status) bool {
	return isOpen(status) || status == StatusCompleted
}

func isTerminal(status Status) bool {
	return status == StatusCompleted || status == StatusCanceled || status == StatusError
}

func isValidStatus(status Status) bool {
	return isOpen(status) || status == StatusCompleted || status == StatusPaused || status == StatusCanceled || status == StatusError
}

func clamp(value, low, high float64) float64 {
	return math.Max(low, math.Min(value, high))
}

func cloneData(data Data) Data {
	data.Files = slices.Clone(data.Files)
	data.Dirs = slices.Clone(data.Dirs)
	if data.Root != nil {
		root := *data.Root
		data.Root = &root
	}
	return data
}

func cloneRecord(record Record) Record {
	record.Data = cloneData(record.Data)
	record.Snapshot = cloneSnapshot(record.Snapshot)
	return record
}

func cloneSnapshot(snapshot Snapshot) Snapshot {
	snapshot.DestinationPaths = slices.Clone(snapshot.DestinationPaths)
	snapshot.DestinationTargets = slices.Clone(snapshot.DestinationTargets)
	if snapshot.QueueGroupID != nil {
		id := *snapshot.QueueGroupID
		snapshot.QueueGroupID = &id
	}
	if snapshot.PlanPhase != nil {
		phase := *snapshot.PlanPhase
		snapshot.PlanPhase = &phase
	}
	if snapshot.PlanProgress != nil {
		progress := *snapshot.PlanProgress
		snapshot.PlanProgress = &progress
	}
	return snapshot
}

func destinationTargetPaths(targets []DestinationTarget) []string {
	paths := make([]string, len(targets))
	for index, target := range targets {
		paths[index] = target.Path
	}
	return paths
}

func transferPathsOverlap(first, second string) bool {
	first = cleanTransferPath(first)
	second = cleanTransferPath(second)
	return pathContains(first, second) || pathContains(second, first)
}

func cleanTransferPath(path string) string {
	if absolute, err := filepath.Abs(path); err == nil {
		path = absolute
	}
	return filepath.Clean(path)
}

func pathContains(parent, child string) bool {
	if strings.EqualFold(parent, child) {
		return true
	}
	if !strings.HasSuffix(parent, string(filepath.Separator)) {
		parent += string(filepath.Separator)
	}
	return strings.HasPrefix(strings.ToLower(child), strings.ToLower(parent))
}
