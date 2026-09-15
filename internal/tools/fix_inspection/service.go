// Package fixinspection tracks which mods need a fix and watches them until the
// warning is repaired or dismissed.
package fixinspection

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"

	"nahida.live/desktop/internal/db"
	"nahida.live/desktop/internal/infra"
)

type FixInspectionSettings interface {
	GetAutoInspectFix(context.Context) (bool, error)
}

type Options struct {
	Log       *infra.Log
	EventEmit func(string, ...any)
	Settings  any
}

type Service struct {
	log      *infra.Log
	emit     func(string, ...any)
	settings any
	client   *db.Client

	inspectors            *FixInspectorRegistry
	fixInspectionRunMu    sync.Mutex
	fixInspectionMu       sync.Mutex
	fixInspections        map[string]*trackedFixInspection
	fixInspectionRevision uint64
	fixInspectionClosed   bool
	fixInspectionCtx      context.Context
	fixInspectionCancel   context.CancelFunc
	fixInspectionWG       sync.WaitGroup
}

func New() *Service { return NewWithOptions(Options{}) }

func NewWithOptions(opts Options) *Service {
	fixInspectionCtx, fixInspectionCancel := context.WithCancel(context.Background())
	return &Service{
		log:                 opts.Log,
		emit:                opts.EventEmit,
		settings:            opts.Settings,
		inspectors:          NewFixInspectorRegistry(),
		fixInspections:      make(map[string]*trackedFixInspection),
		fixInspectionCtx:    fixInspectionCtx,
		fixInspectionCancel: fixInspectionCancel,
	}
}

//wails:ignore
func (t *Service) UseClient(client *db.Client) {
	t.client = client
}

func (t *Service) Register(inspector FixInspector) {
	if t == nil || t.inspectors == nil {
		return
	}
	t.inspectors.Register(inspector)
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

func sameOrChildPath(root, target string) bool {
	relative, err := filepath.Rel(filepath.Clean(root), filepath.Clean(target))
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) &&
		!filepath.IsAbs(relative)
}
