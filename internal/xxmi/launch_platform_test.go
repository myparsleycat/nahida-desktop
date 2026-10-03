package xxmi

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

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
	platform.apply(&spec, "WWMI", ImporterConfig{GameLaunch: "Steam"}, direct.StartExe)
	if spec.StartExe != steam.Exe || strings.Join(spec.StartArgs, " ") != "-silent -applaunch 3513350" ||
		spec.WorkDir != filepath.Dir(steam.Exe) || spec.CustomLaunchCmd != "" || spec.LaunchURI != "" {
		t.Fatalf("Steam launch spec = %+v", spec)
	}

	spec = direct
	platform.apply(&spec, "WWMI", ImporterConfig{GameLaunch: "Epic"}, direct.StartExe)
	want := gameplatform.EpicURIPrefix + `ns%3Aitem%3Aart?action=launch&silent=true&args="-dx11"`
	if spec.StartExe != "" || spec.StartArgs != nil || spec.CustomLaunchCmd != "" || spec.LaunchURI != want {
		t.Fatalf("Epic launch spec = %+v", spec)
	}
}
