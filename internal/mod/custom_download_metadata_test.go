package mod

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"

	"nahida.live/desktop/internal/mod/metadata"
)

func TestWriteDownloadMetadataToPathsWritesOncePerDirectory(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "first")
	second := filepath.Join(root, "second")
	for _, dir := range []string{first, second} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	filePath := filepath.Join(first, "mod.ini")
	if err := os.WriteFile(filePath, []byte("mod"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(first, "nhd.json"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}

	created := 0
	if err := writeDownloadMetadataToPaths([]string{first, filePath, second}, func() metadata.DirectDownload {
		created++
		return metadata.DirectDownload{
			ID:           uuid.NewString(),
			Source:       metadata.DirectSource,
			DownloadedAt: "2025-01-01T00:00:00Z",
		}
	}); err != nil {
		t.Fatal(err)
	}
	if created != 2 {
		t.Fatalf("created metadata = %d, want 2", created)
	}
	ids := map[string]bool{}
	for _, dir := range []string{first, second} {
		raw, err := metadata.Read(dir)
		if err != nil {
			t.Fatal(err)
		}
		var document metadata.DirectDownload
		if err := json.Unmarshal(raw, &document); err != nil || document.Source != metadata.DirectSource ||
			document.DownloadedAt != "2025-01-01T00:00:00Z" || document.ID == "" || ids[document.ID] {
			t.Fatalf("metadata in %s = %s, %v", dir, raw, err)
		}
		ids[document.ID] = true
	}
	if matches, err := filepath.Glob(filepath.Join(root, "*", "nhd.json.backup-*")); err != nil || len(matches) != 0 {
		t.Fatalf("metadata backups = %v, %v", matches, err)
	}
}
