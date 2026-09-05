package app

import (
	"context"

	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/setting"
	"nahida.live/desktop/internal/tools"
	"nahida.live/desktop/internal/transfer"
)

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
				_ = infra.ReportError(log, err, "setting.powerSaveBlockInTransfer", infra.Diagnostic{Severity: infra.DiagnosticError, Operation: "setting.powerSaveBlockInTransfer", Stage: "background"})
			}
		},
		AfterBandwidthLimitChanged: transfers.SetDownloadBandwidthLimitMibps,
		AfterOpenConsoleChanged: func(enabled bool) {
			if windowService != nil {
				windowService.SetConsoleWindowEnabled(enabled)
			}
		},
		AfterPersistTogglesChanged: func(enabled bool) {
			if toolsService == nil {
				return
			}
			if enabled {
				if err := toolsService.StartPersistWatcher(context.Background()); err != nil {
					_ = infra.ReportError(log, err, "Setting.xxmi.persistToggles", infra.Diagnostic{Severity: infra.DiagnosticError, Operation: "Setting.xxmi.persistToggles", Stage: "background"})
				}
				return
			}
			toolsService.StopPersistWatcher()
		},
	}
}
