package xxmi

import (
	"context"
	"path/filepath"
	"testing"

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
