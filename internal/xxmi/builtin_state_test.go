package xxmi

import (
	"context"
	"os"
	"path/filepath"
	"strings"
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
	useBuiltinLauncher(t, service)
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
	service.setLaunching("GIMI", true)
	defer service.setLaunching("GIMI", false)
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
	useBuiltinLauncher(t, service)
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

func TestOverviewKeepsSettingsAvailableForDamagedCache(t *testing.T) {
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	cacheRoot, err := xxmiCacheRoot()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(cacheRoot, "packages", "legacy-3dmigoto", "abcdef123456"), 0o700); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	client, err := db.New(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	if err := client.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	service := New()
	service.UseClient(client)
	useBuiltinLauncher(t, service)
	if err := service.EnableImporter(ctx, "GIMI", filepath.Join(t.TempDir(), "GIMI")); err != nil {
		t.Fatal(err)
	}
	overview, err := service.GetOverview(ctx)
	if err != nil || !overview.Configured || len(overview.CacheIssues) == 0 {
		t.Fatalf("overview = %+v, err = %v", overview, err)
	}
}

func TestOverviewKeepsConfigurationWhenEveryImporterIsDisabled(t *testing.T) {
	t.Setenv("USERPROFILE", t.TempDir())
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
	service := New()
	service.UseClient(client)
	useBuiltinLauncher(t, service)
	cfg, err := DefaultImporterConfig("GIMI", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := service.SaveImporterConfig(ctx, "GIMI", cfg); err != nil {
		t.Fatal(err)
	}
	overview, err := service.GetOverview(ctx)
	if err != nil || !overview.Configured || len(overview.Importers) != 0 || overview.ExternalLauncher != nil {
		t.Fatalf("disabled importer overview = %+v, err = %v", overview, err)
	}
}

func TestOverviewKeepsBuiltinRootWhenExternalLauncherIsFound(t *testing.T) {
	t.Setenv("USERPROFILE", t.TempDir())
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
	service := New()
	service.UseClient(client)
	useBuiltinLauncher(t, service)
	cacheRoot, err := xxmiCacheRoot()
	if err != nil {
		t.Fatal(err)
	}
	overview, err := service.GetOverview(ctx)
	if err != nil || overview.ExternalLauncher == nil || overview.Root != cacheRoot {
		t.Fatalf("default root with external launcher = %+v, err = %v", overview, err)
	}
	chosen := t.TempDir()
	if err := service.SetRoot(ctx, chosen); err != nil {
		t.Fatal(err)
	}
	overview, err = service.GetOverview(ctx)
	if err != nil || overview.Root != chosen {
		t.Fatalf("user root selection = %+v, err = %v", overview, err)
	}
}

func TestResetBuiltinRuntimeForgetsStateAndKeepsFiles(t *testing.T) {
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
	service := New()
	service.UseClient(client)
	external := t.TempDir()
	if err := client.Settings.Upsert(ctx, "xxmi_path", &external); err != nil {
		t.Fatal(err)
	}
	useBuiltinLauncher(t, service)
	root := t.TempDir()
	if err := service.SetRoot(ctx, root); err != nil {
		t.Fatal(err)
	}
	importerFolder := filepath.Join(root, "GIMI")
	if err := os.MkdirAll(filepath.Join(importerFolder, "Mods"), 0o700); err != nil {
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
	autoUpdate := "false"
	if err := client.Settings.Upsert(ctx, "xxmi_auto_update", &autoUpdate); err != nil {
		t.Fatal(err)
	}

	if !service.acquireImporter("GIMI") {
		t.Fatal("acquire GIMI")
	}
	if err := service.ResetBuiltinRuntime(ctx); err == nil || !strings.Contains(err.Error(), "XXMI_BUSY") {
		t.Fatalf("reset while launching err = %v", err)
	}
	service.releaseImporter("GIMI")
	if rows, err := client.XXMIImporters.List(ctx); err != nil || len(rows) != 1 {
		t.Fatalf("importers after refused reset = %+v, err = %v", rows, err)
	}

	if err := service.ResetBuiltinRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	if rows, err := client.XXMIImporters.List(ctx); err != nil || len(rows) != 0 {
		t.Fatalf("importers after reset = %+v, err = %v", rows, err)
	}
	if pkg, err := client.XXMIPackages.Get(ctx, "importer:GIMI"); err != nil || pkg != nil {
		t.Fatalf("package after reset = %+v, err = %v", pkg, err)
	}
	for _, key := range builtinSettingKeys {
		if value, err := client.Settings.GetValue(ctx, key); err != nil || value != nil {
			t.Fatalf("setting %s after reset = %v, err = %v", key, value, err)
		}
	}
	if mode, err := service.GetLauncherMode(ctx); err != nil || mode != LauncherBuiltin {
		t.Fatalf("launcher mode after reset = %q, err = %v", mode, err)
	}
	if path, err := client.Settings.GetValue(ctx, "xxmi_path"); err != nil || path == nil || *path != external {
		t.Fatalf("external launcher path after reset = %v, err = %v", path, err)
	}
	if _, err := os.Stat(filepath.Join(importerFolder, "Mods")); err != nil {
		t.Fatalf("importer folder after reset: %v", err)
	}
	if importers, err := service.GetEnabledImporters(ctx); err != nil || len(importers) != 0 {
		t.Fatalf("enabled importers after reset = %+v, err = %v", importers, err)
	}
}
