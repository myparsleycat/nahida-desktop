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
func bootRuntime(
	ctx context.Context,
	rt *runtime,
	in runtimePathInput,
	configureBrowserArguments func([]string) error,
) (runtimePaths, error) {
	data, err := appdata.Locate(in.HomeDir)
	if err != nil {
		return runtimePaths{}, err
	}
	rt.elevated.UseAppData(data)
	dbPath := in.DBOverride
	if dbPath == "" {
		if in.Packaged {
			dbPath, err = data.Resolve(appdata.DatabaseFile)
			if err != nil {
				return runtimePaths{Root: data.Root()}, err
			}
		} else {
			dbPath = filepath.Join(in.Cwd, "local.db")
		}
	}

	// The restore runs before the root and the logs directory are created because either may be one
	// of the denied entries; its outcome is logged once the log file is configured. Creating the logs
	// directory also creates the root.
	restored, accessErr := rt.appDataAccess.restore(ctx, startupAppDataEntries(data, dbPath))
	logs, err := data.EnsureDir(appdata.LogsDir)
	if err != nil {
		if accessErr != nil {
			return runtimePaths{Root: data.Root()}, accessErr
		}
		return runtimePaths{Root: data.Root()}, err
	}
	paths := runtimePaths{Root: data.Root(), DB: dbPath, Logs: logs}
	rt.configureLog(paths, in.Packaged)
	if accessErr != nil {
		_ = infra.ReportError(rt.log, accessErr.cause, "AppData", infra.Diagnostic{
			Operation: "restore-access", Stage: "startup", Fields: map[string]any{"paths": accessErr.paths},
		})
		return paths, accessErr
	}
	if len(restored) > 0 {
		rt.log.Warn(map[string]any{"paths": restored}, "AppData.restoreAccess")
	}

	rt.appData = data
	rt.mod.UseAppData(data)
	rt.drive.UseAppData(data)
	rt.tools.UseAppData(data)
	rt.reshade.UseAppData(data)
	if err := rt.agent.UseAppData(data); err != nil {
		return runtimePaths{Root: data.Root()}, err
	}
	if err := rt.Init(ctx, paths.DB, configureBrowserArguments); err != nil {
		return paths, err
	}
	return paths, nil
}

func (rt *runtime) configureLog(paths runtimePaths, packaged bool) {
	opts := infra.LogOptions{Dev: !packaged}
	if packaged {
		opts.Dest = infra.DesktopLogPath(paths.Logs)
	}
	rt.log.Configure(opts)
}

func (rt *runtime) Init(ctx context.Context, dbPath string, configureBrowserArguments func([]string) error) error {
	store, err := infra.OpenStore(ctx, dbPath)
	if err != nil {
		return err
	}
	if rt.store != nil {
		_ = infra.ReportError(
			rt.log,
			rt.store.Close(),
			"Runtime",
			infra.Diagnostic{Operation: "startup", Stage: "close-previous-store"},
		)
	}
	rt.store = store
	rt.updater.AttachGitHubRate(store.DB.AppState, rt.log)
	rt.setting.UseClient(store.DB)
	rt.setting.UseLocale(platform.SystemLocale())
	rt.transfer.UseSettings(rt.setting)
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
	if err := rt.initProxy(ctx, configureBrowserArguments); err != nil {
		return rt.failInit(err, "proxy")
	}
	if err := rt.transfer.ApplyBandwidthLimitsFromSettings(ctx); err != nil {
		return rt.failInit(err, "bandwidth-settings")
	}
	rt.auth.UseClient(store.DB)
	rt.xxmi.UseClient(store.DB)
	rt.reshade.UseClient(store.DB)
	rt.gamebanana.UseClient(store.DB)
	rt.mod.UseClient(store.DB)
	rt.mod.UseSettings(rt.setting)
	rt.modRuntime.UseClient(store.DB)
	rt.native.StartFocusTracking()
	rt.tools.UseClient(store.DB)
	rt.backup.UseClient(store.DB)
	if err := rt.agent.UseClient(ctx, store.DB); err != nil {
		return rt.failInit(err, "agent")
	}
	return nil
}

func (rt *runtime) Close() error {
	var err error
	rt.githubRate.Close()
	rt.startup.stop()
	rt.gameLink.Shutdown()
	rt.elevatedLifecycle.shutdown()
	err = errors.Join(err, infra.AnnotateError(rt.elevated.Close(), infra.Diagnostic{Stage: "elevated-helper"}))
	rt.gameBananaLogin.Close()
	_ = infra.ReportError(
		rt.log,
		rt.gamebanana.ServiceShutdown(),
		"Runtime",
		infra.Diagnostic{Operation: "shutdown", Stage: "gamebanana"},
	)
	if rt.proxyRelay != nil {
		err = errors.Join(err, rt.proxyRelay.Close())
		rt.proxyRelay = nil
	}
	err = errors.Join(err, infra.AnnotateError(rt.updater.ServiceShutdown(), infra.Diagnostic{Stage: "updater"}))
	err = errors.Join(err, infra.AnnotateError(rt.protocol.ServiceShutdown(), infra.Diagnostic{Stage: "protocol"}))
	err = errors.Join(err, infra.AnnotateError(rt.transfer.ServiceShutdown(), infra.Diagnostic{Stage: "transfer"}))
	rt.window.CloseTaskbar()
	err = errors.Join(err, infra.AnnotateError(rt.auth.ServiceShutdown(), infra.Diagnostic{Stage: "auth"}))
	err = errors.Join(err, infra.AnnotateError(rt.backup.ServiceShutdown(), infra.Diagnostic{Stage: "backup"}))
	err = errors.Join(err, infra.AnnotateError(rt.xxmi.ServiceShutdown(), infra.Diagnostic{Stage: "xxmi"}))
	err = errors.Join(err, infra.AnnotateError(rt.mod.ServiceShutdown(), infra.Diagnostic{Stage: "mod"}))
	err = errors.Join(err, infra.AnnotateError(rt.agent.ServiceShutdown(), infra.Diagnostic{Stage: "agent"}))
	err = errors.Join(err, infra.AnnotateError(rt.tools.ServiceShutdown(), infra.Diagnostic{Stage: "tools"}))
	err = errors.Join(err, infra.AnnotateError(rt.native.Close(), infra.Diagnostic{Stage: "native"}))
	err = infra.ReportError(rt.log, err, "Runtime", infra.Diagnostic{Operation: "shutdown"})
	if rt.store != nil {
		storeErr := rt.store.Close()
		_ = infra.ReportError(rt.log, storeErr, "Runtime", infra.Diagnostic{Operation: "shutdown", Stage: "store"})
		err = errors.Join(err, storeErr)
	}
	return errors.Join(err, rt.log.Close())
}

func (rt *runtime) failInit(err error, stage string) error {
	_ = infra.ReportError(
		rt.log,
		err,
		"Runtime",
		infra.Diagnostic{Operation: "startup", Stage: stage},
	)
	return err
}
