package app

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"nahida.live/desktop/internal/elevated"
	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/platform"
)

const (
	elevatedStatusEvent    = "elevated:status"
	elevatedHealthInterval = 10 * time.Second
)

// elevatedHelperClient is the lifecycle's view of the helper process.
type elevatedHelperClient interface {
	Start(context.Context) error
	Close() error
	Connected() bool
}

// elevatedLifecycle serializes elevated-helper start and stop requests on one
// worker, so a setting change never blocks on a UAC prompt, and gates the
// startup read against disable and shutdown.
type elevatedLifecycle struct {
	client elevatedHelperClient
	report func(err error, stage string)
	emit   func(string, ...any)

	// ctx is cancelled by shutdown so in-flight work, including health watches
	// and client starts, releases instead of blocking process exit.
	ctx    context.Context
	cancel context.CancelFunc

	gate sync.Mutex
	// generation advances whenever disable or shutdown invalidates a startup
	// read, so a resolve that finished after its gate section can be rejected.
	generation uint64
	stopping   bool
	startup    *elevatedStartup
	starting   atomic.Bool

	mu         sync.Mutex
	cond       *sync.Cond
	done       chan struct{}
	started    bool
	workerStop bool
	version    uint64
	applied    uint64
	desired    bool
	// startCancel cancels the in-flight client.Start so disable can unblock
	// the worker without cancelling l.ctx. Guarded by mu.
	startCancel context.CancelFunc
}

// elevatedStartup tracks the single startup goroutine that reads the setting.
type elevatedStartup struct {
	cancel context.CancelFunc
	done   chan struct{}
}

func newElevatedLifecycle(
	client elevatedHelperClient,
	report func(err error, stage string),
	emit func(string, ...any),
) *elevatedLifecycle {
	lifecycle := &elevatedLifecycle{client: client, report: report, emit: emit, done: make(chan struct{})}
	lifecycle.cond = sync.NewCond(&lifecycle.mu)
	lifecycle.ctx, lifecycle.cancel = context.WithCancel(context.Background())
	return lifecycle
}

// startAsync runs body as the tracked startup goroutine. It refuses to start
// once disable or shutdown has begun so a late startup can never revive the
// helper.
func (l *elevatedLifecycle) startAsync(body func(ctx context.Context)) {
	if l == nil {
		return
	}
	l.gate.Lock()
	if l.stopping || l.startup != nil {
		l.gate.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	startup := &elevatedStartup{cancel: cancel, done: make(chan struct{})}
	l.startup = startup
	l.gate.Unlock()

	go func() {
		defer close(startup.done)
		defer func() {
			l.gate.Lock()
			if l.startup == startup {
				l.startup = nil
			}
			l.gate.Unlock()
		}()
		defer cancel()
		body(ctx)
	}()
}

// request resolves the desired state outside the gate, then enqueues it only
// when no disable or shutdown invalidated the read while it ran.
func (l *elevatedLifecycle) request(resolve func() (enabled bool, ok bool)) {
	if l == nil {
		return
	}
	l.gate.Lock()
	if l.stopping {
		l.gate.Unlock()
		return
	}
	generation := l.generation
	l.gate.Unlock()

	enabled, ok := resolve()

	l.gate.Lock()
	defer l.gate.Unlock()
	if !ok || l.stopping || l.generation != generation {
		return
	}
	l.enqueue(enabled)
}

// setEnabled cancels and waits for the tracked startup goroutine, then enqueues
// the latest state. Waiting keeps disable and re-enable ordered against the
// startup read before the worker starts or closes the client. Disable also
// cancels an in-flight client.Start so the worker can return and process stop.
func (l *elevatedLifecycle) setEnabled(enabled bool) {
	if l == nil {
		return
	}
	l.cancelStartup()
	l.gate.Lock()
	defer l.gate.Unlock()
	if l.stopping {
		return
	}
	l.enqueue(enabled)
}

// shutdown permanently stops the startup and worker goroutines. The caller
// closes the client afterwards, so the close never races a start.
func (l *elevatedLifecycle) shutdown() {
	if l == nil {
		return
	}
	l.cancel()
	l.gate.Lock()
	l.stopping = true
	l.generation++
	startup := l.startup
	if startup != nil {
		startup.cancel()
	}
	l.gate.Unlock()
	if startup != nil {
		<-startup.done
	}

	l.mu.Lock()
	l.workerStop = true
	started := l.started
	l.cond.Broadcast()
	l.mu.Unlock()
	if started {
		<-l.done
	}
}

func (l *elevatedLifecycle) cancelStartup() {
	l.gate.Lock()
	l.generation++
	startup := l.startup
	if startup != nil {
		startup.cancel()
	}
	l.gate.Unlock()
	if startup != nil {
		<-startup.done
	}
}

// enqueue records the latest requested state on the worker, starting it on
// first use. The worker coalesces bursts, so only the newest state is applied.
// A duplicate start while one is pending or in flight is dropped so a second
// click cannot queue another UAC prompt behind a declined first attempt.
func (l *elevatedLifecycle) enqueue(enabled bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.workerStop {
		return
	}
	if !l.started {
		l.started = true
		go l.run()
	}
	if l.desired == enabled && (l.version != l.applied || l.starting.Load()) {
		return
	}
	l.desired = enabled
	l.version++
	if !enabled && l.startCancel != nil {
		l.startCancel()
	}
	l.cond.Signal()
}

func (l *elevatedLifecycle) run() {
	defer close(l.done)

	healthDone := make(chan struct{})
	go func() {
		defer close(healthDone)
		l.watchHealth()
	}()
	defer func() { <-healthDone }()

	for {
		l.mu.Lock()
		for !l.workerStop && l.applied == l.version {
			l.cond.Wait()
		}
		if l.workerStop {
			l.mu.Unlock()
			return
		}
		version, enabled := l.version, l.desired
		l.applied = version
		if enabled {
			l.starting.Store(true)
		}
		l.mu.Unlock()

		stage := "stop"
		var err error
		if enabled {
			stage = "start"
			err = l.startClient()
			l.starting.Store(false)
		} else {
			err = l.stopClient()
		}
		// A cancellation means shutdown or a racing disable superseded this
		// start; it is an expected stop, not a failure worth reporting.
		if err != nil && !errors.Is(err, context.Canceled) && l.report != nil {
			l.report(err, stage)
		}
		if errors.Is(err, context.Canceled) {
			continue
		}
		l.publishStatus()
	}
}

func (l *elevatedLifecycle) startClient() error {
	if l.client == nil {
		return nil
	}

	l.mu.Lock()
	if !l.desired || l.workerStop {
		l.mu.Unlock()
		return context.Canceled
	}
	ctx, cancel := context.WithCancel(l.ctx)
	l.startCancel = cancel
	l.mu.Unlock()
	defer func() {
		cancel()
		l.mu.Lock()
		l.startCancel = nil
		l.mu.Unlock()
	}()

	return l.client.Start(ctx)
}

func (l *elevatedLifecycle) stopClient() error {
	if l.client == nil {
		return nil
	}
	return l.client.Close()
}

func (l *elevatedLifecycle) watchHealth() {
	ticker := time.NewTicker(elevatedHealthInterval)
	defer ticker.Stop()
	for {
		select {
		case <-l.ctx.Done():
			return
		case <-ticker.C:
			l.checkHealth()
		}
	}
}

func (l *elevatedLifecycle) checkHealth() {
	if l.starting.Load() {
		return
	}
	l.mu.Lock()
	desired := l.desired && !l.workerStop
	l.mu.Unlock()
	if !desired {
		return
	}

	// A timed-out session.ping would invalidate the half-duplex pipe, so health
	// only republishes when the connection is already gone. Process death is
	// observed by the watcher; a hung pipe surfaces on the next SendKeys.
	if l.client != nil && l.client.Connected() {
		return
	}
	l.publishStatus()
}

func (l *elevatedLifecycle) status() platform.ElevatedHelperStatus {
	if l == nil {
		return platform.ElevatedHelperStatus{}
	}
	l.mu.Lock()
	enabled := l.desired && !l.workerStop
	l.mu.Unlock()
	if !enabled {
		return platform.ElevatedHelperStatus{}
	}
	running := l.client != nil && l.client.Connected()
	return platform.ElevatedHelperStatus{Enabled: true, Running: running}
}

func (l *elevatedLifecycle) publishStatus() {
	if l == nil || l.emit == nil {
		return
	}
	l.emit(elevatedStatusEvent, l.status())
}

func (rt *runtime) startElevatedHelperIfEnabled() {
	if rt == nil || rt.setting == nil || rt.elevatedLifecycle == nil {
		return
	}
	lifecycle := rt.elevatedLifecycle
	lifecycle.startAsync(func(ctx context.Context) {
		// Resolve the setting off the gate, then let request reject it if a
		// disable or shutdown invalidated the read while it was in flight.
		lifecycle.request(func() (bool, bool) {
			if ctx.Err() != nil {
				return false, false
			}
			enabled, err := rt.setting.GetElevatedHelperEnabled(ctx)
			if err != nil {
				rt.reportElevatedHelperError(err, "read-setting")
				return false, false
			}
			return enabled, true
		})
	})
}

func (rt *runtime) configureElevatedHelper(enabled bool) {
	if rt == nil || rt.elevatedLifecycle == nil {
		return
	}
	rt.elevatedLifecycle.setEnabled(enabled)
}

func (rt *runtime) reportElevatedHelperError(err error, stage string) {
	if rt == nil {
		return
	}
	reportElevatedHelperError(rt.log, err, stage)
}

func reportElevatedHelperError(log *infra.Log, err error, stage string) {
	if err == nil || log == nil {
		return
	}
	_ = infra.ReportError(log, err, "ElevatedHelper", infra.Diagnostic{
		Severity: infra.DiagnosticWarn, Operation: "elevated-helper", Stage: stage,
	})
}

var _ elevatedHelperClient = (*elevated.Client)(nil)
