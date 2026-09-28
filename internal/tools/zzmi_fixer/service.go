// Package zzmifixer applies the ZZMI rule pack to a mod: it updates texture
// hashes, remaps blend buffers, and keeps every change rollbackable.
package zzmifixer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"time"

	"nahida.live/desktop/internal/appdata"
	"nahida.live/desktop/internal/db"
	"nahida.live/desktop/internal/github"
	"nahida.live/desktop/internal/infra"
	fixtool "nahida.live/desktop/internal/tools/fix_tool"
)

type Options struct {
	Log       *infra.Log
	EventEmit func(string, ...any)
	HTTP      *infra.Client
	// Runner gates the fixer so it never runs next to another fix tool.
	Runner *fixtool.Service
	// GitHub serves the rule releases; nil builds one from HTTP without rate gating.
	GitHub *github.Client
}

type Service struct {
	log     *infra.Log
	emit    func(string, ...any)
	github  *github.Client
	runner  *fixtool.Service
	appData *appdata.Store
	client  *db.Client
}

func New() *Service { return NewWithOptions(Options{}) }

func NewWithOptions(opts Options) *Service {
	if opts.Runner == nil {
		opts.Runner = fixtool.New()
	}
	if opts.GitHub == nil {
		opts.GitHub = github.New(github.Options{HTTP: opts.HTTP, Log: opts.Log})
	}
	return &Service{
		log:    opts.Log,
		emit:   opts.EventEmit,
		github: opts.GitHub,
		runner: opts.Runner,
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

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
