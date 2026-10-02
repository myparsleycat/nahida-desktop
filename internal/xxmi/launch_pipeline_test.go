package xxmi

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"nahida.live/desktop/internal/db"
	"nahida.live/desktop/internal/xxmi/inject"
)

type recordingLaunchHelper struct {
	spec     inject.LaunchSpec
	calls    int
	warnings []string
}

func (*recordingLaunchHelper) Acquire(context.Context) (func(), error) { return func() {}, nil }

func (h *recordingLaunchHelper) LaunchXXMI(_ context.Context, spec inject.LaunchSpec) (inject.LaunchResult, error) {
	h.spec = spec
	h.calls++
	return inject.LaunchResult{PID: 42, InjectionVerified: true, Warnings: h.warnings}, nil
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
	helper := &recordingLaunchHelper{warnings: []string{"Could not verify legacy DLL in game process"}}
	var stages []string
	var warnings []string
	service := NewWithOptions(Options{Elevated: helper, EventEmit: func(name string, data ...any) {
		if name == "xxmi:launch-progress" {
			payload := data[0].(map[string]any)
			stages = append(stages, payload["stage"].(string))
			if warning, ok := payload["warning"].(string); ok {
				warnings = append(warnings, warning)
			}
		}
	}})
	service.UseClient(client)
	useBuiltinLauncher(t, service)
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
	want := []string{"pre-launch", "resolve-game", "auto-update", "launch-guard", "xcmd-prelaunch",
		"ensure-runtime", "deploy-runtime", "validate-runtime", "update-ini", "ini-optimizer", "ini-optimizer",
		"game-tweaks", "elevate", "inject-launch", "inject-launch", "post-load", "finish"}
	if !slices.Equal(stages, want) {
		t.Fatalf("launch stages = %v; want %v", stages, want)
	}
	if !slices.Equal(warnings, helper.warnings) {
		t.Fatalf("launch warnings = %v; want %v", warnings, helper.warnings)
	}
	if helper.calls != 1 || helper.spec.Mode != inject.ModeLegacy || helper.spec.StartExe != gameExe ||
		helper.spec.LegacyLoader.Path != filepath.Join(cfg.ImporterFolder, "3DMigoto Loader.exe") {
		t.Fatalf("helper calls = %d, spec = %+v", helper.calls, helper.spec)
	}
	stored, err := service.GetImporterConfig(ctx, "EFMI")
	if err != nil || stored.LaunchCount != 1 {
		t.Fatalf("stored launch count = %d, err = %v", stored.LaunchCount, err)
	}

	// Commands and runtime preparation run after the initial namespace check.
	// A newly discovered incomplete transaction must still prevent injection.
	blocked := errors.New("unfinished namespace transaction after pre-launch")
	preparations := 0
	service.UseNamespaceLaunchPreparation(func(context.Context, string) error {
		preparations++
		if preparations == 2 {
			return blocked
		}
		return nil
	})
	if err := service.StartGame(ctx, "EFMI"); !errors.Is(err, blocked) {
		t.Fatalf("final namespace check = %v", err)
	}
	if preparations != 2 || helper.calls != 1 {
		t.Fatalf("preparations = %d, injections = %d", preparations, helper.calls)
	}
}
