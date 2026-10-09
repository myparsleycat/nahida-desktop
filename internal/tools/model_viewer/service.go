package modelviewer

import (
	"sync"

	"nahida.live/desktop/internal/infra"
)

type Options struct {
	Log                    *infra.Log
	Protocol               *infra.Protocol
	FindModelViewerPreview func(string) *string
}

type Service struct {
	log                      *infra.Log
	protocol                 *infra.Protocol
	findModelViewerPreview   func(string) *string
	modelViewerMu            sync.Mutex
	modelViewerSessions      map[string]*modelViewerSession
	modelViewerClosedWindows map[uint]bool
}

func New() *Service { return NewWithOptions(Options{}) }

func NewWithOptions(opts Options) *Service {
	if opts.Protocol == nil {
		opts.Protocol = infra.NewProtocol()
	}
	return &Service{
		log:                      opts.Log,
		protocol:                 opts.Protocol,
		findModelViewerPreview:   opts.FindModelViewerPreview,
		modelViewerSessions:      make(map[string]*modelViewerSession),
		modelViewerClosedWindows: make(map[uint]bool),
	}
}

func (t *Service) Shutdown() error {
	if t == nil {
		return nil
	}
	return t.shutdownModelViewer()
}
