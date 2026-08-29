//go:build windows

package platform

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestLockingFilesInDirSkipsDotNamedEntriesLikeJwalk(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	visible := filepath.Join(root, "visible.txt")
	hiddenFile := filepath.Join(root, ".hidden.txt")
	hiddenDir := filepath.Join(root, ".hidden")
	visibleNested := filepath.Join(root, "visible", "nested.txt")
	hiddenNested := filepath.Join(hiddenDir, "nested.txt")
	for _, path := range []string{visible, hiddenFile, visibleNested, hiddenNested} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	files := lockingFilesInDir(root)
	if !slices.Contains(files, visible) || !slices.Contains(files, visibleNested) {
		t.Fatalf("visible files missing from %v", files)
	}
	if slices.Contains(files, hiddenFile) || slices.Contains(files, hiddenNested) {
		t.Fatalf("dot-named entries were not skipped: %v", files)
	}
}

func TestLockingFilesInDirAllowsDotNamedRoot(t *testing.T) {
	t.Parallel()
	root := filepath.Join(t.TempDir(), ".root")
	file := filepath.Join(root, "visible.txt")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if files := lockingFilesInDir(root); !slices.Contains(files, file) {
		t.Fatalf("dot-named root should still be traversed: %v", files)
	}
}
