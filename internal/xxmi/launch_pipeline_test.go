package xxmi

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"nahida.live/desktop/internal/db"
	"nahida.live/desktop/internal/xxmi/inject"
)

type recordingLaunchHelper struct {
	spec  inject.LaunchSpec
	calls int
}

func (*recordingLaunchHelper) Acquire(context.Context) (func(), error) { return func() {}, nil }

func (h *recordingLaunchHelper) LaunchXXMI(_ context.Context, spec inject.LaunchSpec) (inject.LaunchResult, error) {
	h.spec = spec
	h.calls++
	return inject.LaunchResult{PID: 42, InjectionVerified: true}, nil
}

func (*recordingLaunchHelper) HelperImageName() string { return "nahida-elevated-helper-test.exe" }

func TestLegacyLaunchPipelineWithTemporaryRuntime(t *testing.T) {
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
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
	cfg, err := DefaultImporterConfig("EFMI", root)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Enabled = true
	cfg.Mode = RuntimeLegacy
	cfg.LegacyRuntime = "abcdef123456"
	cfg.GameFolder = filepath.Join(root, "game")
	cfg.ConfigureGame = false
	cfg.IniOptimizer.Enabled = true
	for _, folder := range []string{cfg.ImporterFolder, cfg.GameFolder} {
		if err := os.MkdirAll(folder, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(cfg.ImporterFolder, "d3dx.ini"), []byte("[Loader]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gameExe := filepath.Join(cfg.GameFolder, "Endfield.exe")
	if err := os.WriteFile(gameExe, []byte("test game"), 0o600); err != nil {
		t.Fatal(err)
	}
	cacheRoot, err := xxmiCacheRoot()
	if err != nil {
		t.Fatal(err)
	}
	legacy := filepath.Join(cacheRoot, "packages", "legacy-3dmigoto", cfg.LegacyRuntime)
	if err := os.MkdirAll(legacy, 0o700); err != nil {
		t.Fatal(err)
	}
	contents := map[string]string{"3DMigoto Loader.exe": "loader", "d3d11.dll": "legacy DLL"}
	source := LegacyRuntimeSource{ZipSHA256: "test zip", Files: map[string]string{}}
	for name, content := range contents {
		if err := os.WriteFile(filepath.Join(legacy, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		source.Files[name] = hashBytes([]byte(content))
	}
	sourceData, err := json.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, "source.json"), sourceData, 0o600); err != nil {
		t.Fatal(err)
	}
	helper := &recordingLaunchHelper{}
	var stages []string
	service := NewWithOptions(Options{Elevated: helper, EventEmit: func(name string, data ...any) {
		if name == "xxmi:launch-progress" {
			stages = append(stages, data[0].(map[string]any)["stage"].(string))
		}
	}})
	service.UseClient(client)
	if err := service.SaveImporterConfig(ctx, "EFMI", cfg); err != nil {
		t.Fatal(err)
	}
	disabled := "false"
	if err := client.Settings.Upsert(ctx, "xxmi_auto_update", &disabled); err != nil {
		t.Fatal(err)
	}
	if err := service.StartGame(ctx, "EFMI"); err != nil {
		t.Fatal(err)
	}
	want := []string{"resolve-game", "auto-update", "launch-guard", "xcmd-prelaunch", "ensure-runtime",
		"deploy-runtime", "validate-runtime", "update-ini", "ini-optimizer", "ini-optimizer", "game-tweaks",
		"pre-launch", "elevate",
		"inject-launch", "post-load", "finish"}
	if !slices.Equal(stages, want) {
		t.Fatalf("launch stages = %v; want %v", stages, want)
	}
	if helper.calls != 1 || helper.spec.Mode != inject.ModeLegacy || helper.spec.StartExe != gameExe ||
		helper.spec.LegacyLoader.Path != filepath.Join(cfg.ImporterFolder, "3DMigoto Loader.exe") {
		t.Fatalf("helper calls = %d, spec = %+v", helper.calls, helper.spec)
	}
	stored, err := service.GetImporterConfig(ctx, "EFMI")
	if err != nil || stored.LaunchCount != 0 {
		t.Fatalf("stored launch count = %d, err = %v", stored.LaunchCount, err)
	}
}
