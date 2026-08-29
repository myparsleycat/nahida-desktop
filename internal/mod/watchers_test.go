package mod

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"nahida.live/desktop/internal/watcher"
)

func TestWaitForWatchedPathRequiresTwoMatchingSnapshots(t *testing.T) {
	first := watchedPathSnapshot{kind: 0, fileCount: 1, totalSize: 1, latestModifiedAt: 1}
	second := watchedPathSnapshot{kind: 0, fileCount: 1, totalSize: 2, latestModifiedAt: 2}
	snapshots := []watchedPathSnapshot{first, second, second, second}
	calls := 0
	sleeps := 0

	waitForWatchedPath(
		watcher.Event{Path: "mod.ini", Op: watcher.Write},
		func() bool { return true },
		time.Millisecond,
		10,
		func(string) (*watchedPathSnapshot, error) {
			result := snapshots[calls]
			calls++
			return &result, nil
		},
		func(time.Duration) { sleeps++ },
	)

	if calls != 4 || sleeps != 3 {
		t.Fatalf("calls = %d, sleeps = %d", calls, sleeps)
	}
}

func TestWaitForWatchedPathSkipsRemovedPaths(t *testing.T) {
	called := false
	waitForWatchedPath(
		watcher.Event{Path: "removed", Op: watcher.Remove},
		func() bool { return true },
		time.Millisecond,
		10,
		func(string) (*watchedPathSnapshot, error) {
			called = true
			return nil, nil
		},
		func(time.Duration) { called = true },
	)
	if called {
		t.Fatal("remove event should not wait for a path snapshot")
	}
}

func TestGetWatchedPathSnapshotRecursesLikeElectron(t *testing.T) {
	root := t.TempDir()
	subdir := filepath.Join(root, "nested")
	if err := os.Mkdir(subdir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "one.bin"), []byte("123"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subdir, "two.bin"), []byte("4567"), 0o600); err != nil {
		t.Fatal(err)
	}

	snapshot, err := getWatchedPathSnapshot(root)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot == nil || snapshot.kind != fs.ModeDir || snapshot.fileCount != 2 ||
		snapshot.directoryCount != 2 || snapshot.totalSize != 7 || snapshot.latestModifiedAt == 0 {
		t.Fatalf("snapshot = %#v", snapshot)
	}
}

func TestWatchGameKeepsPreviousWatcherWhenGamePathIsMissing(t *testing.T) {
	service, _ := newTestMod(t, testSettings{})
	previous := &managedWatcher{}
	service.gameWatcher = previous

	if err := service.WatchGame(context.Background(), "missing"); err != nil {
		t.Fatal(err)
	}
	if service.gameWatcher != previous {
		t.Fatal("missing game path replaced the existing watcher")
	}
	service.gameWatcher = nil
}
