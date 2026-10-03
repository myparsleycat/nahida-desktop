package xxmi

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// This fixture uses the defaults from SpectrumQT/XXMI-Launcher v2.3.8,
// including fresh d3dx_ini objects without the removed logging overrides.
func modernXXMITestConfig(t *testing.T) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "external_launcher_2_3_8.json"))
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatal(err)
	}
	return config
}

func TestExternalConfigSupportsBothFormatsWithoutRewriting(t *testing.T) {
	t.Parallel()
	for _, modern := range []bool{false, true} {
		name := "legacy"
		if modern {
			name = "modern"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			config := xxmiTestConfig()
			if modern {
				config = modernXXMITestConfig(t)
			}
			raw, err := json.Marshal(config)
			if err != nil {
				t.Fatal(err)
			}
			decoded, parsed, err := parseAndValidateConfig(raw)
			if err != nil || parsed.Importers["GIMI"].Importer.ImporterFolder == "" {
				t.Fatalf("parse config: %+v, %v", parsed, err)
			}
			var original map[string]any
			if err := json.Unmarshal(raw, &original); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(decoded, original) {
				t.Fatal("reading config changed the external schema")
			}
		})
	}
}

func TestModernExternalConfigRejectsMalformedFields(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		section string
		field   string
		value   any
	}{
		{"missing game folder", "Importer", "game_folder", nil},
		{"missing game launch", "Importer", "game_launch", nil},
		{"missing start method", "Importer", "start_method", nil},
		{"invalid start method", "Importer", "start_method", "OPTION_REMOVED"},
		{"invalid game launch", "Importer", "game_launch", "UNKNOWN"},
		{"invalid injection mode", "Importer", "xxmi_dll_inject_mode", false},
		{"missing log level", "Migoto", "log_level", nil},
		{"invalid log level", "Migoto", "log_level", "TRACE"},
		{"invalid obsolete boolean", "Migoto", "calls_logging", "false"},
		{"invalid obsolete performance field", "Importer", "apply_perf_tweaks", "false"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			config := modernXXMITestConfig(t)
			wrapper := config["Importers"].(map[string]any)["WWMI"].(map[string]any)
			section := wrapper[tc.section].(map[string]any)
			if tc.value == nil {
				delete(section, tc.field)
			} else {
				section[tc.field] = tc.value
			}
			if err := validateXXMIConfig(config); err == nil {
				t.Fatal("malformed config was accepted")
			}
		})
	}
}

func TestModernExternalConfigRetainsAndValidatesLegacyD3DXOverrides(t *testing.T) {
	t.Parallel()
	config := modernXXMITestConfig(t)
	wrapper := config["Importers"].(map[string]any)["WWMI"].(map[string]any)
	ini := wrapper["Importer"].(map[string]any)["d3dx_ini"].(map[string]any)
	legacy := xxmiTestBaseImporter("WWMI")["d3dx_ini"].(map[string]any)
	for _, name := range []string{"calls_logging", "debug_logging", "mute_warnings"} {
		ini[name] = legacy[name]
	}
	raw, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := parseAndValidateConfig(raw); err != nil {
		t.Fatal(err)
	}
	ini["calls_logging"] = map[string]any{}
	if err := validateXXMIConfig(config); err == nil {
		t.Fatal("malformed retained d3dx override was accepted")
	}
}

func TestModernExternalLauncherServesDataAndPreservesSource(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "Launcher with spaces")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(modernXXMITestConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, xxmiConfigName)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	x := New()
	x.UseClient(newXXMITestClient(t))
	useExternalLauncher(t, x, root)
	detected, err := x.DetectExternalLauncher(ctx)
	if err != nil || detected == nil || len(detected.Importers) != 2 {
		t.Fatalf("detected = %+v, %v", detected, err)
	}
	data, err := x.GetXXMIData(ctx)
	if err != nil || len(data.EnabledImporters) != 2 {
		t.Fatalf("data = %+v, %v", data, err)
	}
	if err := x.SetExternalImporterEnabled(ctx, "WWMI", false); err != nil {
		t.Fatal(err)
	}
	data, err = x.GetXXMIData(ctx)
	if err != nil || len(data.EnabledImporters) != 1 || len(data.DisabledImporters) != 1 {
		t.Fatalf("disabled data = %+v, %v", data, err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != string(raw) {
		t.Fatalf("source config changed: %v", err)
	}
}

func TestMapModernExternalLaunchSettings(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		launch string
		mode   string
		start  string
		custom bool
	}{
		{"DIRECT", "HOOK", "Shell", false},
		{"CUSTOM", "DIRECT", "Shell", true},
		{"MANUAL", "SKIP", "Manual", false},
	} {
		t.Run(tc.launch, func(t *testing.T) {
			t.Parallel()
			wrapper := modernXXMITestConfig(t)["Importers"].(map[string]any)["GIMI"].(map[string]any)
			importer := wrapper["Importer"].(map[string]any)
			importer["game_launch"], importer["start_method"], importer["xxmi_dll_inject_mode"] = tc.launch, "SHELL", tc.mode
			importer["process_priority"], importer["window_mode"] = "ABOVE_NORMAL", "EXCLUSIVE_FULLSCREEN"
			importer["custom_launch"], importer["custom_launch_enabled"] = "custom-command", true
			migoto := wrapper["Migoto"].(map[string]any)
			migoto["log_level"] = "INFO"
			cfg, err := DefaultImporterConfig("GIMI", t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			if err := mapExternalImporterSettings(&cfg, importer, migoto); err != nil {
				t.Fatal(err)
			}
			wantMode := map[string]string{"HOOK": "Hook", "DIRECT": "Inject", "SKIP": "Bypass"}[tc.mode]
			if cfg.ProcessStartMethod != tc.start || cfg.ProcessPriority != "AboveNormal" ||
				cfg.WindowMode != "Exclusive Fullscreen" || cfg.CustomLaunch.Enabled != tc.custom ||
				cfg.XXMIDLLInjectMode != wantMode || cfg.CustomLaunch.InjectMode != wantMode || cfg.Migoto.LogLevel != "INFO" {
				t.Fatalf("mapped config = %+v", cfg)
			}
			if err := ValidateImporterSettings("GIMI", cfg); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestImportModernExternalLauncherPersistsSettings(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	external := t.TempDir()
	config := modernXXMITestConfig(t)
	delete(config["Packages"].(map[string]any)["packages"].(map[string]any), "WWMI")
	importer := config["Importers"].(map[string]any)["GIMI"].(map[string]any)["Importer"].(map[string]any)
	importer["start_method"], importer["process_priority"] = "SHELL", "BELOW_NORMAL"
	raw, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(external, xxmiConfigName)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	writeImportSourceFiles(t, filepath.Join(external, "GIMI"))
	x := New()
	x.UseClient(newXXMITestClient(t))
	fakeImportInstaller(t, x, nil)
	if _, err := x.ImportExternalLauncher(ctx, ImportExternalLauncherInput{
		Path: external, Root: t.TempDir(), UserData: ImportUserDataKeep,
	}); err != nil {
		t.Fatal(err)
	}
	cfg, err := x.GetImporterConfig(ctx, "GIMI")
	if err != nil || cfg.ProcessStartMethod != "Shell" || cfg.ProcessPriority != "BelowNormal" ||
		cfg.XXMIDLLInjectMode != "Hook" || cfg.Migoto.LogLevel != "DISABLED" {
		t.Fatalf("persisted config = %+v, %v", cfg, err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != string(raw) {
		t.Fatalf("source config changed: %v", err)
	}
}

func TestModernPlatformLaunchRemainsExternal(t *testing.T) {
	t.Parallel()
	for _, launch := range []string{"STEAM", "EPIC_GAMES"} {
		t.Run(launch, func(t *testing.T) {
			t.Parallel()
			config := modernXXMITestConfig(t)
			wrapper := config["Importers"].(map[string]any)["GIMI"].(map[string]any)
			importer := wrapper["Importer"].(map[string]any)
			importer["game_launch"] = launch
			if err := validateXXMIConfig(config); err != nil {
				t.Fatal(err)
			}
			cfg, err := DefaultImporterConfig("GIMI", t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			if err := mapExternalImporterSettings(
				&cfg,
				importer,
				wrapper["Migoto"].(map[string]any),
			); err == nil ||
				!strings.Contains(err.Error(), "requires the external XXMI Launcher") {
				t.Fatalf("platform launch import error = %v", err)
			}
		})
	}
}

func TestExternalImporterProcessResolution(t *testing.T) {
	t.Parallel()
	folder := filepath.Join(t.TempDir(), "Game with spaces")
	if err := os.MkdirAll(folder, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, "YuanShen.exe"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	genshin := []string{"GenshinImpact.exe", "YuanShen.exe"}
	for _, tc := range []struct {
		name       string
		importer   externalImporter
		process    string
		names      []string
		executable string
	}{
		{
			name:     "legacy config without a game folder uses the first game executable",
			importer: externalImporter{GameEXENames: genshin},
			process:  "GenshinImpact.exe", names: genshin,
		},
		{
			name:     "direct launch follows the executable present in the game folder",
			importer: externalImporter{GameEXENames: genshin, GameFolder: folder, GameLaunch: "DIRECT"},
			process:  "YuanShen.exe", names: genshin, executable: filepath.Join(folder, "YuanShen.exe"),
		},
		{
			name: "process executables win over game executables",
			importer: externalImporter{
				GameEXENames: []string{"Wuthering Waves.exe"}, ProcessEXENames: []string{"Client-Win64-Shipping.exe"},
			},
			process: "Client-Win64-Shipping.exe", names: []string{"Client-Win64-Shipping.exe"},
		},
		{
			name: "enabled override wins",
			importer: externalImporter{
				GameEXENames: genshin, ProcessEXENames: []string{"Other.exe"}, GameFolder: folder,
				GameProcessEXEEnabled: true, GameProcessEXE: " Custom.exe ",
			},
			process: "Custom.exe", names: []string{"Custom.exe"}, executable: filepath.Join(folder, "YuanShen.exe"),
		},
		{
			name: "disabled override is ignored",
			importer: externalImporter{
				GameEXENames: genshin, GameProcessEXE: "Custom.exe",
			},
			process: "GenshinImpact.exe", names: genshin,
		},
		{
			name:     "platform launch ignores the game folder",
			importer: externalImporter{GameEXENames: genshin, GameFolder: folder, GameLaunch: "STEAM"},
			process:  "GenshinImpact.exe", names: genshin,
		},
		{name: "unconfigured importer has no process", importer: externalImporter{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.importer.processName(); got != tc.process {
				t.Fatalf("process name = %q; want %q", got, tc.process)
			}
			if got := tc.importer.processNames(); !reflect.DeepEqual(got, tc.names) && len(got)+len(tc.names) > 0 {
				t.Fatalf("process names = %q; want %q", got, tc.names)
			}
			if got := tc.importer.gameExecutable(); got != tc.executable {
				t.Fatalf("game executable = %q; want %q", got, tc.executable)
			}
		})
	}
}

func TestModernExternalLauncherResolvesHuntingProcess(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	root := t.TempDir()
	config := modernXXMITestConfig(t)
	importer := config["Importers"].(map[string]any)["GIMI"].(map[string]any)["Importer"].(map[string]any)
	importer["game_process_exe_enabled"], importer["game_process_exe"] = true, "Custom.exe"
	raw, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, xxmiConfigName), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	x := New()
	x.UseClient(newXXMITestClient(t))
	useExternalLauncher(t, x, root)

	runtime, err := x.ResolveHuntingRuntime(ctx, "GIMI")
	if err != nil || !reflect.DeepEqual(runtime.GameEXENames, []string{"Custom.exe"}) {
		t.Fatalf("GIMI hunting runtime = %+v, %v", runtime, err)
	}
	runtime, err = x.ResolveHuntingRuntime(ctx, "WWMI")
	if err != nil || !reflect.DeepEqual(runtime.GameEXENames, []string{"Client-Win64-Shipping.exe"}) {
		t.Fatalf("WWMI hunting runtime = %+v, %v", runtime, err)
	}
}

// A config written by 2.2 and then loaded by 2.3 keeps its old d3dx overrides, and one that
// 2.3 has not saved yet still spells enum values the old way.
func TestUpgradedExternalConfigMapsLegacyEnumSpellings(t *testing.T) {
	t.Parallel()
	wrapper := xxmiTestConfig()["Importers"].(map[string]any)["GIMI"].(map[string]any)
	importer := wrapper["Importer"].(map[string]any)
	importer["process_priority"], importer["window_mode"] = "Above Normal", "Exclusive Fullscreen"
	importer["process_start_method"] = "Shell"
	cfg, err := DefaultImporterConfig("GIMI", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := mapExternalImporterSettings(&cfg, importer, wrapper["Migoto"].(map[string]any)); err != nil {
		t.Fatal(err)
	}
	if cfg.ProcessPriority != "AboveNormal" || cfg.WindowMode != "Exclusive Fullscreen" ||
		cfg.ProcessStartMethod != "Shell" {
		t.Fatalf("mapped config = %+v", cfg)
	}
	if err := ValidateImporterSettings("GIMI", cfg); err != nil {
		t.Fatal(err)
	}
}

func TestModernExternalConfigRejectsMistypedOptionalFields(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		section string
		field   string
		value   any
	}{
		{"Importer", "process_exe_names", "Client-Win64-Shipping.exe"},
		{"Importer", "game_process_exe_enabled", "true"},
		{"Importer", "game_process_exe", false},
		{"Migoto", "input", "1"},
		{"Migoto", "input_disable_mode", 1},
		{"Migoto", "clear_unknown_settings", "yes"},
	} {
		t.Run(tc.section+"."+tc.field, func(t *testing.T) {
			t.Parallel()
			config := modernXXMITestConfig(t)
			wrapper := config["Importers"].(map[string]any)["WWMI"].(map[string]any)
			wrapper[tc.section].(map[string]any)[tc.field] = tc.value
			if err := validateXXMIConfig(config); err == nil {
				t.Fatal("mistyped field was accepted")
			}
		})
	}
}
