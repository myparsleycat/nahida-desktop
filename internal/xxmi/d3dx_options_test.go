package xxmi

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUpdateLaunchINIAppliesOverridesOnlyToOptionsTheFileSets(t *testing.T) {
	t.Parallel()
	folder := t.TempDir()
	path := filepath.Join(folder, "d3dx.ini")
	original := "[Hunting]\r\nhunting = 0\r\nreload_config = no_modifiers VK_F10 ; reload\r\n\r\n" +
		"[Device]\r\n;refresh_rate=60\r\nfull_screen = 0\r\n\r\n" +
		"[Rendering]\r\ncache_shaders = 0\r\ntexture_hash = 0\r\n"
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	service := NewWithOptions(Options{Elevated: stubLaunchHelper{}})
	cfg := ImporterConfig{ImporterFolder: folder, Mode: RuntimeXXMI,
		Migoto: MigotoOptions{LogLevel: "Disabled", EnforceRendering: true, EnableHunting: true},
		D3DXOverrides: map[string]string{
			"Rendering.cache_shaders": "1", "Hunting.reload_config": "no_modifiers VK_F5",
			"Device.refresh_rate": "144", "Device.hide_cursor": "1",
			"Rendering.texture_hash": "1", "Hunting.hunting": "0",
		}}
	if err := service.updateLaunchINI(context.Background(), "GIMI", cfg, "Game.exe", nil); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)

	for _, want := range []string{
		"cache_shaders = 1\r\n", "reload_config = no_modifiers VK_F5 ; reload\r\n", ";refresh_rate=60\r\n",
		// The launch settings win over an override of an option they manage.
		"texture_hash = 0\r\n", "hunting = 2\r\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("updated INI is missing %q: %q", want, got)
		}
	}
	for _, unwanted := range []string{"refresh_rate = 144", "refresh_rate=144", "hide_cursor"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("updated INI gained %q, which the file did not set: %q", unwanted, got)
		}
	}
}

func TestUpdateLaunchINIAppliesOverridesToOptionsTheProviderAdds(t *testing.T) {
	t.Parallel()
	folder := t.TempDir()
	path := filepath.Join(folder, "d3dx.ini")
	original := "[Rendering]\n;cache_shaders = 0\ntexture_hash = 1\n"
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	// The launch writes its own toggle_input default only for libraries that read log_level.
	manifest := fmt.Sprintf(`{"mode":%q,"source":"xxmi-libs@1.2.0"}`, RuntimeXXMI)
	if err := os.WriteFile(filepath.Join(folder, runtimeManifestName), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	service := NewWithOptions(Options{Elevated: stubLaunchHelper{}})
	cfg := ImporterConfig{ImporterFolder: folder, Mode: RuntimeXXMI,
		Migoto: MigotoOptions{LogLevel: "Disabled", EnforceRendering: true, ToggleInput: "VK_F8"},
		D3DXOverrides: map[string]string{
			"Rendering.prefetch_resource_files": "0", "Rendering.cache_shaders": "1", "Rendering.texture_hash": "1",
			"Input.toggle_input": "VK_F9",
		}}
	provider := []byte("[Rendering]\nprefetch_resource_files = 1\ncache_shaders = 0\ntexture_hash = 1\n" +
		"[Input]\ntoggle_input = VK_F10\n")
	if err := service.updateLaunchINI(context.Background(), "GIMI", cfg, "Game.exe", provider); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)

	for _, want := range []string{
		"prefetch_resource_files = 0\n", ";cache_shaders = 0\n", "texture_hash = 0\n", "toggle_input = VK_F9\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("updated INI is missing %q: %q", want, got)
		}
	}
	if strings.Contains(got, "cache_shaders = 1") {
		t.Errorf("override enabled an option the file comments out: %q", got)
	}
}

func TestUpdateLaunchINIRebuildsFromProviderINI(t *testing.T) {
	t.Setenv("USERPROFILE", t.TempDir())
	folder := t.TempDir()
	path := filepath.Join(folder, "d3dx.ini")
	original := "; my note\n[Rendering]\ntexture_hash = 1\nold_only = 4\n"
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest := fmt.Sprintf(`{"mode":%q,"source":"xxmi-libs@1.2.0"}`, RuntimeXXMI)
	if err := os.WriteFile(filepath.Join(folder, runtimeManifestName), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	service := NewWithOptions(Options{Elevated: stubLaunchHelper{}})
	cfg := ImporterConfig{ImporterFolder: folder, Mode: RuntimeXXMI,
		Migoto: MigotoOptions{LogLevel: "Disabled", ToggleInput: "VK_F8"}}
	provider := []byte("; template\n[Rendering]\n; doc\ntexture_hash = 0\nfresh = 1\n" +
		"[Input]\ntoggle_input = VK_F10\n")
	launch := func() string {
		t.Helper()
		if err := service.updateLaunchINI(context.Background(), "GIMI", cfg, "Game.exe", provider); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	backups := func() []string {
		t.Helper()
		root, err := xxmiCacheRoot()
		if err != nil {
			t.Fatal(err)
		}
		found, err := filepath.Glob(filepath.Join(root, "backups", "*", "d3dx.ini"))
		if err != nil {
			t.Fatal(err)
		}
		return found
	}

	got := launch()
	if !strings.HasPrefix(got, "; template\n") {
		t.Fatalf("d3dx.ini was not rebuilt from the provider's: %q", got)
	}
	for _, want := range []string{"texture_hash = 1\n", "fresh = 1\n", "old_only = 4\n", "toggle_input = VK_F8\n"} {
		if !strings.Contains(got, want) {
			t.Errorf("rebuilt INI is missing %q: %q", want, got)
		}
	}
	for _, unwanted := range []string{"VK_F10", "; my note"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("rebuilt INI holds %q: %q", unwanted, got)
		}
	}
	saved := backups()
	if len(saved) != 1 {
		t.Fatalf("backups of the replaced d3dx.ini = %v", saved)
	}
	if data, err := os.ReadFile(saved[0]); err != nil || string(data) != original {
		t.Fatalf("backup = %q, err = %v", data, err)
	}

	// Nothing of the rebuilt file is dropped by rebuilding it, so later launches leave it and its backup alone.
	if again := launch(); again != got {
		t.Fatalf("second launch changed the INI\n got %q\nwant %q", again, got)
	}
	if saved := backups(); len(saved) != 1 {
		t.Fatalf("second launch backed the file up again: %v", saved)
	}
}

func TestGetD3DXOptionsListsOptionsTheNextLaunchAdds(t *testing.T) {
	ctx := context.Background()
	image := testCustomDLLImage("0.2.0")
	ini := []byte("[Rendering]\nshader_regex_background = 0\ncache_shaders = 0\ntexture_hash = 1\n")
	service, _, _ := newProviderTestService(t, []providerTestRelease{{
		tag: "v0.2.0", data: image, digest: providerTestDigest(image), ini: ini, iniDigest: providerTestDigest(ini),
	}})
	cfg, err := DefaultImporterConfig("GIMI", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg.Enabled, cfg.XXMIVersion = true, VersionPin{Follow: "latest"}
	if err := service.SaveImporterConfig(ctx, "GIMI", cfg); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(cfg.ImporterFolder, "d3dx.ini"), []byte("[Rendering]\n;cache_shaders = 0\n"))
	listed := func() map[string]string {
		t.Helper()
		options, err := service.GetD3DXOptions(ctx, "GIMI")
		if err != nil {
			t.Fatal(err)
		}
		values := map[string]string{}
		for _, option := range options {
			values[option.Key] = option.Value
		}
		return values
	}

	if got := listed(); len(got) != 0 {
		t.Fatalf("options without a provider d3dx.ini = %v", got)
	}
	if err := service.SetSharedLibsProvider(ctx, "myparsleycat"); err != nil {
		t.Fatal(err)
	}
	got := listed()
	if got["shader_regex_background"] != "0" || got["texture_hash"] != "1" {
		t.Fatalf("options the next launch adds = %v", got)
	}
	if _, ok := got["cache_shaders"]; ok {
		t.Fatalf("an option the file comments out was listed: %v", got)
	}
}

func TestImporterSettingsRejectUnsafeD3DXOverrides(t *testing.T) {
	t.Parallel()
	cfg, err := DefaultImporterConfig("GIMI", filepath.Join(t.TempDir(), "xxmi"))
	if err != nil {
		t.Fatal(err)
	}
	cfg.D3DXOverrides = map[string]string{
		"Rendering.cache_shaders": "1", "Hunting.reload_config": "no_modifiers VK_F5",
		"Hunting.analyse_options": "dump_rt dump_tex buf txt", "Hunting.monitor_performance_interval": "2.5",
		"Device.get_resolution_from": "depth_stencil", "System.settings_auto_save_interval": "-1",
		"Rendering.recursive_include": "-1",
	}
	if err := ValidateImporterSettings("GIMI", cfg); err != nil {
		t.Fatalf("valid overrides rejected: %v", err)
	}

	for name, overrides := range map[string]map[string]string{
		"unknown option":   {"Loader.target": "evil.exe"},
		"launch managed":   {"Logging.log_level": "debug"},
		"new line":         {"Hunting.reload_config": "VK_F5\r\n[Loader]\r\ntarget = evil.exe"},
		"comment":          {"Hunting.reload_config": "VK_F5 ; note"},
		"bool out of set":  {"Rendering.cache_shaders": "yes"},
		"int out of range": {"System.settings_auto_save_interval": "-2"},
		"unknown enum":     {"Device.get_resolution_from": "window"},
		"unknown flag":     {"Hunting.analyse_options": "dump_rt everything"},
		"not a number":     {"Hunting.monitor_performance_interval": "NaN"},
	} {
		cfg.D3DXOverrides = overrides
		if err := ValidateImporterSettings("GIMI", cfg); err == nil {
			t.Errorf("%s override accepted: %v", name, overrides)
		}
	}
}

func TestINIOptionDropsTrailingComment(t *testing.T) {
	t.Parallel()
	doc := parseINI([]byte("[Hunting]\n;hunting = 1\nhunting = 2 ; soft disabled\nrepeat_rate=6\n"))
	for key, want := range map[string]string{"hunting": "2", "repeat_rate": "6"} {
		if got, ok := doc.Option("hunting", key); !ok || got != want {
			t.Errorf("Option(%q) = %q, %v; want %q", key, got, ok, want)
		}
	}
	if _, ok := doc.Option("Hunting", "marking_mode"); ok {
		t.Error("missing option reported as set")
	}
}
