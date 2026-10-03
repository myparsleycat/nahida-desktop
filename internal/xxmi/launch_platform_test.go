package xxmi

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/sys/windows"

	"nahida.live/desktop/internal/xxmi/gameplatform"
	"nahida.live/desktop/internal/xxmi/inject"
)

// steamLaunchFixture builds a Steam folder whose signed-in account owns Wuthering Waves with
// the launch options "-old".
func steamLaunchFixture(t *testing.T) gameplatform.Steam {
	t.Helper()
	root := filepath.Join(t.TempDir(), "Steam Client")
	files := map[string]string{
		filepath.Join(root, "steam.exe"): "",
		filepath.Join(root, "config", "loginusers.vdf"): `"users" { "76561197960265730" { "AccountName" "tester" ` +
			`"Timestamp" "200" } }`,
		filepath.Join(root, "userdata", "2", "config", "localconfig.vdf"): `"UserLocalConfigStore"
{
	"Software" { "Valve" { "Steam" { "apps" { "3513350" { "LaunchOptions" "-old" } } } } }
}
`,
		filepath.Join(root, "steamapps", "libraryfolders.vdf"): `"libraryfolders" { "0" { "path" "` +
			strings.ReplaceAll(root, `\`, `\\`) + `" } }`,
		filepath.Join(root, "steamapps", "appmanifest_3513350.acf"): `"AppState" { "appid" "3513350" ` +
			`"name" "Wuthering Waves" "installdir" "Wuthering Waves" }`,
	}
	for path, content := range files {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return gameplatform.Steam{Exe: filepath.Join(root, "steam.exe")}
}

func TestResolvePlatformLaunch(t *testing.T) {
	t.Parallel()
	steam := steamLaunchFixture(t)
	manifest := filepath.Join(t.TempDir(), "LauncherInstalled.dat")
	content := `{"InstallationList":[{"InstallLocation":"D:\\Epic Games\\Wuthering_Waves",` +
		`"NamespaceId":"ns","ItemId":"item","ArtifactId":"art"}]}`
	if err := os.WriteFile(manifest, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	service := New()
	service.findSteam = func() (gameplatform.Steam, bool) { return steam, true }
	service.epicManifest = func() string { return manifest }

	platform, err := service.resolvePlatformLaunch("WWMI", ImporterConfig{GameLaunch: "Steam"})
	wantDir := filepath.Join(filepath.Dir(steam.Exe), "steamapps", "common", "Wuthering Waves")
	if err != nil || platform.steamApp.ID != "3513350" || platform.installDir != wantDir {
		t.Fatalf("Steam launch = %+v, %v", platform, err)
	}
	platform, err = service.resolvePlatformLaunch("WWMI", ImporterConfig{GameLaunch: "Epic"})
	if err != nil || platform.installDir != `D:\Epic Games\Wuthering_Waves` {
		t.Fatalf("Epic launch = %+v, %v", platform, err)
	}

	for _, tc := range []struct {
		key, launch string
		want        error
	}{
		{"GIMI", "Steam", errPlatformGameNotFound},
		{"GIMI", "Epic", errPlatformGameNotFound},
	} {
		if _, err := service.resolvePlatformLaunch(
			tc.key,
			ImporterConfig{GameLaunch: tc.launch},
		); !errors.Is(
			err,
			tc.want,
		) {
			t.Errorf("%s through %s: error = %v", tc.key, tc.launch, err)
		}
	}
	service.findSteam = func() (gameplatform.Steam, bool) { return gameplatform.Steam{}, false }
	service.epicManifest = func() string { return filepath.Join(t.TempDir(), "absent.dat") }
	for _, launch := range []string{"Steam", "Epic"} {
		if _, err := service.resolvePlatformLaunch("WWMI", ImporterConfig{GameLaunch: launch}); !errors.Is(
			err, errPlatformNotFound,
		) {
			t.Errorf("missing %s client: error = %v", launch, err)
		}
	}
	if _, err := service.resolvePlatformLaunch("WWMI", ImporterConfig{GameLaunch: "Direct"}); err == nil {
		t.Error("a direct launch was resolved through a store client")
	}
}

func TestPlatformCommandLine(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		key  string
		cfg  ImporterConfig
		want string
	}{
		{"renderer switch only", "WWMI", ImporterConfig{GameLaunch: "Steam"}, "-dx11"},
		{
			"unused launch options", "EFMI",
			ImporterConfig{GameLaunch: "Epic", LaunchOptions: "-a"}, "-force-d3d11",
		},
		{
			"launch options", "GIMI",
			ImporterConfig{GameLaunch: "Steam", UseLaunchOptions: true, LaunchOptions: ` -path C:\Data `},
			"-path C:/Data",
		},
		{
			"skipped game launcher", "ZZMI",
			ImporterConfig{
				GameLaunch: "Steam", SkipPlatformGameLauncher: true, UseLaunchOptions: true, LaunchOptions: "-a",
			},
			`"C:\Game\Game.exe" && %command% -a`,
		},
		{
			"game launcher skip is Steam only", "ZZMI",
			ImporterConfig{GameLaunch: "Epic", SkipPlatformGameLauncher: true}, "",
		},
	} {
		if got := platformCommandLine(tc.key, tc.cfg, `C:\Game\Game.exe`); got != tc.want {
			t.Errorf("%s: command line = %q, want %q", tc.name, got, tc.want)
		}
	}
	cfg := ImporterConfig{GameLaunch: "Steam", SkipPlatformGameLauncher: true}
	if got := platformCommandLine("ZZMI", cfg, ""); got != "" {
		t.Errorf("command line without a located executable = %q", got)
	}
}

func TestPrepareSteamLaunch(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		running   bool
		options   string
		configure bool
		wantCalls []string
		wantSaved string
	}{
		{"unchanged options keep the running client", true, "-old", true, nil, "-old"},
		{"changed options restart the client", true, "-dx11", true, []string{"-shutdown -silent", "-silent"}, "-dx11"},
		{"changed options start a stopped client", false, "-dx11", true, []string{"-silent"}, "-dx11"},
		{"options are left alone on request", false, "-dx11", false, []string{"-silent"}, "-old"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			steam := steamLaunchFixture(t)
			running := tc.running
			var calls []string
			service := New()
			service.findProcess = func(context.Context, string) (int, error) {
				if running {
					return 1, nil
				}
				return 0, nil
			}
			service.runPlatformClient = func(exe string, args ...string) error {
				if exe != steam.Exe {
					t.Errorf("started %q", exe)
				}
				calls = append(calls, strings.Join(args, " "))
				running = !slices.Contains(args, "-shutdown")
				return nil
			}
			platform := platformLaunch{steam: steam, steamApp: gameplatform.SteamApp{ID: "3513350"}}
			if err := service.prepareSteamLaunch(context.Background(), platform, tc.options, tc.configure); err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(calls, tc.wantCalls) {
				t.Errorf("client calls = %q, want %q", calls, tc.wantCalls)
			}
			if saved, err := steam.LaunchOptions("3513350"); err != nil || saved != tc.wantSaved {
				t.Errorf("stored launch options = %q, %v", saved, err)
			}
		})
	}
}

func TestPrepareSteamLaunchReportsOptionFailures(t *testing.T) {
	t.Parallel()
	steam := steamLaunchFixture(t)
	var calls int
	service := New()
	service.findProcess = func(context.Context, string) (int, error) { return 0, nil }
	service.runPlatformClient = func(string, ...string) error {
		calls++
		return nil
	}

	// Steam creates an application's settings block on its first run; this app never adds one.
	platform := platformLaunch{steam: steam, steamApp: gameplatform.SteamApp{ID: "4162040"}}
	err := service.prepareSteamLaunch(context.Background(), platform, "-a", true)
	if !errors.Is(err, errPlatformOptionsFailed) || !errors.Is(err, gameplatform.ErrSteamAppNotConfigured) {
		t.Fatalf("error = %v", err)
	}
	if calls != 0 {
		t.Fatalf("the client was started %d times after the failure", calls)
	}
}

func TestPlatformLaunchReplacesGameStart(t *testing.T) {
	t.Parallel()
	steam := gameplatform.Steam{Exe: filepath.Join(t.TempDir(), "steam.exe")}
	platform := platformLaunch{
		steam: steam, steamApp: gameplatform.SteamApp{ID: "3513350"},
		epicApp: gameplatform.EpicApp{NamespaceID: "ns", ItemID: "item", ArtifactID: "art"},
	}
	direct := inject.LaunchSpec{
		StartExe: `C:\Game\Game.exe`, StartArgs: []string{"-dx11"}, WorkDir: `C:\Game`, CustomLaunchCmd: "start",
	}

	spec := direct
	platform.apply(&spec, ImporterConfig{GameLaunch: "Steam"})
	if spec.StartExe != steam.Exe || strings.Join(spec.StartArgs, " ") != "-silent -applaunch 3513350" ||
		spec.WorkDir != filepath.Dir(steam.Exe) || spec.CustomLaunchCmd != "" {
		t.Fatalf("Steam launch spec = %+v", spec)
	}

	// The Epic Games request is opened by this process, so the helper is left nothing to start.
	spec = direct
	platform.apply(&spec, ImporterConfig{GameLaunch: "Epic"})
	if spec.StartExe != "" || spec.StartArgs != nil || spec.CustomLaunchCmd != "" {
		t.Fatalf("Epic launch spec = %+v", spec)
	}
}

// readyLaunchHelper stands in for the elevated helper of a caller-started launch: it signals the
// ready event the way the helper does once the game may start.
type readyLaunchHelper struct {
	recordingLaunchHelper
	fail error
}

func (h *readyLaunchHelper) LaunchXXMI(_ context.Context, spec inject.LaunchSpec) (inject.LaunchResult, error) {
	h.spec = spec
	h.calls++
	if h.fail != nil {
		return inject.LaunchResult{}, h.fail
	}
	name, err := windows.UTF16PtrFromString(spec.ReadyEvent)
	if err != nil {
		return inject.LaunchResult{}, err
	}
	event, err := windows.OpenEvent(windows.EVENT_MODIFY_STATE, false, name)
	if err != nil {
		return inject.LaunchResult{}, err
	}
	defer func() { _ = windows.CloseHandle(event) }()
	if err := windows.SetEvent(event); err != nil {
		return inject.LaunchResult{}, err
	}
	return inject.LaunchResult{PID: 42}, nil
}

func TestLaunchWithURIWaitsForTheHelper(t *testing.T) {
	t.Parallel()
	const uri = gameplatform.EpicURIPrefix + "ns%3Aitem%3Aart?action=launch&silent=true"
	spec := inject.LaunchSpec{ProcessName: "game.exe", StartMethod: "Native"}

	helper := &readyLaunchHelper{}
	service := NewWithOptions(Options{Elevated: helper})
	var opened []string
	service.openLaunchURI = func(uri string) error {
		opened = append(opened, uri)
		return nil
	}
	result, err := service.launchWithURI(context.Background(), spec, uri)
	if err != nil || result.PID != 42 || !slices.Equal(opened, []string{uri}) {
		t.Fatalf("result = %+v, %v; opened %v", result, err, opened)
	}
	if !strings.HasPrefix(helper.spec.ReadyEvent, inject.ReadyEventPrefix) {
		t.Fatalf("ready event = %q", helper.spec.ReadyEvent)
	}

	// A helper that fails before the game may start leaves the store client alone.
	refused := errors.New("XXMI_GAME_RUNNING")
	service = NewWithOptions(Options{Elevated: &readyLaunchHelper{fail: refused}})
	service.openLaunchURI = func(string) error {
		t.Error("the launch request was opened although the helper never asked for the game")
		return nil
	}
	if _, err := service.launchWithURI(context.Background(), spec, uri); !errors.Is(err, refused) {
		t.Fatalf("error = %v", err)
	}

	// A request that cannot be opened is reported instead of the helper's start timeout.
	unopened := errors.New("no program is registered")
	service = NewWithOptions(Options{Elevated: &readyLaunchHelper{}})
	service.openLaunchURI = func(string) error { return unopened }
	if _, err := service.launchWithURI(context.Background(), spec, uri); !errors.Is(err, unopened) {
		t.Fatalf("error = %v", err)
	}
}

func TestEpicBypassLaunchNeedsNoImporterFiles(t *testing.T) {
	t.Setenv("USERPROFILE", t.TempDir())
	ctx := context.Background()

	// The importer folder sits inside the Epic Games install, and nothing is installed in it.
	game := filepath.Join(t.TempDir(), "Arknights Endfield")
	cfg, err := DefaultImporterConfig("EFMI", game)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Enabled, cfg.GameLaunch, cfg.XXMIDLLInjectMode = true, "Epic", "Bypass"
	cfg.GameProcessExe = "Renamed.exe"
	if err := os.MkdirAll(cfg.ImporterFolder, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(game, "Endfield.exe"), []byte("test game"), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(t.TempDir(), "LauncherInstalled.dat")
	installed, err := json.Marshal(map[string]any{"InstallationList": []map[string]string{{
		"InstallLocation": game, "NamespaceId": "ns", "ItemId": "item", "ArtifactId": "art",
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifest, installed, 0o600); err != nil {
		t.Fatal(err)
	}

	helper := &readyLaunchHelper{}
	var stages, opened []string
	service := NewWithOptions(Options{Elevated: helper, EventEmit: func(name string, data ...any) {
		if name == "xxmi:launch-progress" {
			stages = append(stages, data[0].(map[string]any)["stage"].(string))
		}
	}})
	client := newXXMITestClient(t)
	service.UseClient(client)
	disabled := "false"
	if err := client.Settings.Upsert(ctx, "xxmi_auto_update", &disabled); err != nil {
		t.Fatal(err)
	}
	service.findProcess = noGameProcess
	service.epicManifest = func() string { return manifest }
	service.openLaunchURI = func(uri string) error {
		opened = append(opened, uri)
		return nil
	}

	// The game's own launcher picks the renderer, so the first launch stops for the reminder.
	if err := service.launchBuiltinGameLocked(ctx, "EFMI", cfg); !errors.Is(err, errD3D11ModeNoticeRequired) {
		t.Fatalf("first launch error = %v", err)
	}
	if helper.calls != 0 || len(opened) != 0 {
		t.Fatalf("the game was started before the reminder: %d helper calls, opened %v", helper.calls, opened)
	}
	cfg.D3D11ModeNoticeShown = true
	stages = nil
	if err := service.launchBuiltinGameLocked(ctx, "EFMI", cfg); err != nil {
		t.Fatal(err)
	}

	want := []string{"pre-launch", "resolve-game", "auto-update", "launch-guard", "game-tweaks", "elevate",
		"inject-launch", "post-load", "finish"}
	if !slices.Equal(stages, want) {
		t.Fatalf("launch stages = %v; want %v", stages, want)
	}
	wantURI := gameplatform.EpicURIPrefix + `ns%3Aitem%3Aart?action=launch&silent=true&args="-force-d3d11"`
	if !slices.Equal(opened, []string{wantURI}) {
		t.Fatalf("opened %v; want %s", opened, wantURI)
	}
	spec := helper.spec
	if helper.calls != 1 || spec.StartExe != "" || spec.ModuleDLL != "" || spec.InjectMode != "Bypass" ||
		spec.ProcessName != "Renamed.exe" || spec.WorkDir != cfg.ImporterFolder {
		t.Fatalf("helper calls = %d, spec = %+v", helper.calls, spec)
	}
	if err := inject.ValidateLaunchSpec(spec); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(cfg.ImporterFolder)
	if err != nil || len(entries) != 0 {
		t.Fatalf("importer folder entries = %v, %v", entries, err)
	}
}
