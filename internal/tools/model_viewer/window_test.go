package modelviewer

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"nahida.live/desktop/internal/infra"
)

type viewerMarkerWriter struct {
	marker string
	seen   *atomic.Bool
}

func (w viewerMarkerWriter) Write(p []byte) (int, error) {
	if strings.Contains(string(p), w.marker) {
		w.seen.Store(true)
	}
	return len(p), nil
}

func TestLoadModViewerRecordsPreviewAndStopsAtShutdown(t *testing.T) {
	dir := t.TempDir()
	fixture := loadViewerFixture(t, dir, `[TextureOverrideBody]
ib = ResourceBodyIB
vb0 = ResourcePos
vb1 = ResourceTc
drawindexed = 3, 0, 0
`+viewerBodyResources)
	preview := filepath.Join(dir, "preview.png")
	fixture.service.findModelViewerPreview = func(path string) *string {
		if path != dir {
			t.Fatalf("preview path %q", path)
		}
		return &preview
	}
	result, err := fixture.service.LoadModViewer(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if result.PreviewPath == nil || *result.PreviewPath != preview {
		t.Fatal("transport lost its preview")
	}

	// Shutdown during discovery cancels the load before texture preparation.
	var texturesPrepared atomic.Bool
	fixture.service.log = infra.NewLogWithOptions(infra.LogOptions{
		Dev:         true,
		DisableFile: true,
		Writer:      viewerMarkerWriter{marker: "Texture preparation completed", seen: &texturesPrepared},
	})
	fixture.service.findModelViewerPreview = func(string) *string { _ = fixture.service.Shutdown(); return nil }
	if _, err := fixture.service.LoadModViewer(context.Background(), dir); !errors.Is(err, context.Canceled) {
		t.Fatalf("load during shutdown = %v", err)
	}
	if texturesPrepared.Load() {
		t.Fatal("load kept preparing textures after shutdown")
	}
	if len(fixture.service.modelViewerLoads) != 0 {
		t.Fatalf("finished loads remain registered: %v", fixture.service.modelViewerLoads)
	}
	if len(fixture.service.modelViewerSessions) != 0 {
		t.Fatalf("sessions remain after shutdown: %v", fixture.service.modelViewerSessions)
	}
}
