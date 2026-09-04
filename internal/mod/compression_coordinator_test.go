package mod

import (
	"bytes"
	"context"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"nahida.live/desktop/internal/setting"
)

type blockingCompressionSettings struct {
	*setting.Setting
	started chan struct{}
	release chan struct{}
}

func (s *blockingCompressionSettings) SetCompressionConfig(ctx context.Context, method string, threshold int) error {
	close(s.started)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-s.release:
	}
	return s.Setting.SetCompressionConfig(ctx, method, threshold)
}

func TestCompressionTransitionCommitsOnlyAfterReconciliation(t *testing.T) {
	ctx := context.Background()
	settings, err := setting.Open(ctx, filepath.Join(t.TempDir(), "compression.db"))
	if err != nil {
		t.Fatal(err)
	}
	m := NewWithOptions(Options{Settings: settings})
	m.UseClient(settings.Client())
	t.Cleanup(func() {
		_ = m.ServiceShutdown()
		_ = settings.Close()
	})

	if err := m.StartCompression(ctx); err != nil {
		t.Fatal(err)
	}
	waitForCompression(t, m.compression)
	state, err := m.GetCompressionState(ctx)
	if err != nil || state.Method != "xpress4k" || state.Enabled || state.Status != "idle" {
		t.Fatalf("initial state = %+v, err=%v", state, err)
	}

	state, err = m.SetCompressionConfig(ctx, CompressionConfig{Method: "zstd", ThresholdMiB: 4})
	if err != nil || state.Method != "zstd" || state.ThresholdMiB != 4 {
		t.Fatalf("configured state = %+v, err=%v", state, err)
	}
	if _, err := m.SetCompressionEnabled(ctx, true); err != nil {
		t.Fatal(err)
	}
	waitForCompression(t, m.compression)
	state, err = m.GetCompressionState(ctx)
	if err != nil || !state.Enabled || state.Status != "idle" || state.TargetEnabled != nil {
		t.Fatalf("enabled state = %+v, err=%v", state, err)
	}
	persisted, err := settings.GetCompressionEnabled(ctx)
	if err != nil || !persisted {
		t.Fatalf("persisted enabled = %v, err=%v", persisted, err)
	}
	transition, err := settings.Client().AppState.GetValue(ctx, compressionTransitionKey)
	if err != nil || transition != nil {
		t.Fatalf("transition = %v, err=%v", transition, err)
	}
	if _, err := m.SetCompressionConfig(ctx, CompressionConfig{Method: "xpress4k", ThresholdMiB: 1}); err == nil {
		t.Fatal("configuration must remain locked while compression is enabled")
	}

	if _, err := m.SetCompressionEnabled(ctx, false); err != nil {
		t.Fatal(err)
	}
	waitForCompression(t, m.compression)
	state, err = m.GetCompressionState(ctx)
	if err != nil || state.Enabled || state.Status != "idle" || !state.CanConfigure {
		t.Fatalf("disabled state = %+v, err=%v", state, err)
	}
}

func TestCompressionConfigRejectsConcurrentToggle(t *testing.T) {
	ctx := context.Background()
	settings, err := setting.Open(ctx, filepath.Join(t.TempDir(), "compression.db"))
	if err != nil {
		t.Fatal(err)
	}
	blocking := &blockingCompressionSettings{
		Setting: settings, started: make(chan struct{}), release: make(chan struct{}),
	}
	m := NewWithOptions(Options{Settings: blocking})
	m.UseClient(settings.Client())
	t.Cleanup(func() { _ = settings.Close() })
	if err := m.compression.loadState(ctx); err != nil {
		t.Fatal(err)
	}
	m.compression.mu.Lock()
	m.compression.state.Status = "idle"
	m.compression.deriveCapabilitiesLocked()
	m.compression.mu.Unlock()

	configDone := make(chan error, 1)
	go func() {
		_, err := m.SetCompressionConfig(ctx, CompressionConfig{Method: "zstd", ThresholdMiB: 4})
		configDone <- err
	}()
	<-blocking.started
	if _, err := m.SetCompressionEnabled(ctx, true); err == nil || err.Error() != "COMPRESSION_TOGGLE_LOCKED" {
		t.Fatalf("concurrent toggle error = %v", err)
	}
	close(blocking.release)
	if err := <-configDone; err != nil {
		t.Fatal(err)
	}
	method, err := settings.GetCompressionMethod(ctx)
	if err != nil || method != "zstd" {
		t.Fatalf("method = %q, err=%v", method, err)
	}
	transition, err := settings.Client().AppState.GetValue(ctx, compressionTransitionKey)
	if err != nil || transition != nil {
		t.Fatalf("transition = %v, err=%v", transition, err)
	}
}

func TestCompressionScopesMergeAndFullWorkDominates(t *testing.T) {
	scopes := map[string]struct{}{}
	root := filepath.Join(`C:\`, "Mods")
	first := filepath.Join(root, "First")
	addCompressionScope(scopes, filepath.Join(first, "nested"))
	addCompressionScope(scopes, first)
	addCompressionScope(scopes, filepath.Join(root, "Second"))
	if len(scopes) != 2 {
		t.Fatalf("scopes = %v", scopes)
	}
	if _, ok := scopes[strings.ToLower(first)]; !ok {
		t.Fatalf("parent scope was not retained: %v", scopes)
	}

	c := newCompressionCoordinator(New())
	c.pendingScopes = scopes
	c.pendingFull = true
	work := c.takePendingLocked()
	if !work.full || len(work.scopes) != 2 || c.pendingFull || len(c.pendingScopes) != 0 {
		t.Fatalf("work=%+v pendingFull=%v pendingScopes=%v", work, c.pendingFull, c.pendingScopes)
	}
}

func TestCompressionSelfChangeExpires(t *testing.T) {
	c := newCompressionCoordinator(New())
	path := filepath.Join(t.TempDir(), "payload.bin")
	c.markSelfChanges(path)
	if !c.isSelfChange(path) {
		t.Fatal("fresh self change was not filtered")
	}
	c.selfChangeMu.Lock()
	c.selfChanges[strings.ToLower(filepath.Clean(path))] = time.Now().Add(-time.Second)
	c.selfChangeMu.Unlock()
	if c.isSelfChange(path) {
		t.Fatal("expired self change remained filtered")
	}
}

func TestZstdValidationTempsDoNotRestartWatcher(t *testing.T) {
	ctx := context.Background()
	base := t.TempDir()
	importer := filepath.Join(base, "Importer")
	folder := filepath.Join(importer, "Mods", "DISABLED Watched")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(folder, "payload.bin")
	if err := os.WriteFile(path, bytes.Repeat([]byte("watcher-zstd"), 16*1024), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := compressZstdFile(ctx, folder, path, ignoreCompressionMutations); err != nil {
		t.Fatal(err)
	}
	settings, err := setting.Open(ctx, filepath.Join(base, "compression.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := settings.SetCompressionConfig(ctx, "zstd", 1); err != nil {
		t.Fatal(err)
	}
	if err := settings.SetCompressionEnabled(ctx, true); err != nil {
		t.Fatal(err)
	}
	m := NewWithOptions(Options{
		Settings: settings,
		XXMI:     compressionImporterSource{{Key: "A", ImporterFolder: importer}},
	})
	m.UseClient(settings.Client())
	t.Cleanup(func() {
		_ = m.ServiceShutdown()
		_ = settings.Close()
	})
	if err := m.StartCompression(ctx); err != nil {
		t.Fatal(err)
	}
	waitForCompression(t, m.compression)
	time.Sleep(compressionWatchDebounce + 300*time.Millisecond)
	m.compression.mu.Lock()
	running := m.compression.running
	pending := m.compression.pendingFull || len(m.compression.pendingScopes) > 0 || m.compression.debounceTimer != nil
	m.compression.mu.Unlock()
	if running || pending {
		t.Fatalf("validation restarted watcher: running=%v pending=%v", running, pending)
	}
}

func TestCompressionWatcherMergesMultipleModScopes(t *testing.T) {
	ctx := context.Background()
	base := t.TempDir()
	importer := filepath.Join(base, "Importer")
	modsRoot := filepath.Join(importer, "Mods")
	folders := []string{
		filepath.Join(modsRoot, "DISABLED First"),
		filepath.Join(modsRoot, "DISABLED Second"),
	}
	for _, folder := range folders {
		if err := os.MkdirAll(folder, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	settings, err := setting.Open(ctx, filepath.Join(base, "compression.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := settings.SetCompressionConfig(ctx, "zstd", 1); err != nil {
		t.Fatal(err)
	}
	if err := settings.SetCompressionEnabled(ctx, true); err != nil {
		t.Fatal(err)
	}
	m := NewWithOptions(Options{
		Settings: settings,
		XXMI:     compressionImporterSource{{Key: "A", ImporterFolder: importer}},
	})
	m.UseClient(settings.Client())
	t.Cleanup(func() {
		_ = m.ServiceShutdown()
		_ = settings.Close()
	})
	if err := m.StartCompression(ctx); err != nil {
		t.Fatal(err)
	}
	waitForCompression(t, m.compression)

	for _, folder := range folders {
		if err := os.WriteFile(filepath.Join(folder, "payload.bin"), bytes.Repeat([]byte("scope"), 256*1024), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		complete := true
		for _, folder := range folders {
			if _, err := os.Stat(filepath.Join(folder, "payload.bin.zst")); err != nil {
				complete = false
				break
			}
		}
		if complete {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("watcher did not process every changed mod scope")
}

func waitForCompression(t *testing.T, coordinator *compressionCoordinator) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		coordinator.mu.Lock()
		running, done := coordinator.running, coordinator.done
		coordinator.mu.Unlock()
		if !running {
			return
		}
		select {
		case <-done:
		case <-time.After(10 * time.Millisecond):
		}
	}
	coordinator.mu.Lock()
	state, pendingFull, pendingScopes := coordinator.state, coordinator.pendingFull, maps.Clone(coordinator.pendingScopes)
	coordinator.mu.Unlock()
	t.Fatalf("compression worker did not stop: state=%+v pendingFull=%v pendingScopes=%v", state, pendingFull, pendingScopes)
}
