// Package zzmifixer applies the ZZMI rule pack to a mod: it updates texture
// hashes, remaps blend buffers, and keeps every change rollbackable.
package zzmifixer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"nahida.live/desktop/internal/appdata"
	"nahida.live/desktop/internal/db"
	"nahida.live/desktop/internal/infra"
	fixtool "nahida.live/desktop/internal/tools/fix_tool"
)

type Options struct {
	Log        *infra.Log
	EventEmit  func(string, ...any)
	HTTP       *infra.Client
	GitHubRate *infra.GitHubRateCoordinator
	// Runner gates the fixer so it never runs next to another fix tool.
	Runner *fixtool.Service
}

type Service struct {
	log        *infra.Log
	emit       func(string, ...any)
	http       *infra.Client
	githubRate *infra.GitHubRateCoordinator
	runner     *fixtool.Service
	appData    *appdata.Store
	client     *db.Client
}

func New() *Service { return NewWithOptions(Options{}) }

func NewWithOptions(opts Options) *Service {
	if opts.Runner == nil {
		opts.Runner = fixtool.New()
	}
	return &Service{
		log:        opts.Log,
		emit:       opts.EventEmit,
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

// contractError preserves user-facing Electron error text, including its
// original capitalisation and punctuation.
type contractError string

func (e contractError) Error() string { return string(e) }

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func sameOrChildPath(root, target string) bool {
	relative, err := filepath.Rel(filepath.Clean(root), filepath.Clean(target))
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) &&
		!filepath.IsAbs(relative)
}
