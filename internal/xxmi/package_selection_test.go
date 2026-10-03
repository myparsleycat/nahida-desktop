package xxmi

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nahida.live/desktop/internal/db"
)

func TestImporterPackageSelectionRequiresInstalledVersion(t *testing.T) {
	t.Parallel()
	for _, key := range []string{"GIMI", "SRMI", "ZZMI", "WWMI", "HIMI", "EFMI"} {
		t.Run(key, func(t *testing.T) {
			t.Parallel()
			cfg, err := DefaultImporterConfig(key, t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			cfg.PackageVersion = VersionPin{Pinned: "v1.2.3"}
			if err := validateInstalledImporterPackage(key, cfg); err == nil {
				t.Fatal("missing package accepted")
			}
			writeInstalledImporterPackage(t, key, cfg.ImporterFolder, "1.0.0")
			if err := validateInstalledImporterPackage(key, cfg); err == nil {
				t.Fatal("different installed version accepted")
			}
			writeInstalledImporterPackage(t, key, cfg.ImporterFolder, "1.2.3")
			if err := validateInstalledImporterPackage(key, cfg); err != nil {
				t.Fatalf("matching normalized version rejected: %v", err)
			}
		})
	}
}

func TestSaveAndLaunchRejectUninstalledPackagePin(t *testing.T) {
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
	useBuiltinLauncher(t, service)
	cfg, err := DefaultImporterConfig("GIMI", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg.Enabled = true
	writeInstalledImporterPackage(t, "GIMI", cfg.ImporterFolder, "1.0.0")
	if err := service.SaveImporterConfig(ctx, "GIMI", cfg); err != nil {
		t.Fatal(err)
	}
	cfg.PackageVersion = VersionPin{Pinned: "1.2.3"}
	if err := service.SaveImporterConfig(ctx, "GIMI", cfg); err == nil ||
		!strings.Contains(err.Error(), "XXMI_IMPORTER_NOT_INSTALLED") {
		t.Fatalf("uninstalled package saved: %v", err)
	}
	stored, err := service.GetImporterConfig(ctx, "GIMI")
	if err != nil || stored.PackageVersion.Pinned != "" {
		t.Fatalf("rejected save changed the pin: %+v, err = %v", stored.PackageVersion, err)
	}
	if err := service.SetImporterVersions(ctx, "GIMI", ImporterVersions{Package: &cfg.PackageVersion}); err == nil {
		t.Fatal("version setter accepted an uninstalled package")
	}

	// Simulate a mismatched pin persisted by an older application version.
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.XXMIImporters.Upsert(ctx, "GIMI", string(data)); err != nil {
		t.Fatal(err)
	}
	stages := []string{}
	service.eventEmit = func(_ string, data ...any) {
		if payload, ok := data[0].(map[string]any); ok {
			if stage, ok := payload["stage"].(string); ok {
				stages = append(stages, stage)
			}
		}
	}
	if err := service.StartGame(ctx, "GIMI"); err == nil ||
		!strings.Contains(err.Error(), "XXMI_IMPORTER_NOT_INSTALLED") {
		t.Fatalf("mismatched package launched: %v", err)
	}
	if len(stages) != 1 || stages[0] != "failed" {
		t.Fatalf("launch performed work before rejecting the package: %v", stages)
	}
}

func writeInstalledImporterPackage(t *testing.T, key, folder, version string) {
	t.Helper()
	spec, ok := lookupImporterPackage(key)
	if !ok {
		t.Fatalf("unknown importer %s", key)
	}
	parts := strings.Split(normalizeVersion(version), ".")
	if len(parts) != 3 {
		t.Fatalf("invalid test version %s", version)
	}
	path := filepath.Join(folder, spec.versionFile)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	variable := "$version"
	if key == "WWMI" {
		variable = "$wwmi_version"
	}
	if err := os.WriteFile(
		path,
		[]byte("global "+variable+" = "+parts[0]+"."+parts[1]+parts[2]+"\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
}
