package texture

import (
	"errors"
	"os"
	"sync"

	"nahida.live/desktop/internal/appdata"
	"nahida.live/desktop/internal/db"
	"nahida.live/desktop/internal/infra"
)

type Options struct {
	Log       *infra.Log
	EventEmit func(string, ...any)
	Download  *infra.Download
	Archive   *infra.Archive
}

type Service struct {
	log      *infra.Log
	emit     func(string, ...any)
	download *infra.Download
	archive  *infra.Archive
	client   *db.Client
	appData  *appdata.Store

	textureRuntimeMu sync.Mutex
	textureEventMu   sync.Mutex
	textureMu        sync.Mutex
	textureNextJob   uint64
	textureState     TextureResizeProgressEvent
	textureJobs      map[uint64]TextureResizeProgressEvent
}

func New() *Service { return NewWithOptions(Options{}) }

func NewWithOptions(opts Options) *Service {
	return &Service{
		log:          opts.Log,
		emit:         opts.EventEmit,
		download:     opts.Download,
		archive:      opts.Archive,
		textureState: TextureResizeProgressEvent{Status: "idle"},
		textureJobs:  make(map[uint64]TextureResizeProgressEvent),
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
