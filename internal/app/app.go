package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"sync"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/dialog"

	"nahida.live/desktop/internal/appdata"
	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/platform"
	"nahida.live/desktop/internal/selfupdate"
	"nahida.live/desktop/internal/ui"
)

func Run(icon []byte) (runErr error) {
	selfupdate.HandleHelperMode()
	waitPendingRelaunch()
	launches := &launchDispatcher{}
	// The lock comes before any database, watcher or cleanup work, so a forwarding second instance exits
	// without booting a second backend. The installer checks the mutex this ID names.
	first, err := fairy.SingleInstance("com.nahida.desktop", launches.Enqueue)
	if err != nil || !first {
		return err
	}

	notices := &trayNotices{}
	events := ui.NewEvents()
	var window *Window
	host := newGUIHost(func() *fairy.Window { return window.native() }, notices)
	host.emit = events.Emit
	rt := newRuntime(host)
	window = rt.window
	// Runs once, when Run returns or when the Windows session ends, whichever comes first.
	shutdown := sync.OnceValue(rt.Close)
	defer func() {
		runErr = errors.Join(runErr, shutdown())
	}()
	in, err := livePathInput()
	if err != nil {
		return err
	}
	rt.window.SetStartHidden((shouldStartHidden(os.Args) || xxmiLaunchArgument(os.Args) != "") &&
		nahidaDeepLinkDownload(os.Args) == nil && nahidaDeepLinkRoute(os.Args) == "")
	paths, err := bootRuntime(context.Background(), rt, in)
	if err != nil {
		return err
	}
	rt.logStartupMilestone("essential-ready")
	useCrashOutput(rt.log, filepath.Join(paths.Logs, "crash.log"))
	startProfiler(rt.log)
	// Handlers run on this goroutine inside fairy.Main, so a panic in the UI reaches here before the process ends.
	defer reportPanic(rt.log)
	rt.registerShellIntegration(icon)
	rt.useSettingHooks()
	font, err := ui.RegisterFonts()
	if err != nil {
		_ = infra.ReportError(rt.log, err, "App", infra.Diagnostic{Operation: "register-fonts"})
	}
	shell := ui.NewShell(ui.Deps{
		Events:       events,
		Settings:     rt.setting,
		Updater:      rt.updater,
		Auth:         rt.auth,
		Agent:        rt.agent,
		Mod:          rt.mod,
		Feedback:     rt.feedback,
		Dialog:       rt.dialog,
		Platform:     rt.shell,
		AppStatus:    rt.shell.GetAppStatus(),
		FS:           rt.fs,
		Tools:        rt.tools,
		Transfer:     rt.transfer,
		GameBanana:   rt.gamebanana,
		HTTP:         rt.http,
		XXMI:         rt.xxmi,
		ReShade:      rt.reshade,
		Drive:        rt.drive,
		Backup:       rt.backup,
		ModRuntime:   rt.modRuntime,
		Input:        rt.input,
		Log:          rt.log,
		Restart:      func() error { return restartApp(rt.log, rt.quit) },
		Ready:        rt.startup.wait,
		LibraryReady: rt.startup.waitLibrary,
		Dev:          !in.Packaged,
		CDNTrace:     rt.cdnTrace,
	}, font)
	viewers := newModelViewerWindows(shell, icon, font, rt.log, rt.modelViewerClosed)
	startElevatedHelper := sync.OnceFunc(rt.startElevatedHelperIfEnabled)
	rt.window.Configure(rt.setting, rt.log, icon, shell, font, startElevatedHelper, rt.mainWindowClosed)
	rt.configureGameBananaLogin(icon, font, shell.Dark)
	engine, err := selfupdate.New(selfupdate.Options{
		GitHub:    rt.github,
		Log:       rt.log,
		Quit:      rt.quit,
		HelperLog: filepath.Join(paths.Logs, "update-helper.log"),
	})
	if err != nil {
		return err
	}
	if err := rt.updater.Configure(infra.UpdaterOptions{
		Engine:   engine,
		Settings: rt.setting,
		HTTP:     rt.http,
		Log:      rt.log,
		Emit: func(name string, data ...any) {
			rt.emit(name, data...)
			if name == "updater:status-changed" && len(data) > 0 {
				if status, ok := data[0].(infra.UpdaterStatus); ok {
					rt.observeUpdaterStatus(status)
				}
			}
		},
		Ready: rt.notifyUpdateReady,
	}); err != nil {
		return err
	}
	shell.Load(context.Background())

	// Windows may end the process as soon as the session-end hook returns, before Main returns.
	fairy.OnSessionEnd(func() {
		rt.window.SaveBounds()
		_ = shutdown()
	})
	return fairy.Main(func() error {
		if tray, err := newTray(rt, icon); err != nil {
			_ = infra.ReportError(rt.log, err, "App", infra.Diagnostic{Operation: "tray"})
		} else {
			notices.use(tray)
		}
		if err := rt.startServices(context.Background()); err != nil {
			return err
		}
		// A standalone model viewer does not need the elevated helper.
		if modelViewerArgument(os.Args, in.Cwd) == "" {
			startElevatedHelper()
		}
		rt.startup.start(rt.runStartupWork)
		launches.Start(fairy.Launch{Args: os.Args, Dir: in.Cwd}, newLaunchHandler(
			viewers.Open,
			rt.window.open,
			func([]string) { rt.window.show() },
			func(args []string) {
				rt.dispatchDeepLinkLogin(args)
				rt.dispatchDeepLinkDownload(args)
				rt.dispatchDeepLinkRoute(args)
			},
			func(key string, first bool) { go rt.quickStart(key, first) },
		))
		return nil
	})
}

// registerShellIntegration records the installed version, the URL protocol and the notification identity. A
// failure leaves the app usable, so it is only logged.
func (rt *runtime) registerShellIntegration(icon []byte) {
	report := func(err error, operation string) {
		_ = infra.ReportError(rt.log, err, operation, infra.Diagnostic{
			Severity: infra.DiagnosticError, Operation: operation, Stage: "background",
		})
	}
	if err := platform.SetAppUserModelID(appUserModelID); err != nil {
		report(err, "App:setAppUserModelID")
	}
	if err := rt.registerAppUserModelID(icon); err != nil {
		report(err, "App:registerAppUserModelID")
	}
	if executable, err := os.Executable(); err != nil {
		report(err, "App:registerURLProtocol")
	} else if err := platform.RegisterNahidaURLProtocol(executable); err != nil {
		report(err, "App:registerURLProtocol")
	}
	if err := platform.SyncInstalledVersion(); err != nil {
		report(err, "App:syncInstalledVersion")
	}
}

// appUserModelID names the app to the taskbar and to toast notifications.
const appUserModelID = "com.nahida"

// registerAppUserModelID lets an installed app show toasts that replace each other (tray.Tray.NotifyID). A
// development build skips it and falls back to tray balloons.
func (rt *runtime) registerAppUserModelID(icon []byte) error {
	if !platform.Packaged() {
		return nil
	}
	iconPath := ""
	if len(icon) > 0 {
		current, err := rt.appData.ReadFile(appdata.NotificationIcon)
		if err != nil || !bytes.Equal(current, icon) {
			if err := rt.appData.WriteFile(appdata.NotificationIcon, icon, 0o600); err != nil {
				return err
			}
		}
		path, err := rt.appData.Resolve(appdata.NotificationIcon)
		if err != nil {
			return err
		}
		iconPath = path
	}
	return platform.RegisterAppUserModelID(appUserModelID, "Nahida Desktop", iconPath)
}

func (rt *runtime) useSettingHooks() {
	autostart := platform.NewAutostart(autostartIdentifier)
	autostartSync := func(enabled bool) error { return syncAutostart(autostart, enabled) }
	syncModelViewerMenu := modelViewerMenuSyncer(rt.log)
	hooks := runtimeSettingHooks(
		rt.log,
		rt.transfer,
		rt.updater,
		rt.tools,
		autostartSync,
		rt.emit,
		syncModelViewerMenu,
		rt.configureElevatedHelper,
	)
	hooks.AfterWindowBackdropChanged = rt.window.SetBackdropEnabled
	rt.setting.UseHooks(rt.githubTokenHooks(hooks))

	if language, err := rt.setting.GetLanguage(context.Background()); err == nil {
		syncModelViewerMenu(language)
	} else {
		_ = infra.ReportError(rt.log, err, "App:syncModelViewerMenu", infra.Diagnostic{
			Operation: "App:syncModelViewerMenu", Stage: "read-language",
		})
	}
	enabled, err := rt.setting.GetRunOnStartup(context.Background())
	if err == nil {
		err = autostartSync(enabled)
	}
	if err != nil {
		_ = infra.ReportError(rt.log, err, "App:syncAutostart", infra.Diagnostic{
			Severity: infra.DiagnosticError, Operation: "App:syncAutostart", Stage: "background",
		})
	}
}

// mainWindowClosed quits unless the user keeps the app running in the background.
func (rt *runtime) mainWindowClosed() {
	background, err := rt.setting.GetRunInBackground(context.Background())
	if err != nil {
		_ = infra.ReportError(rt.log, err, "MainWindow.runInBackground", infra.Diagnostic{
			Severity: infra.DiagnosticError, Operation: "MainWindow.runInBackground", Stage: "background",
		})
		return
	}
	if !background {
		rt.quit()
	}
}

// quit ends the message loop after saving the main window's bounds. fairy.Main destroys the windows that are
// still open without running their OnClose, so the bounds must be read on the UI thread before Quit; app calls
// run in order, so the save precedes the quit message.
func (rt *runtime) quit() {
	fairy.Go(rt.window.SaveBounds)
	fairy.Quit()
}

// quickStart launches a game for --xxmi-launch. A cold quick start that never showed the main window ends
// the app afterwards unless it runs in the background.
func (rt *runtime) quickStart(key string, first bool) {
	err := rt.startup.wait(context.Background())
	if err != nil && errors.Is(err, context.Canceled) {
		return
	}
	if err != nil {
		_ = infra.ReportError(rt.log, err, "XXMI.QuickStart", infra.Diagnostic{
			Operation: "launch-game", Stage: "startup", Fields: map[string]any{"importer": key},
		})
	} else {
		// A quick start has no dialog to ask about logging with, so it launches as configured.
		err = rt.xxmi.StartGameWithLogging(context.Background(), key)
	}
	if err != nil {
		rt.window.Focus()
		// The importer settings that a failed launch usually needs live on the XXMI screen.
		rt.emit("fn:navi", string(ui.RouteXXMI))
		dialog.Message(nil, "Nahida Desktop", err.Error(), dialog.MessageError, dialog.ButtonsOK)
		return
	}
	if first {
		background, err := rt.setting.GetRunInBackground(context.Background())
		if err == nil && !background && !rt.window.hasShown() {
			rt.quit()
		}
	}
}

func shouldStartHidden(args []string) bool {
	for index, arg := range args {
		if index > 0 && arg == "--hidden" {
			return true
		}
	}
	return false
}

func livePathInput() (runtimePathInput, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return runtimePathInput{}, err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return runtimePathInput{}, err
	}
	return runtimePathInput{
		HomeDir:    home,
		Cwd:        cwd,
		Packaged:   platform.Packaged(),
		DBOverride: os.Getenv("NAHIDA_DB_PATH"),
	}, nil
}

// reportPanic writes a panic on the UI thread to desktop.log with its stack, then lets it end the process.
func reportPanic(log *infra.Log) {
	r := recover()
	if r == nil {
		return
	}
	err, ok := r.(error)
	if !ok {
		err = fmt.Errorf("%v", r)
	}
	_ = infra.ReportError(log, err, "App", infra.Diagnostic{
		Severity:  infra.DiagnosticError,
		Operation: "panic",
		Fields:    map[string]any{"stackTrace": string(debug.Stack())},
	})
	panic(r)
}

// useCrashOutput keeps the runtime's report of a fatal error on any goroutine in path, which desktop.log cannot
// receive once the process is dying.
func useCrashOutput(log *infra.Log, path string) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err == nil {
		err = debug.SetCrashOutput(file, debug.CrashOptions{})
		_ = file.Close()
	}
	if err != nil {
		_ = infra.ReportError(log, err, "App", infra.Diagnostic{
			Operation: "crash-output", Fields: map[string]any{"path": path},
		})
	}
}
