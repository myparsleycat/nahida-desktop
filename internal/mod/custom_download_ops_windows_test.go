//go:build windows

package mod

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestMovePathOverwriteCopiesDirectoryAcrossWindowsVolumes(t *testing.T) {
	root := t.TempDir()
	sourcePath := filepath.Join(root, "source")
	destinationPath := filepath.Join(root, "destination")
	nestedPath := filepath.Join(sourcePath, "nested")
	if err := os.MkdirAll(nestedPath, 0o750); err != nil {
		t.Fatal(err)
	}
	filePath := filepath.Join(nestedPath, "mod.ini")
	if err := os.WriteFile(filePath, []byte("mod contents"), 0o640); err != nil {
		t.Fatal(err)
	}
	modTime := time.Date(2024, time.January, 2, 3, 4, 5, 0, time.UTC)
	if err := os.Chtimes(filePath, modTime, modTime); err != nil {
		t.Fatal(err)
	}

	realRename := renameMovePath
	t.Cleanup(func() { renameMovePath = realRename })
	renameMovePath = func(string, string) error {
		return &os.LinkError{Op: "rename", Old: sourcePath, New: destinationPath, Err: windows.ERROR_NOT_SAME_DEVICE}
	}

	if err := movePathOverwrite(sourcePath, destinationPath); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(sourcePath); !os.IsNotExist(err) {
		t.Fatalf("source should be removed, stat = %v", err)
	}
	destinationFile := filepath.Join(destinationPath, "nested", "mod.ini")
	raw, err := os.ReadFile(destinationFile)
	if err != nil || string(raw) != "mod contents" {
		t.Fatalf("destination file = %q, %v", raw, err)
	}
	info, err := os.Stat(destinationFile)
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().Equal(modTime) {
		t.Fatalf("mtime = %v, want %v", info.ModTime(), modTime)
	}
}

func TestMovePathOverwriteCleansPartialDestinationWhenCrossVolumeCopyFails(t *testing.T) {
	root := t.TempDir()
	sourcePath := filepath.Join(root, "source.txt")
	destinationPath := filepath.Join(root, "destination.txt")
	if err := os.WriteFile(sourcePath, []byte("source"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destinationPath, []byte("old destination"), 0o600); err != nil {
		t.Fatal(err)
	}

	realRename := renameMovePath
	realCopy := copyMovePath
	t.Cleanup(func() {
		renameMovePath = realRename
		copyMovePath = realCopy
	})
	renameMovePath = func(string, string) error { return windows.ERROR_NOT_SAME_DEVICE }
	copyMovePath = func(_, destination string) error {
		if err := os.WriteFile(destination, []byte("partial"), 0o600); err != nil {
			return err
		}
		return errors.New("simulated cross-volume copy failure")
	}

	err := movePathOverwrite(sourcePath, destinationPath)
	if err == nil || !strings.Contains(err.Error(), "simulated cross-volume copy failure") {
		t.Fatalf("move error = %v", err)
	}
	if raw, err := os.ReadFile(sourcePath); err != nil || string(raw) != "source" {
		t.Fatalf("source = %q, %v", raw, err)
	}
	if _, err := os.Stat(destinationPath); !os.IsNotExist(err) {
		t.Fatalf("partial destination should be removed, stat = %v", err)
	}
}
