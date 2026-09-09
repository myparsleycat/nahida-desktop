package tools

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/wailsapp/wails/v3/pkg/application"

	"nahida.live/desktop/internal/infra"
)

type modelViewerOwnerWindow struct {
	application.Window
	id uint
}

func (w modelViewerOwnerWindow) ID() uint { return w.id }

func TestModelViewerWindowCleanupIsOwnedAndIdempotent(t *testing.T) {
	protocol := infra.NewProtocol()
	service := NewWithOptions(Options{Protocol: protocol})
	ids := make([]string, 3)
	for i := range ids {
		ids[i] = protocol.CreateMemorySession()
		service.modelViewerSessions[ids[i]] = &modelViewerSession{windowID: uint(i), modPath: `C:\SameFolder`}
	}
	service.CleanupModelViewerWindow(1)
	service.CleanupModelViewerWindow(1)
	if removed, err := service.CleanupModelViewer(context.Background(), ids[1]); err != nil || removed {
		t.Fatalf("duplicate cleanup = %v, %v", removed, err)
	}
	if len(service.modelViewerSessions) != 2 || service.modelViewerSessions[ids[0]] == nil ||
		service.modelViewerSessions[ids[2]] == nil {
		t.Fatal("closing window released another window's payload")
	}
	for _, id := range []string{ids[0], ids[2]} {
		if removed, err := service.CleanupModelViewer(context.Background(), id); err != nil || !removed {
			t.Fatalf("cleanup %s: %v %v", id, removed, err)
		}
	}
	service.CleanupModelViewerWindow(2)
	if len(service.modelViewerSessions) != 0 {
		t.Fatal("sessions remain")
	}
}

func TestLoadModViewerRecordsOwnerAndPreviewAndRejectsClosedOwner(t *testing.T) {
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
	ctx := context.WithValue(context.Background(), application.WindowKey, modelViewerOwnerWindow{id: 42})
	result, err := fixture.service.LoadModViewer(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if result.PreviewPath == nil || *result.PreviewPath != preview ||
		fixture.service.modelViewerSessions[result.MemorySessionID].windowID != 42 {
		t.Fatal("transport lost preview or owner")
	}
	fixture.service.CleanupModelViewerWindow(42)
	if fixture.service.modelViewerSessions[fixture.result.MemorySessionID] == nil {
		t.Fatal("unowned session was released")
	}
	// Close during discovery to exercise the final registration fence after payload creation.
	fixture.service.findModelViewerPreview = func(string) *string { fixture.service.CleanupModelViewerWindow(43); return nil }
	closedCtx := context.WithValue(context.Background(), application.WindowKey, modelViewerOwnerWindow{id: 43})
	if _, err := fixture.service.LoadModViewer(closedCtx, dir); !errors.Is(err, context.Canceled) {
		t.Fatalf("late load = %v", err)
	}
	if len(fixture.service.modelViewerSessions) != 1 {
		t.Fatalf("late load registered a session: %v", fixture.service.modelViewerSessions)
	}
}
