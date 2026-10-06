package xxmi

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"nahida.live/desktop/internal/db"
)

func TestAdoptUserRuntimeRecordsModifiedDLL(t *testing.T) {
	ctx := context.Background()
	client, err := db.New(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	if err := client.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	folder := filepath.Join(t.TempDir(), "GIMI")
	if err := os.MkdirAll(folder, 0o700); err != nil {
		t.Fatal(err)
	}
	dll := []byte("user modified")
	if err := os.WriteFile(filepath.Join(folder, "d3d11.dll"), dll, 0o600); err != nil {
		t.Fatal(err)
	}
	service := New()
	service.UseClient(client)
	useBuiltinLauncher(t, service)
	if err := service.EnableImporter(ctx, "GIMI", folder); err != nil {
		t.Fatal(err)
	}
	if err := service.AdoptUserRuntime(ctx, "GIMI"); err != nil {
		t.Fatal(err)
	}
	cfg, err := service.GetImporterConfig(ctx, "GIMI")
	if err != nil || !cfg.Migoto.UnsafeMode {
		t.Fatalf("unsafe mode = %t, %v", cfg.Migoto.UnsafeMode, err)
	}
	data, err := os.ReadFile(filepath.Join(folder, runtimeManifestName))
	if err != nil {
		t.Fatal(err)
	}
	var manifest runtimeManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.UserManaged["d3d11.dll"] != hashBytes(dll) {
		t.Fatalf("user managed hash = %q", manifest.UserManaged["d3d11.dll"])
	}
	importers, err := service.builtinEnabledImporters(ctx)
	if err != nil || len(importers) != 1 || !importers[0].CustomDLL {
		t.Fatalf("importers = %+v, error = %v", importers, err)
	}
}

func TestOfficialDLLConfigOwnsTheLibrariesVersion(t *testing.T) {
	tests := []struct {
		name          string
		version       VersionPin
		sharedVersion string
		want          VersionPin
	}{
		{"shared latest", VersionPin{Follow: followShared}, "", VersionPin{Follow: "latest"}},
		{"shared pin", VersionPin{Follow: followShared}, "1.7.5", VersionPin{Pinned: "1.7.5"}},
		{"shared provider release", VersionPin{Follow: followShared}, "1.7.5-nhd.2", VersionPin{Pinned: "1.7.5"}},
		{
			"own pin",
			VersionPin{Pinned: "1.7.4", Notify: true},
			"1.7.5",
			VersionPin{Pinned: "1.7.4", Notify: true},
		},
		{"own latest", VersionPin{Follow: "latest"}, "1.7.5", VersionPin{Follow: "latest"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := officialDLLConfig(ImporterConfig{XXMIVersion: test.version}, test.sharedVersion)
			if cfg.XXMIVersion != test.want || cfg.LibsProvider != defaultLibsProvider {
				t.Fatalf("version = %+v, provider = %q, want %+v", cfg.XXMIVersion, cfg.LibsProvider, test.want)
			}
		})
	}
}

func TestRestoreOfficialDLLRejectsUnmanagedImporters(t *testing.T) {
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
	service.findProcess = noGameProcess
	useBuiltinLauncher(t, service)
	if _, err := service.RestoreOfficialDLL(ctx, "GIMI"); err == nil || err.Error() != "XXMI_NOT_CONFIGURED" {
		t.Fatalf("disabled importer error = %v", err)
	}

	folder := filepath.Join(t.TempDir(), "GIMI")
	if err := os.MkdirAll(folder, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := service.EnableImporter(ctx, "GIMI", folder); err != nil {
		t.Fatal(err)
	}
	cfg, err := service.GetImporterConfig(ctx, "GIMI")
	if err != nil {
		t.Fatal(err)
	}
	cfg.Mode = RuntimeLegacy
	if err := service.SaveImporterConfig(ctx, "GIMI", cfg); err != nil {
		t.Fatal(err)
	}
	_, err = service.RestoreOfficialDLL(ctx, "GIMI")
	if err == nil || err.Error() != "XXMI_LEGACY_RUNTIME_UNSUPPORTED" {
		t.Fatalf("legacy importer error = %v", err)
	}
}
