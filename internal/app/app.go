package app

import (
	"context"
	"embed"
	"errors"
	"os"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/platform"
)

func Run(assets embed.FS, icon []byte) (runErr error) {
	rt := newRuntime()
	route := nahidaDeepLinkRoute(os.Args)
	rt.window.SetStartHidden(shouldStartHidden(os.Args) && route == "")
	rt.window.SetInitialRoute(route)
	app := application.New(application.Options{
		Name:        "nahida-desktop",
		Description: "Native app for nahida.live",
		Icon:        icon,
		Services:    rt.services(),
		ErrorHandler: func(err error) {
			runtimeErrorHandler(rt.log, err)
		},
		PanicHandler: func(details *application.PanicDetails) {
			runtimePanicHandler(rt.log, details, os.Exit)
		},
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: "com.nahida.desktop",
			OnSecondInstanceLaunch: func(data application.SecondInstanceData) {
				rt.window.HandleArguments(data.Args)
			},
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		// Closing the last window must not tear down the process. Tray and
		// background work stay alive; WindowClosing still calls Quit when
		// runInBackground is off.
		Windows: windowsApplicationOptions(),
	})
	// application.New acquires Wails' single-instance lock. Keep all database,
	// local HTTP, watcher, and cleanup startup work after it so a forwarding
	// second instance exits without booting a second backend.
	in, err := livePathInput()
	if err != nil {
		return err
	}
	if _, err := bootRuntime(context.Background(), rt, in); err != nil {
		return err
	}
	defer func() {
		runErr = errors.Join(runErr, rt.Close())
	}()

	if err := platform.SetAppUserModelID("com.nahida"); err != nil && rt.log != nil {
		_ = infra.ReportError(rt.log, err, "App:setAppUserModelID", infra.Diagnostic{Severity: infra.DiagnosticError, Operation: "App:setAppUserModelID", Stage: "background"})
	}
	if executable, err := os.Executable(); err != nil {
		if rt.log != nil {
			_ = infra.ReportError(rt.log, err, "App:registerURLProtocol", infra.Diagnostic{Severity: infra.DiagnosticError, Operation: "App:registerURLProtocol", Stage: "background"})
		}
	} else if err := platform.RegisterNahidaURLProtocol(executable); err != nil && rt.log != nil {
		_ = infra.ReportError(rt.log, err, "App:registerURLProtocol", infra.Diagnostic{Severity: infra.DiagnosticError, Operation: "App:registerURLProtocol", Stage: "background"})
	}
	autostartSync := func(enabled bool) error {
		return syncAutostart(app.Autostart, enabled)
	}
	rt.setting.UseHooks(runtimeSettingHooks(rt.log, rt.transfer, rt.updater, rt.tools, rt.window, autostartSync, emitAppEvent))
	enabled, err := rt.setting.GetRunOnStartup(context.Background())
	if err == nil {
		err = autostartSync(enabled)
	}
	if err != nil && rt.log != nil {
		_ = infra.ReportError(rt.log, err, "App:syncAutostart", infra.Diagnostic{Severity: infra.DiagnosticError, Operation: "App:syncAutostart", Stage: "background"})
	}

	rt.window.Configure(app, rt.setting, rt.log)
	if rt.gameBananaLogin != nil {
		rt.gameBananaLogin.Configure(app, rt.window, rt.log)
	}
	if err := rt.updater.Configure(infra.UpdaterOptions{
		Engine:   app.Updater,
		Settings: rt.setting,
		HTTP:     rt.http,
		Log:      rt.log,
		Emit: func(name string, data ...any) {
			app.Event.Emit(name, data...)
		},
		Focus:   rt.window.Focus,
		Ready:   rt.window.NotifyUpdateReady,
		Version: platform.AppVersion,
	}); err != nil {
		return err
	}
	app.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		newWindow(app, rt.window)
	})
	newTray(app, rt, icon)
	registerDeepLink(app, rt.window)

	return app.Run()
}

func runtimeErrorHandler(log *infra.Log, err error) {
	if err == nil {
		return
	}
	_ = infra.ReportError(log, err, "Wails", infra.Diagnostic{
		Severity:  infra.DiagnosticError,
		Operation: "runtime",
	})
}

func runtimePanicHandler(log *infra.Log, details *application.PanicDetails, exit func(int)) {
	if details == nil {
		return
	}
	err := details.Error
	if err == nil {
		err = errors.New("unknown Wails panic")
	}
	_ = infra.ReportError(log, err, "Wails", infra.Diagnostic{
		Severity:  infra.DiagnosticError,
		Operation: "panic",
		Fields: map[string]any{
			"stackTrace": details.StackTrace,
		},
	})
	if exit != nil {
		exit(1)
	}
}

func windowsApplicationOptions() application.WindowsOptions {
	return application.WindowsOptions{
		DisableQuitOnLastWindowClosed: true,
		AdditionalBrowserArgs: []string{
			"--enable-experimental-web-platform-features",
			"--disable-renderer-backgrounding",
			"--autoplay-policy=no-user-gesture-required",
		},
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
