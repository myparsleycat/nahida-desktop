package xxmi

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"nahida.live/desktop/internal/db"
)

func TestListCachedLibsSeparatesSelectedVersionFromDeployedReferences(t *testing.T) {
	t.Setenv("USERPROFILE", t.TempDir())
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
	cfg.XXMIVersion = VersionPin{Pinned: "1.2.0"}
	if err := service.SaveImporterConfig(ctx, "GIMI", cfg); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(cfg.ImporterFolder, 0o700); err != nil {
		t.Fatal(err)
	}
	manifest, err := json.Marshal(runtimeManifest{Mode: RuntimeXXMI, Source: "xxmi-libs@1.1.7"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg.ImporterFolder, runtimeManifestName), manifest, 0o600); err != nil {
		t.Fatal(err)
	}
	root, err := xxmiCacheRoot()
	if err != nil {
		t.Fatal(err)
	}
	for _, version := range []string{"1.1.7", "1.2.0", "1.1.6"} {
		if err := os.MkdirAll(filepath.Join(root, "packages", "xxmi-libs", version), 0o700); err != nil {
			t.Fatal(err)
		}
	}

	versions, err := service.ListCachedLibs(ctx)
	if err != nil || len(versions) != 3 {
		t.Fatalf("cached libraries = %+v, err = %v", versions, err)
	}
	for _, version := range versions {
		if version.InUse != (version.Version == "1.2.0") || version.Referenced != (version.Version != "1.1.6") {
			t.Fatalf("cached library usage = %+v", version)
		}
	}
	removed, err := service.PruneLibsCache(ctx)
	if err != nil || len(removed) != 1 || removed[0] != "1.1.6" {
		t.Fatalf("pruned libraries = %v, err = %v", removed, err)
	}
	for _, version := range []string{"1.1.7", "1.2.0"} {
		if _, err := os.Stat(filepath.Join(root, "packages", "xxmi-libs", version)); err != nil {
			t.Fatalf("referenced version %s was removed: %v", version, err)
		}
	}
}

func TestPruneLibsCachePreservesLegacyImportersPinnedXXMIVersion(t *testing.T) {
	t.Setenv("USERPROFILE", t.TempDir())
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
	for _, pin := range []struct {
		key     string
		mode    RuntimeMode
		version string
	}{
		{"GIMI", RuntimeLegacy, "1.2.3"},
		{"WWMI", RuntimeXXMI, "1.3.0"},
	} {
		cfg, err := DefaultImporterConfig(pin.key, t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		cfg.Mode = pin.mode
		cfg.XXMIVersion = VersionPin{Pinned: pin.version}
		if err := service.SaveImporterConfig(ctx, pin.key, cfg); err != nil {
			t.Fatal(err)
		}
	}
	root, err := xxmiCacheRoot()
	if err != nil {
		t.Fatal(err)
	}
	parent := filepath.Join(root, "packages", "xxmi-libs")
	for _, version := range []string{"1.2.3", "1.3.0", "9.9.9"} {
		if err := os.MkdirAll(filepath.Join(parent, version), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	removed, err := service.PruneLibsCache(ctx)
	if err != nil || len(removed) != 1 || removed[0] != "9.9.9" {
		t.Fatalf("pruned versions = %v, err = %v", removed, err)
	}
	for _, version := range []string{"1.2.3", "1.3.0"} {
		if _, err := os.Stat(filepath.Join(parent, version)); err != nil {
			t.Fatalf("pinned version %s was removed: %v", version, err)
		}
	}
}

func TestLibsCacheReferencesLegacyExtraDLLInjector(t *testing.T) {
	t.Setenv("USERPROFILE", t.TempDir())
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
	cfg.Mode = RuntimeLegacy
	cfg.XXMIVersion = VersionPin{Pinned: "1.2.3"}
	cfg.ExtraLibraries.Enabled = true
	cfg.ExtraLibraries.Paths = []string{filepath.Join(t.TempDir(), "extra.dll")}
	if err := service.SaveImporterConfig(ctx, "GIMI", cfg); err != nil {
		t.Fatal(err)
	}
	_, referenced, err := service.libsCacheReferences(ctx)
	if err != nil || !referenced["1.2.3"].Referenced {
		t.Fatalf("references = %v, err = %v", referenced, err)
	}
}

func TestListCachedLibsUsesVersionOrder(t *testing.T) {
	t.Setenv("USERPROFILE", t.TempDir())
	ctx := context.Background()
	client, err := db.New(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	if err := client.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	root, err := xxmiCacheRoot()
	if err != nil {
		t.Fatal(err)
	}
	for _, version := range []string{"1.7.9", "1.7.10", "1.7.10-rc.1"} {
		if err := os.MkdirAll(filepath.Join(root, "packages", "xxmi-libs", version), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	service := New()
	service.UseClient(client)
	versions, err := service.ListCachedLibs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"1.7.10", "1.7.10-rc.1", "1.7.9"}
	if len(versions) != len(want) {
		t.Fatalf("cached library versions = %+v", versions)
	}
	for i, version := range versions {
		if version.Version != want[i] {
			t.Fatalf("cached library versions = %+v, want %v", versions, want)
		}
	}
}
