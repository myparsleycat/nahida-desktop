package xxmi

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"nahida.live/desktop/internal/db"
)

func TestDefaultImporterSettings(t *testing.T) {
	t.Parallel()
	root := filepath.Join(t.TempDir(), "xxmi")
	for _, key := range []string{"GIMI", "SRMI", "ZZMI", "WWMI", "HIMI", "EFMI"} {
		cfg, err := DefaultImporterConfig(key, root)
		if err != nil {
			t.Fatalf("%s default: %v", key, err)
		}
		if cfg.PackageVersion.Follow != "latest" || cfg.XXMIVersion.Follow != "latest" {
			t.Fatalf("%s pins = %+v, %+v", key, cfg.PackageVersion, cfg.XXMIVersion)
		}
		if err := ValidateImporterSettings(key, cfg); err != nil {
			t.Fatalf("%s validation: %v", key, err)
		}
	}
}

func TestImporterSettingsRejectNestedGameFolderAndInvalidPin(t *testing.T) {
	t.Parallel()
	root := filepath.Join(t.TempDir(), "xxmi")
	cfg, err := DefaultImporterConfig("GIMI", root)
	if err != nil {
		t.Fatal(err)
	}
	cfg.GameFolder = filepath.Join(cfg.ImporterFolder, "game")
	if err := ValidateImporterSettings("GIMI", cfg); err == nil {
		t.Fatal("nested game folder accepted")
	}
	cfg.GameFolder = ""
	cfg.XXMIVersion = VersionPin{Follow: "latest", Pinned: "1.7.6"}
	if err := ValidateImporterSettings("GIMI", cfg); err == nil {
		t.Fatal("ambiguous version pin accepted")
	}
	for _, version := range []string{".", "..", "v..", " "} {
		cfg.XXMIVersion = VersionPin{Pinned: version}
		if err := ValidateImporterSettings("GIMI", cfg); err == nil {
			t.Fatalf("dot version pin %q accepted", version)
		}
	}
}

func TestPinnedVersionsSurviveReload(t *testing.T) {
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

	cfg, err := DefaultImporterConfig("GIMI", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg.PackageVersion = VersionPin{Pinned: "1.2.3"}
	cfg.XXMIVersion = VersionPin{Pinned: "1.1.7"}
	if err := service.SaveImporterConfig(ctx, "GIMI", cfg); err != nil {
		t.Fatal(err)
	}

	stored, err := service.GetImporterConfig(ctx, "GIMI")
	if err != nil {
		t.Fatal(err)
	}
	if stored.PackageVersion != cfg.PackageVersion || stored.XXMIVersion != cfg.XXMIVersion {
		t.Fatalf("reloaded pins = %+v, %+v", stored.PackageVersion, stored.XXMIVersion)
	}
	if err := ValidateImporterSettings("GIMI", stored); err != nil {
		t.Fatalf("reloaded config validation: %v", err)
	}
}

func TestImporterSettingsRejectInvalidWindowMode(t *testing.T) {
	t.Parallel()
	cfg, err := DefaultImporterConfig("GIMI", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"Windowed", "Borderless", "Fullscreen", "Exclusive Fullscreen"} {
		cfg.WindowMode = mode
		if err := ValidateImporterSettings("GIMI", cfg); err != nil {
			t.Fatalf("valid window mode %q rejected: %v", mode, err)
		}
	}
	cfg.WindowMode = "other"
	if err := ValidateImporterSettings("GIMI", cfg); err == nil {
		t.Fatal("invalid window mode accepted")
	}
}

func TestModeChangeRejectsLaunchInProgress(t *testing.T) {
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
	cfg, err := DefaultImporterConfig("EFMI", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := service.SaveImporterConfig(ctx, "EFMI", cfg); err != nil {
		t.Fatal(err)
	}
	if !service.acquireImporter("EFMI") {
		t.Fatal("failed to start importer launch")
	}
	err = service.SetImporterMode(ctx, "EFMI", RuntimeLegacy)
	if err == nil || !strings.Contains(err.Error(), "XXMI_GAME_RUNNING") {
		t.Fatalf("mode change during launch = %v", err)
	}
	service.releaseImporter("EFMI")
	stored, err := service.GetImporterConfig(ctx, "EFMI")
	if err != nil || stored.Mode != RuntimeXXMI {
		t.Fatalf("saved mode after rejected change = %q, err = %v", stored.Mode, err)
	}
	if err := service.SetImporterMode(ctx, "EFMI", RuntimeLegacy); err != nil {
		t.Fatal(err)
	}
}
