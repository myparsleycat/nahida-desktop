package xxmi

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"nahida.live/desktop/internal/db"
	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/xxmi/inject"
)

type stubLaunchHelper struct{}

func (stubLaunchHelper) Acquire(context.Context) (func(), error) { return func() {}, nil }

func (stubLaunchHelper) LaunchXXMI(context.Context, inject.LaunchSpec) (inject.LaunchResult, error) {
	return inject.LaunchResult{}, nil
}

func (stubLaunchHelper) HelperImageName() string { return "nahida-elevated-helper-test.exe" }

func TestImporterLaunchLockAllowsOtherImporters(t *testing.T) {
	t.Parallel()
	service := New()
	if !service.acquireImporter("GIMI") {
		t.Fatal("first GIMI launch was rejected")
	}
	if service.acquireImporter("GIMI") {
		t.Fatal("second GIMI launch was accepted")
	}
	if err := service.StartGame(
		context.Background(),
		"GIMI",
	); err == nil ||
		!strings.Contains(err.Error(), "XXMI_BUSY") {
		t.Fatalf("public GIMI launch did not respect the importer lock: %v", err)
	}
	if !service.acquireImporter("SRMI") {
		t.Fatal("SRMI launch was blocked by GIMI")
	}
	service.releaseImporter("GIMI")
	if !service.acquireImporter("GIMI") {
		t.Fatal("GIMI remained locked after release")
	}
	service.releaseImporter("GIMI")
	service.releaseImporter("SRMI")
}

func TestImporterInjectionDefaults(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		key  string
		mode string
	}{
		{"GIMI", "Hook"}, {"SRMI", "Hook"}, {"HIMI", "Hook"}, {"ZZMI", "Hook"},
		{"WWMI", "Inject"}, {"EFMI", "Inject"},
	} {
		cfg, err := DefaultImporterConfig(tc.key, t.TempDir())
		if err != nil || cfg.XXMIDLLInjectMode != tc.mode {
			t.Errorf("%s: inject mode = %q, %v", tc.key, cfg.XXMIDLLInjectMode, err)
		}
	}
}

func TestLaunchReportsGameResolutionFailure(t *testing.T) {
	root := t.TempDir()
	cfg, err := DefaultImporterConfig("GIMI", root)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Enabled = true
	if err := os.MkdirAll(cfg.ImporterFolder, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg.ImporterFolder, "d3dx.ini"), []byte("[Loader]"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stages []string
	var output bytes.Buffer
	log := infra.NewLogWithOptions(infra.LogOptions{Writer: &output, DisableFile: true})
	service := NewWithOptions(Options{Log: log, EventEmit: func(name string, data ...any) {
		if name == "xxmi:launch-progress" {
			stages = append(stages, data[0].(map[string]any)["stage"].(string))
		}
	}})
	err = service.launchBuiltinGameLocked(context.Background(), "GIMI", cfg)
	if err == nil || !strings.Contains(err.Error(), "XXMI_GAME_FOLDER_NOT_CONFIGURED") {
		t.Fatalf("launch error = %v", err)
	}
	if !slices.Equal(stages, []string{"pre-launch", "resolve-game", "failed"}) {
		t.Fatalf("launch stages = %v", stages)
	}
	for _, expected := range []string{
		`"stage":"resolve-game"`, `"source":"xxmi-libs@latest"`, `"rollback":"not-started"`,
		`"gameFolder":""`,
	} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("launch diagnostic is missing %s: %s", expected, output.String())
		}
	}
}

func TestUpdateLaunchINIPreservesUserContentAndSetsHelper(t *testing.T) {
	folder := t.TempDir()
	path := filepath.Join(folder, "d3dx.ini")
	original := []byte("\xef\xbb\xbf; user comment\r\n[Loader]\r\ntarget = old.exe\r\nlaunch = \r\ncustom = keep\r\n")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	service := NewWithOptions(Options{Elevated: stubLaunchHelper{}})
	cfg := ImporterConfig{ImporterFolder: folder, Mode: RuntimeXXMI,
		Migoto: MigotoOptions{LogLevel: "Info", EnforceRendering: true, EnableHunting: true, MuteWarnings: true}}
	if err := service.updateLaunchINI(context.Background(), "GIMI", cfg, "Game.exe"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"; user comment\r\n", "target = Game.exe\r\n", "custom = keep\r\n",
		"loader = nahida-elevated-helper-test.exe\r\n",
		"texture_hash = 0\r\n", "hunting = 2\r\n", "show_warnings = 0\r\n",
		"log_level = info\r\n", "calls = 1\r\n", "debug = 0\r\n"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("updated INI is missing %q: %q", want, data)
		}
	}
	if strings.Contains(string(data), "launch =") {
		t.Errorf("updated INI keeps an empty launch option: %q", data)
	}
}

func TestApplyMigotoINIUsesImporterRenderingProfile(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		want    []string
		missing []string
	}{
		{
			name: "GIMI",
			want: []string{
				"texture_hash = 0",
				"track_texture_updates = 0",
				"track_region_hashes = 0",
				"allow_buffer_resize = 1",
			},
			missing: []string{"track_implicit_index_buffers"},
		},
		{
			name: "SRMI",
			want: []string{
				"texture_hash = 0",
				"track_texture_updates = 0",
				"track_region_hashes = 0",
				"track_implicit_index_buffers = 1",
				"allow_buffer_resize = 1",
			},
		},
		{
			name: "ZZMI",
			want: []string{
				"texture_hash = 0",
				"track_texture_updates = 0",
				"track_region_hashes = 0",
				"allow_buffer_resize = 1",
			},
			missing: []string{"track_implicit_index_buffers"},
		},
		{
			name: "HIMI",
			want: []string{
				"texture_hash = 0",
				"track_texture_updates = 0",
				"track_region_hashes = 0",
				"allow_buffer_resize = 1",
			},
			missing: []string{"track_implicit_index_buffers"},
		},
		{
			name: "WWMI",
			want: []string{
				"texture_hash = 1",
				"track_texture_updates = 1",
				"track_region_hashes = 0",
				"allow_buffer_resize = 1",
			},
			missing: []string{"track_implicit_index_buffers"},
		},
		{
			name: "EFMI",
			want: []string{
				"texture_hash = 0",
				"track_texture_updates = 0",
				"track_region_hashes = 1",
				"track_implicit_index_buffers = 1",
				"allow_buffer_resize = 0",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc := parseINI(nil)
			applyMigotoINI(doc, tc.name, MigotoOptions{EnforceRendering: true})
			data := string(doc.Bytes())
			for _, option := range tc.want {
				if !strings.Contains(data, option) {
					t.Errorf("%s INI is missing %q: %q", tc.name, option, data)
				}
			}
			for _, option := range tc.missing {
				if strings.Contains(data, option) {
					t.Errorf("%s INI unexpectedly contains %q: %q", tc.name, option, data)
				}
			}
		})
	}
}

func TestSplitLaunchOptionsKeepsQuotedArgument(t *testing.T) {
	t.Parallel()
	args, err := splitLaunchOptions(`-screen-width 1920 -data "folder with spaces"`)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"-screen-width", "1920", "-data", "folder with spaces"}
	if len(args) != len(want) {
		t.Fatalf("args = %#v", args)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Fatalf("args = %#v", args)
		}
	}
}

func TestWWMILaunchTargetFollowsLaunchOptions(t *testing.T) {
	folder := t.TempDir()
	game := filepath.Join(folder, "Wuthering Waves")
	wrapper := filepath.Join(game, "Wuthering Waves.exe")
	client := filepath.Join(game, "Client", "Binaries", "Win64", "Client-Win64-Shipping.exe")
	if err := os.MkdirAll(filepath.Dir(client), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{wrapper, client, filepath.Join(folder, "3DMigoto Loader.exe")} {
		if err := os.WriteFile(path, []byte("test"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(folder, runtimeManifestName), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, direct := range []bool{false, true} {
		cfg := ImporterConfig{
			ImporterFolder: folder, Mode: RuntimeLegacy, UseLaunchOptions: direct,
			ProcessStartMethod: "Native", XXMIDLLInjectMode: "Inject",
		}
		spec, err := New().builtinLaunchSpec(context.Background(), "WWMI", cfg, wrapper, "Client-Win64-Shipping.exe")
		if err != nil {
			t.Fatal(err)
		}
		want := wrapper
		if direct {
			want = client
		}
		if spec.StartExe != want || spec.WorkDir != filepath.Dir(want) || len(spec.StartArgs) != 1 ||
			spec.StartArgs[0] != "-dx11" || spec.UseHook || spec.InjectMode != "Inject" {
			t.Fatalf("direct=%t, spec=%+v", direct, spec)
		}
	}
}

func TestLegacyBypassLaunchSpecDoesNotRequireLoader(t *testing.T) {
	root := t.TempDir()
	gameExe := filepath.Join(root, "game.exe")
	if err := os.WriteFile(gameExe, []byte("test"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, runtimeManifestName), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := ImporterConfig{
		ImporterFolder: root, Mode: RuntimeLegacy, GameLaunch: "Custom", XXMIDLLInjectMode: "Bypass",
		CustomLaunch: CustomLaunch{Command: "start game"},
	}
	spec, err := New().builtinLaunchSpec(context.Background(), "GIMI", cfg, gameExe, "game.exe")
	if err != nil {
		t.Fatal(err)
	}
	if spec.InjectMode != "Bypass" || spec.LegacyLoader.Path != "" || spec.CustomLaunchCmd != "start game" {
		t.Fatalf("legacy bypass spec = %+v", spec)
	}
}

func TestInjectionModeControlsLaunchAndDLLUsage(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"Hook", "Inject", "Bypass"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			for _, name := range []string{runtimeManifestName, "game.exe", "3DMigoto Loader.exe"} {
				if err := os.WriteFile(filepath.Join(root, name), []byte("{}"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			cfg, err := DefaultImporterConfig("WWMI", root)
			if err != nil {
				t.Fatal(err)
			}
			cfg.Mode, cfg.ImporterFolder, cfg.XXMIDLLInjectMode = RuntimeLegacy, root, mode
			x := New()
			spec, err := x.builtinLaunchSpec(
				context.Background(),
				"WWMI",
				cfg,
				filepath.Join(root, "game.exe"),
				"game.exe",
			)
			if err != nil || spec.InjectMode != mode || spec.UseHook != (mode == "Hook") {
				t.Fatalf("launch spec = %+v, %v", spec, err)
			}
			used, err := x.migotoDLLUsed(context.Background(), cfg)
			if err != nil || used != (mode != "Bypass") {
				t.Fatalf("DLL used = %t, %v", used, err)
			}
			if mode == "Bypass" {
				cfg.ExtraLibraries = ExtraLibraries{Enabled: true, Paths: []string{filepath.Join(root, "d3d11.dll")}}
				used, err = x.migotoDLLUsed(context.Background(), cfg)
				if err != nil || !used {
					t.Fatalf("extra XXMI DLL used = %t, %v", used, err)
				}
			}
		})
	}
}

func TestLogLevelReachesINI(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		level string
		calls string
		debug string
	}{
		{"Disabled", "0", "0"}, {"Warning", "0", "0"}, {"Info", "1", "0"}, {"Debug", "1", "1"},
	} {
		t.Run(tc.level, func(t *testing.T) {
			t.Parallel()
			doc := parseINI([]byte("[Logging]\nshow_warnings = 1\n"))
			applyMigotoINI(doc, "GIMI", MigotoOptions{LogLevel: tc.level})
			data := string(doc.Bytes())
			for _, want := range []string{
				"log_level = " + strings.ToLower(tc.level), "calls = " + tc.calls, "debug = " + tc.debug,
				"show_warnings = 1",
			} {
				if !strings.Contains(data, want) {
					t.Fatalf("INI is missing %q: %s", want, data)
				}
			}
		})
	}
}

func TestGameLaunchSelectsStartMethodAndCommand(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		launch  string
		method  string
		command string
	}{
		{"Direct", "Shell", ""}, {"Custom", "Shell", "start game"}, {"Manual", "Manual", ""},
	} {
		t.Run(tc.launch, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			for _, name := range []string{runtimeManifestName, "game.exe", "3DMigoto Loader.exe"} {
				if err := os.WriteFile(filepath.Join(root, name), []byte("{}"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			cfg, err := DefaultImporterConfig("GIMI", root)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "d3d11.dll"), []byte("test"), 0o600); err != nil {
				t.Fatal(err)
			}
			cfg.Mode, cfg.ImporterFolder, cfg.GameLaunch = RuntimeLegacy, root, tc.launch
			cfg.CustomLaunch.Command = "start game"
			spec, err := New().builtinLaunchSpec(
				context.Background(), "GIMI", cfg, filepath.Join(root, "game.exe"), "game.exe",
			)
			if err != nil || spec.StartMethod != tc.method || spec.CustomLaunchCmd != tc.command {
				t.Fatalf("launch spec = %+v, %v", spec, err)
			}
			if err := inject.ValidateLaunchSpec(spec); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestNativeLaunchSpecDoesNotRequireLoader(t *testing.T) {
	t.Parallel()
	for _, mode := range []RuntimeMode{RuntimeXXMI, RuntimeLegacy} {
		for _, bypass := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/bypass=%t", mode, bypass), func(t *testing.T) {
				t.Parallel()
				root := t.TempDir()
				for _, name := range []string{"game.exe", "d3d11.dll", "extra.dll"} {
					if err := os.WriteFile(filepath.Join(root, name), []byte("test"), 0o600); err != nil {
						t.Fatal(err)
					}
				}
				if err := os.WriteFile(filepath.Join(root, runtimeManifestName), []byte("{}"), 0o600); err != nil {
					t.Fatal(err)
				}
				cfg, err := DefaultImporterConfig("GIMI", root)
				if err != nil {
					t.Fatal(err)
				}
				cfg.ImporterFolder, cfg.Mode, cfg.InjectionMethod = root, mode, "Native"
				cfg.ExtraLibraries = ExtraLibraries{Enabled: true, Paths: []string{filepath.Join(root, "extra.dll")}}
				cfg.GameLaunch, cfg.CustomLaunch = "Custom", CustomLaunch{Command: "start game"}
				if bypass {
					cfg.XXMIDLLInjectMode = "Bypass"
				}
				spec, err := New().builtinLaunchSpec(context.Background(), "GIMI", cfg, filepath.Join(root, "game.exe"), "game.exe")
				if err != nil {
					t.Fatal(err)
				}
				wantMode := "Inject"
				if bypass {
					wantMode = "Bypass"
				}
				if spec.InjectionMethod != "Native" || spec.UseHook || spec.InjectMode != wantMode ||
					spec.LoaderDLL.Path != "" || spec.LegacyLoader.Path != "" || len(spec.ExtraDLLs) != 1 {
					t.Fatalf("native launch spec = %+v", spec)
				}
				if err := inject.ValidateLaunchSpec(spec); err != nil {
					t.Fatal(err)
				}
				if legacyUsesXXMIInjector(cfg) {
					t.Fatal("native extra DLLs unexpectedly require cached XXMI injector")
				}
			})
		}
	}
}

func TestResolveExtraDLLPathsUsesConfiguredRoot(t *testing.T) {
	ctx := context.Background()
	client, err := db.New(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	if err := client.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	x := New()
	x.UseClient(client)
	useBuiltinLauncher(t, x)
	root := t.TempDir()
	if err := x.SetRoot(ctx, root); err != nil {
		t.Fatal(err)
	}
	absolute := filepath.Join(t.TempDir(), "absolute.dll")
	paths, err := x.resolveExtraDLLPaths(ctx, []string{filepath.Join("dlls", "extra.dll"), absolute})
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 2 || paths[0] != filepath.Join(root, "dlls", "extra.dll") || paths[1] != absolute {
		t.Fatalf("extra DLLs = %q", paths)
	}
}

func TestMigotoDLLUsedBypassExtraLibraries(t *testing.T) {
	ctx := context.Background()
	x := New()
	cfg := ImporterConfig{ImporterFolder: filepath.Join(t.TempDir(), "GIMI")}
	used, err := x.migotoDLLUsed(ctx, cfg)
	if err != nil || !used {
		t.Fatalf("default DLL use = %t, error = %v", used, err)
	}
	cfg.XXMIDLLInjectMode = "Bypass"
	used, err = x.migotoDLLUsed(ctx, cfg)
	if err != nil || used {
		t.Fatalf("bypass DLL use = %t, error = %v", used, err)
	}
	cfg.ExtraLibraries = ExtraLibraries{Enabled: true, Paths: []string{filepath.Join(cfg.ImporterFolder, "D3D11.DLL")}}
	used, err = x.migotoDLLUsed(ctx, cfg)
	if err != nil || !used {
		t.Fatalf("extra DLL use = %t, error = %v", used, err)
	}
	cfg.ExtraLibraries.Enabled = false
	used, err = x.migotoDLLUsed(ctx, cfg)
	if err != nil || used {
		t.Fatalf("disabled extra DLL use = %t, error = %v", used, err)
	}
}
