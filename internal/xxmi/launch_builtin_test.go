package xxmi

import (
	"bytes"
	"context"
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

func TestEFMIMinimumLibrariesVersion(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		version string
		old     bool
	}{
		{"1.1.7", true},
		{"1.7.4", true},
		{"1.7.5", false},
		{"1.8.0", false},
		{"invalid", false},
	} {
		if got := efmiNeedsNewerLibs(tc.version); got != tc.old {
			t.Errorf("version %s: old = %t, want %t", tc.version, got, tc.old)
		}
	}
}

func TestImporterInjectionDefaults(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		key  string
		hook bool
	}{
		{"GIMI", true}, {"SRMI", true}, {"HIMI", true}, {"ZZMI", true},
		{"WWMI", false}, {"EFMI", false},
	} {
		spec, ok := lookupImporterPackage(tc.key)
		if !ok || spec.useHook != tc.hook {
			t.Errorf("%s: useHook = %t, found = %t", tc.key, spec.useHook, ok)
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
	err = service.launchBuiltinGameLocked(context.Background(), "GIMI", cfg, false)
	if err == nil || !strings.Contains(err.Error(), "XXMI_GAME_FOLDER_NOT_CONFIGURED") {
		t.Fatalf("launch error = %v", err)
	}
	if !slices.Equal(stages, []string{"resolve-game", "failed"}) {
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
	original := []byte("\xef\xbb\xbf; user comment\r\n[Loader]\r\ntarget = old.exe\r\ncustom = keep\r\n")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	service := NewWithOptions(Options{Elevated: stubLaunchHelper{}})
	cfg := ImporterConfig{ImporterFolder: folder, Mode: RuntimeXXMI,
		Migoto: MigotoOptions{EnforceRendering: true, EnableHunting: true, MuteWarnings: true}}
	if err := service.updateLaunchINI(context.Background(), "GIMI", cfg, "Game.exe"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"; user comment\r\n", "target = Game.exe\r\n", "custom = keep\r\n",
		"loader = nahida-elevated-helper-test.exe\r\n", "launch = \r\n",
		"texture_hash = 0\r\n", "hunting = 2\r\n", "show_warnings = 0\r\n"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("updated INI is missing %q: %q", want, data)
		}
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
		cfg := ImporterConfig{ImporterFolder: folder, Mode: RuntimeLegacy, UseLaunchOptions: direct}
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
	cfg.CustomLaunch = CustomLaunch{Enabled: true, InjectMode: "Bypass"}
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
