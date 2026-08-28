package watcher

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWatchFileSkipsIdenticalContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "d3dx_user.ini")
	if err := os.WriteFile(path, []byte("initial"), 0o600); err != nil {
		t.Fatal(err)
	}

	events := make(chan Event, 4)
	w, err := WatchFile(path, FileConfig{
		SettleDelay:     100 * time.Millisecond,
		DistinctContent: true,
	}, func(event Event) {
		events <- event
	})
	if err != nil {
		t.Fatal(err)
	}
	closeAfterTest(t, w)

	if err := os.WriteFile(path, []byte("initial"), 0o600); err != nil {
		t.Fatal(err)
	}
	assertNoEvent(t, events, 350*time.Millisecond)

	if err := os.WriteFile(path, []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	event := waitEvent(t, events)
	if event.Hash == "" {
		t.Fatal("changed event hash is empty")
	}

	if err := os.WriteFile(path, []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	assertNoEvent(t, events, 350*time.Millisecond)
}

func TestWatchTreeAddsNewDirectoriesWithinDepth(t *testing.T) {
	root := t.TempDir()
	events := make(chan Event, 4)
	w, err := WatchTree([]string{root}, TreeConfig{
		Depth: 1,
		Ops:   Create | Write,
		Filter: func(event Event) bool {
			return filepath.Ext(event.Path) == ".txt"
		},
	}, func(event Event) {
		events <- event
	})
	if err != nil {
		t.Fatal(err)
	}
	closeAfterTest(t, w)

	subdir := filepath.Join(root, "new")
	if err := os.Mkdir(subdir, 0o755); err != nil {
		t.Fatal(err)
	}
	time.Sleep(150 * time.Millisecond)
	if err := os.WriteFile(filepath.Join(subdir, "seen.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if event := waitEvent(t, events); filepath.Base(event.Path) != "seen.txt" {
		t.Fatalf("event path = %q", event.Path)
	}
}

func TestWatchTreeDoesNotAddDirectoriesBeyondDepth(t *testing.T) {
	root := t.TempDir()
	events := make(chan Event, 4)
	w, err := WatchTree([]string{root}, TreeConfig{
		Depth: 0,
		Filter: func(event Event) bool {
			return filepath.Ext(event.Path) == ".txt"
		},
	}, func(event Event) {
		events <- event
	})
	if err != nil {
		t.Fatal(err)
	}
	closeAfterTest(t, w)

	subdir := filepath.Join(root, "new")
	if err := os.Mkdir(subdir, 0o755); err != nil {
		t.Fatal(err)
	}
	time.Sleep(150 * time.Millisecond)
	if err := os.WriteFile(filepath.Join(subdir, "ignored.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	assertNoEvent(t, events, 350*time.Millisecond)
}

func TestWatchTreeDebouncesBurst(t *testing.T) {
	root := t.TempDir()
	events := make(chan Event, 4)
	w, err := WatchTree([]string{root}, TreeConfig{
		Depth:    0,
		Debounce: 120 * time.Millisecond,
	}, func(event Event) {
		events <- event
	})
	if err != nil {
		t.Fatal(err)
	}
	closeAfterTest(t, w)

	path := filepath.Join(root, "burst.ini")
	for _, content := range []string{"1", "2", "3"} {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	waitEvent(t, events)
	assertNoEvent(t, events, 300*time.Millisecond)
}

func TestSamePath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file.ini")
	if !SamePath(path, filepath.Clean(path)) {
		t.Fatal("cleaned paths should be equal")
	}
}

func waitEvent(t *testing.T, events <-chan Event) Event {
	t.Helper()
	select {
	case event := <-events:
		return event
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for watcher event")
		return Event{}
	}
}

func assertNoEvent(t *testing.T, events <-chan Event, duration time.Duration) {
	t.Helper()
	select {
	case event := <-events:
		t.Fatalf("unexpected watcher event: %#v", event)
	case <-time.After(duration):
	}
}

func closeAfterTest(t *testing.T, w *Watcher) {
	t.Helper()
	t.Cleanup(func() {
		if err := w.Close(); err != nil {
			t.Errorf("close watcher: %v", err)
		}
	})
}
