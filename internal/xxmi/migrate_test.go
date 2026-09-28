package xxmi

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"nahida.live/desktop/internal/db"
)

func TestImportExternalLauncherKeepsImporterFolderAndSourceConfig(t *testing.T) {
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
	external := t.TempDir()
	config := xxmiTestConfig()
	importers := config["Importers"].(map[string]any)
	gimi := importers["GIMI"].(map[string]any)["Importer"].(map[string]any)
	gimi["process_start_method"] = "Native"
	gimi["custom_launch_inject_mode"] = "Hook"
	gimi["game_folder"] = ""
	gimi["unlock_fps"] = true
	gimi["unlock_fps_value"] = 144
	data, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(external, xxmiConfigName)
	if err := os.WriteFile(configPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	service := New()
	service.UseClient(client)
	if err := service.ImportExternalLauncher(ctx, ImportExternalLauncherInput{Path: external}); err != nil {
		t.Fatal(err)
	}
	cfg, err := service.GetImporterConfig(ctx, "GIMI")
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Enabled || cfg.ImporterFolder != filepath.Join(external, "GIMI") || cfg.GIMI == nil ||
		!cfg.GIMI.UnlockFPS || cfg.GIMI.UnlockFPSValue != 144 {
		t.Fatalf("imported config = %+v", cfg)
	}
	if cfg.PackageVersion.Follow != "latest" {
		t.Fatalf("package pin = %+v", cfg.PackageVersion)
	}
	got, err := os.ReadFile(configPath)
	if err != nil || string(got) != string(data) {
		t.Fatalf("source config changed: %v", err)
	}
}
