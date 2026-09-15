// Package touchprofile converts a mod into a Nahida Touch Profile mod by baking
// vertex masks, assets, and INI changes from bone-weight selections.
package touchprofile

import (
	"context"
	"errors"
	"os"
	"sync"

	"nahida.live/desktop/internal/appdata"
	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/platform"
	"nahida.live/desktop/internal/xxmi"
)

type ModDisabler interface {
	Disable(context.Context, string) (string, error)
	Enable(context.Context, string) (string, error)
}

type Options struct {
	Log       *infra.Log
	EventEmit func(string, ...any)
	FS        *platform.FS
	Protocol  *infra.Protocol
	Mod       ModDisabler
	XXMI      *xxmi.XXMI
}

type Service struct {
	log      *infra.Log
	emit     func(string, ...any)
	fs       *platform.FS
	protocol *infra.Protocol
	mod      ModDisabler
	xxmi     *xxmi.XXMI
	appData  *appdata.Store

	touchMu       sync.Mutex
	touchSessions map[string]*touchSession
}

func New() *Service { return NewWithOptions(Options{}) }

func NewWithOptions(opts Options) *Service {
	if opts.FS == nil {
		opts.FS = platform.NewFS()
	}
	if opts.Protocol == nil {
		opts.Protocol = infra.NewProtocol()
	}
	return &Service{
		log:           opts.Log,
		emit:          opts.EventEmit,
		fs:            opts.FS,
		protocol:      opts.Protocol,
		mod:           opts.Mod,
		xxmi:          opts.XXMI,
		touchSessions: make(map[string]*touchSession),
	}
}

//wails:ignore
func (t *Service) UseAppData(data *appdata.Store) {
	t.appData = data
}

func (t *Service) Shutdown() error {
	if t == nil {
		return nil
	}
	return t.shutdownTouchProfiles()
}

func (t *Service) emitEvent(name string, data any) {
	if t != nil && t.emit != nil {
		t.emit(name, data)
	}
}

func (t *Service) reportCleanup(err error, operation string) {
	if err == nil || errors.Is(err, os.ErrNotExist) {
		return
	}
	_ = infra.ReportError(t.log, err, "Tools", infra.Diagnostic{Operation: operation, Stage: "cleanup"})
}
