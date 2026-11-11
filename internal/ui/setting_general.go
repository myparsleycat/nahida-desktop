package ui

import (
	"context"
	"math"
	"strings"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/icons/lucide"
	"github.com/myparsleycat/fairy/kit"

	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/platform"
	"nahida.live/desktop/internal/setting"
)

var generalSettingKeys = []string{
	setting.KeyGeneralRunOnStartup,
	setting.KeyGeneralLanguage,
	setting.KeyGeneralAutoUpdateMode,
	setting.KeyGeneralIncludePrerelease,
	setting.KeyGeneralRunInBackground,
	setting.KeyGeneralTitlebarActivityBadgeClickNavigate,
	setting.KeyGeneralDefaultStartPage,
	setting.KeyGeneralLogLevel,
	setting.KeyGeneralElevatedHelperEnabled,
	setting.KeyGeneralWindowBackdrop,
	setting.KeyGeneralTheme,
}

type generalSection struct {
	// confirmingBackground is set while the dialog about turning off background running is open.
	confirmingBackground bool
}

// updateCheck tracks a check started from the settings footer.
type updateCheck struct {
	pending     bool
	sawChecking bool
	seq         int
	toast       kit.ToastID
}

func (s *Shell) generalSettings() *fairy.El {
	if !s.settingsLoaded(generalSettingKeys...) {
		return s.settingsLoading()
	}
	application := []*fairy.El{}
	if s.deps.AppStatus.SupportsAutostart {
		application = append(application, s.settingSwitch(setting.KeyGeneralRunOnStartup,
			T("page.setting.gen.application.runOnStartup"), T("page.setting.gen.application.runOnStartupDescription")))
	}
	application = append(application,
		s.settingSwitch(setting.KeyGeneralElevatedHelperEnabled, T("page.setting.gen.application.elevatedHelper"),
			T("page.setting.gen.application.elevatedHelperDescription")),
		settingRow(T("page.setting.gen.application.runInBackground"),
			T("page.setting.gen.application.runInBackgroundDescription"),
			kit.Switch(s.boolSetting(setting.KeyGeneralRunInBackground), s.setRunInBackground).
				Label(T("page.setting.gen.application.runInBackground"))),
		s.settingSwitch(setting.KeyGeneralTitlebarActivityBadgeClickNavigate,
			T("page.setting.gen.application.titlebarActivityBadgeClickNavigate"),
			T("page.setting.gen.application.titlebarActivityBadgeClickNavigateDescription")),
	)
	if s.deps.AppStatus.SupportsWindowBackdrop {
		application = append(application, s.settingSwitch(
			setting.KeyGeneralWindowBackdrop,
			T(
				"page.setting.gen.application.windowBackdrop",
			),
			T("page.setting.gen.application.windowBackdropDescription"),
		))
	}
	application = append(application, s.generalSelects())

	return settingsBody(
		settingsGroup(T("page.setting.gen.application.title"), application...),
		settingsGroup(T("page.setting.gen.update.title"), s.updateSettings()...),
	)
}

func (s *Shell) generalSelects() *fairy.El {
	languages := []kit.Item{
		{Value: "ko", Label: "한국어"},
		{Value: "en", Label: "English"},
		{Value: "ja", Label: "日本語"},
		{Value: "zh", Label: "中文"},
	}
	themes := []kit.Item{
		{Value: setting.ThemeSystem, Label: T("page.setting.gen.theme.system")},
		{Value: setting.ThemeLight, Label: T("page.setting.gen.theme.light")},
		{Value: setting.ThemeDark, Label: T("page.setting.gen.theme.dark")},
	}
	locked := s.sessionKnown && !s.signedIn()
	startPages := []kit.Item{
		{Value: string(RouteTransfer), Label: T("page.transfer.title")},
		{Value: string(RouteGameBanana), Label: T("page.gamebanana.title")},
		{Value: string(RouteMod), Label: T("page.mod.title")},
		{Value: "/drive/drive/root", Label: T("page.drive.title"), Disabled: locked},
		{Value: "/drive/share/root", Label: T("page.share_drive.title"), Disabled: locked},
	}
	startPage := s.stringSetting(setting.KeyGeneralDefaultStartPage)
	if route, ok := RouteOf(startPage); locked && ok && route.requiresSession() {
		startPage = string(RouteMod)
	}
	logLevels := []kit.Item{
		{Value: "trace", Label: "Trace"},
		{Value: "debug", Label: "Debug"},
		{Value: "info", Label: "Info"},
		{Value: "warn", Label: "Warn"},
		{Value: "error", Label: "Error"},
		{Value: "fatal", Label: "Fatal"},
	}

	field := func(key, title, placeholder, value string, items []kit.Item) *fairy.El {
		return fairy.Col(
			fairy.Text(title).FontSize(14).LineHeight(20).Medium(),
			// Base UI renders a hidden input after the trigger, so space-y-3 also leaves 12px below it.
			kit.Select("settings."+key, value, items, func(v string) { s.setSetting(key, v) },
				kit.Placeholder(placeholder), kit.Label(title)).MarginEdges(0, 0, 12, 0),
		).Grow(1).Basis(0).MinW(0)
	}
	return fairy.Col(
		fairy.Row(
			field(setting.KeyGeneralLanguage, T("page.setting.gen.language.title"),
				T("page.setting.gen.language.select"), s.stringSetting(setting.KeyGeneralLanguage), languages),
			field(setting.KeyGeneralTheme, T("page.setting.gen.theme.title"), T("page.setting.gen.theme.select"),
				s.themeChoice, themes),
		).Gap(12),
		fairy.Row(
			field(setting.KeyGeneralDefaultStartPage, T("page.setting.gen.startPage.title"),
				T("page.setting.gen.startPage.select"), startPage, startPages),
			field(setting.KeyGeneralLogLevel, T("page.setting.gen.logLevel.title"),
				T("page.setting.gen.logLevel.select"), s.stringSetting(setting.KeyGeneralLogLevel), logLevels),
		).Gap(12),
	).Gap(12).PadXY(0, 12)
}

// setRunInBackground asks first before turning background running off while a feature needs it.
func (s *Shell) setRunInBackground(on bool) {
	if on {
		s.setSetting(setting.KeyGeneralRunInBackground, true)
		return
	}
	if s.settings.general.confirmingBackground {
		return
	}
	go func() {
		values, err := s.deps.Settings.GetMany(context.Background(),
			[]string{setting.KeyXXMIPersistToggles, setting.KeyXXMIToggleViewerAutoGenerate})
		if err != nil {
			err = infra.ReportError(s.deps.Log, err, "Settings.GetMany", infra.Diagnostic{
				Operation: "settings.run-in-background", Stage: "read-dependents",
			})
		}
		fairy.Go(func() {
			if err != nil {
				s.toasts.Show(kit.ToastError, T("shell.setting_load_failed"), err.Error())
				return
			}
			persist, _ := values[setting.KeyXXMIPersistToggles].(bool)
			viewer, _ := values[setting.KeyXXMIToggleViewerAutoGenerate].(bool)
			if !persist && !viewer {
				s.setSetting(setting.KeyGeneralRunInBackground, false)
				return
			}
			s.settings.general.confirmingBackground = true
			s.Confirm(Confirmation{
				Title:        T("page.setting.gen.application.runInBackgroundDisableConfirmTitle"),
				Description:  T("page.setting.gen.application.runInBackgroundDisableConfirmDescription"),
				ConfirmLabel: T("page.setting.gen.application.runInBackgroundDisableConfirmAction"),
				Danger:       true,
				OnResult: func(confirmed bool) {
					s.settings.general.confirmingBackground = false
					if confirmed {
						s.setSetting(setting.KeyGeneralRunInBackground, false)
					}
				},
			})
		})
	}()
}

func (s *Shell) updateSettings() []*fairy.El {
	modes := []kit.Item{
		{Value: "auto", Label: T("page.setting.gen.application.autoUpdateModes.auto.title")},
		{Value: "notify", Label: T("page.setting.gen.application.autoUpdateModes.notify.title")},
		{Value: "off", Label: T("page.setting.gen.application.autoUpdateModes.off.title")},
	}
	mode := s.stringSetting(setting.KeyGeneralAutoUpdateMode)
	description := ""
	if mode != "" {
		description = T("page.setting.gen.application.autoUpdateModes." + mode + ".description")
	}

	status := s.updater
	offerDownload := status.UpdateAvailable && !status.UpdateDownloaded &&
		(status.ShouldPromptForUpdate || status.Mode == "notify")
	var action *fairy.El
	busy := s.updateBusy || status.IsChecking || status.IsDownloading
	switch {
	case status.UpdateDownloaded:
		action = kit.Button(T("updater.actions.install"), s.promptUpdate, kit.Outline, kit.Small,
			kit.Icon(lucide.RefreshCw), kit.Loading(busy))
	case offerDownload:
		action = kit.Button(T("updater.actions.download"), s.downloadUpdate, kit.Outline, kit.Small,
			kit.Icon(lucide.Download), kit.Loading(busy))
	}

	return []*fairy.El{
		settingRow(T("page.setting.gen.application.autoUpdate"), description,
			s.settingSelect(setting.KeyGeneralAutoUpdateMode, modes, 168,
				kit.Label(T("page.setting.gen.application.autoUpdate")))),
		s.settingSwitch(setting.KeyGeneralIncludePrerelease, T("page.setting.gen.application.includePrerelease"),
			T("page.setting.gen.application.includePrereleaseDescription")),
		settingRow(T("page.setting.gen.application.updateStatus"), s.updateStatusText(mode), action),
	}
}

func (s *Shell) updateStatusText(mode string) string {
	status := s.updater
	switch {
	case status.IsChecking:
		return T("updater.status.checking")
	case status.IsDownloading:
		if status.DownloadTotal > 0 {
			return T("updater.status.downloadingProgress",
				"percent", int(math.Round(float64(status.DownloadWritten)/float64(status.DownloadTotal)*100)))
		}
		return T("updater.status.downloading")
	case status.LastError != nil:
		return T("updater.status.failed", "message", *status.LastError)
	case status.UpdateDownloaded:
		return T("updater.status.downloaded")
	case status.UpdateAvailable:
		versionRange := ""
		if status.ReleaseVersion != nil {
			versionRange = " (" + platform.AppVersion + " → " + strings.TrimPrefix(*status.ReleaseVersion, "v") + ")"
		}
		return T("updater.status.available", "versionRangeText", versionRange)
	case mode == "off":
		return T("page.setting.gen.application.autoUpdateModes.off.title")
	}
	return T("updater.status.idle")
}

// settingsFooter shows the version and a manual update check.
func (s *Shell) settingsFooter() *fairy.El {
	t := kit.T()
	check := fairy.Span{Text: T("updater.actions.check"), Underline: true, OnClick: s.checkForUpdates}
	if s.settings.updateCheck.pending || s.updater.IsChecking {
		check = fairy.Span{Text: T("updater.status.checking"), Color: textDisabled(t)}
	}
	return fairy.Box(fairy.RichText(fairy.Span{Text: "v" + platform.AppVersion + " · "}, check)).
		PadXY(16, 12).FontSize(11).LineHeight(15.71).Color(t.MutedForeground)
}

// checkForUpdates checks for an update and reports the outcome as a toast. With automatic updates the check
// also waits for the download, so it counts as done once the backend starts downloading.
func (s *Shell) checkForUpdates() {
	check := &s.settings.updateCheck
	if check.pending {
		return
	}
	check.pending, check.sawChecking = true, false
	check.seq++
	check.toast = s.toasts.Loading(T("updater.status.checking"), "")
	seq := check.seq
	go func() {
		err := s.deps.Updater.CheckForUpdates(context.Background(), true)
		fairy.Go(func() { s.finishUpdateCheck(seq, err) })
	}()
}

func (s *Shell) observeUpdateCheck(status infra.UpdaterStatus) {
	check := &s.settings.updateCheck
	if !check.pending {
		return
	}
	check.sawChecking = check.sawChecking || status.IsChecking
	if check.sawChecking && !status.IsChecking && status.UpdateAvailable && status.IsDownloading {
		s.finishUpdateCheck(check.seq, nil)
	}
}

func (s *Shell) finishUpdateCheck(seq int, err error) {
	check := &s.settings.updateCheck
	if !check.pending || seq != check.seq {
		return
	}
	check.pending = false
	toast := check.toast
	if err != nil {
		err = infra.ReportError(s.deps.Log, err, "Updater.CheckForUpdates", infra.Diagnostic{
			Operation: "updater.check", Stage: "settings",
		})
		s.toasts.Update(toast, kit.ToastError, T("updater.status.failed", "message", err.Error()), "")
		return
	}
	go func() {
		status, err := s.deps.Updater.GetStatus(context.Background())
		fairy.Go(func() {
			if err == nil {
				s.setUpdater(status)
			}
			switch {
			case s.updater.UpdateDownloaded:
				s.toasts.Update(toast, kit.ToastSuccess, T("updater.status.downloaded"), "")
			case s.updater.UpdateAvailable:
				s.toasts.Update(toast, kit.ToastSuccess, T("updater.toast.available.title"), "")
			default:
				s.toasts.Update(toast, kit.ToastSuccess, T("updater.toast.notAvailable.title"), "")
			}
		})
	}()
}
