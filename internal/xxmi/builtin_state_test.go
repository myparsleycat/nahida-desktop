package xxmi

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"nahida.live/desktop/internal/db"
)

func TestBuiltinStateFeedsExistingConsumers(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	client, err := db.New(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	if err := client.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	service := New()
	service.UseClient(client)
	if err := service.SetRoot(ctx, root); err != nil {
		t.Fatal(err)
	}
	importerFolder := filepath.Join(root, "GIMI")
	if err := os.MkdirAll(filepath.Join(importerFolder, "Core", "GIMI"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(importerFolder, "Core", "GIMI", "main.ini"),
		[]byte("global $version = 1.23\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	if err := service.EnableImporter(ctx, "GIMI", importerFolder); err != nil {
		t.Fatal(err)
	}
	latest := "1.2.4"
	if err := client.XXMIPackages.Upsert(ctx, db.XXMIPackageRow{
		Package: "importer:GIMI", LatestVersion: &latest,
	}); err != nil {
		t.Fatal(err)
	}
	importers, err := service.GetEnabledImporters(ctx)
	if err != nil || len(importers) != 1 || importers[0].InstalledVersion == nil ||
		*importers[0].InstalledVersion != "1.2.3" {
		t.Fatalf("enabled importers = %+v, err = %v", importers, err)
	}
	path, err := service.GetXXMIPath(ctx)
	if err != nil || path == nil || *path != root {
		t.Fatalf("XXMI root = %v, err = %v", path, err)
	}
	state, err := service.GetXXMIData(ctx)
	if err != nil || state.XXMIPath == nil || *state.XXMIPath != root || len(state.EnabledImporters) != 1 {
		t.Fatalf("XXMI data = %+v, err = %v", state, err)
	}
	if !service.acquireImporter("GIMI") {
		t.Fatal("GIMI launch lock is unavailable")
	}
	defer service.releaseImporter("GIMI")
	overview, err := service.GetOverview(ctx)
	if err != nil || len(overview.Importers) != 1 || !overview.Importers[0].Running ||
		!overview.Importers[0].UpdateAvailable || overview.Importers[0].PackageInfo.DeployedVersion != "1.2.3" {
		t.Fatalf("XXMI overview = %+v, err = %v", overview, err)
	}
	hunting, err := service.ResolveHuntingRuntime(ctx, "GIMI")
	if err != nil || hunting.ImporterFolder != importerFolder || len(hunting.GameEXENames) == 0 {
		t.Fatalf("hunting runtime = %+v, err = %v", hunting, err)
	}
}

func TestBuiltinConsumersIgnoreUnimportedExternalLauncher(t *testing.T) {
	ctx := context.Background()
	client, err := db.New(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	if err := client.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	external := t.TempDir()
	writeXXMITestConfig(t, external)
	if err := client.Settings.Upsert(ctx, xxmiPathKey, &external); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	service := New()
	service.UseClient(client)
	if err := service.SetRoot(ctx, root); err != nil {
		t.Fatal(err)
	}
	path, err := service.GetXXMIPath(ctx)
	if err != nil || path == nil || *path != root {
		t.Fatalf("built-in root = %v, %v", path, err)
	}
	importers, err := service.GetEnabledImporters(ctx)
	if err != nil || len(importers) != 0 {
		t.Fatalf("unimported external importers = %+v, %v", importers, err)
	}
	data, err := service.GetXXMIData(ctx)
	if err != nil || data.XXMIPath == nil || *data.XXMIPath != root || len(data.EnabledImporters) != 0 {
		t.Fatalf("built-in data = %+v, %v", data, err)
	}
	if _, err := service.ResolveHuntingRuntime(ctx, "GIMI"); err == nil {
		t.Fatal("unimported external importer used for hunting")
	}
}
