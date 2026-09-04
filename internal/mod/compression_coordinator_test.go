package mod

import (
	"bytes"
	"context"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/setting"
	"nahida.live/desktop/internal/xxmi"
)

type blockingCompressionSettings struct {
	*setting.Setting
	started chan struct{}
	release chan struct{}
}

type failingCompressionSettings struct {
	*setting.Setting
	err error
}

type cancelableCompressionImporterSource struct {
	mu       sync.Mutex
	calls    int
	started  chan struct{}
	canceled chan struct{}
}

func (s *cancelableCompressionImporterSource) GetEnabledImporters(ctx context.Context) ([]xxmi.EnabledImporter, error) {
	s.mu.Lock()
	s.calls++
	first := s.calls == 1
	s.mu.Unlock()
	if !first {
		return nil, nil
	}
	close(s.started)
	<-ctx.Done()
	close(s.canceled)
	return nil, ctx.Err()
}

func (s *failingCompressionSettings) GetCompressionMethod(context.Context) (string, error) {
	return "", s.err
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

func TestCompressionCapabilitiesAllowOnlyInterruptingEnable(t *testing.T) {
	trueValue, falseValue := true, false
	tests := []struct {
		name      string
		status    string
		target    *bool
		canToggle bool
	}{
		{name: "idle", status: "idle", canToggle: true},
		{name: "checking enable", status: "checking", target: &trueValue, canToggle: true},
		{name: "compressing", status: "compressing", target: &trueValue, canToggle: true},
		{name: "checking disable", status: "checking", target: &falseValue},
		{name: "decompressing", status: "decompressing", target: &falseValue},
		{name: "startup checking", status: "checking"},
		{name: "blocked", status: "blocked"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			coordinator := newCompressionCoordinator(New())
			coordinator.state.Status = test.status
			coordinator.state.TargetEnabled = test.target
			coordinator.deriveCapabilitiesLocked()
			if coordinator.state.CanToggle != test.canToggle {
				t.Fatalf("CanToggle = %v, want %v for state %+v", coordinator.state.CanToggle, test.canToggle, coordinator.state)
			}
		})
	}
}

func TestCompressionCheckingUsesCurrentEnabledTarget(t *testing.T) {
	coordinator := newCompressionCoordinator(New())
	coordinator.state.Enabled = true
	coordinator.state.Status = "idle"
	coordinator.state.TargetEnabled = nil

	coordinator.resetCheckingLocked()
	coordinator.deriveCapabilitiesLocked()

	state := coordinator.state
	if state.Status != "checking" || state.TargetEnabled == nil || !*state.TargetEnabled || !state.CanToggle {
		t.Fatalf("checking state = %+v", state)
	}
}

func TestCompressionToggleOffCancelsActiveEnableAndRestores(t *testing.T) {
	ctx := context.Background()
	settings, err := setting.Open(ctx, filepath.Join(t.TempDir(), "compression.db"))
	if err != nil {
		t.Fatal(err)
	}
	importers := &cancelableCompressionImporterSource{
		started: make(chan struct{}), canceled: make(chan struct{}),
	}
	m := NewWithOptions(Options{Settings: settings, XXMI: importers})
	m.UseClient(settings.Client())
	t.Cleanup(func() {
		_ = m.ServiceShutdown()
		_ = settings.Close()
	})
	if err := m.compression.loadState(ctx); err != nil {
		t.Fatal(err)
	}
	m.compression.mu.Lock()
	m.compression.state.Status = "idle"
	m.compression.deriveCapabilitiesLocked()
	m.compression.mu.Unlock()

	if _, err := m.SetCompressionEnabled(ctx, true); err != nil {
		t.Fatal(err)
	}
	select {
	case <-importers.started:
	case <-time.After(2 * time.Second):
		t.Fatal("compression did not start")
	}
	state, err := m.GetCompressionState(ctx)
	if err != nil || state.TargetEnabled == nil || !*state.TargetEnabled || !state.CanToggle {
		t.Fatalf("active compression state = %+v, err=%v", state, err)
	}

	toggleDone := make(chan error, 1)
	go func() {
		_, err := m.SetCompressionEnabled(ctx, false)
		toggleDone <- err
	}()
	select {
	case err := <-toggleDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("toggle off deadlocked while waiting for compression cancellation")
	}
	select {
	case <-importers.canceled:
	default:
		t.Fatal("active compression context was not canceled")
	}

	waitForCompression(t, m.compression)
	state, err = m.GetCompressionState(ctx)
	if err != nil || state.Enabled || state.Status != "idle" || state.TargetEnabled != nil || state.Error != "" {
		t.Fatalf("restored compression state = %+v, err=%v", state, err)
	}
	persisted, err := settings.GetCompressionEnabled(ctx)
	if err != nil || persisted {
		t.Fatalf("persisted enabled = %v, err=%v", persisted, err)
	}
	transition, err := settings.Client().AppState.GetValue(ctx, compressionTransitionKey)
	if err != nil || transition != nil {
		t.Fatalf("transition = %v, err=%v", transition, err)
	}
}

func TestCompressionToggleOffResumesEnableWhenTransitionSaveFails(t *testing.T) {
	ctx := context.Background()
	settings, err := setting.Open(ctx, filepath.Join(t.TempDir(), "compression.db"))
	if err != nil {
		t.Fatal(err)
	}
	importers := &cancelableCompressionImporterSource{
		started: make(chan struct{}), canceled: make(chan struct{}),
	}
	m := NewWithOptions(Options{Settings: settings, XXMI: importers})
	m.UseClient(settings.Client())
	t.Cleanup(func() {
		_ = m.ServiceShutdown()
		_ = settings.Close()
	})
	if err := m.compression.loadState(ctx); err != nil {
		t.Fatal(err)
	}
	m.compression.mu.Lock()
	m.compression.state.Status = "idle"
	m.compression.deriveCapabilitiesLocked()
	m.compression.mu.Unlock()

	if _, err := m.SetCompressionEnabled(ctx, true); err != nil {
		t.Fatal(err)
	}
	select {
	case <-importers.started:
	case <-time.After(2 * time.Second):
		t.Fatal("compression did not start")
	}
	if _, err := settings.Client().SQL().ExecContext(ctx, `CREATE TRIGGER fail_disable_transition
BEFORE UPDATE ON app_state
WHEN NEW.key = 'mod_compression:transition' AND NEW.value LIKE '%"targetEnabled":false%'
BEGIN
    SELECT RAISE(ABORT, 'disable transition failed');
END`); err != nil {
		t.Fatal(err)
	}

	if _, err := m.SetCompressionEnabled(ctx, false); err == nil {
		t.Fatal("toggle off unexpectedly succeeded")
	}
	select {
	case <-importers.canceled:
	default:
		t.Fatal("active compression context was not canceled")
	}
	waitForCompression(t, m.compression)
	state, err := m.GetCompressionState(ctx)
	if err != nil || !state.Enabled || state.Status != "idle" || state.TargetEnabled != nil || state.Error != "" {
		t.Fatalf("resumed compression state = %+v, err=%v", state, err)
	}
	persisted, err := settings.GetCompressionEnabled(ctx)
	if err != nil || !persisted {
		t.Fatalf("persisted enabled = %v, err=%v", persisted, err)
	}
	transition, err := settings.Client().AppState.GetValue(ctx, compressionTransitionKey)
	if err != nil || transition != nil {
		t.Fatalf("transition = %v, err=%v", transition, err)
	}
}

func TestStartCompressionPublishesLoadFailure(t *testing.T) {
	ctx := context.Background()
	settings, err := setting.Open(ctx, filepath.Join(t.TempDir(), "compression.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = settings.Close() })
	wantErr := errors.New("load compression settings")
	var published []CompressionState
	m := NewWithOptions(Options{
		Settings: &failingCompressionSettings{Setting: settings, err: wantErr},
		EventEmit: func(name string, data ...any) {
			if name == compressionEvent {
				published = append(published, data[0].(CompressionState))
			}
		},
	})

	if err := m.StartCompression(ctx); !errors.Is(err, wantErr) {
		t.Fatalf("StartCompression error = %v", err)
	}
	if len(published) != 1 {
		t.Fatalf("published states = %d, want 1", len(published))
	}
	if got := published[0]; got.Status != "error" || got.Error != wantErr.Error() {
		t.Fatalf("published failure state = %+v", got)
	}
}

func TestCompressionProgressThrottlesEventsWithoutDroppingCounters(t *testing.T) {
	var published []CompressionState
	m := NewWithOptions(Options{EventEmit: func(name string, data ...any) {
		if name == compressionEvent {
			published = append(published, data[0].(CompressionState))
		}
	}})
	c := m.compression
	c.resetProgress("compressing", true, "zstd", 4)
	c.progress("first.bin", 10, false)
	c.progress("second.bin", 20, true)

	if len(published) != 1 {
		t.Fatalf("events inside throttle interval = %d, want initial state only", len(published))
	}
	if got := c.snapshot(); got.ProcessedFiles != 2 || got.ProcessedBytes != 30 || got.FailedFiles != 1 || got.CurrentFileName != "second.bin" {
		t.Fatalf("internal progress = %+v", got)
	}
	c.mu.Lock()
	c.lastProgressEmit = time.Now().Add(-compressionProgressEvery)
	c.mu.Unlock()
	c.progress("third.bin", 30, false)
	if len(published) != 2 {
		t.Fatalf("events after throttle interval = %d, want 2", len(published))
	}
	if got := published[1]; got.ProcessedFiles != 3 || got.ProcessedBytes != 60 || got.CurrentFileName != "third.bin" {
		t.Fatalf("published progress = %+v", got)
	}
}

func TestRestoreBeforeEnableLogsTransitionLoadFailure(t *testing.T) {
	ctx := context.Background()
	settings, err := setting.Open(ctx, filepath.Join(t.TempDir(), "compression.db"))
	if err != nil {
		t.Fatal(err)
	}
	client := settings.Client()
	if err := settings.Close(); err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	m := NewWithOptions(Options{Log: infra.NewLogWithOptions(infra.LogOptions{
		Writer: &logs, DisableFile: true,
	})})
	m.UseClient(client)
	folder := filepath.Join(t.TempDir(), "Mod")

	err = m.compression.restoreBeforeEnable(ctx, folder)
	if err == nil {
		t.Fatal("restoreBeforeEnable unexpectedly succeeded")
	}
	output := logs.String()
	for _, want := range []string{
		"Mod:compression", "stage=load-transition-before-enable", `method=xpress4k`,
		`path="`, filepath.Base(folder), "cleanup=pending", err.Error(),
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("log does not contain %q: %s", want, output)
		}
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

func TestMarkSelfChangesReusesExpiryTimer(t *testing.T) {
	coordinator := newCompressionCoordinator(nil)
	coordinator.markSelfChanges(`C:\mods\first.bin`)
	coordinator.selfChangeMu.Lock()
	firstTimer := coordinator.selfChangeTimer
	firstGeneration := coordinator.selfChangeGeneration
	coordinator.selfChangeMu.Unlock()

	coordinator.markSelfChanges(`C:\mods\second.bin`)
	coordinator.selfChangeMu.Lock()
	secondTimer := coordinator.selfChangeTimer
	secondGeneration := coordinator.selfChangeGeneration
	if coordinator.selfChangeTimer != nil {
		coordinator.selfChangeTimer.Stop()
		coordinator.selfChangeTimer = nil
	}
	coordinator.selfChangeMu.Unlock()

	if firstTimer == nil || firstTimer != secondTimer {
		t.Fatal("self-change timer was replaced for a later expiration")
	}
	if firstGeneration != secondGeneration {
		t.Fatalf("timer generation changed from %d to %d", firstGeneration, secondGeneration)
	}
}
