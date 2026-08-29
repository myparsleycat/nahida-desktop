package mod

import (
	"os"
	"path/filepath"
	"testing"
)

func writePreviewFile(t *testing.T, root, relative string) string {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("preview"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func previewEqual(t *testing.T, got *string, want string) {
	t.Helper()
	if want == "" {
		if got != nil {
			t.Fatalf("preview = %q, want nil", *got)
		}
		return
	}
	if got == nil || *got != want {
		gotValue := "<nil>"
		if got != nil {
			gotValue = *got
		}
		t.Fatalf("preview = %q, want %q", gotValue, want)
	}
}

func TestFindPreview(t *testing.T) {
	t.Parallel()

	t.Run("accepts a media file that is not named preview", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		screenshot := writePreviewFile(t, root, "screenshot.png")
		previewEqual(t, findPreview(root, false), screenshot)
	})

	t.Run("accepts ogg in the NTE preview finder", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		preview := writePreviewFile(t, root, "preview.ogg")
		previewEqual(t, findPreview(root, false), preview)
	})

	t.Run("prefers a preview-named file over a generic root image", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		preview := writePreviewFile(t, root, "preview.webp")
		writePreviewFile(t, root, "cover.jpg")
		previewEqual(t, findPreview(root, false), preview)
	})

	t.Run("accepts a file whose name contains preview", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		preview := writePreviewFile(t, root, "mod_preview.png")
		previewEqual(t, findPreview(root, false), preview)
	})

	t.Run("searches nested folders only when asked", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		nested := writePreviewFile(t, root, "images/cover.jpg")
		previewEqual(t, findPreview(root, true), nested)
		previewEqual(t, findPreview(root, false), "")
	})

	t.Run("prefers a root image over a nested preview in a disabled folder", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		screenshot := writePreviewFile(t, root, "screenshot.png")
		writePreviewFile(t, root, "DISABLED Nested/preview.png")
		previewEqual(t, findPreview(root, true), screenshot)
	})

	t.Run("prefers a group-root preview over a child folder", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		rootPreview := writePreviewFile(t, root, "preview.png")
		writePreviewFile(t, root, "Enabled Mod/preview.png")
		previewEqual(t, findPreview(root, true), rootPreview)
	})

	t.Run("prefers an enabled child folder before a disabled one", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		enabledPreview := writePreviewFile(t, root, "Enabled Mod/nested/deeper/preview.png")
		writePreviewFile(t, root, "DISABLED Other Mod/preview.png")
		previewEqual(t, findPreview(root, true), enabledPreview)
	})

	t.Run("falls back to a disabled child folder", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		disabledPreview := writePreviewFile(t, root, "DISABLED Other Mod/preview.png")
		previewEqual(t, findPreview(root, true), disabledPreview)
	})

	t.Run("does not search child folders at depth one", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		writePreviewFile(t, root, "Enabled Mod/preview.png")
		previewEqual(t, findGroupPreview(root, 1), "")
	})

	t.Run("caps child preview search at rust depth three", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		found := writePreviewFile(t, root, "Enabled Mod/nested/deeper/preview.png")
		writePreviewFile(t, root, "Enabled Mod/a/b/c/d/too-deep.png")
		previewEqual(t, findPreview(root, true), found)
	})

	t.Run("ignores previews deeper than rust child depth three", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		writePreviewFile(t, root, "Enabled Mod/a/b/c/d/preview.png")
		previewEqual(t, findPreview(root, true), "")
	})

	t.Run("accepts a non-preview filename in a child folder", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		cover := writePreviewFile(t, root, "Enabled Mod/cover.jpg")
		previewEqual(t, findPreview(root, true), cover)
	})

	t.Run("recognizes a disabled preview file suffix", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		preview := writePreviewFile(t, root, "preview.png.disabled")
		previewEqual(t, findPreview(root, false), preview)
	})

	t.Run("ignores texture-like names", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		writePreviewFile(t, root, "normal.png")
		writePreviewFile(t, root, "light.jpg")
		writePreviewFile(t, root, "material.webp")
		writePreviewFile(t, root, "diffuse.png")
		previewEqual(t, findPreview(root, false), "")
	})
}
