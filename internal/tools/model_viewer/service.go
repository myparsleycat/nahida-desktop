package modelviewer

import (
	"runtime/debug"
	"sync"
	"time"

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
	modelViewerLoads         map[uint]map[*modelViewerLoad]struct{}
	// modelViewerStopRelease stops the pending deferred memory release, if any.
	modelViewerStopRelease       func() bool
	modelViewerReleaseGeneration uint64
	modelViewerShutdown          bool
	freeOSMemory                 func()
	afterFunc                    func(time.Duration, func()) func() bool
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
		modelViewerLoads:         make(map[uint]map[*modelViewerLoad]struct{}),
		freeOSMemory:             debug.FreeOSMemory,
		afterFunc: func(delay time.Duration, release func()) func() bool {
			return time.AfterFunc(delay, release).Stop
		},
	}
}

func (t *Service) Shutdown() error {
	return t.shutdownModelViewer()
}
