package modelviewer

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"image/png"
	"os"
	"path/filepath"
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
		writeGridPreviewTestFile(t, path, []byte("other"))
		// Same-size edits still invalidate via nanosecond file modification time.
		if err := os.Chtimes(path, time.Now(), time.Now().Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
		if next := fingerprint(); next == previous {
			t.Fatalf("editing %s did not invalidate fingerprint", name)
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
	writeGridPreviewTestFile(t, texture, []byte("other!!"))
	// Same-size edits still invalidate via nanosecond file modification time.
	if err := os.Chtimes(texture, time.Now(), time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if next := fingerprint(); next == previous {
		t.Fatal("texture timestamp-only edit did not invalidate fingerprint")
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
	if initial.Fingerprint == "" || initial.Image != "" {
		t.Fatalf("unexpected initial cache: %+v", initial)
	}
	encoded := gridPreviewTestEncodedImage(t)
	if err := service.SaveModGridPreviewCache(ctx, dir, "settings-a", initial.Fingerprint, encoded); err != nil {
		t.Fatal(err)
	}
	service = New()
	service.UseAppData(data)
	if get("settings-a").Image != encoded {
		t.Fatal("new service did not reuse disk cache")
	}
	writeGridPreviewTestFile(t, filepath.Join(dir, "readme.txt"), []byte("new notes"))
	if get("settings-a").Image != encoded || get("settings-b").Image != "" {
		t.Fatal("ignored files or render settings were not handled correctly")
	}
	writeGridPreviewTestFile(t, path, []byte("changed during rendering"))
	if err := service.SaveModGridPreviewCache(ctx, dir, "settings-a", initial.Fingerprint, encoded); err != nil {
		t.Fatal(err)
	}
	changed := get("settings-a")
	if changed.Image != "" || changed.Fingerprint == initial.Fingerprint {
		t.Fatal("stale image was reused after a source edit")
	}
	if err := service.SaveModGridPreviewCache(ctx, dir, "settings-a", changed.Fingerprint, encoded); err != nil {
		t.Fatal(err)
	}
	if get("settings-a").Image != encoded {
		t.Fatal("replacement image was not saved")
	}
	cachePath, err := service.gridPreviewCachePath(dir, "settings-a")
	if err != nil {
		t.Fatal(err)
	}
	writeGridPreviewTestFile(t, cachePath, []byte("corrupt"))
	if get("settings-a").Image != "" {
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
	encoded := gridPreviewTestEncodedImage(t)
	if err := service.SaveModGridPreviewCache(ctx, dir, "settings-a", initial.Fingerprint, encoded); err != nil {
		t.Fatal(err)
	}
	if cached, readErr := service.GetModGridPreviewCache(ctx, dir, "settings-a"); readErr != nil {
		t.Fatal(readErr)
	} else if cached.Image != encoded {
		t.Fatal("texture render was not cached")
	}
	// The INI and mesh files are untouched; only the texture changes.
	writeGridPreviewTestFile(t, texture, []byte("second-content"))
	cached, err := service.GetModGridPreviewCache(ctx, dir, "settings-a")
	if err != nil {
		t.Fatal(err)
	}
	if cached.Image != "" || cached.Fingerprint == initial.Fingerprint {
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

func writeGridPreviewTestFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func gridPreviewTestEncodedImage(t *testing.T) string {
	t.Helper()
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, image.NewRGBA(image.Rect(0, 0, 512, 512))); err != nil {
		t.Fatal(err)
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buffer.Bytes())
}
