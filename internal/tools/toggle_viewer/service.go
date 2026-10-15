// Package toggleviewer generates toggle-viewer.ini/.txt overlays that list a mod's cycle keys
// in game, and keeps them in sync with the mods of every enabled XXMI importer.
package toggleviewer

import (
	"context"
	"errors"
	"sync"
	"time"

	"nahida.live/desktop/internal/db"
	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/xxmi"
)

const logsEvent = "tools:toggle-viewer-logs"

type Settings interface {
	GetToggleViewerAutoGenerate(context.Context) (bool, error)
	SetToggleViewerAutoGenerate(context.Context, bool) error
	GetToggleViewerHotkey(context.Context) (string, error)
}

type Importers interface {
	GetEnabledImporters(context.Context) ([]xxmi.EnabledImporter, error)
}

type Options struct {
	Log       *infra.Log
	EventEmit func(string, ...any)
	Settings  Settings
	Importers Importers
}

type State struct {
	IsRunning bool    `json:"isRunning"`
	Mode      *string `json:"mode"`
}

type task struct {
	mode   string
	cancel context.CancelFunc
}

type Service struct {
	log         *infra.Log
	emit        func(string, ...any)
	settings    Settings
	importers   Importers
	client      *db.Client
	diagnostics infra.DiagnosticThrottle

	// watchMu serializes watcher replacement and holds it closed during importer maintenance.
	watchMu sync.Mutex
	// writeMu serializes artifact writes of scans with hotkey application so the stored hotkey is
	// the last one written. Tasks take it without watchMu, so it is always acquired after watchMu.
	writeMu sync.Mutex

	retryDelay time.Duration

	mu           sync.Mutex
	task         *task
	logs         []string
	pending      bool
	watcher      *treeWatcher
	retry        *time.Timer
	hotkeyRetry  *time.Timer
	hotkeyCancel context.CancelFunc
	closing      bool
	suspended    bool
	tasks        sync.WaitGroup
	// hotkeyRuns is separate from tasks because a running hotkey retry waits for watchMu, which
	// importer maintenance holds while it waits for tasks. Only Shutdown waits for both.
	hotkeyRuns sync.WaitGroup
}

func New() *Service { return NewWithOptions(Options{}) }

func NewWithOptions(opts Options) *Service {
	return &Service{
		log:        opts.Log,
		emit:       opts.EventEmit,
		settings:   opts.Settings,
		importers:  opts.Importers,
		retryDelay: defaultRetryDelay,
	}
}

func (s *Service) UseClient(client *db.Client) {
	s.client = client
}

func (s *Service) requireClient() (*db.Client, error) {
	if s == nil || s.client == nil {
		return nil, errors.New("toggle viewer is not bound to a database")
	}
	return s.client, nil
}

// SuspendImporterWatcher closes the importer tree watcher and drains the running task so importer
// folders can be renamed; the returned resume function restarts watching from the stored setting.
func (s *Service) SuspendImporterWatcher(ctx context.Context) (func() error, error) {
	s.watchMu.Lock()
	if err := ctx.Err(); err != nil {
		s.watchMu.Unlock()
		return nil, err
	}

	s.mu.Lock()
	s.suspended = true
	if s.task != nil {
		s.task.cancel()
	}
	s.mu.Unlock()
	_, stopErr := s.stopWatcherLocked()
	s.tasks.Wait()

	return func() error {
		s.mu.Lock()
		s.suspended = false
		s.mu.Unlock()
		defer s.watchMu.Unlock()
		return errors.Join(stopErr, s.startWatcherLocked(context.WithoutCancel(ctx), true))
	}, nil
}

func (s *Service) Shutdown() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	s.closing = true
	s.stopRetryLocked()
	if s.hotkeyRetry != nil {
		s.hotkeyRetry.Stop()
		s.hotkeyRetry = nil
	}
	if s.hotkeyCancel != nil {
		s.hotkeyCancel()
	}
	if s.task != nil {
		s.task.cancel()
	}
	watcher := s.watcher
	s.watcher = nil
	s.mu.Unlock()

	var err error
	if watcher != nil {
		err = watcher.watcher.Close()
	}

	done := make(chan struct{})
	go func() {
		s.tasks.Wait()
		s.hotkeyRuns.Wait()
		close(done)
	}()
	select {
	case <-done:
		return err
	case <-time.After(5 * time.Second):
		return errors.Join(err, errors.New("timed out waiting for toggle viewer task to stop"))
	}
}
