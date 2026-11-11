package ui

import (
	"github.com/myparsleycat/fairy"

	"nahida.live/desktop/internal/setting"
)

var transferSettingKeys = []string{
	setting.KeyTransferDownloadConcurrency,
	setting.KeyTransferDownloadBandwidthLimitMibps,
	setting.KeyTransferUploadConcurrency,
	setting.KeyGeneralMoveTransferPageWhenStartTransfer,
	setting.KeyGeneralPowerSaveBlockInTransfer,
}

func (s *Shell) transferSettings() *fairy.El {
	if !s.settingsLoaded(transferSettingKeys...) {
		return s.settingsLoading()
	}
	const prefix = "page.setting.transfer."
	number := func(key, name string) *fairy.El {
		return settingRow(T(prefix+name+".title"), T(prefix+name+".description"),
			s.settingNumber(key, T(prefix+name+".title"), 112))
	}
	return settingsBody(
		settingsGroup(T(prefix+"concurrency.title"),
			number(setting.KeyTransferDownloadConcurrency, "downloadConcurrency"),
			number(setting.KeyTransferUploadConcurrency, "uploadConcurrency"),
		),
		settingsGroup(T(prefix+"bandwidth.title"),
			number(setting.KeyTransferDownloadBandwidthLimitMibps, "downloadBandwidthLimitMibps"),
		),
		settingsGroup(T(prefix+"other.title"),
			s.settingSwitch(setting.KeyGeneralMoveTransferPageWhenStartTransfer,
				T("page.setting.gen.other.moveTransferPageWhenStartTransfer"),
				T("page.setting.gen.other.moveTransferPageWhenStartTransferDescription")),
			s.settingSwitch(setting.KeyGeneralPowerSaveBlockInTransfer,
				T("page.setting.gen.other.powerSaveBlockInTransfer"),
				T("page.setting.gen.other.powerSaveBlockInTransferDescription")),
		),
	)
}

func (s *Shell) toolsSettings() *fairy.El {
	if !s.settingsLoaded(setting.KeyToolsWuwaFixerUpdateNotification) {
		return s.settingsLoading()
	}
	const prefix = "page.setting.tools.wuwaFixer."
	return settingsBody(
		settingsGroup(T(prefix+"title"),
			s.settingSwitch(setting.KeyToolsWuwaFixerUpdateNotification, T(prefix+"autoUpdateNotification.title"),
				T(prefix+"autoUpdateNotification.description")),
		),
	)
}
