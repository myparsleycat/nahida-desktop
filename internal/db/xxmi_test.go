package db

import (
	"context"
	"encoding/json"
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

func TestXXMILaunchCountUpdatePreservesConcurrentSettings(t *testing.T) {
	t.Parallel()
	client := mustNewTemp(t)
	ctx := context.Background()
	if err := client.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	if err := client.XXMIImporters.Upsert(
		ctx,
		"GIMI",
		`{"mode":"xxmi","launchCount":4,"gameFolder":"new"}`,
	); err != nil {
		t.Fatal(err)
	}
	if err := client.XXMIImporters.IncrementLaunchCount(ctx, "GIMI"); err != nil {
		t.Fatal(err)
	}
	row, err := client.XXMIImporters.Get(ctx, "GIMI")
	if err != nil || row == nil {
		t.Fatalf("updated importer = %+v, err = %v", row, err)
	}
	var settings struct {
		Mode        string `json:"mode"`
		LaunchCount int    `json:"launchCount"`
		GameFolder  string `json:"gameFolder"`
	}
	if err := json.Unmarshal([]byte(row.Config), &settings); err != nil {
		t.Fatal(err)
	}
	if settings.Mode != "xxmi" || settings.LaunchCount != 5 || settings.GameFolder != "new" {
		t.Fatalf("updated settings = %+v", settings)
	}
}

func TestXXMILaunchCountStartsAtOneFromUnknownCount(t *testing.T) {
	t.Parallel()
	client := mustNewTemp(t)
	ctx := context.Background()
	if err := client.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	for _, config := range []string{`{"launchCount":-1}`, `{}`} {
		if err := client.XXMIImporters.Upsert(ctx, "GIMI", config); err != nil {
			t.Fatal(err)
		}
		if err := client.XXMIImporters.IncrementLaunchCount(ctx, "GIMI"); err != nil {
			t.Fatal(err)
		}
		row, err := client.XXMIImporters.Get(ctx, "GIMI")
		if err != nil || row == nil {
			t.Fatalf("updated importer = %+v, err = %v", row, err)
		}
		var settings struct {
			LaunchCount int `json:"launchCount"`
		}
		if err := json.Unmarshal([]byte(row.Config), &settings); err != nil {
			t.Fatal(err)
		}
		if settings.LaunchCount != 1 {
			t.Fatalf("launch count from %s = %d, want 1", config, settings.LaunchCount)
		}
	}
}

func TestXXMIImportRollsBackAllStores(t *testing.T) {
	t.Parallel()
	client := mustNewTemp(t)
	ctx := context.Background()
	if err := client.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := client.SQL().ExecContext(ctx, `CREATE TRIGGER fail_xxmi_import
BEFORE INSERT ON setting WHEN NEW.key = 'xxmi_root'
BEGIN SELECT RAISE(ABORT, 'import failure'); END`); err != nil {
		t.Fatal(err)
	}
	root := "C:\\XXMI"
	latest := "1.2.3"
	err := client.XXMIImporters.ApplyImport(ctx,
		[]XXMIImporterRow{{Key: "GIMI", Config: `{}`}},
		[]XXMIPackageRow{{Package: "importer:GIMI", LatestVersion: &latest}},
		map[string]*string{"xxmi_root": &root},
	)
	if err == nil {
		t.Fatal("expected import failure")
	}
	if row, err := client.XXMIImporters.Get(ctx, "GIMI"); err != nil || row != nil {
		t.Fatalf("importer survived rollback: row = %+v, err = %v", row, err)
	}
	if row, err := client.XXMIPackages.Get(ctx, "importer:GIMI"); err != nil || row != nil {
		t.Fatalf("package survived rollback: row = %+v, err = %v", row, err)
	}
	if value, err := client.Settings.GetValue(ctx, "xxmi_root"); err != nil || value != nil {
		t.Fatalf("setting survived rollback: value = %v, err = %v", value, err)
	}
}

func TestXXMISetConfigFlagKeepsTheRestOfTheConfig(t *testing.T) {
	t.Parallel()
	client := mustNewTemp(t)
	ctx := context.Background()
	if err := client.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}

	// A missing importer is not created.
	if err := client.XXMIImporters.SetConfigFlag(ctx, "WWMI", "$.wwmi.pending", false); err != nil {
		t.Fatal(err)
	}
	if row, err := client.XXMIImporters.Get(ctx, "WWMI"); err != nil || row != nil {
		t.Fatalf("importer = %+v, err = %v", row, err)
	}

	if err := client.XXMIImporters.Upsert(ctx, "WWMI", `{"mode":"xxmi","wwmi":{"unlockFPS":true}}`); err != nil {
		t.Fatal(err)
	}
	for _, value := range []bool{true, false} {
		if err := client.XXMIImporters.SetConfigFlag(ctx, "WWMI", "$.wwmi.pending", value); err != nil {
			t.Fatal(err)
		}
		row, err := client.XXMIImporters.Get(ctx, "WWMI")
		if err != nil || row == nil {
			t.Fatalf("importer = %+v, err = %v", row, err)
		}
		var settings struct {
			Mode string `json:"mode"`
			WWMI struct {
				UnlockFPS bool  `json:"unlockFPS"`
				Pending   *bool `json:"pending"`
			} `json:"wwmi"`
		}
		if err := json.Unmarshal([]byte(row.Config), &settings); err != nil {
			t.Fatal(err)
		}
		if settings.Mode != "xxmi" || !settings.WWMI.UnlockFPS || settings.WWMI.Pending == nil ||
			*settings.WWMI.Pending != value {
			t.Fatalf("config after setting %t = %s", value, row.Config)
		}
	}
}
