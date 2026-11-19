package xxmi

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLaunchOptionsKeepSmoothMotionWithoutBypassingOtherGuards(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	root := t.TempDir()
	config := xxmiTestConfig()
	migoto := config["Importers"].(map[string]any)["GIMI"].(map[string]any)["Migoto"].(map[string]any)
	migoto["log_level"] = "INFO"
	if err := writeXXMIConfig(filepath.Join(root, xxmiConfigName), config); err != nil {
		t.Fatal(err)
	}
	service := New()
	service.UseClient(newXXMITestClient(t))
	useExternalLauncher(t, service, root)
	settings := &fakeLaunch{smooth: true, dcr: true}
	service.launchSettings = settings
	options := LaunchOptions{IgnoreSmoothMotion: true}
	if err := service.StartGameWithOptions(ctx, "GIMI", options); !errors.Is(err, errLoggingEnabled) {
		t.Fatalf("ignoring Smooth Motion bypassed the logging guard: %v", err)
	}
	options.KeepLogging = true
	if err := service.StartGameWithOptions(ctx, "GIMI", options); !errors.Is(err, errGimiDCREnabled) ||
		errors.Is(err, errSmoothMotionEnabled) {
		t.Fatalf("ignoring Smooth Motion should still ask about DCR: %v", err)
	}
	if err := service.ClearLaunchBlockersWithOptions(ctx, "GIMI", options); err != nil {
		t.Fatal(err)
	}
	if settings.dcr || settings.dcrDisabled != 1 || settings.smoothDisabled != 0 || settings.smoothReads != 0 {
		t.Fatalf("clearing DCR with Smooth Motion ignored changed unexpected settings: %+v", settings)
	}
	err := service.StartGameWithOptions(ctx, "GIMI", options)
	if err == nil || !strings.Contains(err.Error(), "XXMI Launcher not found at") {
		t.Fatalf("expected to reach the missing fixture launcher after the guards: %v", err)
	}
	if err := service.StartGameUnattended(ctx, "GIMI"); !errors.Is(err, errSmoothMotionEnabled) {
		t.Fatalf("the next launch should check Smooth Motion again: %v", err)
	}
	guards, err := service.launchGuards(ctx)
	if err != nil || !guards.smoothMotion || !settings.smooth || settings.smoothDisabled != 0 {
		t.Fatalf(
			"one-launch override changed persistent settings: guards=%+v, settings=%+v, error=%v",
			guards,
			settings,
			err,
		)
	}
}

func TestConfiguredGameExecutableUsesAbsoluteGamePath(t *testing.T) {
	t.Parallel()
	folder := t.TempDir()
	second := filepath.Join(folder, "GameBeta.exe")
	if err := os.WriteFile(second, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	got := configuredGameExecutable(folder, []string{"Game.exe", "GameBeta.exe"})
	if got != second {
		t.Fatalf("configuredGameExecutable = %q, want %q", got, second)
	}
}

func TestConfiguredGameExecutableFallsBackToBasenameWithoutAbsoluteFolder(t *testing.T) {
	t.Parallel()
	if got := configuredGameExecutable("", []string{"Game.exe"}); got != "" {
		t.Fatalf("configuredGameExecutable = %q, want empty", got)
	}
}

func TestStartGameAsksAboutShaderCacheUnlessAnswered(t *testing.T) {
	ctx := t.Context()
	client := newXXMITestClient(t)
	service := New()
	service.UseClient(client)
	service.findProcess = noGameProcess
	useBuiltinLauncher(t, service)
	store := func(key, value string) {
		t.Helper()
		if err := client.Settings.Upsert(ctx, key, &value); err != nil {
			t.Fatal(err)
		}
	}
	store("xxmi_root", t.TempDir())
	// The Epic DirectX 11 notice stops the launch before it touches the host, past the shader cache check.
	if err := client.XXMIImporters.Upsert(ctx, "EFMI",
		`{"schemaVersion":2,"enabled":true,"gameLaunch":"Epic"}`); err != nil {
		t.Fatal(err)
	}
	cfg, err := service.GetImporterConfig(ctx, "EFMI")
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(cfg.ImporterFolder, "d3dx.ini"), []byte("[Rendering]\ncache_shaders = 0\n"))

	if err := service.StartGame(ctx, "EFMI"); !errors.Is(err, errShaderCacheDisabled) {
		t.Fatalf("StartGame = %v, want the shader cache question", err)
	}
	for name, start := range map[string]func() error{
		"unattended": func() error { return service.StartGameUnattended(ctx, "EFMI") },
		"answered": func() error {
			return service.StartGameWithOptions(ctx, "EFMI", LaunchOptions{KeepShaderCacheOff: true})
		},
	} {
		if err := start(); !errors.Is(err, errD3D11ModeNoticeRequired) {
			t.Errorf("%s launch = %v, want it past the shader cache check", name, err)
		}
	}
	store(launchGuardShaderCacheKey, "false")
	if err := service.StartGame(ctx, "EFMI"); !errors.Is(err, errD3D11ModeNoticeRequired) {
		t.Fatalf("StartGame with the guard off = %v, want it past the shader cache check", err)
	}
}
