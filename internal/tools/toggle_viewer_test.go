package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nahida.live/desktop/internal/db"
	"nahida.live/desktop/internal/setting"
	"nahida.live/desktop/internal/xxmi"
)

func TestToggleViewerBatchGenerateHotkeyAndDelete(t *testing.T) {
	ctx := context.Background()
	client := openToolsTestDB(t)
	launcher := t.TempDir()
	writeToggleViewerXXMIConfig(t, launcher)
	modsRoot := filepath.Join(launcher, "GIMI", "mods")
	modRoot := filepath.Join(modsRoot, "Character")
	if err := os.MkdirAll(modRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	modINI := strings.Join([]string{
		"[KeySwap]", "type = cycle", "key = ctrl H", "$swapvar = 0, 1", "",
		"[TextureOverrideBodyPosition]", "hash = 1234abcd",
	}, "\n")
	targetINI := filepath.Join(modRoot, "mod.ini")
	if err := os.WriteFile(targetINI, []byte(modINI), 0o600); err != nil {
		t.Fatal(err)
	}
	xxmiService := xxmi.New()
	xxmiService.UseClient(client)
	if err := xxmiService.SaveXXMIPath(ctx, launcher); err != nil {
		t.Fatal(err)
	}
	settings := setting.New(client)
	service := NewWithOptions(Options{Settings: settings, XXMI: xxmiService})
	service.UseClient(client)
	useToolsTestAppData(t, service, t.TempDir())

	if err := service.ToggleViewerRunBatchGenerate(ctx); err != nil {
		t.Fatal(err)
	}
	toggleINIPath := filepath.Join(modRoot, "toggle-viewer.ini")
	toggleTXTPath := filepath.Join(modRoot, "toggle-viewer.txt")
	generated, err := os.ReadFile(toggleINIPath)
	if err != nil || !strings.Contains(string(generated), "hash = 1234abcd") || !strings.Contains(string(generated), "key = ctrl H") {
		t.Fatalf("generated ini = %q, %v", generated, err)
	}
	if _, err := os.Stat(toggleTXTPath); err != nil {
		t.Fatal(err)
	}
	records, err := client.ToggleViewerArtifacts.List(ctx)
	if err != nil || len(records) != 1 || records[0].TargetIniPath != targetINI {
		t.Fatalf("records = %#v, %v", records, err)
	}
	if err := service.ToggleViewerApplyHotkeyToArtifacts(ctx, "alt J"); err != nil {
		t.Fatal(err)
	}
	updated, err := os.ReadFile(toggleINIPath)
	if err != nil || !strings.Contains(string(updated), "key = alt J") {
		t.Fatalf("updated ini = %q, %v", updated, err)
	}
	if err := service.ToggleViewerRunBatchDelete(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(toggleINIPath); !os.IsNotExist(err) {
		t.Fatalf("toggle ini remains: %v", err)
	}
	if _, err := os.Stat(toggleTXTPath); !os.IsNotExist(err) {
		t.Fatalf("toggle txt remains: %v", err)
	}
	records, err = client.ToggleViewerArtifacts.List(ctx)
	if err != nil || len(records) != 0 {
		t.Fatalf("records after delete = %#v, %v", records, err)
	}
}

func writeToggleViewerXXMIConfig(t *testing.T, root string) {
	t.Helper()
	importers := map[string]any{}
	for _, name := range []string{"GIMI", "SRMI", "WWMI", "ZZMI", "EFMI", "HIMI"} {
		importer := toggleViewerXXMIBaseImporter(name)
		switch name {
		case "GIMI":
			importer["unlock_fps"], importer["disable_dcr"], importer["enable_hdr"] = false, false, false
		case "WWMI":
			importer["apply_perf_tweaks"] = false
			importer["perf_tweaks"] = map[string]any{"SystemSettings": map[string]any{}}
			importer["mesh_lod_distance_scale"], importer["mesh_lod_distance_offset"] = 1, 0
			importer["texture_streaming_boost"], importer["texture_streaming_min_boost"] = 1, 0
			importer["texture_streaming_use_all_mips"], importer["texture_streaming_pool_size"] = false, 0
			importer["texture_streaming_limit_to_vram"], importer["texture_streaming_fixed_pool_size"] = false, false
		case "HIMI":
			importer["unlock_fps"], importer["unlock_fps_value"] = false, 60
			importer["disable_dcr"], importer["enable_hdr"] = false, false
		}
		importers[name] = map[string]any{
			"Importer": importer,
			"Migoto": map[string]any{
				"enforce_rendering": false, "enable_hunting": false, "dump_shaders": false,
				"mute_warnings": false, "calls_logging": false, "debug_logging": false,
				"unsafe_mode": false, "unsafe_mode_signature": "",
			},
		}
	}
	config := map[string]any{
		"Launcher": map[string]any{
			"auto_update": false, "pre_release": false, "update_channel": "stable", "auto_close": false,
			"start_timeout": 60, "gui_theme": "default", "theme_mode": "system", "active_importer": "GIMI",
			"enabled_importers": []string{"GIMI"}, "log_level": "INFO", "config_version": "1",
			"theme_dev_mode": false, "github_token": "", "verify_ssl": true, "credits_shown": true, "locale": "en_US",
			"proxy": map[string]any{
				"enable": false, "type": "http", "host": "", "port": "", "use_credentials": false,
				"user": "", "password": "", "proxy_dns_via_socks5": false,
			},
		},
		"Packages": map[string]any{"packages": map[string]any{
			"GIMI": map[string]any{
				"latest_version": "v1", "skipped_version": "", "deployed_version": "v1",
				"update_check_time": 0, "latest_release_notes": "", "deployed_release_notes": "",
			},
		}},
		"Importers": importers,
		"Security":  map[string]any{"user_signature": ""},
	}
	raw, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "XXMI Launcher Config.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

func toggleViewerXXMIBaseImporter(name string) map[string]any {
	return map[string]any{
		"game_exe_names": []string{name + ".exe"}, "game_folder_names": []string{},
		"game_folder_children": []string{}, "package_name": name, "importer_folder": name,
		"game_folder": "", "use_launch_options": false, "overwrite_ini": false,
		"process_start_method": "direct", "xxmi_dll_init_delay": 0, "process_priority": "Normal",
		"window_mode": "windowed", "run_pre_launch_enabled": false, "run_pre_launch": "",
		"run_pre_launch_signature": "", "run_pre_launch_wait": false, "custom_launch_enabled": false,
		"custom_launch": "", "custom_launch_signature": "", "custom_launch_inject_mode": "",
		"run_post_load_enabled": false, "run_post_load": "", "run_post_load_signature": "",
		"run_post_load_wait": false, "extra_libraries_enabled": false, "extra_libraries": "",
		"extra_libraries_signature": "", "deployed_migoto_signatures": map[string]string{},
		"shortcut_deployed": false, "configure_game": false, "launch_count": 0, "launch_options": "",
		"d3dx_ini": map[string]any{
			"core":              map[string]any{"Loader": map[string]any{"loader": "d3d11.dll"}},
			"enforce_rendering": map[string]any{"Rendering": map[string]any{"texture_hash": 0, "track_texture_updates": 0}},
			"calls_logging":     map[string]any{"Logging": map[string]any{"calls": map[string]any{"on": 1, "off": 0}}},
			"debug_logging":     map[string]any{"Logging": map[string]any{"debug": map[string]any{"on": 1, "off": 0}}},
			"mute_warnings":     map[string]any{"Logging": map[string]any{"show_warnings": map[string]any{"on": 1, "off": 0}}},
			"enable_hunting":    map[string]any{"Hunting": map[string]any{"hunting": map[string]any{"on": 1, "off": 0}}},
			"dump_shaders":      map[string]any{"Hunting": map[string]any{"marking_actions": map[string]any{"on": "mark", "off": "no_mark"}}},
		},
	}
}

func TestToggleViewerScanRemovesStaleManagedArtifacts(t *testing.T) {
	ctx := context.Background()
	client := openToolsTestDB(t)
	root := t.TempDir()
	targetINI := filepath.Join(root, "removed.ini")
	toggleINI := filepath.Join(root, "toggle-viewer.ini")
	toggleTXT := filepath.Join(root, "toggle-viewer.txt")
	if err := os.WriteFile(toggleINI, []byte("managed"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(toggleTXT, []byte("managed"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := client.ToggleViewerArtifacts.Upsert(ctx, dbToggleArtifact("stale", targetINI, toggleTXT, toggleINI)); err != nil {
		t.Fatal(err)
	}
	service := New()
	service.UseClient(client)
	if err := service.deleteStaleToggleViewerRecords(ctx, map[string]bool{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(toggleINI); !os.IsNotExist(err) {
		t.Fatalf("managed ini remains: %v", err)
	}
}

func dbToggleArtifact(id, target, txt, ini string) db.ToggleViewerArtifactRow {
	return db.ToggleViewerArtifactRow{
		ID: id, TargetIniPath: target, ToggleTxtPath: txt, ToggleIniPath: ini,
		ToggleTxtHash: "txt", ToggleIniHash: "ini", UpdatedAt: "now",
	}
}
