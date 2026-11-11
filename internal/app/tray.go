package app

import (
	"context"
	"strings"
	"sync"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/tray"

	"nahida.live/desktop/internal/infra"
)

const updateCheckNotificationID = "updater-check"

// trayUpdateCheck follows a tray-initiated check until its outcome has been
// announced. The check call alone is not enough: it blocks through an
// auto-mode download, and it returns at once when another check or download
// is already running.
type trayUpdateCheck struct {
	mu             sync.Mutex
	active         bool
	watching       bool
	awaitingSettle bool
	sawDownload    bool
	lastNotice     string
}

type updateCheckTexts struct {
	failed         string
	downloadFailed string
	checking       string
	downloading    string
	ready          string
	available      string
	upToDate       string
	noUpdates      string
}

var updateCheckTextsByLanguage = map[string]updateCheckTexts{
	"en": {
		failed:         "Update check failed",
		downloadFailed: "Update download failed",
		checking:       "Checking for updates...",
		downloading:    "Downloading the update...",
		ready:          "The update is ready to install.",
		available:      "A new version is available.",
		upToDate:       "You're up to date.",
		noUpdates:      "There are no updates available right now.",
	},
	"ja": {
		failed:         "アップデートの確認に失敗しました",
		downloadFailed: "アップデートのダウンロードに失敗しました",
		checking:       "アップデートを確認しています...",
		downloading:    "アップデートをダウンロードしています...",
		ready:          "アップデートをインストールする準備ができました。",
		available:      "新しいバージョンが利用可能です。",
		upToDate:       "最新バージョンを使用しています。",
		noUpdates:      "現在利用可能なアップデートはありません。",
	},
	"ko": {
		failed:         "업데이트 확인에 실패했습니다",
		downloadFailed: "업데이트 다운로드에 실패했습니다",
		checking:       "업데이트를 확인하는 중입니다...",
		downloading:    "업데이트를 다운로드하는 중입니다...",
		ready:          "업데이트를 설치할 준비가 되었습니다.",
		available:      "새로운 버전이 출시되었습니다.",
		upToDate:       "최신 버전을 사용 중입니다.",
		noUpdates:      "현재 사용할 수 있는 업데이트가 없습니다.",
	},
	"zh": {
		failed:         "检查更新失败",
		downloadFailed: "下载更新失败",
		checking:       "正在检查更新...",
		downloading:    "正在下载更新...",
		ready:          "更新已准备好安装。",
		available:      "有新版本可用。",
		upToDate:       "当前已是最新版本。",
		noUpdates:      "目前没有可用更新。",
	},
}

// updateCheckNotice describes the outcome of a tray-initiated check. The tray
// has no window to show it in, so the result is delivered as a notification.
func updateCheckNotice(
	language string,
	status infra.UpdaterStatus,
	err error,
	downloadFailed bool,
) (title, body string) {
	texts, ok := updateCheckTextsByLanguage[language]
	if !ok {
		texts = updateCheckTextsByLanguage["en"]
	}
	version := ""
	if status.ReleaseVersion != nil {
		version = "v" + strings.TrimPrefix(*status.ReleaseVersion, "v")
	}
	if err != nil {
		if !downloadFailed {
			return texts.failed, err.Error()
		}
		if version == "" {
			return texts.downloadFailed, err.Error()
		}
		return texts.downloadFailed, version + ": " + err.Error()
	}

	switch {
	case status.UpdateDownloaded:
		return texts.ready, version
	case status.IsDownloading:
		return texts.downloading, version
	case status.UpdateAvailable:
		return texts.available, version
	case status.IsChecking:
		return texts.checking, ""
	default:
		return texts.upToDate, texts.noUpdates
	}
}

func (rt *runtime) checkForUpdatesFromTray() {
	ctx := context.Background()
	check := &rt.updateCheck
	check.mu.Lock()
	if check.active || check.watching {
		check.mu.Unlock()
		return
	}
	check.active = true
	check.watching, check.awaitingSettle, check.sawDownload, check.lastNotice = true, false, false, ""
	check.mu.Unlock()

	err := rt.updater.CheckForUpdates(ctx, true)
	// From here a settled status is the result, even if it arrives before the
	// status read below.
	check.mu.Lock()
	check.awaitingSettle = true
	check.mu.Unlock()
	status, statusErr := rt.updater.GetStatus(ctx)
	if err == nil {
		err = statusErr
	}

	check.mu.Lock()
	defer func() {
		check.active = false
		check.mu.Unlock()
	}()
	if err != nil {
		check.watching = false
		if infra.IsCancellationError(err) {
			return
		}
		if rt.log != nil {
			err = infra.ReportError(rt.log, err, "updater.manualCheck", infra.Diagnostic{
				Severity:  infra.DiagnosticError,
				Operation: "updater.manualCheck",
				Stage:     "background",
			})
		}
		rt.sendUpdateCheckNotice(status, err)
		return
	}
	if !check.watching {
		return
	}
	check.watching = status.IsChecking || status.IsDownloading
	rt.sendUpdateCheckNotice(status, nil)
}

// observeUpdaterStatus announces progress and results the check call itself
// cannot: a download started by the check, and the outcome of a check or
// download that was already running when the tray item was clicked.
func (rt *runtime) observeUpdaterStatus(status infra.UpdaterStatus) {
	check := &rt.updateCheck
	check.mu.Lock()
	defer check.mu.Unlock()
	if !check.watching {
		return
	}
	switch {
	case status.IsDownloading:
		check.sawDownload = true
	case status.IsChecking || !check.awaitingSettle:
		return
	default:
		check.watching = false
		if check.sawDownload && status.UpdateAvailable && !status.UpdateDownloaded {
			return
		}
	}
	rt.sendUpdateCheckNotice(status, nil)
}

// sendUpdateCheckNotice requires rt.updateCheck.mu.
func (rt *runtime) sendUpdateCheckNotice(status infra.UpdaterStatus, err error) {
	if rt.notify == nil {
		return
	}
	language := "en"
	if rt.setting != nil {
		if value, langErr := rt.setting.GetLanguage(context.Background()); langErr == nil {
			language = value
		}
	}
	title, body := updateCheckNotice(language, status, err, rt.updateCheck.sawDownload)
	notice := title + "\n" + body
	if notice == rt.updateCheck.lastNotice {
		return
	}
	rt.updateCheck.lastNotice = notice

	notifyErr := rt.notify(updateCheckNotificationID, title, body)
	if notifyErr != nil && rt.log != nil {
		_ = infra.ReportError(rt.log, notifyErr, "updater.manualCheck", infra.Diagnostic{
			Severity:  infra.DiagnosticWarn,
			Operation: "updater.manualCheck",
			Stage:     "notify",
		})
	}
}

// newTray runs on the UI thread.
func newTray(rt *runtime, icon []byte) (*tray.Tray, error) {
	return tray.New(tray.Options{
		Icon:    icon,
		Tooltip: "Nahida Desktop",
		OnClick: rt.window.Focus,
		// A notice is an update check result or a downloaded update, which the window shows.
		OnNotificationClick: rt.window.Focus,
		Menu: func() []fairy.MenuItem {
			items := []fairy.MenuItem{
				{Label: "Open Nahida Desktop", OnClick: rt.window.Focus},
				{Label: "Check for Updates...", OnClick: func() { go rt.checkForUpdatesFromTray() }},
			}
			if status, err := rt.updater.GetStatus(context.Background()); err == nil && status.UpdateDownloaded {
				items = append(
					items,
					fairy.MenuItem{Label: "Restart to Update", OnClick: func() { go rt.installUpdate() }},
				)
			}
			return append(items,
				fairy.MenuItem{Label: "Setting", OnClick: func() {
					rt.window.Focus()
					rt.emit("fn:navi", "/setting/gen")
				}},
				fairy.MenuItem{Separator: true},
				fairy.MenuItem{Label: "Quit", OnClick: rt.quit},
			)
		},
	})
}

func (rt *runtime) installUpdate() {
	if err := rt.updater.InstallUpdate(context.Background()); err != nil {
		_ = infra.ReportError(rt.log, err, "updater.install", infra.Diagnostic{
			Severity:  infra.DiagnosticError,
			Operation: "updater.install",
			Stage:     "restart",
		})
	}
}

// notifyUpdateReady announces a release the updater downloaded on its own.
func (rt *runtime) notifyUpdateReady() {
	if rt.notify == nil {
		return
	}
	language := "en"
	if value, err := rt.setting.GetLanguage(context.Background()); err == nil {
		language = value
	}
	status, _ := rt.updater.GetStatus(context.Background())
	title, body := updateCheckNotice(language, status, nil, false)
	_ = rt.notify(updateCheckNotificationID, title, body)
}
