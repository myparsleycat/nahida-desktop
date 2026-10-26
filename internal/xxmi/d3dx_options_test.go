package xxmi

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
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
			"input.TOGGLE_INPUT": "VK_F9",
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

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for range 2 {
		if err := service.updateLaunchINI(ctx, "GIMI", cfg, "Game.exe", provider); !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled INI write = %v; want context.Canceled", err)
		}
		if data, err := os.ReadFile(path); err != nil || string(data) != original {
			t.Fatalf("failed write changed the original INI: %q, err = %v", data, err)
		}
		root, err := xxmiCacheRoot()
		if err != nil {
			t.Fatal(err)
		}
		if entries, err := os.ReadDir(filepath.Join(root, "backups")); err != nil || len(entries) != 0 {
			t.Fatalf("failed write left backup folders: %v, err = %v", entries, err)
		}
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

	// A provider release that changes one of its defaults reaches the file, while the values the file chose stay.
	provider = []byte(strings.Replace(string(provider), "fresh = 1", "fresh = 3", 1))
	got = launch()
	for _, want := range []string{"texture_hash = 1\n", "fresh = 3\n", "old_only = 4\n", "toggle_input = VK_F8\n"} {
		if !strings.Contains(got, want) {
			t.Errorf("INI rebuilt from the next release is missing %q: %q", want, got)
		}
	}
	if saved := backups(); len(saved) != 1 {
		t.Fatalf("a changed provider default backed the file up again: %v", saved)
	}
	if base, err := os.ReadFile(filepath.Join(folder, iniBaseName)); err != nil || string(base) != string(provider) {
		t.Fatalf("recorded base = %q, err = %v; want the provider's d3dx.ini", base, err)
	}
}

func TestGetD3DXOptionsListsOptionsTheNextLaunchAdds(t *testing.T) {
	ctx := context.Background()
	image := testCustomDLLImage("0.2.0")
	ini := []byte("[Rendering]\nshader_regex_background = 0\ncache_shaders = 0\ntexture_hash = 1\nfuture_option = 3\n" +
		"[Logging]\nlog_level = disabled\n[TextureOverrideBody]\nhash = 1234abcd\n")
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
	// The libraries deployed now read log_level, and the provider's release the next launch deploys does not.
	writeTestFile(t, filepath.Join(cfg.ImporterFolder, runtimeManifestName),
		fmt.Appendf(nil, `{"mode":%q,"source":"xxmi-libs@1.2.0"}`, RuntimeXXMI))
	managed := map[string]D3DXOptionManaged{}
	listed := func() map[string]string {
		t.Helper()
		options, err := service.GetD3DXOptions(ctx, "GIMI")
		if err != nil {
			t.Fatal(err)
		}
		values := map[string]string{}
		for _, option := range options {
			values[option.Key], managed[option.Key] = option.Value, option.Managed
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
	if got["shader_regex_background"] != "0" || got["texture_hash"] != "1" || got["future_option"] != "3" ||
		got["log_level"] != "disabled" {
		t.Fatalf("options the next launch adds = %v", got)
	}
	if managed["log_level"] != D3DXOptionUnmanaged {
		t.Fatalf("log_level managed = %q, want the lock of the release the next launch deploys", managed["log_level"])
	}
	if _, ok := got["cache_shaders"]; ok {
		t.Fatalf("an option the file comments out was listed: %v", got)
	}
	if _, ok := got["hash"]; ok {
		t.Fatalf("a command section line was listed: %v", got)
	}
}

func TestSaveImporterConfigWritesSettingsToD3DXINI(t *testing.T) {
	ctx := context.Background()
	service, _, _ := newProviderTestService(t, nil)
	cfg, err := DefaultImporterConfig("GIMI", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(cfg.ImporterFolder, "d3dx.ini"),
		[]byte("[Hunting]\nhunting = 0\n[Rendering]\ncache_shaders = 0\n"))
	cfg.Enabled, cfg.XXMIVersion = true, VersionPin{Follow: "latest"}
	cfg.Migoto.EnableHunting = true
	cfg.D3DXOverrides = map[string]string{"Rendering.cache_shaders": "1"}
	if err := service.SaveImporterConfig(ctx, "GIMI", cfg); err != nil {
		t.Fatal(err)
	}

	options, err := service.GetD3DXOptions(ctx, "GIMI")
	if err != nil {
		t.Fatal(err)
	}
	values := map[string]string{}
	for _, option := range options {
		values[option.Key] = option.Value
	}
	if values["hunting"] != "2" || values["cache_shaders"] != "1" {
		t.Fatalf("options after saving = %v, want the saved settings without a launch", values)
	}
}

func TestSaveImporterConfigLateINIWriteKeepsNewerSave(t *testing.T) {
	ctx := context.Background()
	service, _, _ := newProviderTestService(t, nil)
	cfg, err := DefaultImporterConfig("GIMI", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(cfg.ImporterFolder, "d3dx.ini")
	writeTestFile(t, path, []byte("[Hunting]\nhunting = 0\n"))
	cfg.Enabled, cfg.XXMIVersion = true, VersionPin{Follow: "latest"}

	// The first save enables the importer, so it stops in the change notice after committing and before its
	// d3dx.ini write, while a second save commits and writes in full.
	reached, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	service.UseImportersChanged(func(context.Context) {
		once.Do(func() {
			close(reached)
			<-release
		})
	})
	older := cfg
	older.Migoto.EnableHunting = true
	done := make(chan error, 1)
	go func() { done <- service.SaveImporterConfig(ctx, "GIMI", older) }()
	<-reached
	if err := service.SaveImporterConfig(ctx, "GIMI", cfg); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}

	saved, err := service.GetImporterConfig(ctx, "GIMI")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Migoto.EnableHunting || !strings.Contains(string(data), "hunting = 0") {
		t.Fatalf("saved hunting = %t, d3dx.ini = %q; want the newer save in both", saved.Migoto.EnableHunting, data)
	}
}

func TestGetD3DXOptionsLocksOnlyOptionsTheDeployedDLLReads(t *testing.T) {
	ctx := context.Background()
	service, _, _ := newProviderTestService(t, nil)
	cfg, err := DefaultImporterConfig("GIMI", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg.Enabled, cfg.XXMIVersion = true, VersionPin{Follow: "latest"}
	if err := service.SaveImporterConfig(ctx, "GIMI", cfg); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(cfg.ImporterFolder, runtimeManifestName),
		fmt.Appendf(nil, `{"mode":%q,"source":"xxmi-libs@1.2.0"}`, RuntimeXXMI))
	writeTestFile(
		t,
		filepath.Join(cfg.ImporterFolder, "d3dx.ini"),
		[]byte(
			"[Logging]\nlog_level = disabled\ncalls = 0\n[Input]\ntoggle_input = VK_F8\n[Rendering]\ntexture_hash = 0\n",
		),
	)

	options, err := service.GetD3DXOptions(ctx, "GIMI")
	if err != nil {
		t.Fatal(err)
	}
	managed := map[string]D3DXOptionManaged{}
	for _, option := range options {
		managed[option.Key] = option.Managed
	}
	// A DLL that reads log_level ignores calls, which the settings therefore leave to the user, and the hotkey
	// setting only fills in a missing one.
	want := map[string]D3DXOptionManaged{
		"log_level": D3DXOptionManagedLaunch, "calls": D3DXOptionUnmanaged, "toggle_input": D3DXOptionUnmanaged,
		"texture_hash": D3DXOptionManagedRendering,
	}
	for key, value := range want {
		if managed[key] != value {
			t.Errorf("%s managed = %q, want %q (all: %v)", key, managed[key], value, managed)
		}
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
		"Device.get_resolution_from": "depth_stencil", "Rendering.future_option": "-1",
		// The settings write an option they manage after the override, which therefore has no effect.
		"Logging.log_level": "debug",
	}
	if err := ValidateImporterSettings("GIMI", cfg); err != nil {
		t.Fatalf("valid overrides rejected: %v", err)
	}

	for name, overrides := range map[string]map[string]string{
		"loader":          {"Loader.target": "evil.exe"},
		"command section": {"TextureOverrideBody.hash": "1234abcd"},
		"new line":        {"Hunting.reload_config": "VK_F5\r\n[Loader]\r\ntarget = evil.exe"},
		"comment":         {"Hunting.reload_config": "VK_F5 ; note"},
		"empty":           {"Rendering.cache_shaders": ""},
		"padded":          {"Rendering.cache_shaders": " 1"},
		"case duplicate":  {"Rendering.cache_shaders": "1", "rendering.CACHE_SHADERS": "0"},
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
