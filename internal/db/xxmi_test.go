package db

import (
	"context"
	"testing"
)

func TestXXMIStores(t *testing.T) {
	t.Parallel()
	client := mustNewTemp(t)
	ctx := context.Background()
	if err := client.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}

	if err := client.XXMIImporters.Upsert(ctx, "GIMI", `{"mode":"xxmi"}`); err != nil {
		t.Fatal(err)
	}
	if err := client.XXMIImporters.Upsert(ctx, "GIMI", `{"mode":"legacy"}`); err != nil {
		t.Fatal(err)
	}
	importer, err := client.XXMIImporters.Get(ctx, "GIMI")
	if err != nil || importer == nil || importer.Config != `{"mode":"legacy"}` {
		t.Fatalf("importer = %+v, err = %v", importer, err)
	}
	if rows, err := client.XXMIImporters.List(ctx); err != nil || len(rows) != 1 {
		t.Fatalf("importer rows = %+v, err = %v", rows, err)
	}

	latest, skipped := "1.7.6", "1.7.5"
	if err := client.XXMIPackages.Upsert(ctx, XXMIPackageRow{
		Package: "xxmi-libs", LatestVersion: &latest, UpdateCheckTime: 123, SkippedVersion: &skipped,
	}); err != nil {
		t.Fatal(err)
	}
	pkg, err := client.XXMIPackages.Get(ctx, "xxmi-libs")
	if err != nil || pkg == nil || pkg.LatestVersion == nil || *pkg.LatestVersion != latest ||
		pkg.SkippedVersion == nil || *pkg.SkippedVersion != skipped || pkg.UpdateCheckTime != 123 {
		t.Fatalf("package = %+v, err = %v", pkg, err)
	}
	if err := client.XXMIImporters.Delete(ctx, "GIMI"); err != nil {
		t.Fatal(err)
	}
	if row, err := client.XXMIImporters.Get(ctx, "GIMI"); err != nil || row != nil {
		t.Fatalf("deleted importer = %+v, err = %v", row, err)
	}
}
