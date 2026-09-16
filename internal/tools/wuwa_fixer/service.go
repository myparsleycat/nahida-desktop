// Package wuwafixer installs, updates, and runs the Wuwa Mod Fixer binary against
// a mod folder, and keeps its backups rollbackable.
package wuwafixer

import (
	"context"
	"errors"
	"os"
	"sync"
	"time"

	"nahida.live/desktop/internal/appdata"
	"nahida.live/desktop/internal/db"
	"nahida.live/desktop/internal/infra"
	fixtool "nahida.live/desktop/internal/tools/fix_tool"
)

type Options struct {
	Log        *infra.Log
	EventEmit  func(string, ...any)
	Notify     func(title, body string) error
	Settings   any
	HTTP       *infra.Client
	GitHubRate *infra.GitHubRateCoordinator
	// Runner gates the fixer binary so it never runs next to another fix tool.
	Runner *fixtool.Service
}

type Service struct {
	log        *infra.Log
	emit       func(string, ...any)
	notify     func(title, body string) error
	settings   any
	http       *infra.Client
	githubRate *infra.GitHubRateCoordinator
	runner     *fixtool.Service
	appData    *appdata.Store
	client     *db.Client

	wuwaDiagnostic infra.DiagnosticThrottle
	wuwaMu         sync.Mutex
	wuwaInstallMu  sync.Mutex
	wuwaAutoCancel context.CancelFunc
	wuwaAutoDone   chan struct{}
}

func New() *Service { return NewWithOptions(Options{}) }

func NewWithOptions(opts Options) *Service {
	if opts.Runner == nil {
		opts.Runner = fixtool.New()
	}
	return &Service{
		log:        opts.Log,
		emit:       opts.EventEmit,
		notify:     opts.Notify,
		settings:   opts.Settings,
		http:       opts.HTTP,
		githubRate: opts.GitHubRate,
		runner:     opts.Runner,
	}
}

//wails:ignore
func (t *Service) UseClient(client *db.Client) {
	t.client = client
}

//wails:ignore
func (t *Service) UseAppData(data *appdata.Store) {
	t.appData = data
}

func (t *Service) Shutdown() error {
	if t == nil {
		return nil
	}
	return t.stopWuwaAutoUpdateCheck()
}

func (t *Service) requireClient() (*db.Client, error) {
	if t == nil || t.client == nil {
		return nil, errors.New("tools service is not bound to a database")
	}
	return t.client, nil
}

func (t *Service) appDataPath(relative string) (string, error) {
	if t == nil || t.appData == nil {
		return "", errors.New("tools service has no app data store")
	}
	return t.appData.Resolve(relative)
}

func (t *Service) getAppState(ctx context.Context, key string) (*string, error) {
	client, err := t.requireClient()
	if err != nil {
		return nil, err
	}
	return client.AppState.GetValue(ctx, key)
}

func (t *Service) setAppState(ctx context.Context, key, value string) error {
	client, err := t.requireClient()
	if err != nil {
		return err
	}
	return client.AppState.Upsert(ctx, key, value, time.Now().UTC().Format(time.RFC3339Nano))
}

func (t *Service) logError(err error, where string) {
	if err != nil && t != nil && t.log != nil {
		_ = infra.ReportError(t.log, err, "Tools", infra.Diagnostic{
			Severity: infra.DiagnosticError, Operation: where, Stage: "background",
		})
	}
}

func (t *Service) reportCleanup(err error, operation string) {
	if err == nil || errors.Is(err, os.ErrNotExist) {
		return
	}
	_ = infra.ReportError(t.log, err, "Tools", infra.Diagnostic{Operation: operation, Stage: "cleanup"})
}
