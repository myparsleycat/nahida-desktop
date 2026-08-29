package tools

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCleanupStaleModelViewerDirsOnlyRemovesMatchingDirectories(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	stale := filepath.Join(root, legacyModelViewerTempPrefix+"old-session")
	keepDir := filepath.Join(root, "other-temp")
	keepFile := filepath.Join(root, legacyModelViewerTempPrefix+"not-a-directory")
	for _, path := range []string{filepath.Join(stale, "nested"), keepDir} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(stale, "nested", "stale.glb"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keepFile, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := cleanupStaleModelViewerDirs(root); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("stale directory still exists: %v", err)
	}
	for _, path := range []string{keepDir, keepFile} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("unrelated path %q was removed: %v", path, err)
		}
	}
}
