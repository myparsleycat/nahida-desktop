package transfer

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
)

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
	reportFailure      func(error, map[string]any) error
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
		reportFailure:      opts.ReportFailure,
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
func (t *Transfer) RefreshPowerSaveBlock(ctx context.Context) (returnErr error) {
	defer func() {
		if returnErr != nil && t.reportFailure != nil {
			returnErr = t.reportFailure(returnErr, map[string]any{"operation": "refresh-power-save", "stage": "power-state"})
		}
	}()
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

func (t *Transfer) logError(err error, where string) {
	if err == nil || t.log == nil {
		return
	}
	if t.reportFailure != nil {
		_ = t.reportFailure(err, map[string]any{"operation": where, "stage": "background"})
		return
	}
	t.log.Error(err.Error(), where)
}

func (t *Transfer) logRecord(record map[string]any, where string) {
	if t.log == nil {
		return
	}
	t.log.Error(record, where)
}
