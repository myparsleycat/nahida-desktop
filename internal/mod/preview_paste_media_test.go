package mod

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"nahida.live/desktop/internal/infra"
)

func TestPastePreviewPreservesSupportedVideoPathExtension(t *testing.T) {
	ctx := context.Background()
	service, root := newTestMod(t, testSettings{})
	modsRoot := filepath.Join(root, "mods")
	modPath := filepath.Join(modsRoot, "Mod")
	if err := os.MkdirAll(modPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := service.AddGame(ctx, "Game", modsRoot, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "clip.MP4")
	content := []byte("video bytes")
	if err := os.WriteFile(source, content, 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := service.PastePreview(ctx, modPath, source, "path", nil)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(result) != "preview.mp4" {
		t.Fatalf("preview path = %q", result)
	}
	raw, err := os.ReadFile(result)
	if err != nil || string(raw) != string(content) {
		t.Fatalf("preview content = %q, %v", raw, err)
	}
	if preview := findPreview(modPath, false); preview == nil || !samePath(*preview, result) {
		t.Fatalf("scanner preview = %v, want %s", preview, result)
	}
}

func TestPastePreviewUsesSupportedAudioMIMEExtension(t *testing.T) {
	content := []byte("ogg bytes")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "audio/ogg; codecs=opus")
		_, _ = w.Write(content)
	}))
	defer server.Close()

	ctx := context.Background()
	service, root := newTestMod(t, testSettings{})
	service.http = infra.NewClientWithOptions(infra.ClientOptions{HTTPClient: server.Client()})
	modsRoot := filepath.Join(root, "mods")
	modPath := filepath.Join(modsRoot, "Mod")
	if err := os.MkdirAll(modPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := service.AddGame(ctx, "Game", modsRoot, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}

	result, err := service.PastePreview(ctx, modPath, server.URL, "url", nil)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(result) != "preview.ogg" {
		t.Fatalf("preview path = %q", result)
	}
	raw, err := os.ReadFile(result)
	if err != nil || string(raw) != string(content) {
		t.Fatalf("preview content = %q, %v", raw, err)
	}
}

func TestPreviewExtensionNormalizesVideoMIMEAliases(t *testing.T) {
	tests := map[string]string{
		"video/mp4":        ".mp4",
		"video/quicktime":  ".mov",
		"video/x-msvideo":  ".avi",
		"video/x-matroska": ".mkv",
		"video/webm":       ".webm",
		"application/ogg":  ".ogg",
		"text/plain":       ".png",
	}
	for contentType, want := range tests {
		if got := previewExtension(contentType, nil); got != want {
			t.Errorf("previewExtension(%q) = %q, want %q", contentType, got, want)
		}
	}
}
