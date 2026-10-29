package xxmi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

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
func (p platformLaunch) apply(spec *inject.LaunchSpec, cfg ImporterConfig) {
	if cfg.GameLaunch == "Steam" {
		// Steam applies the stored launch options itself, so none are passed on the command line.
		spec.StartExe, spec.StartArgs = p.steam.Exe, p.steam.LaunchArgs(p.steamApp.ID)
		spec.WorkDir = filepath.Dir(p.steam.Exe)
		return
	}

	// The Epic Games launch request is opened by launchWithURI, not by the helper.
	spec.StartExe, spec.StartArgs = "", nil
}

func (x *XXMI) launchWithURI(ctx context.Context, spec inject.LaunchSpec, uri string) (inject.LaunchResult, error) {
	return x.launchWhenReady(ctx, spec, func(context.Context) error {
		if err := x.openLaunchURI(uri); err != nil {
			return fmt.Errorf("open the store launch request: %w", err)
		}
		return nil
	})
}

// Hook installation and legacy loader preparation must precede caller-side game creation.
func (x *XXMI) launchWhenReady(
	ctx context.Context, spec inject.LaunchSpec, start func(context.Context) error,
) (inject.LaunchResult, error) {
	if err := ctx.Err(); err != nil {
		return inject.LaunchResult{}, err
	}
	spec.StartMethod = "Native"
	spec.StartExe, spec.StartArgs, spec.ParentExe = "", nil, ""
	var suffix [16]byte
	_, _ = rand.Read(suffix[:])
	spec.ReadyEvent = inject.ReadyEventPrefix + hex.EncodeToString(suffix[:])
	name, err := windows.UTF16PtrFromString(spec.ReadyEvent)
	if err != nil {
		return inject.LaunchResult{}, err
	}

	// The helper may run under another administrator account, which the default access list of
	// an event created here would lock out.
	descriptor, err := windows.SecurityDescriptorFromString("D:(A;;0x100002;;;AU)")
	if err != nil {
		return inject.LaunchResult{}, err
	}
	attributes := windows.SecurityAttributes{SecurityDescriptor: descriptor}
	attributes.Length = uint32(unsafe.Sizeof(attributes))
	event, err := windows.CreateEvent(&attributes, 1, 0, name)
	if err != nil {
		return inject.LaunchResult{}, fmt.Errorf("create launch ready event: %w", err)
	}
	defer func() { _ = windows.CloseHandle(event) }()

	// Cancelling stops both sides: the helper when the request cannot be opened, and the wait
	// below when the helper returns without asking for the game.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	opened := make(chan error, 1)
	go func() {
		for {
			state, err := windows.WaitForSingleObject(event, 100)
			switch {
			case err != nil:
				cancel()
				opened <- err
				return
			case state == windows.WAIT_OBJECT_0:
				if err = ctx.Err(); err == nil {
					err = start(ctx)
				}
				if err != nil {
					cancel()
				}
				opened <- err
				return
			case ctx.Err() != nil:
				opened <- nil
				return
			}
		}
	}()

	result, err := x.elevated.LaunchXXMI(ctx, spec)
	if err == nil {
		err = ctx.Err()
	}
	if err != nil || ctx.Err() != nil {
		cancel()
	} else if state, waitErr := windows.WaitForSingleObject(event, 0); waitErr != nil || state != windows.WAIT_OBJECT_0 {
		cancel()
		err = errors.New("helper completed without launch readiness")
	}
	if openErr := <-opened; openErr != nil {
		return inject.LaunchResult{}, openErr
	}
	return result, err
}

// openShellURI hands a launch request to the program registered for its scheme.
func openShellURI(uri string) error {
	verb, _ := windows.UTF16PtrFromString("open")
	target, err := windows.UTF16PtrFromString(uri)
	if err != nil {
		return err
	}
	return windows.ShellExecute(0, verb, target, nil, nil, windows.SW_SHOWNORMAL)
}

// platformCommandLine builds the game arguments a store client passes on: the renderer switch
// of the importer's game, then the user's launch options.
func platformCommandLine(key string, cfg ImporterConfig, gameExe string) string {
	var parts []string

	// Steam runs the text before %command% as the program, so naming the game executable there
	// skips the game's own launcher.
	if skipsPlatformGameLauncher(key, cfg) && gameExe != "" {
		parts = append(parts, `"`+gameExe+`" && %command%`)
	}
	if renderer := rendererSwitch(key, cfg); renderer != "" {
		parts = append(parts, renderer)
	}
	if options := strings.TrimSpace(cfg.LaunchOptions); cfg.UseLaunchOptions && options != "" {
		parts = append(parts, strings.ReplaceAll(options, `\`, "/"))
	}
	return strings.Join(parts, " ")
}

// skipsPlatformGameLauncher reports whether the Steam launch options should start the game executable
// instead of the launcher the Steam release opens first. Like the reference launcher, Zenless Zone Zero
// leaves this to the user and Arknights: Endfield always skips it.
func skipsPlatformGameLauncher(key string, cfg ImporterConfig) bool {
	return cfg.GameLaunch == "Steam" && (key == "EFMI" || key == "ZZMI" && cfg.SkipPlatformGameLauncher)
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
