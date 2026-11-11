package modelviewer

import (
	"context"
	"runtime/debug"
	"sync"
	"time"

	"nahida.live/desktop/internal/appdata"
	"nahida.live/desktop/internal/infra"
)

type Options struct {
	Log                    *infra.Log
	Protocol               *infra.Protocol
	FindModelViewerPreview func(string) *string
}

type Service struct {
	log                    *infra.Log
	protocol               *infra.Protocol
	findModelViewerPreview func(string) *string
	// appData holds the mesh spool; without it, viewer sessions keep mesh buffers in memory.
	appData             *appdata.Store
	modelViewerMu       sync.Mutex
	modelViewerSessions map[string]*modelViewerSession
	modelViewerLoads    map[*modelViewerLoad]struct{}
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
		log:                    opts.Log,
		protocol:               opts.Protocol,
		findModelViewerPreview: opts.FindModelViewerPreview,
		modelViewerSessions:    make(map[string]*modelViewerSession),
		modelViewerLoads:       make(map[*modelViewerLoad]struct{}),
		freeOSMemory:           debug.FreeOSMemory,
		afterFunc: func(delay time.Duration, release func()) func() bool {
			return time.AfterFunc(delay, release).Stop
		},
	}
}

func (t *Service) UseAppData(data *appdata.Store) { t.appData = data }

// ReadResource returns the bytes behind a geometry, texture or position URL of a loaded transport.
func (t *Service) ReadResource(ctx context.Context, url string) ([]byte, error) {
	return t.protocol.Read(ctx, url)
}

func (t *Service) Shutdown() error {
	return t.shutdownModelViewer()
}
