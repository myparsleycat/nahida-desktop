package modelviewer

import (
	"context"
	"runtime/debug"
	"sync"
	"time"

	"nahida.live/desktop/internal/appdata"
	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/nativecompute"
)

type Options struct {
	Log                    *infra.Log
	Protocol               *infra.Protocol
	FindModelViewerPreview func(string) *string
	// Download fetches character motion files.
	Download *infra.Download
	// EventEmit reports meshes a Blender live link replaced (EventLiveMeshes).
	EventEmit func(string, ...any)
}

type Service struct {
	log                    *infra.Log
	protocol               *infra.Protocol
	findModelViewerPreview func(string) *string
	download               *infra.Download
	packedSkinning         func() uintptr
	// motionFiles admits one character motion download or cache prune at a time, so two viewers never write the
	// same file and a prune never removes a download in progress.
	motionFiles chan struct{}
	// motionPrune clears the motion cache of files the index no longer names, once per process.
	motionPrune sync.Once
	// originalFiles and originalPrune do the same for the original model cache.
	originalFiles chan struct{}
	originalPrune sync.Once
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
	blenderLink                  *BlenderLink
}

func New() *Service { return NewWithOptions(Options{}) }

func NewWithOptions(opts Options) *Service {
	if opts.Protocol == nil {
		opts.Protocol = infra.NewProtocol()
	}
	service := &Service{
		log:                    opts.Log,
		protocol:               opts.Protocol,
		findModelViewerPreview: opts.FindModelViewerPreview,
		download:               opts.Download,
		motionFiles:            make(chan struct{}, 1),
		originalFiles:          make(chan struct{}, 1),
		modelViewerSessions:    make(map[string]*modelViewerSession),
		modelViewerLoads:       make(map[*modelViewerLoad]struct{}),
		freeOSMemory:           debug.FreeOSMemory,
		afterFunc: func(delay time.Duration, release func()) func() bool {
			return time.AfterFunc(delay, release).Stop
		},
	}
	service.blenderLink = newBlenderLink(service, opts.EventEmit)
	return service
}

// BlenderLink is the handler of Blender live link clients, which rebuilds meshes of this service's sessions.
func (t *Service) BlenderLink() *BlenderLink { return t.blenderLink }

func (t *Service) UseAppData(data *appdata.Store) { t.appData = data }

func (t *Service) UseNativeCompute(library *nativecompute.Library) {
	t.packedSkinning = sync.OnceValue(func() uintptr {
		frame, err := lookupPackedSkinningFrame(library)
		if err != nil && t.log != nil {
			_ = infra.ReportError(t.log, err, "ModelViewer.packedSkinning", infra.Diagnostic{
				Operation: "bind-packed-skinning", Severity: infra.DiagnosticWarn,
			})
		}
		return frame
	})
}

// ReadResource returns the bytes behind a geometry, texture or position URL of a loaded transport.
func (t *Service) ReadResource(ctx context.Context, url string) ([]byte, error) {
	return t.protocol.Read(ctx, url)
}

func (t *Service) Shutdown() error {
	return t.shutdownModelViewer()
}
