//go:build windows

package watcher

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestWatchTreeValidatesAllRootsBeforeOpeningHandles(t *testing.T) {
	root := t.TempDir()
	missing := filepath.Join(t.TempDir(), "missing")
	if _, err := WatchTree([]string{root, missing}, TreeConfig{Depth: -1}, func(Event) {}); err == nil {
		t.Fatal("watching a missing root succeeded")
	}

	rootPtr, err := windows.UTF16PtrFromString(root)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := windows.CreateFile(
		rootPtr,
		windows.FILE_LIST_DIRECTORY,
		0,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS,
		0,
	)
	if err != nil {
		t.Fatalf("open first root exclusively after validation failure: %v", err)
	}
	if err := windows.CloseHandle(handle); err != nil {
		t.Fatal(err)
	}
}

func TestWatchTreeReturnsAfterInitialReadIsPending(t *testing.T) {
	root := t.TempDir()
	events := make(chan Event, 1)
	w, err := WatchTree([]string{root}, TreeConfig{Depth: -1}, func(event Event) {
		events <- event
	})
	if err != nil {
		t.Fatal(err)
	}
	closeAfterTest(t, w)

	w.roots[0].mu.Lock()
	pending := w.roots[0].pending
	recursive := w.roots[0].recursive
	w.roots[0].mu.Unlock()
	if !pending {
		t.Fatal("initial ReadDirectoryChangesW request is not pending")
	}
	if !recursive {
		t.Fatal("unlimited tree watcher is not recursive")
	}

	path := filepath.Join(root, "immediate.ini")
	if err := os.WriteFile(path, []byte("test"), 0o600); err != nil {
		t.Fatal(err)
	}
	if event := waitEvent(t, events); !SamePath(event.Path, path) {
		t.Fatalf("event path = %q, want %q", event.Path, path)
	}
}

func TestWatchFileUsesNonRecursiveRoot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "d3dx_user.ini")
	if err := os.WriteFile(path, []byte("test"), 0o600); err != nil {
		t.Fatal(err)
	}
	w, err := WatchFile(path, FileConfig{}, func(Event) {})
	if err != nil {
		t.Fatal(err)
	}
	closeAfterTest(t, w)
	if w.roots[0].recursive {
		t.Fatal("exact-file watcher recursively watches the parent tree")
	}
}

func TestWatchTreeAllowsRenamingNestedDirectory(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "Character", "Mod")
	if err := os.MkdirAll(source, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "mod.ini"), []byte("test"), 0o600); err != nil {
		t.Fatal(err)
	}

	w, err := WatchTree([]string{root}, TreeConfig{Depth: -1}, func(Event) {})
	if err != nil {
		t.Fatal(err)
	}
	closeAfterTest(t, w)

	destination := filepath.Join(root, "Character", "DISABLED Mod")
	if err := os.Rename(source, destination); err != nil {
		t.Fatalf("rename watched mod directory: %v", err)
	}
}

func TestWatchTreeAllowsRemovingNestedDirectory(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "Character", "Mod", "Nested")
	if err := os.MkdirAll(target, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "mod.ini"), []byte("test"), 0o600); err != nil {
		t.Fatal(err)
	}

	w, err := WatchTree([]string{root}, TreeConfig{Depth: -1}, func(Event) {})
	if err != nil {
		t.Fatal(err)
	}
	closeAfterTest(t, w)

	if err := os.RemoveAll(filepath.Join(root, "Character", "Mod")); err != nil {
		t.Fatalf("remove watched mod directory: %v", err)
	}
}

func TestWatchTreeRecursivelyObservesNewNestedDirectory(t *testing.T) {
	root := t.TempDir()
	events := make(chan Event, 8)
	w, err := WatchTree([]string{root}, TreeConfig{
		Depth: -1,
		Ops:   Create | Write,
		Filter: func(event Event) bool {
			return filepath.Ext(event.Path) == ".ini"
		},
	}, func(event Event) {
		events <- event
	})
	if err != nil {
		t.Fatal(err)
	}
	closeAfterTest(t, w)

	nested := filepath.Join(root, "Character", "Mod", "Nested")
	if err := os.MkdirAll(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	path := filepath.Join(nested, "mod.ini")
	if err := os.WriteFile(path, []byte("test"), 0o600); err != nil {
		t.Fatal(err)
	}
	if event := waitEvent(t, events); !SamePath(event.Path, path) {
		t.Fatalf("event path = %q, want %q", event.Path, path)
	}
}
