package mod

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteModDownloadMetadataToDirectoriesRestoresEveryDirectoryWhenHideFails(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "first")
	second := filepath.Join(root, "second")
	for _, dir := range []string{first, second} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	firstMetadata := filepath.Join(first, modDownloadMetadataFileName)
	if err := os.WriteFile(firstMetadata, []byte("original metadata\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	realHide := hideModDownloadMetadataFile
	t.Cleanup(func() { hideModDownloadMetadataFile = realHide })
	hideModDownloadMetadataFile = func(path string) error {
		if filepath.Dir(path) == second {
			return errors.New("simulated hidden-attribute failure")
		}
		return nil
	}

	err := writeModDownloadMetadataToDirectories([]string{first, second}, map[string]any{"source": "mod"})
	if err == nil || !strings.Contains(err.Error(), "simulated hidden-attribute failure") {
		t.Fatalf("write error = %v", err)
	}
	if raw, err := os.ReadFile(firstMetadata); err != nil || string(raw) != "original metadata\n" {
		t.Fatalf("restored first metadata = %q, %v", raw, err)
	}
	if _, err := os.Stat(filepath.Join(second, modDownloadMetadataFileName)); !os.IsNotExist(err) {
		t.Fatalf("new second metadata should be removed, stat = %v", err)
	}
	if matches, err := filepath.Glob(filepath.Join(root, "*", "nhd.json.backup-*")); err != nil || len(matches) != 0 {
		t.Fatalf("metadata backups = %v, %v", matches, err)
	}
}

func TestWriteModDownloadMetadataToDirectoriesBacksUpOnceForDuplicateDirectory(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "mod.ini")
	if err := os.WriteFile(filePath, []byte("mod"), 0o600); err != nil {
		t.Fatal(err)
	}
	metadataPath := filepath.Join(dir, modDownloadMetadataFileName)
	if err := os.WriteFile(metadataPath, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}

	realHide := hideModDownloadMetadataFile
	t.Cleanup(func() { hideModDownloadMetadataFile = realHide })
	hideCalls := 0
	hideModDownloadMetadataFile = func(string) error {
		hideCalls++
		return nil
	}

	if err := writeModDownloadMetadataToDirectories([]string{dir, filePath}, map[string]any{"source": "mod"}); err != nil {
		t.Fatal(err)
	}
	if hideCalls != 1 {
		t.Fatalf("hide calls = %d, want 1", hideCalls)
	}
	raw, err := os.ReadFile(metadataPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"source": "mod"`) || !strings.HasSuffix(string(raw), "\n") {
		t.Fatalf("metadata = %q", raw)
	}
	if matches, err := filepath.Glob(filepath.Join(dir, "nhd.json.backup-*")); err != nil || len(matches) != 0 {
		t.Fatalf("metadata backups = %v, %v", matches, err)
	}
}
