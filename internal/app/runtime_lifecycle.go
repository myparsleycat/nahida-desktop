package app

import (
	"context"
	"errors"
	"path/filepath"

	"nahida.live/desktop/internal/appdata"
	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/platform"
	"nahida.live/desktop/internal/setting"
)

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
		_ = infra.ReportError(rt.log, rt.store.Close(), "Runtime", infra.Diagnostic{Operation: "startup", Stage: "close-previous-store"})
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
		return rt.failInit(err, "migrate-settings")
	}
	if _, err := rt.setting.Get(ctx, setting.KeyGeneralLanguage); err != nil {
		return rt.failInit(err, "read-language")
	}
	level, err := rt.setting.Get(ctx, setting.KeyGeneralLogLevel)
	if err != nil {
		return rt.failInit(err, "read-log-level")
	}
	s, _ := level.(string)
	rt.log.SetLevel(s)
	if rt.transfer != nil {
		if err := rt.transfer.ApplyBandwidthLimitsFromSettings(ctx); err != nil {
			return rt.failInit(err, "bandwidth-settings")
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
		if err := rt.mod.StartCompression(ctx); err != nil {
			_ = infra.ReportError(rt.log, err, "Mod:compression:start", infra.Diagnostic{Severity: infra.DiagnosticError, Operation: "Mod:compression:start", Stage: "background"})
		}
	}
	if rt.native != nil {
		rt.native.StartFocusTracking()
	}
	if rt.tools != nil {
		rt.tools.UseClient(store.DB)
		if err := rt.tools.CleanupStaleModelViewerDirs(); err != nil {
			_ = infra.ReportError(rt.log, err, "StaticGlb.cleanupStaleViewerTempDirs", infra.Diagnostic{Severity: infra.DiagnosticWarn, Operation: "StaticGlb.cleanupStaleViewerTempDirs", Stage: "background"})
		}
		if err := rt.tools.CleanupStaleD3DBuilds(ctx); err != nil {
			_ = infra.ReportError(rt.log, err, "4001Fixer:cleanupStaleBuildDirs", infra.Diagnostic{Severity: infra.DiagnosticWarn, Operation: "4001Fixer:cleanupStaleBuildDirs", Stage: "background"})
		}
		rt.tools.Start4001ReleasePrefetch()
		if err := rt.tools.RecoverBisects(ctx); err != nil {
			_ = infra.ReportError(rt.log, err, "ModBisect", infra.Diagnostic{Severity: infra.DiagnosticError, Operation: "ModBisect", Stage: "background"})
		}
		rt.tools.StartWuwaAutoUpdateCheck()
		if err := rt.tools.StartPersistWatcher(ctx); err != nil {
			_ = infra.ReportError(rt.log, err, "TogglePersist", infra.Diagnostic{Severity: infra.DiagnosticError, Operation: "TogglePersist", Stage: "background"})
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
		_ = infra.ReportError(rt.log, rt.gamebanana.ServiceShutdown(), "Runtime", infra.Diagnostic{Operation: "shutdown", Stage: "gamebanana"})
	}
	var err error
	if rt.localHTTP != nil {
		err = errors.Join(err, infra.AnnotateError(rt.localHTTP.ServiceShutdown(), infra.Diagnostic{Stage: "localHTTP"}))
	}
	if rt.updater != nil {
		err = errors.Join(err, infra.AnnotateError(rt.updater.ServiceShutdown(), infra.Diagnostic{Stage: "updater"}))
	}
	if rt.protocol != nil {
		err = errors.Join(err, infra.AnnotateError(rt.protocol.ServiceShutdown(), infra.Diagnostic{Stage: "protocol"}))
	}
	if rt.transfer != nil {
		err = errors.Join(err, infra.AnnotateError(rt.transfer.ServiceShutdown(), infra.Diagnostic{Stage: "transfer"}))
	}
	if rt.window != nil {
		rt.window.CloseTaskbar()
	}
	if rt.auth != nil {
		err = errors.Join(err, infra.AnnotateError(rt.auth.ServiceShutdown(), infra.Diagnostic{Stage: "auth"}))
	}
	if rt.mod != nil {
		err = errors.Join(err, infra.AnnotateError(rt.mod.ServiceShutdown(), infra.Diagnostic{Stage: "mod"}))
	}
	if rt.tools != nil {
		err = errors.Join(err, infra.AnnotateError(rt.tools.ServiceShutdown(), infra.Diagnostic{Stage: "tools"}))
	}
	if rt.native != nil {
		err = errors.Join(err, infra.AnnotateError(rt.native.Close(), infra.Diagnostic{Stage: "native"}))
	}
	err = infra.ReportError(rt.log, err, "Runtime", infra.Diagnostic{Operation: "shutdown"})
	if rt.log != nil {
		err = errors.Join(err, rt.log.Close())
	}
	if rt.store != nil {
		storeErr := rt.store.Close()
		_ = infra.ReportError(rt.log, storeErr, "Runtime", infra.Diagnostic{Operation: "shutdown", Stage: "store"})
		err = errors.Join(err, storeErr)
		if storeErr != nil && rt.log != nil {
			err = errors.Join(err, rt.log.Close())
		}
	}
	return err
}

func (rt *runtime) failInit(err error, stage string) error {
	cleanupErr := rt.Close()
	_ = infra.ReportError(rt.log, infra.WithCause(err, infra.AnnotateError(cleanupErr, infra.Diagnostic{Stage: "cleanup"})), "Runtime", infra.Diagnostic{Operation: "startup", Stage: stage})
	return err
}
