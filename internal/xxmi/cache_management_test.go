package xxmi

import (
	"context"
	"path/filepath"
	"testing"

	"nahida.live/desktop/internal/db"
)

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
