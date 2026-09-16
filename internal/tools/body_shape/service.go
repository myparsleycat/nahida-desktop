// Package bodyshape rewrites the position and blend buffers of a mod so the
// shape of a selected mesh is baked into a copy of the mod.
package bodyshape

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"sync"

	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/platform"
)

type ModDisabler interface {
	Disable(context.Context, string) (string, error)
	Enable(context.Context, string) (string, error)
}

type Options struct {
	Log      *infra.Log
	FS       *platform.FS
	Protocol *infra.Protocol
	Mod      ModDisabler
}

type Service struct {
	log      *infra.Log
	fs       *platform.FS
	protocol *infra.Protocol
	mod      ModDisabler

	bodyShapeMu       sync.Mutex
	bodyShapeSessions map[string]*bodyShapeSession
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
		log:               opts.Log,
		fs:                opts.FS,
		protocol:          opts.Protocol,
		mod:               opts.Mod,
		bodyShapeSessions: make(map[string]*bodyShapeSession),
	}
}

func (t *Service) logError(err error, where string) {
	if err != nil && t != nil && t.log != nil {
		_ = infra.ReportError(t.log, err, "Tools", infra.Diagnostic{
			Severity: infra.DiagnosticError, Operation: where, Stage: "background",
		})
	}
}

func newID() (string, error) {
	data := make([]byte, 16)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}
