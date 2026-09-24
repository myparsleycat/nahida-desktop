package app

import (
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"

	"nahida.live/desktop/internal/agent"
	"nahida.live/desktop/internal/appdata"
	"nahida.live/desktop/internal/auth"
	"nahida.live/desktop/internal/backup"
	"nahida.live/desktop/internal/drive"
	"nahida.live/desktop/internal/elevated"
	"nahida.live/desktop/internal/gamebanana"
	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/mod"
	"nahida.live/desktop/internal/platform"
	"nahida.live/desktop/internal/setting"
	"nahida.live/desktop/internal/tools"
	"nahida.live/desktop/internal/transfer"
	"nahida.live/desktop/internal/xxmi"
)

type runtime struct {
	startup           *startupWork
	proxyRelay        *infra.ProxyRelay
	appData           *appdata.Store
	log               *infra.Log
	store             *infra.Store
	http              *infra.Client
	cdnTrace          *infra.CDNTrace
	fs                *platform.FS
	native            *platform.Native
	updater           *infra.Updater
	protocol          *infra.Protocol
	download          *infra.Download
	archive           *infra.Archive
	agent             *agent.Service
	auth              *auth.Auth
	backup            *backup.Backup
	setting           *setting.Setting
	drive             *drive.Drive
	transfer          *transfer.Transfer
	gamebanana        *gamebanana.GameBanana
	mod               *mod.Mod
	xxmi              *xxmi.XXMI
	tools             *tools.Tools
	dialog            *platform.Dialog
	shell             *platform.Shell
	input             *platform.Input
	elevated          *elevated.Client
	elevatedLifecycle *elevatedLifecycle
	screen            *platform.Screen
	window            *Window
	localHTTP         *infra.LocalHTTP
	gameBananaLogin   *gameBananaLogin
	notifications     *notifications.NotificationService
}

func newRuntime() *runtime {
	log := infra.NewLogWithOptions(infra.LogOptions{Dev: !platform.Packaged()})
	httpClient := infra.NewClient()
	httpClient.UseLog(log)
	shell := platform.NewShell()
	shell.UseDiagnostic(func(err error, stage string, fields map[string]any) {
		_ = infra.ReportError(
			log,
			err,
			"Shell",
			infra.Diagnostic{Severity: infra.DiagnosticWarn, Operation: "open-external", Stage: stage, Fields: fields},
		)
	})
	fs := platform.NewFS()
	fs.UseDiagnostic(func(err error, stage string, fields map[string]any) {
		_ = infra.ReportError(log, err, "FS", infra.Diagnostic{Operation: "write-access", Stage: stage, Fields: fields})
	})
	input := platform.NewInput()
	elevatedClient := elevated.NewClient()
	elevatedHelper := newElevatedLifecycle(elevatedClient, func(err error, stage string) {
		reportElevatedHelperError(log, err, stage)
	}, emitAppEvent)
	elevatedClient.UseDisconnect(elevatedHelper.publishStatus)
	input.UseElevatedInput(elevatedClient)
	input.UseElevatedHelperStatus(elevatedHelper.status)
	input.UseDiagnostic(func(err error, stage string, fields map[string]any) {
		_ = infra.ReportError(
			log,
			err,
			"Input",
			infra.Diagnostic{
				Severity:  infra.DiagnosticWarn,
				Operation: "send-keys",
				Stage:     stage,
				Fields:    fields,
			},
		)
	})
	screen := platform.NewScreen()
	screen.UseDiagnostic(func(err error, stage string, fields map[string]any) {
		_ = infra.ReportError(
			log,
			err,
			"Screen",
			infra.Diagnostic{
				Severity:  infra.DiagnosticWarn,
				Operation: "capture-window",
				Stage:     stage,
				Fields:    fields,
			},
		)
	})
	native := platform.NewNative()
	window := NewWindow()
	notifier := notifications.New()
	eventEmit := emitAppEvent
	transferService := transfer.NewWithOptions(transfer.Options{
		Log: log,
		ReportFailure: func(err error, fields map[string]any) error {
			return infra.ReportError(log, err, "Transfer", infra.Diagnostic{Fields: fields})
		},
		EventEmit:         eventEmit,
		PreventSuspension: native.PreventAppSuspension,
		SyncWindowProgress: func(progress *transfer.WindowProgress) {
			if progress == nil {
				window.SetProgressBar(nil, "")
				return
			}
			value := progress.Value
			window.SetProgressBar(&value, string(progress.Mode))
		},
	})
	settings := setting.NewWithOptions(nil, setting.Options{Locale: platform.SystemLocale()})
	download := infra.NewDownload()
	archive := infra.NewArchive()
	archive.UseLog(log)
	protocolService := infra.NewProtocol()
	protocolService.Configure(httpClient, log)
	githubRate := infra.NewGitHubRateCoordinator()
	githubRate.UseHTTP(httpClient)
	githubRate.UseLog(log)
	xxmiService := xxmi.NewWithOptions(xxmi.Options{
		HTTP: httpClient, Log: log, Download: download, Archive: archive, EventEmit: eventEmit,
	})
	dialog := platform.NewDialog()
	login := newGameBananaLogin()
	gameBananaService := gamebanana.NewWithOptions(gamebanana.Options{
		HTTP:              httpClient,
		Log:               log,
		Crypto:            platform.NewCrypto(),
		OpenLogin:         login.Open,
		ClearLoginCookies: login.ClearCookies,
	})
	modService := mod.NewWithOptions(mod.Options{
		FS: fs, Settings: settings, Archive: archive, HTTP: httpClient, EventEmit: eventEmit,
		XXMI: xxmiService, Log: log, Dialog: dialog, Transfer: transferService,
		GameBanana: gameBananaService, Native: native,
	})
	download.UseClient(httpClient)
	download.UseLimiter(transferService)
	transferService.UseSettings(settings)
	updaterService := infra.NewUpdater()
	settings.UseHooks(runtimeSettingHooks(log, transferService, updaterService, nil, window, nil, eventEmit, nil, nil))
	rt := &runtime{
		startup:  newStartupWork(),
		log:      log,
		store:    infra.NewStore(),
		http:     httpClient,
		cdnTrace: infra.NewCDNTrace(httpClient),
		fs:       fs,
		native:   native,
		updater:  updaterService,
		protocol: protocolService,
		download: download,
		archive:  archive,
		auth: auth.NewWithOptions(auth.Options{
			Crypto: platform.NewCrypto(),
			HTTP:   httpClient,
			Log:    log,
			Shell:  shell,
		}),
		setting: settings,
		drive: drive.NewWithOptions(drive.Options{
			HTTP:           httpClient,
			FS:             fs,
			Log:            log,
			Transfer:       transferService,
			Download:       download,
			Archive:        archive,
			Dialog:         dialog,
			UploadSettings: settings,
			EventEmit:      eventEmit,
		}),
		transfer:   transferService,
		gamebanana: gameBananaService,
		mod:        modService,
		xxmi:       xxmiService,
		tools: tools.NewWithOptions(tools.Options{
			FindModelViewerPreview: modService.FindModelViewerPreview,
			Log:                    log,
			EventEmit:              eventEmit,
			Settings:               settings,
			XXMI:                   xxmiService,
			FS:                     fs,
			HTTP:                   httpClient,
			Download:               download,
			Archive:                archive,
			Protocol:               protocolService,
			GitHubRate:             githubRate,
			Mod:                    modService,
			Notify: func(title, body string) error {
				return notifier.SendNotification(notifications.NotificationOptions{
					ID: "wuwa-mod-fixer-updated", Title: title, Body: body,
				})
			},
		}),
		dialog:            dialog,
		shell:             shell,
		input:             input,
		elevated:          elevatedClient,
		elevatedLifecycle: elevatedHelper,
		screen:            screen,
		window:            window,
		localHTTP: infra.NewLocalHTTPWithOptions(infra.LocalHTTPOptions{
			Version: platform.AppVersion,
			Log:     log,
		}),
		gameBananaLogin: login,
		notifications:   notifier,
	}
	rt.agent = agent.New(agent.Options{
		HTTP:      httpClient.HTTPClient(),
		Remote:    httpClient,
		Crypto:    platform.NewCrypto(),
		Tools:     rt.tools,
		Mod:       rt.mod,
		Setting:   rt.setting,
		Transfer:  rt.transfer,
		XXMI:      rt.xxmi,
		Log:       log,
		EventEmit: eventEmit,
		Shell:     shell,
		Input:     rt.input,
		Screen:    rt.screen,
	})
	rt.backup = backup.New(backup.Options{
		Remote:    rt.drive,
		Settings:  rt.setting,
		Session:   rt.auth,
		Log:       log,
		EventEmit: eventEmit,
	})
	rt.localHTTP.UseHandler(rt.handleLocalHTTPMessage)
	if rt.mod != nil {
		rt.mod.UseFocus(rt.window.Focus)
		rt.mod.UseWindowReady(rt.window.WaitReady)
	}
	if rt.drive != nil && rt.mod != nil {
		rt.drive.UsePathSelector(rt.mod)
	}
	if rt.tools != nil {
		queueFixInspections := rt.tools.QueueFixInspections
		if rt.mod != nil {
			rt.mod.UseFixInspection(queueFixInspections)
		}
		if rt.drive != nil {
			rt.drive.UseFixInspection(queueFixInspections)
		}
	}
	settings.UseHooks(
		runtimeSettingHooks(log, transferService, updaterService, rt.tools, rt.window, nil, eventEmit, nil, nil),
	)
	return rt
}

func emitAppEvent(name string, data ...any) {
	if app := application.Get(); app != nil {
		app.Event.Emit(name, data...)
	}
}

func (rt *runtime) services() []application.Service {
	return []application.Service{
		newLoggedService(rt, "Agent", rt.agent),
		newLoggedService(rt, "Auth", rt.auth),
		newLoggedService(rt, "Backup", rt.backup),
		newLoggedService(rt, "CDNTrace", rt.cdnTrace),
		newLoggedService(rt, "Dialog", rt.dialog),
		newLoggedService(rt, "Drive", rt.drive),
		newLoggedService(rt, "FS", rt.fs),
		newLoggedService(rt, "GameBanana", rt.gamebanana),
		newLoggedService(rt, "Input", rt.input),
		application.NewService(rt.log),
		newLoggedService(rt, "Mod", rt.mod),
		application.NewService(rt.notifications),
		newLoggedServiceWithOptions(rt, "Protocol", rt.protocol, application.ServiceOptions{Route: "/protocol"}),
		newLoggedService(rt, "Screen", rt.screen),
		newLoggedService(rt, "Setting", rt.setting),
		newLoggedService(rt, "Shell", rt.shell),
		newLoggedService(rt, "Tools", rt.tools),
		newLoggedService(rt, "Transfer", rt.transfer),
		newLoggedService(rt, "Updater", rt.updater),
		newLoggedService(rt, "Window", rt.window),
		newLoggedService(rt, "XXMI", rt.xxmi),
	}
}

// fileMutatingServices are Wails services whose bindings wait for startup
// maintenance. Add a service here when it writes user files.
func (rt *runtime) fileMutatingServices() []application.Service {
	return []application.Service{
		application.NewService(rt.agent),
		application.NewService(rt.backup),
		application.NewService(rt.drive),
		application.NewService(rt.fs),
		application.NewService(rt.mod),
		application.NewService(rt.tools),
		application.NewService(rt.transfer),
		application.NewService(rt.xxmi),
	}
}

func newLoggedService[T any](rt *runtime, name string, instance *T) application.Service {
	return newLoggedServiceWithOptions(rt, name, instance, application.ServiceOptions{})
}

func newLoggedServiceWithOptions[T any](
	rt *runtime,
	name string,
	instance *T,
	options application.ServiceOptions,
) application.Service {
	if rt != nil && rt.log != nil {
		options.MarshalError = rt.log.ServiceErrorMarshaler(name)
	}
	return application.NewServiceWithOptions(instance, options)
}
