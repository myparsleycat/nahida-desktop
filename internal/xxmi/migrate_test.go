package xxmi

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"nahida.live/desktop/internal/db"
)

func TestImportExternalLauncherInstallsImporterIntoBuiltinRoot(t *testing.T) {
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
	source := filepath.Join(external, "GIMI")
	writeImportSourceFiles(t, source)
	config := xxmiTestConfig()
	config["Launcher"].(map[string]any)["pre_release"] = true
	packages := config["Packages"].(map[string]any)["packages"].(map[string]any)
	packages["GIMI"].(map[string]any)["skipped_version"] = "v1.3.0"
	packages["XXMI"] = xxmiTestPackage("v1.7.6")
	packages["XXMI"].(map[string]any)["skipped_version"] = "v1.7.6"
	packages["GI-FPS-Unlocker"] = xxmiTestPackage("v2.0.0")
	importers := config["Importers"].(map[string]any)
	gimi := importers["GIMI"].(map[string]any)["Importer"].(map[string]any)
	gimi["process_start_method"] = "Native"
	gimi["custom_launch_inject_mode"] = "Hook"
	gimi["game_folder"] = ""
	gimi["unlock_fps"] = true
	gimi["unlock_fps_value"] = 144
	gimi["overwrite_ini"] = false
	gimi["extra_libraries_enabled"] = true
	gimi["extra_libraries"] = filepath.Join("extensions", "sample.dll")
	data, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(external, xxmiConfigName)
	if err := os.WriteFile(configPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	target := filepath.Join(root, "GIMI")
	service := New()
	service.UseClient(client)
	installs := fakeImportInstaller(t, service, nil)
	staleShared := "1.0.0"
	if err := client.Settings.Upsert(ctx, sharedLibsVersionKey, &staleShared); err != nil {
		t.Fatal(err)
	}

	imported, err := service.ImportExternalLauncher(ctx, ImportExternalLauncherInput{
		Path: external, Root: root, UserData: ImportUserDataKeep,
	})
	if err != nil {
		t.Fatal(err)
	}
	if shared, err := client.Settings.GetValue(
		ctx,
		sharedLibsVersionKey,
	); err != nil ||
		shared != nil && *shared != "" {
		t.Fatalf("imported shared libraries version = %v, err = %v", shared, err)
	}
	want := []ImportedImporter{{Key: "GIMI", PreviousFolder: source, ImporterFolder: target}}
	if !reflect.DeepEqual(imported, want) {
		t.Fatalf("imported = %+v, want %+v", imported, want)
	}
	if !reflect.DeepEqual(*installs, []string{"GIMI@1.2.3"}) {
		t.Fatalf("installs = %q", *installs)
	}
	cfg, err := service.GetImporterConfig(ctx, "GIMI")
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Enabled || cfg.ImporterFolder != target || cfg.GIMI == nil ||
		!cfg.GIMI.UnlockFPS || cfg.GIMI.UnlockFPSValue != 144 {
		t.Fatalf("imported config = %+v", cfg)
	}
	if cfg.WindowMode != "Windowed" {
		t.Fatalf("imported window mode = %q", cfg.WindowMode)
	}
	if cfg.OverwriteINI {
		t.Fatal("imported overwrite_ini=false was not preserved")
	}
	if len(cfg.ExtraLibraries.Paths) != 1 ||
		cfg.ExtraLibraries.Paths[0] != filepath.Join(external, "extensions", "sample.dll") {
		t.Fatalf("imported extra libraries = %q", cfg.ExtraLibraries.Paths)
	}
	if cfg.PackageVersion != (VersionPin{Pinned: "1.2.3"}) {
		t.Fatalf("imported package selection = %+v", cfg.PackageVersion)
	}
	if cfg.XXMIVersion != (VersionPin{Follow: followShared}) {
		t.Fatalf("imported XXMI libraries selection = %+v", cfg.XXMIVersion)
	}

	// Kept user data stays in the external folder and is reached through a junction.
	if info, err := os.Lstat(filepath.Join(target, "Mods")); err != nil || !isInstallReparsePoint(info) {
		t.Fatalf("Mods is not linked: info = %v, err = %v", info, err)
	}
	if err := os.WriteFile(filepath.Join(target, "Mods", "new.ini"), []byte("new mod"), 0o600); err != nil {
		t.Fatal(err)
	}
	assertFile(t, filepath.Join(source, "Mods", "new.ini"), "new mod")
	if _, err := os.Lstat(filepath.Join(target, "ShaderFixes")); !os.IsNotExist(err) {
		t.Fatalf("ShaderFixes was carried over: %v", err)
	}
	assertFile(t, filepath.Join(source, "ShaderFixes", "fix.hlsl"), "user shader")
	assertFile(t, filepath.Join(target, "d3dx.ini"), "user ini")
	assertFile(t, filepath.Join(target, "d3dx_user.ini"), "user state")
	assertFile(t, filepath.Join(source, "d3dx_user.ini"), "user state")

	for id, want := range map[string]struct{ latest, skipped string }{
		"importer:GIMI":   {"1", "1.3.0"},
		"xxmi-libs":       {"1.7.6", "1.7.6"},
		"gi-fps-unlocker": {"2.0.0", ""},
	} {
		state, err := client.XXMIPackages.Get(ctx, id)
		if err != nil || state == nil {
			t.Fatalf("imported %s state = %+v, err = %v", id, state, err)
		}
		latest, skipped := "", ""
		if state.LatestVersion != nil {
			latest = *state.LatestVersion
		}
		if state.SkippedVersion != nil {
			skipped = *state.SkippedVersion
		}
		if latest != want.latest || skipped != want.skipped {
			t.Fatalf("imported %s versions = %q, %q; want %+v", id, latest, skipped, want)
		}
	}
	for key, want := range map[string]string{
		"xxmi_root": root, "xxmi_auto_update": "false", "xxmi_include_prereleases": "true",
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

func TestMapExternalImporterSettingsFixture(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(filepath.Join("testdata", "external_importer_settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixture map[string]map[string]any
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	cfg, err := DefaultImporterConfig("GIMI", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	want := cfg
	want.ProcessStartMethod = "Native"
	want.ProcessPriority = "High"
	want.WindowMode = "Fullscreen"
	want.ProcessTimeout = 45
	want.XXMIDLLInitDelay = 250
	want.UseLaunchOptions = false
	want.LaunchOptions = "--sample"
	want.ConfigureGame = false
	want.LaunchCount = 9
	want.RunPreLaunch = CommandHook{Enabled: true, Command: "pre-command", Wait: false}
	want.RunPostLoad = CommandHook{Enabled: true, Command: "post-command", Wait: false}
	want.GameLaunch = "Custom"
	want.XXMIDLLInjectMode = "Inject"
	want.CustomLaunch = CustomLaunch{Command: "custom-command"}
	want.ExtraLibraries = ExtraLibraries{Enabled: true, Paths: []string{`C:\DLLs\first.dll`, `C:\DLLs\second.dll`}}
	want.DeployedSignatures = map[string]string{"d3d11.dll": "sample-signature"}
	want.Migoto.LogLevel = "Debug"
	want.Migoto.EnforceRendering, want.Migoto.EnableHunting, want.Migoto.DumpShaders = false, true, true
	want.Migoto.MuteWarnings, want.Migoto.UnsafeMode = false, true
	want.GIMI = &GIMIOptions{UnlockFPS: true, UnlockFPSValue: 144, EnableHDR: true}

	if err := mapExternalImporterSettings(&cfg, fixture["Importer"], fixture["Migoto"]); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cfg, want) {
		t.Fatalf("imported settings = %+v; want %+v", cfg, want)
	}
}

// Settings XXMI Launcher 2.3 retired are dropped on import instead of rejected.
func TestMapExternalWWMIDropsRetiredGraphicsSettings(t *testing.T) {
	t.Parallel()
	cfg, err := DefaultImporterConfig("WWMI", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := mapExternalImporterSettings(&cfg, map[string]any{
		"deployed_migoto_signatures":     map[string]any{"d3d11.dll": "signed-by-external-launcher"},
		"mesh_lod_distance_lod_base_fov": 180.0,
		"mesh_lod_distance_scale":        0.75,
		"texture_streaming_boost":        12.5,
		"apply_perf_tweaks":              true,
		"perf_tweaks": map[string]any{"SystemSettings": map[string]any{
			"r.Streaming.HLODStrategy": 3.0,
		}},
		"process_start_method": "Manual",
	}, map[string]any{"calls_logging": true}); err != nil {
		t.Fatal(err)
	}
	want := WWMIOptions{MeshLODDistanceBaseFOV: 180, ResourceTier: "HD"}
	if *cfg.WWMI != want || cfg.DeployedSignatures["d3d11.dll"] != "signed-by-external-launcher" ||
		cfg.GameLaunch != "Manual" || cfg.ProcessStartMethod != "Native" || cfg.Migoto.LogLevel != "Info" {
		t.Fatalf("imported WWMI settings = %+v, %+v", cfg, *cfg.WWMI)
	}
	if err := ValidateImporterSettings("WWMI", cfg); err != nil {
		t.Fatal(err)
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
	if _, err := service.ImportExternalLauncher(ctx, ImportExternalLauncherInput{
		Path: external, Root: t.TempDir(), UserData: ImportUserDataKeep,
	}); err == nil ||
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
