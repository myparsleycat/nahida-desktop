package app

import (
	"context"
	"sync"

	"nahida.live/desktop/internal/elevated"
	"nahida.live/desktop/internal/infra"
)

// elevatedLifecycle serializes elevated-helper start and stop requests on one
// worker, so a setting change never blocks on a UAC prompt, and gates the
// startup read against disable and shutdown.
type elevatedLifecycle struct {
	client *elevated.Client
	report func(err error, stage string)

	gate     sync.Mutex
	stopping bool
	startup  *elevatedStartup

	mu         sync.Mutex
	cond       *sync.Cond
	done       chan struct{}
	started    bool
	workerStop bool
	version    uint64
	applied    uint64
	desired    bool
}

// elevatedStartup tracks the single startup goroutine that reads the setting.
type elevatedStartup struct {
	cancel context.CancelFunc
	done   chan struct{}
}

func newElevatedLifecycle(client *elevated.Client, report func(err error, stage string)) *elevatedLifecycle {
	lifecycle := &elevatedLifecycle{client: client, report: report, done: make(chan struct{})}
	lifecycle.cond = sync.NewCond(&lifecycle.mu)
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

// request rechecks the desired state under the gate and enqueues it. Holding
// the gate excludes a concurrent disable or shutdown, so a startup read that
// lost the race applies nothing.
func (l *elevatedLifecycle) request(resolve func() (enabled bool, ok bool)) {
	if l == nil {
		return
	}
	l.gate.Lock()
	defer l.gate.Unlock()
	if l.stopping {
		return
	}
	enabled, ok := resolve()
	if !ok {
		return
	}
	l.enqueue(enabled)
}

// setEnabled cancels and waits for the tracked startup goroutine, then enqueues
// the latest state. Waiting keeps disable and re-enable ordered against the
// startup read before the worker starts or closes the client.
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
	l.gate.Lock()
	l.stopping = true
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
	l.desired = enabled
	l.version++
	l.cond.Signal()
}

func (l *elevatedLifecycle) run() {
	defer close(l.done)
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
		l.mu.Unlock()

		stage := "stop"
		var err error
		if enabled {
			stage = "start"
			err = l.client.Start(context.Background())
		} else {
			err = l.client.Close()
		}
		l.report(err, stage)
	}
}

func (rt *runtime) startElevatedHelperIfEnabled() {
	if rt == nil || rt.setting == nil || rt.elevatedLifecycle == nil {
		return
	}
	lifecycle := rt.elevatedLifecycle
	lifecycle.startAsync(func(ctx context.Context) {
		// Recheck under the gate immediately before enqueueing, so a disable or
		// shutdown that started first cannot be overtaken by this startup.
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
