package app

import (
	"context"
	"sync"

	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/mod"
	"nahida.live/desktop/internal/platform"
	"nahida.live/desktop/internal/setting"
	"nahida.live/desktop/internal/tools"
	"nahida.live/desktop/internal/transfer"
)

func runtimeSettingHooks(
	log *infra.Log,
	transfers *transfer.Transfer,
	updater *infra.Updater,
	toolsService *tools.Tools,
	windowService *Window,
	autostart func(bool) error,
	emit func(string, ...any),
	syncModelViewerMenu func(language string),
	elevatedHelperChanged func(enabled bool),
	modServices ...*mod.Mod,
) setting.Hooks {
	var persistTransition sync.Mutex
	return setting.Hooks{
		AfterRunOnStartupChanged:   autostart,
		AfterElevatedHelperChanged: elevatedHelperChanged,
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
			if syncModelViewerMenu != nil {
				syncModelViewerMenu(language)
			}
		},
		AfterAutoUpdateModeChanged: func(mode string) {
			if updater != nil {
				updater.HandleAutoUpdateModeChanged(mode)
			}
		},
		AfterIncludePrereleaseChanged: func(enabled bool) {
			if updater != nil {
				updater.HandleIncludePrereleaseChanged(enabled)
			}
		},
		AfterLogLevelChanged: log.SetLevel,
		AfterPowerSaveBlockChanged: func() {
			if transfers == nil {
				return
			}
			if err := transfers.RefreshPowerSaveBlock(context.Background()); err != nil {
				_ = infra.ReportError(
					log,
					err,
					"setting.powerSaveBlockInTransfer",
					infra.Diagnostic{
						Severity:  infra.DiagnosticError,
						Operation: "setting.powerSaveBlockInTransfer",
						Stage:     "background",
					},
				)
			}
		},
		AfterBandwidthLimitChanged: transfers.SetDownloadBandwidthLimitMibps,
		AfterOpenConsoleChanged: func(enabled bool) {
			if windowService != nil {
				windowService.SetConsoleWindowEnabled(enabled)
			}
		},
		AfterPersistTogglesChanged: func(bool) {
			persistTransition.Lock()
			defer persistTransition.Unlock()

			if len(modServices) > 0 && modServices[0] != nil {
				mods := modServices[0]
				if err := mods.StopNamespaceIsolation(); err != nil {
					_ = infra.ReportError(log, err, "Setting.xxmi.persistToggles", infra.Diagnostic{
						Operation: "namespace-isolation", Stage: "stop",
					})
				}
				if toolsService != nil {
					toolsService.StopPersistWatcher()
				}
				if err := mods.StartNamespaceIsolation(context.Background()); err != nil {
					_ = infra.ReportError(log, err, "Setting.xxmi.persistToggles", infra.Diagnostic{
						Operation: "namespace-isolation", Stage: "start",
					})
				}
			}
			if toolsService == nil {
				return
			}
			// StartPersistWatcher reads the current stored setting. A delayed callback
			// must not override a newer transition using its stale boolean argument.
			if err := toolsService.StartPersistWatcher(context.Background()); err != nil {
				_ = infra.ReportError(
					log,
					err,
					"Setting.xxmi.persistToggles",
					infra.Diagnostic{
						Severity:  infra.DiagnosticError,
						Operation: "Setting.xxmi.persistToggles",
						Stage:     "background",
					},
				)
			}
		},
	}
}

func modelViewerMenuSyncer(log *infra.Log) func(language string) {
	return func(language string) {
		if err := platform.UpdateModelViewerContextMenu(language); err != nil {
			_ = infra.ReportError(
				log,
				err,
				"App:syncModelViewerMenu",
				infra.Diagnostic{Operation: "App:syncModelViewerMenu", Stage: "background"},
			)
		}
	}
}
