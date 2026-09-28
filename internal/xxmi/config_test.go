package xxmi

import (
	"path/filepath"
	"testing"
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
}
