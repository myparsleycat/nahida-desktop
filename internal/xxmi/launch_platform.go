package xxmi

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"nahida.live/desktop/internal/xxmi/gameplatform"
	"nahida.live/desktop/internal/xxmi/inject"
)

// The renderer keys its launch error messages off these literals; keep them in sync with
// frontend/src/hooks/use-launch-guard.tsx.
var (
	errPlatformNotFound      = errors.New("XXMI_PLATFORM_NOT_FOUND")
	errPlatformGameNotFound  = errors.New("XXMI_PLATFORM_GAME_NOT_FOUND")
	errPlatformOptionsFailed = errors.New("XXMI_PLATFORM_OPTIONS_FAILED")
)

// steamShutdownTimeout bounds the wait for Steam to exit before its settings are rewritten.
const steamShutdownTimeout = 30 * time.Second

// platformLaunch is a game located in a store client.
type platformLaunch struct {
	// installDir is where the client installed the game.
	installDir string
	steam      gameplatform.Steam
	steamApp   gameplatform.SteamApp
	epicApp    gameplatform.EpicApp
}

// resolvePlatformLaunch finds the importer's game in the client selected by cfg.GameLaunch.
func (x *XXMI) resolvePlatformLaunch(key string, cfg ImporterConfig) (platformLaunch, error) {
	game, ok := gameplatform.GameFor(key)
	if !ok {
		return platformLaunch{}, fmt.Errorf("unknown importer %q", key)
	}
	switch cfg.GameLaunch {
	case "Steam":
		steam, ok := x.findSteam()
		if !ok {
			return platformLaunch{}, fmt.Errorf("%w: Steam client is not installed", errPlatformNotFound)
		}
		app, found, err := steam.FindGame(game)
		if err != nil {
			return platformLaunch{}, fmt.Errorf("search the Steam library for %s: %w", game.Name, err)
		}
		if !found {
			return platformLaunch{}, fmt.Errorf(
				"%w: %s is not in the Steam library",
				errPlatformGameNotFound,
				game.Name,
			)
		}
		return platformLaunch{installDir: app.InstallDir, steam: steam, steamApp: app}, nil
	case "Epic":
		app, found, err := gameplatform.FindEpicGame(x.epicManifest(), game)
		if errors.Is(err, fs.ErrNotExist) {
			return platformLaunch{}, fmt.Errorf("%w: Epic Games Launcher is not installed", errPlatformNotFound)
		}
		if err != nil {
			return platformLaunch{}, fmt.Errorf("read the Epic Games installation list: %w", err)
		}
		if !found {
			return platformLaunch{}, fmt.Errorf(
				"%w: %s is not in the Epic Games library", errPlatformGameNotFound, game.Name,
			)
		}
		return platformLaunch{installDir: app.InstallDir, epicApp: app}, nil
	}
	return platformLaunch{}, fmt.Errorf("game launch %q does not use a store client", cfg.GameLaunch)
}

// apply turns a launch spec built for the game executable into one that asks the store client instead.
func (p platformLaunch) apply(spec *inject.LaunchSpec, key string, cfg ImporterConfig, gameExe string) {
	spec.CustomLaunchCmd = ""
	if cfg.GameLaunch == "Steam" {
		// Steam applies the stored launch options itself, so none are passed on the command line.
		spec.StartExe, spec.StartArgs = p.steam.Exe, p.steam.LaunchArgs(p.steamApp.ID)
		spec.WorkDir = filepath.Dir(p.steam.Exe)
		return
	}
	spec.StartExe, spec.StartArgs = "", nil
	spec.LaunchURI = p.epicApp.LaunchURI(platformCommandLine(key, cfg, gameExe))
}

// platformCommandLine builds the game arguments a store client passes on: the DirectX 11 switch
// the importer needs, then the user's launch options.
func platformCommandLine(key string, cfg ImporterConfig, gameExe string) string {
	var parts []string

	// The Steam release of Zenless Zone Zero opens its own launcher first. Steam runs the text
	// before %command% as the program, so naming the game executable there skips that launcher.
	if cfg.GameLaunch == "Steam" && key == "ZZMI" && cfg.SkipPlatformGameLauncher && gameExe != "" {
		parts = append(parts, `"`+gameExe+`" && %command%`)
	}
	switch key {
	case "WWMI":
		parts = append(parts, "-dx11")
	case "EFMI":
		parts = append(parts, "-force-d3d11")
	}
	if options := strings.TrimSpace(cfg.LaunchOptions); cfg.UseLaunchOptions && options != "" {
		parts = append(parts, strings.ReplaceAll(options, `\`, "/"))
	}
	return strings.Join(parts, " ")
}

// prepareSteamLaunch stores the launch options in Steam and leaves a running, unelevated client behind.
// The elevated helper only hands the launch request to that client, so the game is not started elevated.
func (x *XXMI) prepareSteamLaunch(
	ctx context.Context, platform platformLaunch, options string, configure bool,
) error {
	steam, appID := platform.steam, platform.steamApp.ID
	running, err := x.findProcess(ctx, steam.Exe)
	if err != nil {
		return err
	}
	if configure {
		current, err := steam.LaunchOptions(appID)
		if err != nil {
			return fmt.Errorf("%w: read Steam launch options: %w", errPlatformOptionsFailed, err)
		}
		if current != options {
			// Steam keeps its settings in memory and writes them back on exit, so it has to be closed first.
			if running != 0 {
				if err := x.runPlatformClient(steam.Exe, "-shutdown", "-silent"); err != nil {
					return fmt.Errorf("%w: stop Steam: %w", errPlatformOptionsFailed, err)
				}
				if err := waitForGameProcesses(
					ctx,
					[]string{steam.Exe},
					steamShutdownTimeout,
					x.findProcess,
				); err != nil {
					return fmt.Errorf("%w: Steam is still running", errPlatformOptionsFailed)
				}
				running = 0
			}
			if err := steam.SetLaunchOptions(appID, options); err != nil {
				return fmt.Errorf("%w: write Steam launch options: %w", errPlatformOptionsFailed, err)
			}
		}
	}
	if running != 0 {
		return nil
	}
	if err := x.runPlatformClient(steam.Exe, "-silent"); err != nil {
		return fmt.Errorf("start Steam: %w", err)
	}
	return nil
}

// startPlatformClient starts a store client without waiting for it.
func startPlatformClient(exe string, args ...string) error {
	command := exec.Command(exe, args...)
	command.Dir = filepath.Dir(exe)
	if err := command.Start(); err != nil {
		return err
	}
	return command.Process.Release()
}
