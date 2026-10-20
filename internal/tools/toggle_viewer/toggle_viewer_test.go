package toggleviewer

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"nahida.live/desktop/internal/db"
	"nahida.live/desktop/internal/platform"
	"nahida.live/desktop/internal/watcher"
	"nahida.live/desktop/internal/xxmi"
)

type fakeSettings struct {
	mu           sync.Mutex
	autoGenerate bool
	hotkey       string
	onHotkey     func()
}

func (f *fakeSettings) GetToggleViewerAutoGenerate(context.Context) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.autoGenerate, nil
}

func (f *fakeSettings) SetToggleViewerAutoGenerate(_ context.Context, enabled bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.autoGenerate = enabled
	return nil
}

func (f *fakeSettings) GetToggleViewerHotkey(context.Context) (string, error) {
	f.mu.Lock()
	hotkey, hook := f.hotkey, f.onHotkey
	f.mu.Unlock()
	if hook != nil {
		hook()
	}
	return hotkey, nil
}

func (f *fakeSettings) setHotkey(hotkey string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.hotkey = hotkey
}

type fakeImporters []xxmi.EnabledImporter

func (f fakeImporters) GetEnabledImporters(context.Context) ([]xxmi.EnabledImporter, error) {
	return f, nil
}

const cycleModINI = "[KeySwap]\ntype = cycle\nkey = ctrl H\n$swapvar = 0, 1\n\n" +
	"[TextureOverrideBodyPosition]\nhash = 1234abcd\n"

func TestBatchGenerateHotkeyAndDelete(t *testing.T) {
	ctx := context.Background()
	importer := t.TempDir()
	modRoot := filepath.Join(importer, "Mods", "Character")
	writeTestFile(t, filepath.Join(modRoot, "mod.ini"), cycleModINI)
	settings := &fakeSettings{autoGenerate: true, hotkey: "ctrl H"}
	client := openTestDB(t)
	service := NewWithOptions(Options{
		Settings:  settings,
		Importers: fakeImporters{{Key: "GIMI", ImporterFolder: importer}},
	})
	service.UseClient(client)
	t.Cleanup(func() { _ = service.Shutdown() })

	if err := service.RunBatchGenerate(ctx); err != nil {
		t.Fatal(err)
	}
	finalModRoot := finalPath(t, modRoot)
	toggleINIPath := filepath.Join(finalModRoot, artifactININame)
	toggleTXTPath := filepath.Join(finalModRoot, artifactTXTName)
	generated, err := os.ReadFile(toggleINIPath)
	if err != nil || !strings.Contains(string(generated), "hash = 1234abcd") ||
		!strings.Contains(string(generated), "key = ctrl H") {
		t.Fatalf("generated ini = %q, %v", generated, err)
	}
	if _, err := os.Stat(toggleTXTPath); err != nil {
		t.Fatal(err)
	}
	records, err := client.ToggleViewerArtifacts.List(ctx)
	if err != nil || len(records) != 1 ||
		!platform.SamePathFold(records[0].TargetIniPath, filepath.Join(finalModRoot, "mod.ini")) {
		t.Fatalf("records = %#v, %v", records, err)
	}

	settings.setHotkey("alt J")
	if err := service.ApplyHotkey(ctx, "alt J"); err != nil {
		t.Fatal(err)
	}
	updated, err := os.ReadFile(toggleINIPath)
	if err != nil || !strings.Contains(string(updated), "key = alt J") {
		t.Fatalf("updated ini = %q, %v", updated, err)
	}

	if err := service.RunBatchDelete(ctx); err != nil {
		t.Fatal(err)
	}
	if enabled, _ := settings.GetToggleViewerAutoGenerate(ctx); enabled {
		t.Fatal("batch delete left auto generation enabled")
	}
	for _, path := range []string{toggleINIPath, toggleTXTPath} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("%s remains: %v", path, err)
		}
	}
	records, err = client.ToggleViewerArtifacts.List(ctx)
	if err != nil || len(records) != 0 {
		t.Fatalf("records after delete = %#v, %v", records, err)
	}
}

// Without auto generation nothing rescans after a mod folder is disabled or renamed, so its records
// keep the old location while the artifacts move with the folder.
func TestBatchDeleteRemovesArtifactsOfMovedFolder(t *testing.T) {
	ctx := context.Background()
	service, client, moved := generateAndMoveMod(t)
	userTXT := filepath.Join(moved, artifactTXTName)
	writeTestFile(t, userTXT, "user notes")
	// The records still point at the old location, where the user may have created a file since.
	userAtOldPath := filepath.Join(filepath.Dir(moved), "Character", artifactTXTName)
	writeTestFile(t, userAtOldPath, "user notes")

	if err := service.RunBatchDelete(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(moved, artifactININame)); !os.IsNotExist(err) {
		t.Fatalf("moved artifact remains: %v", err)
	}
	for _, path := range []string{userTXT, userAtOldPath} {
		if content, err := os.ReadFile(path); err != nil || string(content) != "user notes" {
			t.Fatalf("user file %s = %q, %v", path, content, err)
		}
	}
	if records, err := client.ToggleViewerArtifacts.List(ctx); err != nil || len(records) != 0 {
		t.Fatalf("records = %#v, %v", records, err)
	}
}

func TestApplyHotkeyRewritesArtifactsOfMovedFolder(t *testing.T) {
	ctx := context.Background()
	service, client, moved := generateAndMoveMod(t)

	if err := service.ApplyHotkey(ctx, "alt J"); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(moved, artifactININame))
	if err != nil || !strings.Contains(string(content), "key = alt J") {
		t.Fatalf("moved ini = %q, %v", content, err)
	}

	// The rewritten artifact must still be recognised as generated.
	if err := service.RunBatchDelete(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(moved, artifactININame)); !os.IsNotExist(err) {
		t.Fatalf("moved artifact remains: %v", err)
	}
	if records, err := client.ToggleViewerArtifacts.List(ctx); err != nil || len(records) != 0 {
		t.Fatalf("records = %#v, %v", records, err)
	}
}

// Once the moved folder no longer qualifies its artifacts are only identified through the stale
// record's hashes, so the record must not be dropped before they are removed.
func TestScanRemovesArtifactsOfMovedFolderBeforeForgettingRecord(t *testing.T) {
	ctx := context.Background()
	service, client, moved := generateAndMoveMod(t)
	if err := os.Remove(filepath.Join(moved, "mod.ini")); err != nil {
		t.Fatal(err)
	}

	if err := service.RunBatchGenerate(ctx); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{artifactININame, artifactTXTName} {
		if _, err := os.Stat(filepath.Join(moved, name)); !os.IsNotExist(err) {
			t.Fatalf("moved %s remains: %v", name, err)
		}
	}
	if records, err := client.ToggleViewerArtifacts.List(ctx); err != nil || len(records) != 0 {
		t.Fatalf("records = %#v, %v", records, err)
	}
}

// A scan reuses an INI's parsed source while its size and modification time are unchanged, so an edit
// that keeps the size must still be picked up through the new modification time.
func TestScanRereadsSourceWithNewModTime(t *testing.T) {
	ctx := context.Background()
	importer := t.TempDir()
	iniPath := filepath.Join(importer, "Mods", "Character", "mod.ini")
	writeTestFile(t, iniPath, cycleModINI)
	modTime := time.Now().Add(-time.Hour)
	if err := os.Chtimes(iniPath, modTime, modTime); err != nil {
		t.Fatal(err)
	}
	service := NewWithOptions(Options{
		Settings:  &fakeSettings{},
		Importers: fakeImporters{{Key: "GIMI", ImporterFolder: importer}},
	})
	service.UseClient(openTestDB(t))
	t.Cleanup(func() { _ = service.Shutdown() })
	if err := service.RunBatchGenerate(ctx); err != nil {
		t.Fatal(err)
	}

	writeTestFile(t, iniPath, strings.Replace(cycleModINI, "ctrl H", "ctrl J", 1))
	modTime = modTime.Add(time.Minute)
	if err := os.Chtimes(iniPath, modTime, modTime); err != nil {
		t.Fatal(err)
	}
	if _, err := service.scanAll(ctx); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(filepath.Dir(iniPath), artifactTXTName))
	if err != nil || !strings.Contains(string(content), "Ctrl + J") {
		t.Fatalf("txt = %q, %v", content, err)
	}
}

func TestRelevantEvent(t *testing.T) {
	mods := filepath.Join(t.TempDir(), "Mods")
	character := filepath.Join(mods, "Character")
	writeTestFile(t, filepath.Join(character, "mod.ini"), cycleModINI)
	writeTestFile(t, filepath.Join(character, "body.dds"), "texture")
	writeTestFile(t, filepath.Join(mods, "DISABLED Old", "mod.ini"), cycleModINI)
	roots := []string{mods}
	known := &Service{sourceDirs: []string{character, filepath.Join(mods, "Removed")}}
	unknown := &Service{}

	for _, tc := range []struct {
		name    string
		service *Service
		path    string
		op      watcher.Op
		want    bool
	}{
		{"source ini write", known, filepath.Join(character, "mod.ini"), watcher.Write, true},
		{"asset write", known, filepath.Join(character, "body.dds"), watcher.Write, false},
		{"artifact write", known, filepath.Join(character, artifactININame), watcher.Write, false},
		{"disabled folder ini", known, filepath.Join(mods, "DISABLED Old", "mod.ini"), watcher.Write, false},
		{"folder write", known, character, watcher.Write, false},
		{"folder create", known, character, watcher.Create, true},
		{"root overflow", known, mods, watcher.Write, true},
		{"removed source folder", known, filepath.Join(mods, "Removed"), watcher.Remove, true},
		{"removed unrelated", known, filepath.Join(mods, "Other"), watcher.Remove, false},
		{"removed before first scan", unknown, filepath.Join(mods, "Other"), watcher.Remove, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.service.relevantEvent(roots, watcher.Event{Path: tc.path, Op: tc.op}); got != tc.want {
				t.Fatalf("relevantEvent = %v, want %v", got, tc.want)
			}
		})
	}
}

// 3DMigoto does not load a disabled folder, so its artifacts are cleaned up instead of regenerated.
func TestScanSkipsDisabledFolder(t *testing.T) {
	ctx := context.Background()
	service, client, moved := generateAndMoveMod(t)

	if err := service.RunBatchGenerate(ctx); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{artifactININame, artifactTXTName} {
		if _, err := os.Stat(filepath.Join(moved, name)); !os.IsNotExist(err) {
			t.Fatalf("%s remains in disabled folder: %v", name, err)
		}
	}
	if records, err := client.ToggleViewerArtifacts.List(ctx); err != nil || len(records) != 0 {
		t.Fatalf("records = %#v, %v", records, err)
	}
}

// generateAndMoveMod batch generates one mod with auto generation off and then disables its folder,
// returning the moved folder.
func generateAndMoveMod(t *testing.T) (*Service, *db.Client, string) {
	t.Helper()
	importer := t.TempDir()
	modRoot := filepath.Join(importer, "Mods", "Character")
	writeTestFile(t, filepath.Join(modRoot, "mod.ini"), cycleModINI)
	client := openTestDB(t)
	service := NewWithOptions(Options{
		Settings:  &fakeSettings{},
		Importers: fakeImporters{{Key: "GIMI", ImporterFolder: importer}},
	})
	service.UseClient(client)
	t.Cleanup(func() { _ = service.Shutdown() })
	if err := service.RunBatchGenerate(context.Background()); err != nil {
		t.Fatal(err)
	}

	moved := filepath.Join(importer, "Mods", "DISABLED Character")
	if err := os.Rename(modRoot, moved); err != nil {
		t.Fatal(err)
	}
	return service, client, moved
}

// A hotkey stored while a scan was reading sources must not be overwritten by the scan's artifacts,
// because ApplyHotkey may already have finished and generated files are not watched.
func TestPersistAppliesHotkeyStoredDuringScan(t *testing.T) {
	ctx := context.Background()
	modsPath := filepath.Join(t.TempDir(), "Mods")
	writeTestFile(t, filepath.Join(modsPath, "Character", "mod.ini"), cycleModINI)
	settings := &fakeSettings{hotkey: "ctrl H"}
	client := openTestDB(t)
	service := NewWithOptions(Options{Settings: settings})
	service.UseClient(client)

	artifacts, _, err := service.scanRoot(ctx, modsPath, "ctrl H", map[string]cachedSource{})
	if err != nil || len(artifacts) != 1 {
		t.Fatalf("artifacts = %#v, %v", artifacts, err)
	}
	settings.setHotkey("alt J")
	if _, err := service.persistArtifacts(ctx, artifacts, "ctrl H"); err != nil {
		t.Fatal(err)
	}

	content, err := os.ReadFile(artifacts[0].toggleINIPath)
	if err != nil || !strings.Contains(string(content), "key = alt J") {
		t.Fatalf("persisted ini = %q, %v", content, err)
	}
	records, err := client.ToggleViewerArtifacts.List(ctx)
	if err != nil || len(records) != 1 || records[0].ToggleIniHash != toggleContentSHA(string(content)) {
		t.Fatalf("records = %#v, %v", records, err)
	}
}

func TestScanRemovesStaleManagedArtifacts(t *testing.T) {
	ctx := context.Background()
	client := openTestDB(t)
	root := t.TempDir()
	targetINI := filepath.Join(root, "removed.ini")
	toggleINI := filepath.Join(root, artifactININame)
	toggleTXT := filepath.Join(root, artifactTXTName)
	writeTestFile(t, toggleINI, "managed")
	writeTestFile(t, toggleTXT, "managed")
	if err := client.ToggleViewerArtifacts.Upsert(ctx, db.ToggleViewerArtifactRow{
		ID: "stale", TargetIniPath: targetINI, ToggleTxtPath: toggleTXT, ToggleIniPath: toggleINI,
		ToggleTxtHash: toggleContentSHA("managed"), ToggleIniHash: toggleContentSHA("managed"), UpdatedAt: "now",
	}); err != nil {
		t.Fatal(err)
	}
	service := New()
	service.UseClient(client)

	if _, err := service.deleteStaleRecords(ctx, []string{finalPath(t, root)}, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(toggleINI); !os.IsNotExist(err) {
		t.Fatalf("managed ini remains: %v", err)
	}
	if records, err := client.ToggleViewerArtifacts.List(ctx); err != nil || len(records) != 0 {
		t.Fatalf("records = %#v, %v", records, err)
	}
}

func TestScanKeepsArtifactsOfHiddenImporter(t *testing.T) {
	ctx := context.Background()
	visible, hidden := t.TempDir(), t.TempDir()
	writeTestFile(t, filepath.Join(visible, "Mods", "Character", "mod.ini"), cycleModINI)
	hiddenModRoot := filepath.Join(hidden, "Mods", "Character")
	writeTestFile(t, filepath.Join(hiddenModRoot, "mod.ini"), cycleModINI)
	client := openTestDB(t)
	both := NewWithOptions(Options{
		Settings: &fakeSettings{},
		Importers: fakeImporters{
			{Key: "GIMI", ImporterFolder: visible},
			{Key: "SRMI", ImporterFolder: hidden},
		},
	})
	both.UseClient(client)
	t.Cleanup(func() { _ = both.Shutdown() })
	if err := both.RunBatchGenerate(ctx); err != nil {
		t.Fatal(err)
	}

	visibleOnly := NewWithOptions(Options{
		Settings:  &fakeSettings{},
		Importers: fakeImporters{{Key: "GIMI", ImporterFolder: visible}},
	})
	visibleOnly.UseClient(client)
	t.Cleanup(func() { _ = visibleOnly.Shutdown() })
	if _, err := visibleOnly.scanAll(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(hiddenModRoot, artifactININame)); err != nil {
		t.Fatalf("hidden importer artifact: %v", err)
	}
	if records, err := client.ToggleViewerArtifacts.List(ctx); err != nil || len(records) != 2 {
		t.Fatalf("records = %#v, %v", records, err)
	}
}

func TestScanKeepsArtifactsAfterSourceRename(t *testing.T) {
	ctx := context.Background()
	importer := t.TempDir()
	modRoot := filepath.Join(importer, "Mods", "Character")
	writeTestFile(t, filepath.Join(modRoot, "mod.ini"), cycleModINI)
	client := openTestDB(t)
	service := NewWithOptions(Options{
		Settings:  &fakeSettings{},
		Importers: fakeImporters{{Key: "GIMI", ImporterFolder: importer}},
	})
	service.UseClient(client)
	t.Cleanup(func() { _ = service.Shutdown() })
	if err := service.RunBatchGenerate(ctx); err != nil {
		t.Fatal(err)
	}

	if err := os.Rename(filepath.Join(modRoot, "mod.ini"), filepath.Join(modRoot, "renamed.ini")); err != nil {
		t.Fatal(err)
	}
	if err := service.RunBatchGenerate(ctx); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{artifactININame, artifactTXTName} {
		if _, err := os.Stat(filepath.Join(modRoot, name)); err != nil {
			t.Fatalf("%s after rename: %v", name, err)
		}
	}
	records, err := client.ToggleViewerArtifacts.List(ctx)
	if err != nil || len(records) != 1 || filepath.Base(records[0].TargetIniPath) != "renamed.ini" {
		t.Fatalf("records = %#v, %v", records, err)
	}
}

// A change made after the batch read its sources is only covered by the rescan the watcher queued,
// which the batch must not cancel when it finishes.
func TestBatchGenerateKeepsRescanForChangeDuringBatch(t *testing.T) {
	ctx := context.Background()
	importer := t.TempDir()
	writeTestFile(t, filepath.Join(importer, "Mods", "First", "mod.ini"), cycleModINI)
	settings := &fakeSettings{autoGenerate: true}
	service := NewWithOptions(Options{
		Settings:  settings,
		Importers: fakeImporters{{Key: "GIMI", ImporterFolder: importer}},
	})
	service.UseClient(openTestDB(t))
	t.Cleanup(func() { _ = service.Shutdown() })
	if err := service.StartWatcher(ctx); err != nil {
		t.Fatal(err)
	}
	service.tasks.Wait()

	// The second hotkey lookup of the batch comes from persistArtifacts, after every source was read.
	var calls atomic.Int32
	sourcesRead, release := make(chan struct{}), make(chan struct{})
	settings.mu.Lock()
	settings.onHotkey = func() {
		if calls.Add(1) == 2 {
			close(sourcesRead)
			<-release
		}
	}
	settings.mu.Unlock()
	done := make(chan error, 1)
	go func() { done <- service.RunBatchGenerate(ctx) }()
	<-sourcesRead

	second := filepath.Join(importer, "Mods", "Second")
	writeTestFile(t, filepath.Join(second, "mod.ini"), cycleModINI)
	waitFor(t, "queued rescan", func() bool {
		service.mu.Lock()
		defer service.mu.Unlock()
		return service.pending
	})
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	waitFor(t, "artifact of the folder added during the batch", func() bool {
		_, err := os.Stat(filepath.Join(second, artifactININame))
		return err == nil
	})
}

func TestSuspendImporterWatcherReleasesImporterFolder(t *testing.T) {
	ctx := context.Background()
	importer := filepath.Join(t.TempDir(), "GIMI")
	writeTestFile(t, filepath.Join(importer, "Mods", "Character", "mod.ini"), cycleModINI)
	service := NewWithOptions(Options{
		Settings:  &fakeSettings{autoGenerate: true},
		Importers: fakeImporters{{Key: "GIMI", ImporterFolder: importer}},
	})
	service.UseClient(openTestDB(t))
	t.Cleanup(func() { _ = service.Shutdown() })
	if err := service.StartWatcher(ctx); err != nil {
		t.Fatal(err)
	}

	resume, err := service.SuspendImporterWatcher(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.RunBatchGenerate(ctx); err == nil {
		t.Fatal("batch generate ran during importer maintenance")
	}
	// A watch handle below the importer blocks renaming the importer folder itself.
	moved := importer + ".moved"
	if err := os.Rename(importer, moved); err != nil {
		t.Fatalf("rename importer folder while suspended: %v", err)
	}
	if err := os.Rename(moved, importer); err != nil {
		t.Fatal(err)
	}
	if err := resume(); err != nil {
		t.Fatal(err)
	}

	service.mu.Lock()
	watching := service.watcher != nil
	service.mu.Unlock()
	if !watching {
		t.Fatal("resume did not restart the watcher")
	}
}

func openTestDB(t *testing.T) *db.Client {
	t.Helper()
	client, err := db.New(filepath.Join(t.TempDir(), "tools.db"))
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	if err := client.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	return client
}

func waitFor(t *testing.T, what string, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !done() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// finalPath expands runner aliases such as RUNNER~1, which the service resolves away.
func finalPath(t *testing.T, path string) string {
	t.Helper()
	resolved, err := platform.FinalPath(path)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}
