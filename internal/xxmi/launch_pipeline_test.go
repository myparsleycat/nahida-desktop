package xxmi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/sys/windows"

	"nahida.live/desktop/internal/db"
	"nahida.live/desktop/internal/elevated"
	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/xxmi/inject"
)

type recordingLaunchHelper struct {
	spec       inject.LaunchSpec
	calls      int
	warnings   []string
	leases     int
	released   int
	applyFiles func(context.Context, []elevated.FileOp) error
}

func (h *recordingLaunchHelper) Acquire(context.Context) (func(), error) {
	h.leases++
	return func() { h.released++ }, nil
}

func (h *recordingLaunchHelper) LaunchXXMI(_ context.Context, spec inject.LaunchSpec) (inject.LaunchResult, error) {
	h.spec = spec
	h.calls++
	return inject.LaunchResult{PID: 42, InjectionVerified: true, Warnings: h.warnings}, nil
}

func (h *recordingLaunchHelper) ApplyFiles(ctx context.Context, ops []elevated.FileOp) error {
	if h.applyFiles != nil {
		return h.applyFiles(ctx, ops)
	}
	return nil
}

func (*recordingLaunchHelper) HelperImageName() string { return "nahida-elevated-helper-test.exe" }

func TestLegacyLaunchPipelineWithTemporaryRuntime(t *testing.T) {
	for _, restore := range []string{"unused", "restored", "failed"} {
		t.Run(restore, func(t *testing.T) { testLegacyLaunchPipeline(t, restore) })
	}
}

func testLegacyLaunchPipeline(t *testing.T, restore string) {
	t.Helper()
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
	gamesExe := filepath.Join(root, "1.6.0", "Games.exe")
	if err := os.MkdirAll(filepath.Dir(gamesExe), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{filepath.Join(root, "Launcher.exe"), gamesExe} {
		if err := os.WriteFile(file, nil, 0o600); err != nil {
			t.Fatal(err)
		}
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
	locked := filepath.Join(cfg.ImporterFolder, "Mods", "locked.ini")
	if restore != "unused" {
		if err := os.MkdirAll(filepath.Dir(locked), 0o700); err != nil {
			t.Fatal(err)
		}
		for name, content := range map[string]string{"locked.ini": "[TextureOverrideTest]\n", "rogue.ini": "[Loader]\n"} {
			if err := os.WriteFile(filepath.Join(filepath.Dir(locked), name), []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		user, err := windows.GetCurrentProcessToken().GetTokenUser()
		if err != nil {
			t.Fatal(err)
		}
		sid := "*" + user.User.Sid.String()
		if output, err := exec.Command("icacls", locked, "/deny", sid+":(RD)").CombinedOutput(); err != nil {
			t.Fatalf("deny fixture access: %v (%s)", err, output)
		}
		t.Cleanup(func() { _ = exec.Command("icacls", locked, "/remove:d", sid).Run() })
		helper.applyFiles = func(_ context.Context, ops []elevated.FileOp) error {
			if len(ops) != 1 || ops[0].Kind != elevated.FileOpResetAccess {
				t.Fatalf("unexpected restore operations: %v", ops)
			}
			if restore == "failed" {
				return errors.New("fixture access restore rejected")
			}
			output, err := exec.Command("icacls", locked, "/remove:d", sid).CombinedOutput()
			if err != nil {
				t.Fatalf("restore fixture access: %v (%s)", err, output)
			}
			return nil
		}
	}
	var logs bytes.Buffer
	log := infra.NewLogWithOptions(infra.LogOptions{Writer: &logs, DisableFile: true})
	optimized := 0
	var stages []string
	var warnings []string
	service := NewWithOptions(Options{Elevated: helper, Log: log, EventEmit: func(name string, data ...any) {
		if name == "xxmi:launch-progress" {
			payload := data[0].(map[string]any)
			stages = append(stages, payload["stage"].(string))
			if warning, ok := payload["warning"].(string); ok {
				warnings = append(warnings, warning)
			}
			if count, ok := payload["optimized"].(int); ok {
				optimized = count
			}
		}
	}})
	service.UseClient(client)
	service.launchSettings = &fakeLaunch{}
	service.findProcess = noGameProcess
	service.gryphlinkFolders = func() []string { return nil }
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
	if restore == "failed" {
		want = slices.Insert(want, 11, "ini-optimizer")
	}
	if !slices.Equal(stages, want) {
		t.Fatalf("launch stages = %v; want %v", stages, want)
	}
	if restore == "failed" {
		if len(warnings) != 2 || !strings.Contains(warnings[0], locked) || warnings[1] != helper.warnings[0] {
			t.Fatalf("launch warnings = %v, want skipped file and helper warning", warnings)
		}
		if !strings.Contains(logs.String(), "fixture access restore rejected") ||
			!strings.Contains(logs.String(), "restore-importer-access") {
			t.Fatalf("restore failure lost its cause: %s", logs.String())
		}
	} else if !slices.Equal(warnings, helper.warnings) {
		t.Fatalf("launch warnings = %v; want %v", warnings, helper.warnings)
	}
	if restore != "unused" && optimized != 1 {
		t.Fatalf("optimized = %d, want the change applied before the access restore", optimized)
	}
	if helper.calls != 1 || helper.spec.Mode != inject.ModeLegacy || helper.spec.StartExe != gameExe ||
		helper.spec.ParentExe != gamesExe || helper.spec.LegacyLoader.Path != filepath.Join(cfg.ImporterFolder, "3DMigoto Loader.exe") {
		t.Fatalf("helper calls = %d, spec = %+v", helper.calls, helper.spec)
	}
	if helper.leases != 1 || helper.released != 1 {
		t.Fatalf(
			"helper leases = %d, released = %d; want one lease held for the launch",
			helper.leases,
			helper.released,
		)
	}
	stored, err := service.GetImporterConfig(ctx, "EFMI")
	if err != nil || stored.LaunchCount != 1 {
		t.Fatalf("stored launch count = %d, err = %v", stored.LaunchCount, err)
	}
}
