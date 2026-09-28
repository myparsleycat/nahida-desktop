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
	config["Launcher"].(map[string]any)["pre_release"] = true
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
	for key, want := range map[string]string{
		"xxmi_root": external, "xxmi_auto_update": "false", "xxmi_include_prereleases": "true",
	} {
		value, err := client.Settings.GetValue(ctx, key)
		if err != nil || value == nil || *value != want {
			t.Fatalf("imported %s = %v, err = %v; want %q", key, value, err, want)
		}
	}
	got, err := os.ReadFile(configPath)
	if err != nil || string(got) != string(data) {
		t.Fatalf("source config changed: %v", err)
	}
}

func TestMapExternalWWMIGraphicsSettings(t *testing.T) {
	t.Parallel()
	cfg, err := DefaultImporterConfig("WWMI", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	mapExternalImporterSettings(&cfg, map[string]any{
		"mesh_lod_distance_lod_base_fov":    180.0,
		"mesh_lod_distance_scale":           0.75,
		"mesh_lod_distance_offset":          -8.5,
		"texture_streaming_boost":           12.5,
		"texture_streaming_min_boost":       1.25,
		"texture_streaming_use_all_mips":    false,
		"texture_streaming_pool_size":       1024.0,
		"texture_streaming_limit_to_vram":   false,
		"texture_streaming_fixed_pool_size": false,
		"perf_tweaks": map[string]any{"SystemSettings": map[string]any{
			"r.Streaming.HLODStrategy":                         3.0,
			"wp.Runtime.KuroRuntimeStreamingRangeOverallScale": 0.75,
		}},
	}, nil)
	got := cfg.WWMI
	if got.MeshLODDistanceBaseFOV != 180 || got.MeshLODDistanceScale != 0.75 ||
		got.MeshLODDistanceOffset != -8.5 || got.TextureStreamingBoost != 12.5 ||
		got.TextureStreamingMinBoost != 1.25 || got.TextureStreamingUseAll ||
		got.TextureStreamingPoolSize != 1024 || got.TextureStreamingLimitVRAM || got.TextureStreamingFixedPool ||
		len(got.PerfTweaks) != 2 || got.PerfTweaks["r.Streaming.HLODStrategy"] != 3 ||
		got.PerfTweaks["wp.Runtime.KuroRuntimeStreamingRangeOverallScale"] != 0.75 {
		t.Fatalf("imported WWMI settings = %+v", got)
	}
}

func TestImportExternalLauncherValidatesAllImportersBeforeSaving(t *testing.T) {
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
	importers["GIMI"].(map[string]any)["Importer"].(map[string]any)["process_start_method"] = "Native"
	importers["SRMI"].(map[string]any)["Importer"].(map[string]any)["process_start_method"] = "invalid"
	config["Packages"].(map[string]any)["packages"].(map[string]any)["SRMI"] = xxmiTestPackage("v1")
	data, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(external, xxmiConfigName), data, 0o600); err != nil {
		t.Fatal(err)
	}
	service := New()
	service.UseClient(client)
	if err := service.ImportExternalLauncher(ctx, ImportExternalLauncherInput{Path: external}); err == nil ||
		!strings.Contains(err.Error(), "import SRMI config") {
		t.Fatalf("invalid later importer result = %v", err)
	}
	if row, err := client.XXMIImporters.Get(ctx, "GIMI"); err != nil || row != nil {
		t.Fatalf("earlier importer was partially saved: row = %+v, err = %v", row, err)
	}
	if value, err := client.Settings.GetValue(ctx, "xxmi_root"); err != nil || value != nil {
		t.Fatalf("root was partially saved: value = %v, err = %v", value, err)
	}
}
