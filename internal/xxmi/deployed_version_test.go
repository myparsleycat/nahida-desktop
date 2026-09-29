package xxmi

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"nahida.live/desktop/internal/db"
)

func TestDeployedLibsVersionUsesImporterManifest(t *testing.T) {
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
	service := New()
	service.UseClient(client)
	useBuiltinLauncher(t, service)
	if err := service.EnableImporter(ctx, "GIMI", folder); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, runtimeManifestName),
		[]byte(`{"mode":"xxmi","source":"xxmi-libs@1.7.6","files":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	version, ok := service.DeployedLibsVersion(ctx, "GIMI")
	if !ok || version != "1.7.6" {
		t.Fatalf("deployed version = %q, %t", version, ok)
	}
	cfg, err := service.GetImporterConfig(ctx, "GIMI")
	if err != nil {
		t.Fatal(err)
	}
	cfg.Mode = RuntimeLegacy
	if err := service.SaveImporterConfig(ctx, "GIMI", cfg); err != nil {
		t.Fatal(err)
	}
	if version, ok := service.DeployedLibsVersion(ctx, "GIMI"); ok || version != "" {
		t.Fatalf("legacy version = %q, %t", version, ok)
	}
}
