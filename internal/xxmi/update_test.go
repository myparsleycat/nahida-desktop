package xxmi

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"nahida.live/desktop/internal/db"
)

func TestSkipVersionPreservesPinnedImporter(t *testing.T) {
	t.Parallel()
	client, err := db.New(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	ctx := context.Background()
	if err := client.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	x := New()
	x.UseClient(client)
	cfg, err := DefaultImporterConfig("GIMI", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg.PackageVersion = VersionPin{Pinned: "1.0.0"}
	if err := x.SaveImporterConfig(ctx, "GIMI", cfg); err != nil {
		t.Fatal(err)
	}
	latest := "1.1.0"
	if err := client.XXMIPackages.Upsert(
		ctx,
		db.XXMIPackageRow{Package: "importer:GIMI", LatestVersion: &latest},
	); err != nil {
		t.Fatal(err)
	}
	if !updateAvailable(latest, "1.0.0", "") {
		t.Fatal("pinned importer should still show the newer release")
	}
	if err := x.SkipVersion(ctx, "importer:GIMI", latest); err != nil {
		t.Fatal(err)
	}
	if updateAvailable(latest, "1.0.0", latest) {
		t.Fatal("skipped release should not be announced")
	}
	stored, err := x.GetImporterConfig(ctx, "GIMI")
	if err != nil || stored.PackageVersion.Pinned != "1.0.0" {
		t.Fatalf("pin changed: %+v, err = %v", stored.PackageVersion, err)
	}
}

func TestCheckUpdatesHonorsHourlyThrottleAndForce(t *testing.T) {
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
	x := New()
	x.UseClient(client)
	cfg, err := DefaultImporterConfig("GIMI", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg.Enabled = true
	if err := x.SaveImporterConfig(ctx, "GIMI", cfg); err != nil {
		t.Fatal(err)
	}
	for _, pkg := range []string{"importer:GIMI", "xxmi-libs"} {
		latest := "1.2.3"
		if err := client.XXMIPackages.Upsert(ctx, db.XXMIPackageRow{
			Package: pkg, LatestVersion: &latest, UpdateCheckTime: time.Now().Unix(),
		}); err != nil {
			t.Fatal(err)
		}
	}
	statuses, err := x.CheckUpdates(ctx, false)
	if err != nil || len(statuses) != 2 {
		t.Fatalf("cached update statuses = %+v, err = %v", statuses, err)
	}
	if _, err := x.CheckUpdates(ctx, true); err == nil {
		t.Fatal("force update check did not attempt a release refresh")
	}
}

func TestCheckUpdatesDoesNotReportLegacyRuntimeAsInstalledLibraries(t *testing.T) {
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
	x := New()
	x.UseClient(client)
	cfg, err := DefaultImporterConfig("GIMI", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg.Enabled = true
	cfg.Mode = RuntimeLegacy
	cfg.XXMIVersion = VersionPin{Pinned: "1.7.6"}
	if err := x.SaveImporterConfig(ctx, "GIMI", cfg); err != nil {
		t.Fatal(err)
	}
	manifest, err := json.Marshal(runtimeManifest{Mode: RuntimeLegacy, Source: "legacy@123456789abc"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(cfg.ImporterFolder, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg.ImporterFolder, runtimeManifestName), manifest, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, pkg := range []string{"importer:GIMI", "xxmi-libs"} {
		latest := "1.7.7"
		if err := client.XXMIPackages.Upsert(ctx, db.XXMIPackageRow{
			Package: pkg, LatestVersion: &latest, UpdateCheckTime: time.Now().Unix(),
		}); err != nil {
			t.Fatal(err)
		}
	}
	statuses, err := x.CheckUpdates(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, status := range statuses {
		if status.Package == "xxmi-libs" {
			if status.Installed != "" || !status.Pinned {
				t.Fatalf("legacy runtime reported as XXMI libraries: %+v", status)
			}
			return
		}
	}
	t.Fatalf("XXMI libraries status missing: %+v", statuses)
}

func TestAutoUpdateUsesEnabledDefaultBeforeSettingsPageOpens(t *testing.T) {
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
	checks := 0
	x := NewWithOptions(Options{EventEmit: func(name string, _ ...any) {
		if name == "xxmi:updates" {
			checks++
		}
	}})
	x.UseClient(client)
	cfg, err := DefaultImporterConfig("GIMI", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg.Enabled = true
	if err := x.SaveImporterConfig(ctx, "GIMI", cfg); err != nil {
		t.Fatal(err)
	}
	for _, pkg := range []string{"importer:GIMI", "xxmi-libs"} {
		if err := client.XXMIPackages.Upsert(ctx, db.XXMIPackageRow{
			Package: pkg, UpdateCheckTime: time.Now().Unix(),
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := x.autoUpdateForLaunch(ctx, "GIMI"); err != nil || checks != 1 {
		t.Fatalf("default auto-update: checks = %d, err = %v", checks, err)
	}
	disabled := "false"
	if err := client.Settings.Upsert(ctx, "xxmi_auto_update", &disabled); err != nil {
		t.Fatal(err)
	}
	if err := x.autoUpdateForLaunch(ctx, "GIMI"); err != nil || checks != 1 {
		t.Fatalf("disabled auto-update: checks = %d, err = %v", checks, err)
	}
}
