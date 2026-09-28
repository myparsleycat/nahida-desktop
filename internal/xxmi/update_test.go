package xxmi

import (
	"context"
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
