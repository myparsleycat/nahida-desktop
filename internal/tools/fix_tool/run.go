// Package fixtool stores the user's fix scripts and presets and runs them, one
// at a time, streaming their output to the fix tool log.
package fixtool

import (
	"context"
	"errors"
	"sync"
	"time"

	"nahida.live/desktop/internal/db"
	"nahida.live/desktop/internal/infra"
)

type Options struct {
	Log       *infra.Log
	EventEmit func(string, ...any)
}

type Service struct {
	log    *infra.Log
	emit   func(string, ...any)
	client *db.Client

	runMu sync.Mutex
	run   *Run
}

// Run is an exclusive fix tool run. Only one run exists at a time: its context is
// cancelled when the user cancels the run or the service shuts down.
type Run struct {
	ctx      context.Context
	cancel   context.CancelFunc
	executor *scriptExecutor
	done     chan struct{}
}

func New() *Service { return NewWithOptions(Options{}) }

func NewWithOptions(opts Options) *Service {
	return &Service{log: opts.Log, emit: opts.EventEmit}
}

//wails:ignore
func (t *Service) UseClient(client *db.Client) {
	t.client = client
}

func (t *Service) Shutdown() error {
	if t == nil {
		return nil
	}
	t.runMu.Lock()
	run := t.run
	if run != nil {
		run.cancel()
	}
	t.runMu.Unlock()
	if run == nil {
		return nil
	}
	select {
	case <-run.done:
		return nil
	case <-time.After(5 * time.Second):
		return errors.New("timed out waiting for tools process to stop")
	}
}

// Begin starts an exclusive run and fails while another one is active.
func (t *Service) Begin(parent context.Context) (*Run, error) {
	t.runMu.Lock()
	defer t.runMu.Unlock()
	if t.run != nil {
		return nil, infra.ContractError("Another process is running.")
	}
	ctx, cancel := context.WithCancel(parent)
	executor := newScriptExecutor(t.Log)
	executor.onError = func(err error) { t.logError(err, "script-output-read") }
	run := &Run{ctx: ctx, cancel: cancel, executor: executor, done: make(chan struct{})}
	t.run = run
	return run, nil
}

// Finish releases the run gate and cancels the run context.
func (t *Service) Finish(run *Run) {
	if run == nil {
		return
	}
	t.runMu.Lock()
	if t.run == run {
		t.run = nil
		run.cancel()
		close(run.done)
	}
	t.runMu.Unlock()
}

// Context is the run context, cancelled when the run is cancelled or shut down.
func (r *Run) Context() context.Context { return r.ctx }

// Execute runs a program inside the active run and streams its output to the log.
func (r *Run) Execute(path string, kind db.ScriptType, dir string, args []string) error {
	return r.executor.execute(r.ctx, path, kind, dir, args)
}

func (t *Service) requireClient() (*db.Client, error) {
	if t == nil || t.client == nil {
		return nil, errors.New("tools service is not bound to a database")
	}
	return t.client, nil
}

func (t *Service) emitEvent(name string, data any) {
	if t != nil && t.emit != nil {
		t.emit(name, data)
	}
}

func (t *Service) logError(err error, where string) {
	if err != nil && t != nil && t.log != nil {
		_ = infra.ReportError(t.log, err, "Tools", infra.Diagnostic{
			Severity: infra.DiagnosticError, Operation: where, Stage: "background",
		})
	}
}
