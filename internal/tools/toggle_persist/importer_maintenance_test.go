package togglepersist

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"nahida.live/desktop/internal/db"
	"nahida.live/desktop/internal/xxmi"
)

type persistWatcherSettings struct{}

func (persistWatcherSettings) GetPersistToggles(context.Context) (bool, error) { return true, nil }

func TestSuspendImporterWatcherCancelsOldWritesAndRestartsOnFinalConfig(t *testing.T) {
	ctx := t.Context()
	base := t.TempDir()
	client, err := db.New(filepath.Join(base, "settings.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	if err := client.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	importers := xxmi.New()
	importers.UseClient(client)
	if err := importers.SetLauncherMode(ctx, xxmi.LauncherBuiltin); err != nil {
		t.Fatal(err)
	}
	cfg, err := xxmi.DefaultImporterConfig("GIMI", filepath.Join(base, "old"))
	if err != nil {
		t.Fatal(err)
	}
	cfg.Enabled = true
	if err := os.MkdirAll(cfg.ImporterFolder, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(cfg.ImporterFolder, "d3dx_user.ini"),
		[]byte("[Constants]\n$old = 1\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	if err := importers.SaveImporterConfig(ctx, "GIMI", cfg); err != nil {
		t.Fatal(err)
	}
	service := NewWithOptions(Options{Settings: persistWatcherSettings{}, XXMI: importers})
	t.Cleanup(func() { _ = service.Shutdown() })
	service.persist.useFakeClock()
	if err := service.StartPersistWatcher(ctx); err != nil {
		t.Fatal(err)
	}
	oldGeneration := service.persist.generation
	oldWrite := false
	service.persist.afterFile("old.ini", 1, func() { oldWrite = true })
	resume, err := service.SuspendImporterWatcher(ctx)
	if err != nil {
		t.Fatal(err)
	}
	resumed := false
	defer func() {
		if !resumed {
			_ = resume()
		}
	}()
	if service.persistMu.TryLock() {
		service.persistMu.Unlock()
		t.Fatal("persist writes were allowed during maintenance")
	}
	if service.watchMu.TryLock() {
		service.watchMu.Unlock()
		t.Fatal("persist watcher could restart during maintenance")
	}
	service.persist.Advance(1000)
	if oldWrite || service.persist.active(oldGeneration) {
		t.Fatal("old persist work remained active")
	}
	newFolder := filepath.Join(base, "new", "GIMI")
	if err := os.MkdirAll(filepath.Dir(newFolder), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(cfg.ImporterFolder, newFolder); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(newFolder, "d3dx_user.ini"),
		[]byte("[Constants]\n$new = 2\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	cfg.ImporterFolder = newFolder
	if err := importers.SaveImporterConfig(ctx, "GIMI", cfg); err != nil {
		t.Fatal(err)
	}
	resumed = true
	if err := resume(); err != nil {
		t.Fatal(err)
	}
	if value := service.persist.cachedValue("GIMI", "$new"); value != "2" {
		t.Fatalf("resumed persist data = %q, want final config's data", value)
	}
}
