package mod

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"nahida.live/desktop/internal/setting"
	"nahida.live/desktop/internal/xxmi"
)

func TestSuspendImporterWatchersFollowsMovedUserDataAndRollback(t *testing.T) {
	for _, moved := range []bool{false, true} {
		name := "rollback"
		if moved {
			name = "move"
		}
		t.Run(name, func(t *testing.T) {
			m := New()
			t.Cleanup(func() { _ = m.ServiceShutdown() })
			source := filepath.Join(t.TempDir(), "GIMI")
			character := filepath.Join(source, "Mods", "Character")
			if err := os.MkdirAll(character, 0o755); err != nil {
				t.Fatal(err)
			}
			events := make(chan string, 8)
			emit := func(name string, _ ...any) { events <- name }
			var err error
			m.gameWatcher, err = newManagedWatcher(filepath.Join(source, "Mods"), 1, "game", emit)
			if err != nil {
				t.Fatal(err)
			}
			m.characterWatcher, err = newManagedWatcher(character, 1, "character", emit)
			if err != nil {
				t.Fatal(err)
			}
			resume, err := m.SuspendImporterWatchers(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = resume(nil) }()
			target := filepath.Join(t.TempDir(), "GIMI")
			if err := os.MkdirAll(target, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(filepath.Join(source, "Mods"), filepath.Join(target, "Mods")); err != nil {
				t.Fatal(err)
			}
			backup := filepath.Join(filepath.Dir(target), ".nahida-gimi-install.backup")
			if err := os.Rename(target, backup); err != nil {
				t.Fatalf("rename with watchers suspended: %v", err)
			}
			if err := os.Rename(backup, target); err != nil {
				t.Fatal(err)
			}
			var moves []xxmi.ImportedImporter
			final := source
			if moved {
				moves = []xxmi.ImportedImporter{{Key: "GIMI", PreviousFolder: source, ImporterFolder: target}}
				final = target
			} else if err := os.Rename(filepath.Join(target, "Mods"), filepath.Join(source, "Mods")); err != nil {
				t.Fatal(err)
			}
			if err := resume(moves); err != nil {
				t.Fatal(err)
			}
			if m.gameWatcher.root != filepath.Join(final, "Mods") ||
				m.characterWatcher.root != filepath.Join(final, "Mods", "Character") {
				t.Fatal("watchers did not follow the final user data paths")
			}
			if err := os.WriteFile(
				filepath.Join(final, "Mods", "Character", "new.ini"),
				[]byte("mod"),
				0o600,
			); err != nil {
				t.Fatal(err)
			}
			seen := map[string]bool{}
			deadline := time.NewTimer(5 * time.Second)
			defer deadline.Stop()
			for len(seen) < 2 {
				select {
				case event := <-events:
					seen[event] = true
				case <-deadline.C:
					t.Fatalf("resumed watcher events = %v", seen)
				}
			}
		})
	}
}

func TestSuspendImporterWatchersDrainsCompressionAndBlocksNewWork(t *testing.T) {
	m := New()
	t.Cleanup(func() { _ = m.ServiceShutdown() })
	c := m.compression
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	c.cancel, c.done = cancel, make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	workerDone := c.done
	go func() {
		<-ctx.Done()
		<-release
		close(workerDone)
	}()
	type result struct {
		resume func([]xxmi.ImportedImporter) error
		err    error
	}
	paused := make(chan result, 1)
	go func() {
		resume, err := m.SuspendImporterWatchers(t.Context())
		paused <- result{resume: resume, err: err}
	}()
	select {
	case <-ctx.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("compression was not canceled")
	}
	select {
	case <-paused:
		t.Fatal("maintenance began before the worker finished")
	default:
	}
	releaseOnce.Do(func() { close(release) })
	var pause result
	select {
	case pause = <-paused:
	case <-time.After(5 * time.Second):
		t.Fatal("maintenance did not wait for the worker")
	}
	if pause.err != nil {
		t.Fatal(pause.err)
	}
	defer func() { _ = pause.resume(nil) }()
	if c.requestMu.TryLock() {
		c.requestMu.Unlock()
		t.Fatal("compression requests are allowed during maintenance")
	}
	if c.opMu.TryLock() {
		c.opMu.Unlock()
		t.Fatal("synchronous compression is allowed during maintenance")
	}
	if err := pause.resume(nil); err != nil {
		t.Fatal(err)
	}
	if !c.requestMu.TryLock() {
		t.Fatal("compression requests remain blocked after maintenance")
	}
	c.requestMu.Unlock()
}

func TestSuspendImporterWatchersRestartsCompressionOnMovedRoot(t *testing.T) {
	base := t.TempDir()
	source := filepath.Join(base, "external", "GIMI")
	target := filepath.Join(base, "builtin", "GIMI")
	folder := filepath.Join(source, "Mods", "DISABLED Test")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	settings, err := setting.Open(t.Context(), filepath.Join(base, "settings.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = settings.Close() })
	if err := settings.SetCompressionConfig(t.Context(), "zstd", 1); err != nil {
		t.Fatal(err)
	}
	if err := settings.SetCompressionEnabled(t.Context(), true); err != nil {
		t.Fatal(err)
	}
	importers := compressionImporterSource{{Key: "GIMI", ImporterFolder: source}}
	m := NewWithOptions(Options{Settings: settings, XXMI: importers})
	m.UseClient(settings.Client())
	t.Cleanup(func() { _ = m.ServiceShutdown() })
	if err := m.StartCompression(t.Context()); err != nil {
		t.Fatal(err)
	}
	waitForCompression(t, m.compression)
	resume, err := m.SuspendImporterWatchers(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resume(nil) }()
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(source, "Mods"), filepath.Join(target, "Mods")); err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(filepath.Dir(target), ".nahida-gimi-install.backup")
	if err := os.Rename(target, backup); err != nil {
		t.Fatalf("compression watcher still holds the moved tree: %v", err)
	}
	if err := os.Rename(backup, target); err != nil {
		t.Fatal(err)
	}
	importers[0].ImporterFolder = target
	if err := resume(
		[]xxmi.ImportedImporter{{Key: "GIMI", PreviousFolder: source, ImporterFolder: target}},
	); err != nil {
		t.Fatal(err)
	}
	waitForCompression(t, m.compression)
	path := filepath.Join(target, "Mods", "DISABLED Test", "new.bin")
	if err := os.WriteFile(path, make([]byte, 2*1024*1024), 0o600); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path + managedZstdExtension); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("compression watcher did not process a new file under the moved root")
}
