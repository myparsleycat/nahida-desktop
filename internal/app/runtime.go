package app

import (
	"context"
	"errors"
	"path/filepath"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"

	"nahida.live/desktop/internal/appdata"
	"nahida.live/desktop/internal/auth"
	"nahida.live/desktop/internal/drive"
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
	appData         *appdata.Store
	log             *infra.Log
	store           *infra.Store
	http            *infra.Client
	fs              *platform.FS
	native          *platform.Native
	updater         *infra.Updater
	protocol        *infra.Protocol
	download        *infra.Download
	archive         *infra.Archive
	auth            *auth.Auth
	setting         *setting.Setting
	drive           *drive.Drive
	transfer        *transfer.Transfer
	gamebanana      *gamebanana.GameBanana
	mod             *mod.Mod
	xxmi            *xxmi.XXMI
	tools           *tools.Tools
	dialog          *platform.Dialog
	shell           *platform.Shell
	window          *Window
	localHTTP       *infra.LocalHTTP
	gameBananaLogin *gameBananaLogin
	notifications   *notifications.NotificationService
}

type runtimePathInput struct {
	HomeDir    string
	Cwd        string
	Packaged   bool
	DBOverride string
}

type runtimePaths struct {
	Root string
	DB   string
	Logs string
}

func newRuntime() *runtime {
	log := infra.NewLogWithOptions(infra.LogOptions{Dev: !platform.Packaged()})
	httpClient := infra.NewClient()
	httpClient.UseLog(log)
	shell := platform.NewShell()
	fs := platform.NewFS()
	native := platform.NewNative()
	window := NewWindow()
	notifier := notifications.New()
	eventEmit := emitAppEvent
	transferService := transfer.NewWithOptions(transfer.Options{
		Log:               log,
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
	settings.UseHooks(runtimeSettingHooks(log, transferService, updaterService, nil, window, nil, eventEmit))
	rt := &runtime{
		log:      log,
		store:    infra.NewStore(),
		http:     httpClient,
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
			Log: log, EventEmit: eventEmit, Settings: settings, XXMI: xxmiService,
			FS: fs, HTTP: httpClient, Download: download, Archive: archive, Protocol: protocolService, GitHubRate: githubRate, Mod: modService,
			Notify: func(title, body string) error {
				return notifier.SendNotification(notifications.NotificationOptions{
					ID: "wuwa-mod-fixer-updated", Title: title, Body: body,
				})
			},
		}),
		dialog: dialog,
		shell:  shell,
		window: window,
		localHTTP: infra.NewLocalHTTPWithOptions(infra.LocalHTTPOptions{
			Version: platform.AppVersion,
			Log:     log,
		}),
		gameBananaLogin: login,
		notifications:   notifier,
	}
	rt.localHTTP.UseHandler(rt.handleLocalHTTPMessage)
	if rt.mod != nil {
		rt.mod.UseFocus(rt.window.Focus)
		rt.mod.UseWindowReady(rt.window.WaitReady)
	}
	if rt.drive != nil && rt.mod != nil {
		rt.drive.UsePathSelector(rt.mod)
	}
	settings.UseHooks(runtimeSettingHooks(log, transferService, updaterService, rt.tools, rt.window, nil, eventEmit))
	return rt
}

func emitAppEvent(name string, data ...any) {
	if app := application.Get(); app != nil {
		app.Event.Emit(name, data...)
	}
}

func runtimeSettingHooks(log *infra.Log, transfers *transfer.Transfer, updater *infra.Updater, toolsService *tools.Tools, windowService *Window, autostart func(bool) error, emit func(string, ...any)) setting.Hooks {
	return setting.Hooks{
		AfterRunOnStartupChanged: autostart,
		AfterSet: func(key string, value any) {
			if emit != nil {
				emit("setting:update", map[string]any{"key": key, "value": value})
			}
		},
		AfterRendererReload: func() {
			if emit != nil {
				emit("renderer:reload")
			}
		},
		AfterLanguageChanged: func(language string) {
			if emit != nil {
				emit("language:update", language)
			}
			if updater != nil {
				updater.HandleLanguageChanged(language)
			}
		},
		AfterAutoUpdateModeChanged: func(mode string) {
			if updater != nil {
				updater.HandleAutoUpdateModeChanged(mode)
			}
		},
		AfterLogLevelChanged: log.SetLevel,
		AfterPowerSaveBlockChanged: func() {
			if transfers == nil {
				return
			}
			if err := transfers.RefreshPowerSaveBlock(context.Background()); err != nil {
				log.Error(err.Error(), "setting.powerSaveBlockInTransfer")
			}
		},
		AfterBandwidthLimitChanged: transfers.SetDownloadBandwidthLimitMibps,
		AfterOpenConsoleChanged: func(enabled bool) {
			if windowService != nil {
				windowService.SetConsoleWindowEnabled(enabled)
			}
		},
		AfterToggleViewerAutoGenerateChanged: func(enabled bool) {
			if toolsService == nil {
				return
			}
			if enabled {
				state := toolsService.ToggleViewerGetState()
				if state.Mode != nil && *state.Mode == "generate" {
					return
				}
				if err := toolsService.StartToggleViewerWatcher(context.Background()); err != nil {
					log.Error(err.Error(), "Setting.xxmi.toggleViewerAutoGenerate")
				}
				return
			}
			toolsService.ToggleViewerCancelCurrentWork()
			if err := toolsService.StopToggleViewerWatcher(); err != nil {
				log.Error(err.Error(), "Setting.xxmi.toggleViewerAutoGenerate")
			}
		},
		AfterToggleViewerHotkeyChanged: func(hotkey string) {
			if toolsService != nil {
				if err := toolsService.ToggleViewerApplyHotkeyToArtifacts(context.Background(), hotkey); err != nil {
					log.Error(err.Error(), "Setting.xxmi.toggleViewerHotkey")
				}
			}
		},
		AfterPersistTogglesChanged: func(enabled bool) {
			if toolsService == nil {
				return
			}
			if enabled {
				if err := toolsService.StartPersistWatcher(context.Background()); err != nil {
					log.Error(err.Error(), "Setting.xxmi.persistToggles")
				}
				return
			}
			toolsService.StopPersistWatcher()
		},
	}
}

// bootRuntime opens the shared app data root, configures the logger,
// and always opens the store. This is the path app.Run uses.
func bootRuntime(ctx context.Context, rt *runtime, in runtimePathInput) (runtimePaths, error) {
	data, err := appdata.Open(in.HomeDir)
	if err != nil {
		return runtimePaths{}, err
	}
	logs, err := data.EnsureDir(appdata.LogsDir)
	if err != nil {
		return runtimePaths{Root: data.Root()}, err
	}
	dbPath := in.DBOverride
	if dbPath == "" {
		if in.Packaged {
			dbPath, err = data.Resolve(appdata.DatabaseFile)
			if err != nil {
				return runtimePaths{Root: data.Root(), Logs: logs}, err
			}
		} else {
			dbPath = filepath.Join(in.Cwd, "local.db")
		}
	}
	paths := runtimePaths{Root: data.Root(), DB: dbPath, Logs: logs}
	rt.appData = data
	if rt.mod != nil {
		rt.mod.UseAppData(data)
	}
	if rt.tools != nil {
		rt.tools.UseAppData(data)
	}
	rt.configureLog(paths, in.Packaged)
	if err := rt.Init(ctx, paths.DB); err != nil {
		return paths, err
	}
	if rt.localHTTP != nil {
		if err := rt.localHTTP.Start(); err != nil {
			return paths, err
		}
	}
	return paths, nil
}

func (rt *runtime) configureLog(paths runtimePaths, packaged bool) {
	if rt == nil || rt.log == nil {
		return
	}
	opts := infra.LogOptions{Dev: !packaged}
	if packaged {
		opts.Dest = infra.DesktopLogPath(paths.Logs)
	}
	rt.log.Configure(opts)
}

func (rt *runtime) Init(ctx context.Context, dbPath string) error {
	store, err := infra.OpenStore(ctx, dbPath)
	if err != nil {
		return err
	}
	if rt.store != nil {
		_ = rt.store.Close()
	}
	rt.store = store
	if rt.updater != nil && store.DB != nil {
		rt.updater.AttachGitHubRate(store.DB.AppState, rt.http, rt.log)
	}
	if rt.log == nil {
		rt.log = infra.NewLogWithOptions(infra.LogOptions{Dev: !platform.Packaged()})
	}
	if rt.setting == nil {
		rt.setting = setting.NewWithOptions(store.DB, setting.Options{
			Locale: platform.SystemLocale(),
			Hooks:  runtimeSettingHooks(rt.log, rt.transfer, rt.updater, rt.tools, rt.window, nil, emitAppEvent),
		})
	} else {
		rt.setting.UseClient(store.DB)
		rt.setting.UseLocale(platform.SystemLocale())
		rt.setting.UseHooks(runtimeSettingHooks(rt.log, rt.transfer, rt.updater, rt.tools, rt.window, nil, emitAppEvent))
	}
	if rt.transfer != nil {
		rt.transfer.UseSettings(rt.setting)
	}
	if err := rt.setting.MigrateElectronStorage(ctx); err != nil {
		_ = rt.Close()
		return err
	}
	if _, err := rt.setting.Get(ctx, setting.KeyGeneralLanguage); err != nil {
		_ = rt.Close()
		return err
	}
	level, err := rt.setting.Get(ctx, setting.KeyGeneralLogLevel)
	if err != nil {
		_ = rt.Close()
		return err
	}
	s, _ := level.(string)
	rt.log.SetLevel(s)
	if rt.transfer != nil {
		if err := rt.transfer.ApplyBandwidthLimitsFromSettings(ctx); err != nil {
			_ = rt.Close()
			return err
		}
	}
	if rt.auth != nil {
		rt.auth.UseClient(store.DB)
	}
	if rt.xxmi != nil {
		rt.xxmi.UseClient(store.DB)
	}
	if rt.gamebanana != nil {
		rt.gamebanana.UseClient(store.DB)
	}
	if rt.mod != nil {
		rt.mod.UseClient(store.DB)
		rt.mod.UseSettings(rt.setting)
	}
	if rt.native != nil {
		rt.native.StartFocusTracking()
	}
	if rt.tools != nil {
		rt.tools.UseClient(store.DB)
		if err := rt.tools.CleanupStaleModelViewerDirs(); err != nil {
			rt.log.Warn(err.Error(), "StaticGlb.cleanupStaleViewerTempDirs")
		}
		if err := rt.tools.CleanupStaleD3DBuilds(ctx); err != nil {
			rt.log.Warn(err.Error(), "4001Fixer:cleanupStaleBuildDirs")
		}
		rt.tools.Start4001ReleasePrefetch()
		if err := rt.tools.RecoverBisects(ctx); err != nil {
			rt.log.Error(err.Error(), "ModBisect")
		}
		rt.tools.StartWuwaAutoUpdateCheck()
		if err := rt.tools.StartToggleViewerWatcher(ctx); err != nil {
			rt.log.Error(err.Error(), "ToggleViewer")
		}
		if err := rt.tools.StartPersistWatcher(ctx); err != nil {
			rt.log.Error(err.Error(), "TogglePersist")
		}
	}
	return nil
}

func (rt *runtime) Close() error {
	if rt == nil {
		return nil
	}
	if rt.gameBananaLogin != nil {
		rt.gameBananaLogin.Close()
	}
	if rt.gamebanana != nil {
		_ = rt.gamebanana.ServiceShutdown()
	}
	var err error
	if rt.localHTTP != nil {
		err = errors.Join(err, rt.localHTTP.ServiceShutdown())
	}
	if rt.updater != nil {
		err = errors.Join(err, rt.updater.ServiceShutdown())
	}
	if rt.protocol != nil {
		err = errors.Join(err, rt.protocol.ServiceShutdown())
	}
	if rt.transfer != nil {
		err = errors.Join(err, rt.transfer.ServiceShutdown())
	}
	if rt.window != nil {
		rt.window.CloseTaskbar()
	}
	if rt.auth != nil {
		err = errors.Join(err, rt.auth.ServiceShutdown())
	}
	if rt.mod != nil {
		err = errors.Join(err, rt.mod.ServiceShutdown())
	}
	if rt.tools != nil {
		err = errors.Join(err, rt.tools.ServiceShutdown())
	}
	if rt.native != nil {
		err = errors.Join(err, rt.native.Close())
	}
	if rt.log != nil {
		err = errors.Join(err, rt.log.Close())
	}
	if rt.store != nil {
		err = errors.Join(err, rt.store.Close())
	}
	return err
}

func (rt *runtime) services() []application.Service {
	return []application.Service{
		application.NewService(rt.auth),
		application.NewService(rt.dialog),
		application.NewService(rt.drive),
		application.NewService(rt.fs),
		application.NewService(rt.gamebanana),
		application.NewService(rt.log),
		application.NewService(rt.mod),
		application.NewService(rt.notifications),
		application.NewServiceWithOptions(rt.protocol, application.ServiceOptions{Route: "/protocol"}),
		application.NewService(rt.setting),
		application.NewService(rt.shell),
		application.NewService(rt.tools),
		application.NewService(rt.transfer),
		application.NewService(rt.updater),
		application.NewService(rt.window),
		application.NewService(rt.xxmi),
	}
}
