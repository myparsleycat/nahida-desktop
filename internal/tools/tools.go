package tools

import (
	"context"
	"errors"
	"sync"
	"time"

	"nahida.live/desktop/internal/appdata"
	"nahida.live/desktop/internal/db"
	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/platform"
	"nahida.live/desktop/internal/xxmi"
)

type BisectSettings interface {
	GetBisectPreserveD3dx(context.Context) (bool, error)
	GetDisabledPrefixStyle(context.Context) (string, error)
}

type FixInspectionSettings interface {
	GetAutoInspectFix(context.Context) (bool, error)
}

type ModDisabler interface {
	Disable(context.Context, string) (string, error)
	Enable(context.Context, string) (string, error)
}

type PEDiversifier interface {
	Diversify(context.Context, string, string) (PEDiversificationReport, error)
}

// contractError preserves user-facing Electron error text, including its
// original capitalisation and punctuation.
type contractError string

func (e contractError) Error() string { return string(e) }

type Options struct {
	Log        *infra.Log
	EventEmit  func(string, ...any)
	Notify     func(title, body string) error
	Settings   BisectSettings
	XXMI       *xxmi.XXMI
	FS         *platform.FS
	HTTP       *infra.Client
	Download   *infra.Download
	Archive    *infra.Archive
	Protocol   *infra.Protocol
	GitHubRate *infra.GitHubRateCoordinator
	Mod        ModDisabler
	// PEDiversifier diversifies packed executable content.
	PEDiversifier PEDiversifier
}

type Tools struct {
	appData    *appdata.Store
	client     *db.Client
	log        *infra.Log
	emit       func(string, ...any)
	notify     func(title, body string) error
	settings   BisectSettings
	xxmi       *xxmi.XXMI
	fs         *platform.FS
	http       *infra.Client
	download   *infra.Download
	archive    *infra.Archive
	protocol   *infra.Protocol
	githubRate *infra.GitHubRateCoordinator
	mod        ModDisabler

	runMu sync.Mutex
	run   *toolRun

	bisectMu sync.Mutex
	bisect   *bisectSession
	d3dx     *d3dxGuard

	fixerMu       sync.Mutex
	fixerTask     *string
	fixerProgress string
	fixerError    string
	releaseCache  map[string]releaseCacheEntry
	releaseCalls  map[string]*releaseFetchCall

	persistMu sync.Mutex

	wuwaMu         sync.Mutex
	wuwaInstallMu  sync.Mutex
	wuwaAutoCancel context.CancelFunc
	wuwaAutoDone   chan struct{}

	textureRuntimeMu sync.Mutex
	textureEventMu   sync.Mutex
	textureMu        sync.Mutex
	textureNextJob   uint64
	textureState     TextureResizeProgressEvent
	textureJobs      map[uint64]TextureResizeProgressEvent

	peDiversifier PEDiversifier

	touchMu       sync.Mutex
	touchSessions map[string]*touchSession

	bodyShapeMu       sync.Mutex
	bodyShapeSessions map[string]*bodyShapeSession

	modelViewerMu       sync.Mutex
	modelViewerSessions map[string]*modelViewerSession
	persist             *persistEngine

	fixInspectors         *FixInspectorRegistry
	fixInspectionRunMu    sync.Mutex
	fixInspectionMu       sync.Mutex
	fixInspections        map[string]*trackedFixInspection
	fixInspectionRevision uint64
	fixInspectionClosed   bool
	fixInspectionCtx      context.Context
	fixInspectionCancel   context.CancelFunc
	fixInspectionWG       sync.WaitGroup
}

type toolRun struct {
	cancel   context.CancelFunc
	executor *scriptExecutor
	done     chan struct{}
}

func New() *Tools { return NewWithOptions(Options{}) }

func NewWithOptions(opts Options) *Tools {
	if opts.FS == nil {
		opts.FS = platform.NewFS()
	}
	if opts.Protocol == nil {
		opts.Protocol = infra.NewProtocol()
	}
	fixInspectionCtx, fixInspectionCancel := context.WithCancel(context.Background())
	t := &Tools{
		log: opts.Log, emit: opts.EventEmit, notify: opts.Notify, settings: opts.Settings, xxmi: opts.XXMI,
		fs: opts.FS, http: opts.HTTP, download: opts.Download, archive: opts.Archive, protocol: opts.Protocol, githubRate: opts.GitHubRate, mod: opts.Mod,
		peDiversifier: opts.PEDiversifier,
		textureState:  TextureResizeProgressEvent{Status: "idle"},
		textureJobs:   make(map[uint64]TextureResizeProgressEvent), releaseCache: make(map[string]releaseCacheEntry), releaseCalls: make(map[string]*releaseFetchCall),
		touchSessions:       make(map[string]*touchSession),
		bodyShapeSessions:   make(map[string]*bodyShapeSession),
		modelViewerSessions: make(map[string]*modelViewerSession),
		persist:             newPersistEngine(),
		fixInspectors:       NewFixInspectorRegistry(),
		fixInspections:      make(map[string]*trackedFixInspection),
		fixInspectionCtx:    fixInspectionCtx,
		fixInspectionCancel: fixInspectionCancel,
	}
	t.fixInspectors.Register(NewZZMIFixInspector(t))
	t.persist.emit = func(logs []string) { t.emitEvent("setting:xxmi:persistLogs", logs) }
	t.persist.infoFn = func(message string) {
		if t.log != nil {
			t.log.Info(message, "TogglePersist")
		}
	}
	t.persist.errorFn = func(message string) {
		if t.log != nil {
			t.log.Error(message, "TogglePersist")
		}
	}
	return t
}

//wails:ignore
func (t *Tools) UseClient(client *db.Client) {
	t.client = client
	if t.githubRate != nil && client != nil {
		t.githubRate.UseAppState(client.AppState)
	}
}

//wails:ignore
func (t *Tools) UseAppData(data *appdata.Store) {
	t.appData = data
	if err := t.zzmiCleanupAbandonedStaging(); err != nil {
		t.logError(err, "ZZMIFixerCleanup")
	}
}

func (t *Tools) appDataPath(relative string) (string, error) {
	if t == nil || t.appData == nil {
		return "", errors.New("tools service has no app data store")
	}
	return t.appData.Resolve(relative)
}

func (t *Tools) requireClient() (*db.Client, error) {
	if t == nil || t.client == nil {
		return nil, errors.New("tools service is not bound to a database")
	}
	return t.client, nil
}

func (t *Tools) getAppState(ctx context.Context, key string) (*string, error) {
	client, err := t.requireClient()
	if err != nil {
		return nil, err
	}
	return client.AppState.GetValue(ctx, key)
}

func (t *Tools) setAppState(ctx context.Context, key, value string) error {
	client, err := t.requireClient()
	if err != nil {
		return err
	}
	return client.AppState.Upsert(ctx, key, value, time.Now().UTC().Format(time.RFC3339Nano))
}

func (t *Tools) deleteAppState(ctx context.Context, key string) error {
	client, err := t.requireClient()
	if err != nil {
		return err
	}
	return client.AppState.Delete(ctx, key)
}

func (t *Tools) emitEvent(name string, data any) {
	if t != nil && t.emit != nil {
		t.emit(name, data)
	}
}

func (t *Tools) logError(err error, where string) {
	if err != nil && t != nil && t.log != nil {
		_ = infra.ReportError(t.log, err, "Tools", infra.Diagnostic{
			Severity: infra.DiagnosticError, Operation: where, Stage: "background",
		})
	}
}

func (t *Tools) beginToolRun(parent context.Context, executor *scriptExecutor) (*toolRun, context.Context, error) {
	t.runMu.Lock()
	defer t.runMu.Unlock()
	if t.run != nil {
		return nil, nil, contractError("Another process is running.")
	}
	ctx, cancel := context.WithCancel(parent)
	run := &toolRun{cancel: cancel, executor: executor, done: make(chan struct{})}
	t.run = run
	return run, ctx, nil
}

func (t *Tools) beginScriptRun(parent context.Context) (*toolRun, context.Context, error) {
	return t.beginToolRun(parent, newScriptExecutor(t.emitFixToolLog))
}

func (t *Tools) finishScriptRun(run *toolRun) {
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

//wails:ignore
func (t *Tools) ServiceShutdown() error {
	if t == nil {
		return nil
	}
	t.runMu.Lock()
	run := t.run
	if run != nil {
		run.cancel()
	}
	t.runMu.Unlock()
	var err error
	if run != nil {
		select {
		case <-run.done:
		case <-time.After(5 * time.Second):
			err = errors.New("timed out waiting for tools process to stop")
		}
	}
	return errors.Join(err, t.shutdownFixInspections(), t.shutdownBisect(), t.stopWuwaAutoUpdateCheck(), t.shutdownTouchProfiles(), t.shutdownBodyShape(), t.shutdownModelViewer(), t.shutdownPersistWatcher())
}
