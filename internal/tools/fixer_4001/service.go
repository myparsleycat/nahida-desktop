// Package fixer4001 builds the XXMI d3d11 proxy DLL for a mod from source and
// diversifies the compiled binary's padding.
package fixer4001

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"os"
	"sync"
	"time"

	"nahida.live/desktop/internal/appdata"
	"nahida.live/desktop/internal/db"
	"nahida.live/desktop/internal/github"
	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/platform"
	"nahida.live/desktop/internal/xxmi"
)

type PEDiversifier interface {
	Diversify(context.Context, string, string) (PEDiversificationReport, error)
}

type Options struct {
	Log           *infra.Log
	EventEmit     func(string, ...any)
	FS            *platform.FS
	HTTP          *infra.Client
	Download      *infra.Download
	Archive       *infra.Archive
	XXMI          *xxmi.XXMI
	PEDiversifier PEDiversifier
	// GitHub serves release lists and source archives; nil builds one from HTTP and Download.
	GitHub *github.Client
}

type Service struct {
	log           *infra.Log
	emit          func(string, ...any)
	fs            *platform.FS
	github        *github.Client
	download      *infra.Download
	archive       *infra.Archive
	xxmi          *xxmi.XXMI
	peDiversifier PEDiversifier
	client        *db.Client
	appData       *appdata.Store

	fixerMu       sync.Mutex
	fixerTask     *string
	fixerProgress string
	fixerError    string
}

func New() *Service { return NewWithOptions(Options{}) }

func NewWithOptions(opts Options) *Service {
	if opts.FS == nil {
		opts.FS = platform.NewFS()
	}
	if opts.GitHub == nil {
		opts.GitHub = github.New(github.Options{HTTP: opts.HTTP, Download: opts.Download, Log: opts.Log})
	}
	return &Service{
		log:           opts.Log,
		emit:          opts.EventEmit,
		fs:            opts.FS,
		github:        opts.GitHub,
		download:      opts.Download,
		archive:       opts.Archive,
		xxmi:          opts.XXMI,
		peDiversifier: opts.PEDiversifier,
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

func (t *Service) deleteAppState(ctx context.Context, key string) error {
	client, err := t.requireClient()
	if err != nil {
		return err
	}
	return client.AppState.Delete(ctx, key)
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

func (t *Service) reportCleanup(err error, operation string) {
	if err == nil || errors.Is(err, os.ErrNotExist) {
		return
	}
	_ = infra.ReportError(t.log, err, "Tools", infra.Diagnostic{Operation: operation, Stage: "cleanup"})
}

func cloneStringPointer(value *string) *string {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func newID() (string, error) {
	data := make([]byte, 16)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}
