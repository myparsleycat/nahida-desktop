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
			cfg.PackageSource = PackageSources(key)[0]
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

func TestImporterPackageSelectionRequiresSelectedSource(t *testing.T) {
	t.Parallel()
	cfg, err := DefaultImporterConfig("WWMI", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg.PackageVersion = VersionPin{Pinned: "1.2.2"}
	writeInstalledImporterPackage(t, "WWMI", cfg.ImporterFolder, "1.2.2")
	if err := validateInstalledImporterPackage("WWMI", cfg); err == nil {
		t.Fatal("official package without a recorded source accepted for the default source")
	}

	// The fork's version format names its source when no record does, and the official one reads it as 1.2.21.
	cfg.PackageVersion = VersionPin{Pinned: "1.2.2.1"}
	writeInstalledImporterPackage(t, "WWMI", cfg.ImporterFolder, "1.2.2.1")
	if err := validateInstalledImporterPackage("WWMI", cfg); err != nil {
		t.Fatalf("fork package without a recorded source rejected: %v", err)
	}

	record, err := json.Marshal(PackageVerification{Version: "1.2.2.1", Method: "digest", Source: "myparsleycat"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg.ImporterFolder, packageVerificationName), record, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := validateInstalledImporterPackage("WWMI", cfg); err != nil {
		t.Fatalf("recorded default source rejected: %v", err)
	}
	cfg.PackageSource = "spectrumqt"
	if err := validateInstalledImporterPackage("WWMI", cfg); err == nil {
		t.Fatal("package of the default source accepted for the official source")
	}

	// A game left to pick its renderer may render with DirectX 12 or Vulkan, which only the fork applies mods to.
	cfg.SkipD3D11Switch = true
	if source, forced := PackageSource("WWMI", cfg); source != "myparsleycat" || !forced {
		t.Fatalf("PackageSource = %q, %v; want myparsleycat, forced", source, forced)
	}
	if err := validateInstalledImporterPackage("WWMI", cfg); err != nil {
		t.Fatalf("forced source rejected: %v", err)
	}

	// A recorded official package keeps its source although the fork's pattern also reads 1.210, as 1.2.1.0.
	official, err := DefaultImporterConfig("WWMI", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	official.PackageSource, official.PackageVersion = "spectrumqt", VersionPin{Pinned: "1.2.10"}
	writeInstalledImporterPackage(t, "WWMI", official.ImporterFolder, "1.2.10")
	record, err = json.Marshal(PackageVerification{Version: "1.2.10", Method: "ecdsa"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(official.ImporterFolder, packageVerificationName), record, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := validateInstalledImporterPackage("WWMI", official); err != nil {
		t.Fatalf("recorded official package rejected: %v", err)
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
	useTestAppData(t, service)
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

	// The stuck importer can still be turned off, and turning it back on needs the package again.
	if err := service.DisableImporter(ctx, "GIMI"); err != nil {
		t.Fatalf("disabling an importer with an uninstalled pin: %v", err)
	}
	stored, err = service.GetImporterConfig(ctx, "GIMI")
	if err != nil || stored.Enabled || stored.PackageVersion.Pinned != "1.2.3" {
		t.Fatalf("disabled config = %+v, err = %v", stored, err)
	}
	if err := service.EnableImporter(ctx, "GIMI", ""); err == nil ||
		!strings.Contains(err.Error(), "XXMI_IMPORTER_NOT_INSTALLED") {
		t.Fatalf("importer with an uninstalled pin enabled: %v", err)
	}
}

func writeInstalledImporterPackage(t *testing.T, key, folder, version string) {
	t.Helper()
	spec, ok := lookupImporterPackage(key)
	if !ok {
		t.Fatalf("unknown importer %s", key)
	}
	// A fourth component is the revision a package source appends after the official version digits.
	parts := strings.Split(normalizeVersion(version), ".")
	if len(parts) != 3 && len(parts) != 4 {
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
		[]byte("global "+variable+" = "+parts[0]+"."+strings.Join(parts[1:], "")+"\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
}
