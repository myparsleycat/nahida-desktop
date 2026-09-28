package xxmi

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"nahida.live/desktop/internal/db"
)

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
	if err != nil || !referenced["1.2.3"] {
		t.Fatalf("references = %v, err = %v", referenced, err)
	}
}
