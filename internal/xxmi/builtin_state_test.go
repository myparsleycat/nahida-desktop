package xxmi

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"nahida.live/desktop/internal/db"
)

func TestBuiltinStateFeedsExistingConsumers(t *testing.T) {
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
	root := t.TempDir()
	service := New()
	service.UseClient(client)
	if err := service.SetRoot(ctx, root); err != nil {
		t.Fatal(err)
	}
	importerFolder := filepath.Join(root, "GIMI")
	if err := os.MkdirAll(filepath.Join(importerFolder, "Core", "GIMI"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(importerFolder, "Core", "GIMI", "main.ini"),
		[]byte("global $version = 1.23\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	if err := service.EnableImporter(ctx, "GIMI", importerFolder); err != nil {
		t.Fatal(err)
	}
	importers, err := service.GetEnabledImporters(ctx)
	if err != nil || len(importers) != 1 || importers[0].InstalledVersion == nil ||
		*importers[0].InstalledVersion != "1.2.3" {
		t.Fatalf("enabled importers = %+v, err = %v", importers, err)
	}
	path, err := service.GetXXMIPath(ctx)
	if err != nil || path == nil || *path != root {
		t.Fatalf("XXMI root = %v, err = %v", path, err)
	}
	state, err := service.GetXXMIData(ctx)
	if err != nil || state.XXMIPath == nil || *state.XXMIPath != root || len(state.EnabledImporters) != 1 {
		t.Fatalf("XXMI data = %+v, err = %v", state, err)
	}
	hunting, err := service.ResolveHuntingRuntime(ctx, "GIMI")
	if err != nil || hunting.ImporterFolder != importerFolder || len(hunting.GameEXENames) == 0 {
		t.Fatalf("hunting runtime = %+v, err = %v", hunting, err)
	}
}
