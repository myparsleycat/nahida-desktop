package app

import (
	"context"
	"errors"

	"nahida.live/desktop/internal/agent"
	"nahida.live/desktop/internal/appdata"
	"nahida.live/desktop/internal/auth"
	"nahida.live/desktop/internal/backup"
	"nahida.live/desktop/internal/diskio"
	"nahida.live/desktop/internal/drive"
	"nahida.live/desktop/internal/elevated"
	"nahida.live/desktop/internal/feedback"
	"nahida.live/desktop/internal/gamebanana"
	"nahida.live/desktop/internal/gamelink"
	"nahida.live/desktop/internal/github"
	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/mod"
	"nahida.live/desktop/internal/modruntime"
	"nahida.live/desktop/internal/platform"
	"nahida.live/desktop/internal/reshade"
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
	githubRate        *infra.GitHubRateCoordinator
	github            *github.Client
	protocol          *infra.Protocol
	download          *infra.Download
	archive           *infra.Archive
	agent             *agent.Service
	feedback          *feedback.Service
	auth              *auth.Auth
	backup            *backup.Backup
	setting           *setting.Setting
	drive             *drive.Drive
	transfer          *transfer.Transfer
	gamebanana        *gamebanana.GameBanana
	gameBananaLogin   *gameBananaLogin
	mod               *mod.Mod
	modRuntime        *modruntime.Manager
	gameLink          *gamelink.Manager
	xxmi              *xxmi.XXMI
	reshade           *reshade.ReShade
	tools             *tools.Tools
	dialog            *platform.Dialog
	shell             *platform.Shell
	input             *platform.Input
	elevated          *elevated.Client
	elevatedLifecycle *elevatedLifecycle
	appDataAccess     appDataAccess
	screen            *platform.Screen
	window            *Window
	emit              func(name string, data ...any)
	notify            func(id, title, body string) error
	updateCheck       trayUpdateCheck
}

func newRuntime(host uiHost) *runtime {
	if host.emit == nil {
		host.emit = func(string, ...any) {}
	}
	log := infra.NewLogWithOptions(infra.LogOptions{Dev: !platform.Packaged()})
	diskio.UseLog(log)
	httpClient := infra.NewClient()
	httpClient.UseLog(log)
	shell := platform.NewShell(host.shell)
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
	elevatedClient.UseAnnotate(func(err error, stage string, fields map[string]any) error {
		return infra.AnnotateError(err, infra.Diagnostic{Operation: "elevated-helper", Stage: stage, Fields: fields})
	})
	elevatedHelper := newElevatedLifecycle(elevatedClient, func(err error, stage string) {
		reportElevatedHelperError(log, err, stage)
	}, host.emit)
	elevatedClient.UseDisconnect(elevatedHelper.publishStatus)
	elevatedFiles := elevatedFileGateway{lifecycle: elevatedHelper, client: elevatedClient}
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
	eventEmit := host.emit
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
	githubRate.UseLog(log)
	githubRate.OnTokenRejected(func() { eventEmit("github:token-rejected") })
	githubClient := github.New(github.Options{HTTP: httpClient, Download: download, Rate: githubRate, Log: log})
	var xxmiService *xxmi.XXMI
	reshadeService := reshade.New(reshade.Options{
		GitHub:    githubClient,
		Download:  download,
		Archive:   archive,
		Transfer:  transferService,
		Log:       log,
		EventEmit: eventEmit,
		Root: func(ctx context.Context) (string, error) {
			root, err := xxmiService.GetXXMIPath(ctx)
			if err != nil {
				return "", err
			}
			if root == nil {
				return "", errors.New("XXMI folder is not configured")
			}
			return *root, nil
		},
	})
	xxmiService = xxmi.NewWithOptions(xxmi.Options{
		HTTP: httpClient, Log: log, Download: download, Archive: archive, EventEmit: eventEmit, GitHub: githubClient,
		Elevated: xxmiElevatedLauncher{lifecycle: elevatedHelper, client: elevatedClient},
		ReShade:  reshadeService,
	})
	dialog := platform.NewDialog(host.dialog)
	gameLink := gamelink.NewManager(gamelink.Options{Log: log, EventEmit: eventEmit, Processes: xxmiService})
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
		GameBanana: gameBananaService, Native: native, Download: download, Elevated: elevatedFiles,
		GameProcesses: xxmiService.GameProcesses, GitHub: githubClient,
	})
	download.UseClient(httpClient)
	modRuntime := modruntime.New(modruntime.Options{
		Log: log, Mod: modService, Trash: shell.Trash, Specs: tools.ModRuntimes(),
		Capabilities: func(ctx context.Context, importer string) (modruntime.Capabilities, error) {
			features, err := xxmiService.ImporterDLLFeatures(ctx, importer)
			return modruntime.Capabilities{ShaderSamplerSlots: features.ShaderSamplerSlots, DX12: features.DX12}, err
		},
	})
	download.UseLimiter(transferService)
	transferService.UseSettings(settings)
	updaterService := infra.NewUpdater()
	updaterService.UseGitHubRate(githubRate)
	rt := &runtime{
		startup:    newStartupWork(),
		log:        log,
		store:      infra.NewStore(),
		http:       httpClient,
		cdnTrace:   infra.NewCDNTrace(httpClient),
		fs:         fs,
		native:     native,
		updater:    updaterService,
		githubRate: githubRate,
		github:     githubClient,
		protocol:   protocolService,
		download:   download,
		archive:    archive,
		auth: auth.NewWithOptions(auth.Options{
			Crypto: platform.NewCrypto(),
			HTTP:   httpClient,
			Log:    log,
			Shell:  shell,
			Emit:   func(name string, data any) { eventEmit(name, data) },
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
		transfer:        transferService,
		gamebanana:      gameBananaService,
		gameBananaLogin: login,
		mod:             modService,
		modRuntime:      modRuntime,
		gameLink:        gameLink,
		xxmi:            xxmiService,
		reshade:         reshadeService,
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
			GitHub:                 githubClient,
			Mod:                    modService,
			Runtimes:               modRuntime,
			GameLink:               gameLink,
			Elevated:               elevatedFiles,
			Notify: func(title, body string) error {
				if host.notify == nil {
					return nil
				}
				return host.notify("wuwa-mod-fixer-updated", title, body)
			},
		}),
		dialog:            dialog,
		shell:             shell,
		input:             input,
		elevated:          elevatedClient,
		elevatedLifecycle: elevatedHelper,
		appDataAccess: appDataAccess{
			denied:  platform.AccessDenied,
			confirm: platform.ConfirmStartup,
			files:   elevatedFiles,
			locale:  platform.SystemLocale,
		},
		screen: screen,
		window: window,
		emit:   host.emit,
		notify: host.notify,
	}
	rt.xxmi.UseImportersChanged(func(ctx context.Context) {
		if err := errors.Join(rt.mod.SyncImporterModFolders(ctx), rt.mod.InvalidateImporterGames(ctx)); err != nil {
			_ = infra.ReportError(log, err, "XXMI.importersChanged", infra.Diagnostic{
				Severity:  infra.DiagnosticWarn,
				Operation: "mod.sync-importer-mod-folders",
				Stage:     "importer-selection",
			})
		}
		rt.mod.RefreshCompressionImporters(ctx)
		rt.mod.InvalidateLibraries()
		if err := rt.tools.StartToggleViewerWatcher(ctx); err != nil {
			_ = infra.ReportError(log, err, "XXMI.importersChanged", infra.Diagnostic{
				Severity:  infra.DiagnosticError,
				Operation: "toggle-viewer.watcher",
				Stage:     "importer-selection",
			})
		}
	})
	rt.xxmi.UseDLLChanged(func(ctx context.Context, importer string) {
		if err := rt.modRuntime.Refresh(ctx, importer); err != nil {
			_ = infra.ReportError(log, err, "XXMI.dllChanged", infra.Diagnostic{
				Severity:  infra.DiagnosticWarn,
				Operation: "mod-runtime-refresh",
				Stage:     "dll-selection",
				Fields:    map[string]any{"importer": importer},
			})
		}
	})
	rt.xxmi.UseModRuntimeUpdate(rt.modRuntime.Refresh)
	rt.xxmi.UseRunningChanged(rt.gameLink.Wake)
	rt.xxmi.UseImporterMaintenance(func(ctx context.Context) (func([]xxmi.ImportedImporter) error, error) {
		resumeTools, err := rt.tools.SuspendImporterWatchers(ctx)
		if err != nil {
			return nil, err
		}
		resumeMods, err := rt.mod.SuspendImporterWatchers(ctx)
		if err != nil {
			return nil, errors.Join(err, resumeTools(nil))
		}
		return func(moved []xxmi.ImportedImporter) error {
			return errors.Join(resumeMods(moved), resumeTools(moved))
		}, nil
	})
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
	rt.feedback = feedback.New(feedback.Options{
		Remote:    httpClient,
		Log:       log,
		EventEmit: eventEmit,
		LogPath: func() (string, error) {
			logs, err := rt.appData.Resolve(appdata.LogsDir)
			if err != nil {
				return "", err
			}
			return infra.DesktopLogPath(logs), nil
		},
		ModFileRule: func(ctx context.Context) (feedback.ModFileRule, error) {
			rules, err := rt.drive.UploadRules(ctx)
			if err != nil {
				return nil, err
			}
			return func(name string, size int64) bool {
				maxSize, ok := rules.MaxSizeFor(name)
				return ok && size <= maxSize
			}, nil
		},
		Mods: rt.drive,
	})
	rt.backup = backup.New(backup.Options{
		Remote:    rt.drive,
		Settings:  rt.setting,
		Session:   rt.auth,
		Log:       log,
		EventEmit: eventEmit,
	})
	rt.mod.UseFocus(rt.window.Focus)
	rt.mod.UseWindowReady(rt.window.WaitReady)
	rt.drive.UsePathSelector(rt.mod)
	rt.mod.UseFixInspection(rt.tools.QueueFixInspections)
	rt.drive.UseFixInspection(rt.tools.QueueFixInspections)
	rt.mod.UseToggleState(rt.tools)
	return rt
}

// startServices runs the Start methods of feature services. The UI host calls it once with the application
// context, after the database is open and before any window exists.
func (rt *runtime) startServices(ctx context.Context) error {
	for _, start := range []func(context.Context) error{rt.auth.Start, rt.backup.Start, rt.transfer.Start, rt.xxmi.Start} {
		if err := start(ctx); err != nil {
			return err
		}
	}
	return nil
}
