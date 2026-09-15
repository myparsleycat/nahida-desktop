// Package modbisect runs a binary search over a mod's enabled INI files to find
// the one that breaks the game, and keeps interrupted sessions recoverable.
package modbisect

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"sync"

	"nahida.live/desktop/internal/appdata"
	"nahida.live/desktop/internal/db"
	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/xxmi"
)

type Settings interface {
	GetBisectPreserveD3dx(context.Context) (bool, error)
	GetDisabledPrefixStyle(context.Context) (string, error)
}

type Options struct {
	Log       *infra.Log
	EventEmit func(string, ...any)
	Settings  Settings
	XXMI      *xxmi.XXMI
}

type Service struct {
	log      *infra.Log
	emit     func(string, ...any)
	settings Settings
	xxmi     *xxmi.XXMI
	appData  *appdata.Store
	client   *db.Client

	bisectMu         sync.Mutex
	bisect           *bisectSession
	bisectRecovering bool
	d3dx             *d3dxGuard
}

func New() *Service { return NewWithOptions(Options{}) }

func NewWithOptions(opts Options) *Service {
	return &Service{
		log:      opts.Log,
		emit:     opts.EventEmit,
		settings: opts.Settings,
		xxmi:     opts.XXMI,
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
	t.bisectMu.Lock()
	defer t.bisectMu.Unlock()
	return t.cancelBisectLocked()
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
