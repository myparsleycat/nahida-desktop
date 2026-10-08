package modelviewer

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"nahida.live/desktop/internal/appdata"
)

func TestGridPreviewFingerprintTracksModFiles(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	fingerprint := func() string {
		t.Helper()
		value, err := gridPreviewFingerprint(context.Background(), dir)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	previous := fingerprint()
	for _, name := range []string{
		"mesh.IB", "mesh.vb", "mod.ini", "nested/data.buf", "mesh.fmt", "shader.hlsl",
		"body.dds", "diffuse.png", "normal.jpg", "material.JPEG",
	} {
		path := filepath.Join(dir, name)
		writeGridPreviewTestFile(t, path, []byte("first"))
		if next := fingerprint(); next == previous {
			t.Fatalf("adding %s did not invalidate fingerprint", name)
		} else {
			previous = next
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		writeGridPreviewTestFile(t, path, []byte("resized"))
		if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
			t.Fatal(err)
		}
		if next := fingerprint(); next == previous {
			t.Fatalf("resizing %s did not invalidate fingerprint", name)
		} else {
			previous = next
		}
		touched := info.ModTime().Add(time.Hour)
		if err := os.Chtimes(path, touched, touched); err != nil {
			t.Fatal(err)
		}
		if next := fingerprint(); next == previous {
			t.Fatalf("touching %s did not invalidate fingerprint", name)
		} else {
			previous = next
		}
		if err := os.Rename(path, path+".disabled"); err != nil {
			t.Fatal(err)
		}
		if next := fingerprint(); next == previous {
			t.Fatalf("removing %s did not invalidate fingerprint", name)
		} else {
			previous = next
		}
	}
	for _, name := range []string{"clip.mp4", "readme.txt", "notes.md"} {
		writeGridPreviewTestFile(t, filepath.Join(dir, name), []byte("ignored"))
	}
	if fingerprint() != previous {
		t.Fatal("ordinary media and text files invalidated fingerprint")
	}
}

func TestGridPreviewFingerprintTracksTextureOnlyEdits(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeGridPreviewTestFile(t, filepath.Join(dir, "mod.ini"), []byte("ini"))
	texture := filepath.Join(dir, "Textures", "body.dds")
	if err := os.MkdirAll(filepath.Dir(texture), 0o700); err != nil {
		t.Fatal(err)
	}
	writeGridPreviewTestFile(t, texture, []byte("first"))
	fingerprint := func() string {
		t.Helper()
		value, err := gridPreviewFingerprint(context.Background(), dir)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	previous := fingerprint()
	// Replace only the texture; the INI and mesh files stay byte-identical.
	writeGridPreviewTestFile(t, texture, []byte("second-content"))
	if next := fingerprint(); next == previous {
		t.Fatal("texture-only edit did not invalidate fingerprint")
	} else {
		previous = next
	}
	info, err := os.Stat(texture)
	if err != nil {
		t.Fatal(err)
	}
	writeGridPreviewTestFile(t, texture, []byte("SECOND-CONTENT"))
	// Same-size edits are detected through the modification time alone.
	touched := info.ModTime().Add(time.Hour)
	if err := os.Chtimes(texture, touched, touched); err != nil {
		t.Fatal(err)
	}
	if next := fingerprint(); next == previous {
		t.Fatal("same-size texture edit did not invalidate fingerprint")
	} else {
		previous = next
	}

	// Contents are not hashed, so an edit that keeps size and modification time is not detected.
	writeGridPreviewTestFile(t, texture, []byte("third--content"))
	if err := os.Chtimes(texture, touched, touched); err != nil {
		t.Fatal(err)
	}
	if fingerprint() != previous {
		t.Fatal("fingerprint read file contents")
	}
}

func TestGridPreviewCachePersistsAndRejectsStaleImages(t *testing.T) {
	t.Parallel()
	data, err := appdata.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "mod.ini")
	writeGridPreviewTestFile(t, path, []byte("original"))
	service := New()
	service.UseAppData(data)
	ctx := context.Background()
	get := func(variant string) GridPreviewCache {
		t.Helper()
		cached, err := service.GetModGridPreviewCache(ctx, dir, variant)
		if err != nil {
			t.Fatal(err)
		}
		return cached
	}
	initial := get("settings-a")
	if initial.Fingerprint == "" || initial.URL != "" {
		t.Fatalf("unexpected initial cache: %+v", initial)
	}
	sourceInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	image := gridPreviewTestImage(t)
	saveGridPreviewTestImage(t, service, dir, "settings-a", initial.Fingerprint, image)
	service = New()
	service.UseAppData(data)
	saved := get("settings-a").URL
	if saved == "" {
		t.Fatal("new service did not reuse disk cache")
	}
	request := httptest.NewRequest(http.MethodGet, saved, nil)
	response := httptest.NewRecorder()
	service.protocol.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !bytes.Equal(response.Body.Bytes(), image) {
		t.Fatalf("cached image status=%d bytes=%d", response.Code, response.Body.Len())
	}
	writeGridPreviewTestFile(t, filepath.Join(dir, "readme.txt"), []byte("new notes"))
	if get("settings-a").URL != saved || get("settings-b").URL != "" {
		t.Fatal("ignored files or render settings were not handled correctly")
	}
	writeGridPreviewTestFile(t, path, []byte("replaced"))
	edited := sourceInfo.ModTime().Add(time.Hour)
	if err := os.Chtimes(path, edited, edited); err != nil {
		t.Fatal(err)
	}
	saveGridPreviewTestImage(t, service, dir, "settings-a", initial.Fingerprint, image)
	changed := get("settings-a")
	if changed.URL != "" || changed.Fingerprint == initial.Fingerprint {
		t.Fatal("stale image was reused after a source edit")
	}
	saveGridPreviewTestImage(t, service, dir, "settings-a", changed.Fingerprint, image)
	if replaced := get("settings-a").URL; replaced == "" || replaced == saved {
		t.Fatal("replacement image was not cached after a source edit")
	}
	stalePath, err := service.gridPreviewCachePath(dir, "settings-a", initial.Fingerprint)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stalePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("render of the earlier fingerprint was kept: %v", err)
	}
	cachePath, err := service.gridPreviewCachePath(dir, "settings-a", changed.Fingerprint)
	if err != nil {
		t.Fatal(err)
	}
	writeGridPreviewTestFile(t, cachePath, []byte("corrupt"))
	if get("settings-a").URL != "" {
		t.Fatal("corrupt cache was reused")
	}
}

func TestGridPreviewCacheInvalidatesOnTextureOnlyEdit(t *testing.T) {
	t.Parallel()
	data, err := appdata.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeGridPreviewTestFile(t, filepath.Join(dir, "mod.ini"), []byte("ini"))
	texture := filepath.Join(dir, "Textures", "body.dds")
	if err := os.MkdirAll(filepath.Dir(texture), 0o700); err != nil {
		t.Fatal(err)
	}
	writeGridPreviewTestFile(t, texture, []byte("first"))
	service := New()
	service.UseAppData(data)
	ctx := context.Background()
	initial, err := service.GetModGridPreviewCache(ctx, dir, "settings-a")
	if err != nil {
		t.Fatal(err)
	}
	saveGridPreviewTestImage(t, service, dir, "settings-a", initial.Fingerprint, gridPreviewTestImage(t))
	if cached, readErr := service.GetModGridPreviewCache(ctx, dir, "settings-a"); readErr != nil {
		t.Fatal(readErr)
	} else if cached.URL == "" {
		t.Fatal("texture render was not cached")
	}
	// The INI and mesh files are untouched; only the texture changes.
	writeGridPreviewTestFile(t, texture, []byte("second-content"))
	cached, err := service.GetModGridPreviewCache(ctx, dir, "settings-a")
	if err != nil {
		t.Fatal(err)
	}
	if cached.URL != "" || cached.Fingerprint == initial.Fingerprint {
		t.Fatal("texture-only edit reused the stale cached render")
	}
}

func TestGridPreviewFingerprintRejectsMissingAndCancelledSources(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if _, err := gridPreviewFingerprint(context.Background(), filepath.Join(dir, "missing")); err == nil {
		t.Fatal("missing directory accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := gridPreviewFingerprint(ctx, dir); err == nil {
		t.Fatal("cancelled scan succeeded")
	}
}

func TestValidateGridPreviewImageRejectsTruncatedPNG(t *testing.T) {
	t.Parallel()
	const pngHeaderLength = 33
	truncated := gridPreviewTestImage(t)[:pngHeaderLength]
	if err := validateGridPreviewImageConfig(bytes.NewReader(truncated)); err != nil {
		t.Fatalf("truncated PNG did not retain a valid configuration: %v", err)
	}
	err := validateGridPreviewImage(truncated)
	if err == nil {
		t.Fatal("truncated PNG was accepted")
	}
	if !strings.Contains(err.Error(), "decode grid preview PNG") || errors.Unwrap(err) == nil {
		t.Fatalf("truncated PNG returned an unexpected error: %v", err)
	}
}

func writeGridPreviewTestFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func gridPreviewTestImage(t *testing.T) []byte {
	t.Helper()
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, image.NewRGBA(image.Rect(0, 0, 512, 512))); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

// saveGridPreviewTestImage follows the renderer's flow: open an upload slot in
// a model session, PUT the image, then commit it to the cache.
func saveGridPreviewTestImage(
	t *testing.T,
	service *Service,
	modPath, variant, fingerprint string,
	image []byte,
) {
	t.Helper()
	sessionID := service.protocol.CreateMemorySession()
	defer service.protocol.CleanupMemorySession(sessionID)
	uploadURL, err := service.PrepareModGridPreviewCacheUpload(sessionID, int64(len(image)))
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPut, uploadURL, bytes.NewReader(image))
	request.Header.Set("Content-Type", "application/octet-stream")
	response := httptest.NewRecorder()
	service.protocol.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("upload status = %d", response.Code)
	}
	if err := service.SaveModGridPreviewCache(
		context.Background(),
		modPath,
		variant,
		fingerprint,
		sessionID,
	); err != nil {
		t.Fatal(err)
	}
}
