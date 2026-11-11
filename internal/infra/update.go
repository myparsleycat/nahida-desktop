package infra

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/fxamacker/cbor/v2"
	"github.com/samber/lo"
)

// An automatic check reuses release metadata younger than the check interval,
// so a new release is seen within two intervals of the last fetch: one hour.
const (
	updaterCheckInterval    = 30 * time.Minute
	updaterProgressInterval = 250 * time.Millisecond
	translationEndpoint     = "https://api.nahida.live/translate"
	maxTranslationPayload   = 8 << 20
)

// UpdateChannelPrerelease is the UpdateRelease.Channel of a GitHub pre-release.
const UpdateChannelPrerelease = "prerelease"

type UpdaterSettings interface {
	GetAutoUpdateMode(context.Context) (string, error)
	GetIncludePrerelease(context.Context) (bool, error)
	GetLanguage(context.Context) (string, error)
}

type UpdateRelease struct {
	Version string
	// Channel is UpdateChannelPrerelease for a pre-release and empty otherwise.
	Channel string
	Notes   string
}

// UpdateEngine finds, stages and installs application releases for Updater.
type UpdateEngine interface {
	// Check returns the newest installable release above the running version, or nil when there is none.
	Check(ctx context.Context, includePrerelease bool) (*UpdateRelease, error)
	// DownloadAndInstall stages the release found by the last Check, reporting the bytes written so far and the
	// total, which is 0 while unknown.
	DownloadAndInstall(ctx context.Context, progress func(written, total int64)) error
	// Restart replaces the running application with the staged release; the process exits on success.
	Restart(ctx context.Context) error
}

type UpdaterOptions struct {
	Engine   UpdateEngine
	Settings UpdaterSettings
	HTTP     *Client
	Log      *Log
	Emit     func(name string, data ...any)
	Ready    func()
	Interval time.Duration
}

type UpdaterReleaseNotes struct {
	Original           *string `json:"original"`
	Translated         *string `json:"translated"`
	TranslatedLanguage *string `json:"translatedLanguage"`
}

type UpdaterStatus struct {
	Mode                  string               `json:"mode"`
	UpdateAvailable       bool                 `json:"updateAvailable"`
	UpdateDownloaded      bool                 `json:"updateDownloaded"`
	ReleaseVersion        *string              `json:"releaseVersion"`
	ReleaseNotes          *UpdaterReleaseNotes `json:"releaseNotes"`
	ShouldPromptForUpdate bool                 `json:"shouldPromptForUpdate"`
	IsChecking            bool                 `json:"isChecking"`
	IsDownloading         bool                 `json:"isDownloading"`
	DownloadWritten       int64                `json:"downloadWritten"`
	DownloadTotal         int64                `json:"downloadTotal"`
	// LastError is the message of the last failed download, cleared by the next check or download.
	LastError *string `json:"lastError"`
}

type Updater struct {
	mu sync.Mutex

	engine   UpdateEngine
	settings UpdaterSettings
	http     *Client
	log      *Log
	rate     *GitHubRateCoordinator
	emit     func(string, ...any)
	ready    func()

	available                   bool
	downloaded                  bool
	releaseVersion              string
	releaseChannel              string
	notifiedVersion             string
	originalNotes               string
	translatedNotes             string
	translatedLang              string
	dialogDismissed             bool
	checking                    bool
	downloading                 bool
	downloadWritten             int64
	downloadTotal               int64
	progressSent                time.Time
	lastError                   string
	installing                  bool
	recheckAfterBusy            bool
	channelRefreshPending       bool
	includePrereleaseGeneration uint64
	translationSerial           uint64

	ctx      context.Context
	cancel   context.CancelFunc
	interval time.Duration
	loopWG   sync.WaitGroup
}

func NewUpdater() *Updater { return &Updater{interval: updaterCheckInterval} }

// UseGitHubRate shares the runtime's request budget and metadata cache.
func (u *Updater) UseGitHubRate(rate *GitHubRateCoordinator) {
	u.mu.Lock()
	u.rate = rate
	u.mu.Unlock()
}

// AttachGitHubRate binds the shared GitHub core-rate coordinator used to gate
// CheckForUpdates, matching Electron's GitHubRateCoordinator.
func (u *Updater) AttachGitHubRate(store githubRateStore, log *Log) {
	if u == nil {
		return
	}
	u.mu.Lock()
	if u.rate == nil {
		u.rate = NewGitHubRateCoordinator()
	}
	rate := u.rate
	u.mu.Unlock()
	rate.UseAppState(store)
	rate.UseLog(log)
}

func (u *Updater) Configure(opts UpdaterOptions) error {
	if u == nil {
		return errors.New("updater is nil")
	}
	if opts.Engine == nil {
		return errors.New("updater engine is not configured")
	}
	interval := opts.Interval
	if interval <= 0 {
		interval = updaterCheckInterval
	}
	ctx, cancel := context.WithCancel(context.Background())
	u.mu.Lock()
	if u.cancel != nil {
		u.cancel()
	}
	u.engine, u.settings, u.http, u.log = opts.Engine, opts.Settings, opts.HTTP, opts.Log
	u.emit, u.ready, u.interval = opts.Emit, opts.Ready, interval
	u.ctx, u.cancel = ctx, cancel
	u.mu.Unlock()
	u.loopWG.Add(1)
	go func() {
		defer u.loopWG.Done()
		u.automaticLoop(ctx, interval)
	}()
	return nil
}

func (u *Updater) automaticLoop(ctx context.Context, interval time.Duration) {
	u.runAutomaticCheck(ctx)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			u.runAutomaticCheck(ctx)
		}
	}
}

func (u *Updater) runAutomaticCheck(ctx context.Context) {
	mode, err := u.mode(ctx)
	if err != nil {
		u.logError(err, "updater.automaticCheck")
		return
	}
	if mode == "off" {
		return
	}
	if err := u.CheckForUpdates(ctx, false); err != nil {
		u.logError(err, "updater.automaticCheck")
	}
}

func (u *Updater) CheckForUpdates(ctx context.Context, userInitiated bool) error {
	if u == nil {
		return errors.New("updater is nil")
	}
	u.mu.Lock()
	if u.checking || u.installing {
		u.mu.Unlock()
		return nil
	}
	refreshPending := u.channelRefreshPending
	u.mu.Unlock()
	if refreshPending {
		return u.refreshUpdateCandidate(ctx, userInitiated, true)
	}
	allowed, err := u.githubRateAllowsCheck(ctx, userInitiated)
	if err != nil {
		return err
	}
	if !allowed {
		return nil
	}

	u.mu.Lock()
	if u.checking || u.installing {
		u.mu.Unlock()
		return nil
	}
	engine := u.engine
	available, downloading := u.available, u.downloading
	u.mu.Unlock()
	if engine == nil {
		u.mu.Lock()
		u.downloading = false
		u.mu.Unlock()
		return errors.New("updater is not configured")
	}
	if available {
		// A newer release can ship while a candidate is known or already
		// downloaded, so every check looks again instead of keeping it.
		if downloading {
			return nil
		}
		return u.refreshUpdateCandidate(ctx, userInitiated, false)
	}
	mode, err := u.mode(ctx)
	if err != nil {
		return err
	}

	u.mu.Lock()
	u.checking = true
	u.lastError = ""
	u.mu.Unlock()
	u.broadcastStatus(ctx)
	checkCtx := WithGitHubRefresh(u.metadataContext(ctx, "app-update"), userInitiated)
	release, err := u.checkRelease(checkCtx, engine)
	if err != nil {
		u.resetCheckedRelease(ctx)
		u.finishIncludePrereleaseChange(ctx)
		return err
	}
	if release == nil {
		u.mu.Lock()
		u.channelRefreshPending = false
		u.mu.Unlock()
		u.resetCheckedRelease(ctx)
		u.finishIncludePrereleaseChange(ctx)
		return nil
	}
	u.mu.Lock()
	u.checking = false
	u.channelRefreshPending = false
	u.available = true
	u.releaseVersion = release.Version
	u.releaseChannel = release.Channel
	u.originalNotes = normalizeReleaseNotes(release.Notes)
	u.clearTranslationLocked()
	u.mu.Unlock()
	u.broadcast("updater:update-available")
	u.broadcastStatus(ctx)
	u.translateCurrentReleaseNotes(ctx)
	if mode == "auto" {
		if u.skipPrereleaseDownload(ctx) {
			u.finishIncludePrereleaseChange(ctx)
			return nil
		}
		return u.DownloadUpdate(ctx)
	}
	u.finishIncludePrereleaseChange(ctx)
	return nil
}

// checkRelease reads the pre-release setting for each check, so the engine follows a setting changed since
// the previous one.
func (u *Updater) checkRelease(ctx context.Context, engine UpdateEngine) (*UpdateRelease, error) {
	include, err := u.includePrerelease(ctx)
	if err != nil {
		return nil, err
	}
	return engine.Check(ctx, include)
}

func (u *Updater) metadataContext(ctx context.Context, operation string) context.Context {
	u.mu.Lock()
	interval := u.interval
	u.mu.Unlock()
	return WithGitHubMaxAge(WithGitHubOperation(ctx, operation), interval)
}

func (u *Updater) resetCheckedRelease(ctx context.Context) {
	u.mu.Lock()
	u.resetCheckedReleaseLocked()
	u.mu.Unlock()
	u.broadcastStatus(ctx)
}

func (u *Updater) resetCheckedReleaseLocked() {
	u.checking, u.downloading = false, false
	u.available, u.downloaded = false, false
	u.releaseVersion, u.releaseChannel, u.originalNotes = "", "", ""
	u.clearTranslationLocked()
	u.dialogDismissed = false
}

func (u *Updater) DownloadUpdate(ctx context.Context) error {
	u.mu.Lock()
	if u.downloaded || !u.available || u.downloading || u.installing {
		u.mu.Unlock()
		return nil
	}
	engine := u.engine
	u.downloading = true
	u.downloadWritten, u.downloadTotal, u.lastError = 0, 0, ""
	u.mu.Unlock()
	if engine == nil {
		return errors.New("updater is not configured")
	}
	u.broadcastStatus(ctx)
	if err := engine.DownloadAndInstall(
		ctx,
		func(written, total int64) { u.downloadProgress(ctx, written, total) },
	); err != nil {
		u.mu.Lock()
		u.downloading = false
		if !IsCancellationError(err) {
			u.lastError = err.Error()
		}
		u.mu.Unlock()
		u.broadcastStatus(ctx)
		u.finishIncludePrereleaseChange(ctx)
		return err
	}
	u.mu.Lock()
	channel := u.releaseChannel
	deferRecheck := u.recheckAfterBusy
	u.mu.Unlock()
	if deferRecheck && channel == UpdateChannelPrerelease {
		include, err := u.includePrerelease(ctx)
		if err != nil || !include {
			u.mu.Lock()
			u.downloading = false
			u.mu.Unlock()
			u.broadcastStatus(ctx)
			u.finishIncludePrereleaseChange(ctx)
			if err != nil {
				return err
			}
			return nil
		}
	}
	u.mu.Lock()
	u.downloading = false
	u.downloaded = true
	u.dialogDismissed = false
	// The completed release is claimed as announced in the same critical
	// section, so an automatic recheck finishing concurrently cannot announce
	// it a second time.
	u.notifiedVersion = u.releaseVersion
	u.mu.Unlock()
	u.broadcastStatus(ctx)
	u.notifyReady()
	u.finishIncludePrereleaseChange(ctx)
	return nil
}

// downloadProgress records the download's progress and announces it at most a few times a second.
func (u *Updater) downloadProgress(ctx context.Context, written, total int64) {
	u.mu.Lock()
	u.downloadWritten, u.downloadTotal = written, total
	now := time.Now()
	send := now.Sub(u.progressSent) >= updaterProgressInterval || (total > 0 && written >= total)
	if send {
		u.progressSent = now
	}
	u.mu.Unlock()
	if send {
		u.broadcastStatus(ctx)
	}
}

func (u *Updater) InstallUpdate(ctx context.Context) error {
	u.mu.Lock()
	if u.installing {
		u.mu.Unlock()
		return nil
	}
	ready := u.downloaded && u.available
	engine := u.engine
	if !ready || engine == nil {
		u.mu.Unlock()
		if !ready {
			return errors.New("no update available to install")
		}
		return errors.New("updater is not configured")
	}
	// The restart helper takes the staged file by path, and the engine deletes
	// that file when another download starts. Holding this until the process
	// exits keeps a check from replacing the release being installed.
	u.installing = true
	u.mu.Unlock()

	if err := engine.Restart(ctx); err != nil {
		u.mu.Lock()
		u.installing = false
		u.mu.Unlock()
		return err
	}
	return nil
}

func (u *Updater) DismissUpdateDialog() {
	if u == nil {
		return
	}
	u.mu.Lock()
	if !u.downloaded {
		u.mu.Unlock()
		return
	}
	u.dialogDismissed = true
	ctx := u.ctx
	u.mu.Unlock()
	u.broadcastStatus(ctx)
}

func (u *Updater) GetStatus(ctx context.Context) (UpdaterStatus, error) {
	mode, err := u.mode(ctx)
	if err != nil {
		return UpdaterStatus{}, err
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	status := UpdaterStatus{
		Mode:                  mode,
		UpdateAvailable:       u.available,
		UpdateDownloaded:      u.downloaded,
		ShouldPromptForUpdate: u.downloaded && !u.dialogDismissed,
		IsChecking:            u.checking,
		IsDownloading:         u.downloading,
		DownloadWritten:       u.downloadWritten,
		DownloadTotal:         u.downloadTotal,
	}
	if u.lastError != "" {
		status.LastError = lo.ToPtr(u.lastError)
	}
	if u.releaseVersion != "" {
		status.ReleaseVersion = lo.ToPtr(u.releaseVersion)
	}
	if u.originalNotes != "" || u.translatedNotes != "" {
		status.ReleaseNotes = &UpdaterReleaseNotes{}
		if u.originalNotes != "" {
			status.ReleaseNotes.Original = lo.ToPtr(u.originalNotes)
		}
		if u.translatedNotes != "" {
			status.ReleaseNotes.Translated = lo.ToPtr(u.translatedNotes)
		}
		if u.translatedLang != "" {
			status.ReleaseNotes.TranslatedLanguage = lo.ToPtr(u.translatedLang)
		}
	}
	return status, nil
}

func (u *Updater) HandleIncludePrereleaseChanged(enabled bool) {
	if u == nil {
		return
	}
	u.mu.Lock()
	u.includePrereleaseGeneration++
	generation := u.includePrereleaseGeneration
	ctx := u.ctx
	if u.checking || u.downloading {
		u.recheckAfterBusy = true
		u.mu.Unlock()
		return
	}
	u.mu.Unlock()
	if ctx == nil {
		ctx = context.Background()
	}
	go u.runLogged(
		func() error { return u.applyIncludePrereleaseChange(ctx, enabled, generation) },
		"updater.includePrereleaseChanged",
	)
}

func (u *Updater) applyIncludePrereleaseChange(ctx context.Context, enabled bool, generation uint64) error {
	u.mu.Lock()
	if generation != u.includePrereleaseGeneration {
		u.mu.Unlock()
		return nil
	}
	if enabled {
		u.channelRefreshPending = true
		u.mu.Unlock()
		mode, err := u.mode(ctx)
		if err != nil {
			return err
		}
		if mode == "off" {
			return nil
		}
		return u.refreshUpdateCandidate(ctx, false, true)
	}
	u.channelRefreshPending = false
	if u.checking || u.downloading {
		u.recheckAfterBusy = true
		u.mu.Unlock()
		return nil
	}
	if u.installing {
		u.mu.Unlock()
		return nil
	}
	if u.releaseChannel != UpdateChannelPrerelease {
		u.mu.Unlock()
		return nil
	}
	u.clearUpdateCandidateLocked()
	u.mu.Unlock()
	u.broadcastStatus(ctx)
	mode, err := u.mode(ctx)
	if err != nil {
		return err
	}
	if mode == "off" {
		return nil
	}
	return u.CheckForUpdates(ctx, false)
}

// refreshUpdateCandidate looks for a release again while one is already known.
// channelChange marks a refresh caused by the pre-release setting: it bypasses
// the metadata cache and is retried once a busy updater settles.
func (u *Updater) refreshUpdateCandidate(ctx context.Context, userInitiated, channelChange bool) error {
	u.mu.Lock()
	if u.checking || u.downloading {
		u.recheckAfterBusy = u.recheckAfterBusy || channelChange
		u.mu.Unlock()
		return nil
	}
	previousVersion := u.releaseVersion
	engine := u.engine
	u.mu.Unlock()
	if engine == nil {
		return errors.New("updater is not configured")
	}
	allowed, err := u.githubRateAllowsCheck(ctx, userInitiated)
	if err != nil || !allowed {
		return err
	}

	u.mu.Lock()
	if u.checking || u.downloading {
		u.recheckAfterBusy = u.recheckAfterBusy || channelChange
		u.mu.Unlock()
		return nil
	}
	u.checking = true
	u.mu.Unlock()
	u.broadcastStatus(ctx)

	operation := lo.Ternary(channelChange, "app-update-channel", "app-update")
	checkCtx := WithGitHubRefresh(u.metadataContext(ctx, operation), userInitiated || channelChange)
	release, err := u.checkRelease(checkCtx, engine)
	if err != nil {
		u.mu.Lock()
		u.checking = false
		u.mu.Unlock()
		u.broadcastStatus(ctx)
		u.finishIncludePrereleaseChange(ctx)
		return err
	}
	mode, err := u.mode(ctx)
	if err != nil {
		u.mu.Lock()
		u.checking = false
		u.mu.Unlock()
		u.broadcastStatus(ctx)
		u.finishIncludePrereleaseChange(ctx)
		return err
	}

	// The install check and the result share one critical section, so an
	// install request cannot start between them and lose its release.
	u.mu.Lock()
	u.checking = false
	if u.installing {
		u.mu.Unlock()
		u.broadcastStatus(ctx)
		return nil
	}
	u.channelRefreshPending = false
	if release == nil {
		u.resetCheckedReleaseLocked()
		u.mu.Unlock()
		u.broadcastStatus(ctx)
		u.finishIncludePrereleaseChange(ctx)
		return nil
	}
	u.available = true
	u.releaseChannel = release.Channel
	sameRelease := release.Version == previousVersion && previousVersion != ""
	notify := false
	if sameRelease {
		if userInitiated && u.downloaded {
			u.dialogDismissed = false
		}
		// Automatic checks repeat on a timer, so the ready prompt is announced
		// once per release unless the user asked for the check. Claiming the
		// version here keeps overlapping checks from announcing it twice.
		notify = u.downloaded && (userInitiated || u.releaseVersion != u.notifiedVersion)
		if notify {
			u.notifiedVersion = u.releaseVersion
		}
	} else {
		u.downloaded = false
		u.dialogDismissed = false
		u.notifiedVersion = ""
		u.releaseVersion = release.Version
		u.originalNotes = normalizeReleaseNotes(release.Notes)
		u.clearTranslationLocked()
	}
	downloaded := u.downloaded
	u.mu.Unlock()
	if !sameRelease {
		u.broadcast("updater:update-available")
		u.translateCurrentReleaseNotes(ctx)
	}
	u.broadcastStatus(ctx)
	if notify {
		u.notifyReady()
	}
	if mode == "auto" && !downloaded {
		if u.skipPrereleaseDownload(ctx) {
			u.finishIncludePrereleaseChange(ctx)
			return nil
		}
		return u.DownloadUpdate(ctx)
	}
	u.finishIncludePrereleaseChange(ctx)
	return nil
}

func (u *Updater) skipPrereleaseDownload(ctx context.Context) bool {
	u.mu.Lock()
	deferRecheck := u.recheckAfterBusy
	channel := u.releaseChannel
	u.mu.Unlock()
	if !deferRecheck || channel != UpdateChannelPrerelease {
		return false
	}
	include, err := u.includePrerelease(ctx)
	if err != nil {
		u.logError(err, "updater.includePrereleaseChanged")
		return true
	}
	return !include
}

func (u *Updater) clearUpdateCandidateLocked() {
	u.available, u.downloaded = false, false
	u.releaseVersion, u.releaseChannel, u.originalNotes = "", "", ""
	u.notifiedVersion = ""
	u.dialogDismissed = false
	u.clearTranslationLocked()
}

func (u *Updater) includePrerelease(ctx context.Context) (bool, error) {
	u.mu.Lock()
	settings := u.settings
	u.mu.Unlock()
	if settings == nil {
		return false, nil
	}
	return settings.GetIncludePrerelease(ctx)
}

func (u *Updater) finishIncludePrereleaseChange(ctx context.Context) {
	u.mu.Lock()
	if !u.recheckAfterBusy || u.checking || u.downloading {
		u.mu.Unlock()
		return
	}
	u.recheckAfterBusy = false
	generation := u.includePrereleaseGeneration
	u.mu.Unlock()
	include, err := u.includePrerelease(ctx)
	if err != nil {
		u.logError(err, "updater.includePrereleaseChanged")
		return
	}
	if err := u.applyIncludePrereleaseChange(ctx, include, generation); err != nil {
		u.logError(err, "updater.includePrereleaseChanged")
	}
}

func (u *Updater) HandleAutoUpdateModeChanged(mode string) {
	if u == nil {
		return
	}
	u.mu.Lock()
	ctx := u.ctx
	available, downloaded, downloading, checking := u.available, u.downloaded, u.downloading, u.checking
	refreshPending := u.channelRefreshPending
	u.mu.Unlock()
	if ctx == nil {
		ctx = context.Background()
	}
	switch {
	case mode != "off" && refreshPending && !checking && !downloading:
		go u.runLogged(func() error { return u.CheckForUpdates(ctx, false) }, "updater.modeChanged")
	case mode == "auto" && available && !downloaded && !downloading:
		go u.runLogged(func() error { return u.DownloadUpdate(ctx) }, "updater.modeChanged")
	case mode != "off" && !available && !downloaded && !checking:
		go u.runLogged(func() error { return u.CheckForUpdates(ctx, false) }, "updater.modeChanged")
	default:
		u.broadcastStatus(ctx)
	}
}

func (u *Updater) HandleLanguageChanged(_ string) {
	if u == nil {
		return
	}
	u.mu.Lock()
	ctx := u.ctx
	u.mu.Unlock()
	if ctx == nil {
		ctx = context.Background()
	}
	u.translateCurrentReleaseNotes(ctx)
}

func (u *Updater) translateCurrentReleaseNotes(ctx context.Context) {
	u.mu.Lock()
	original, version := u.originalNotes, u.releaseVersion
	u.translationSerial++
	serial := u.translationSerial
	httpClient, settings := u.http, u.settings
	u.mu.Unlock()
	if original == "" || version == "" || httpClient == nil || settings == nil {
		return
	}
	language, err := settings.GetLanguage(ctx)
	if err != nil {
		u.logError(err, "updater.translateReleaseNotes")
		return
	}
	if language != "ko" && language != "ja" && language != "zh" {
		u.mu.Lock()
		hadTranslation := false
		if u.translationSerial == serial {
			hadTranslation = u.translatedNotes != "" || u.translatedLang != ""
			u.translatedNotes, u.translatedLang = "", ""
		}
		u.mu.Unlock()
		if hadTranslation {
			u.broadcastStatus(ctx)
		}
		return
	}
	go func() {
		translated, translateErr := u.translateReleaseNotes(ctx, original, language)
		current, shouldBroadcast := u.applyTranslationResult(
			serial,
			original,
			version,
			translated,
			language,
			translateErr,
		)
		if !current {
			return
		}
		if translateErr != nil {
			u.logError(translateErr, "updater.translateReleaseNotes")
		}
		if shouldBroadcast {
			u.broadcastStatus(ctx)
		}
	}()
}

func (u *Updater) applyTranslationResult(
	serial uint64,
	original string,
	version string,
	translated string,
	language string,
	translateErr error,
) (current bool, shouldBroadcast bool) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.translationSerial != serial || u.originalNotes != original || u.releaseVersion != version {
		return false, false
	}
	hadTranslation := u.translatedNotes != "" || u.translatedLang != ""
	if translateErr == nil && translated != "" && translated != original {
		u.translatedNotes, u.translatedLang = translated, language
		return true, true
	}
	u.translatedNotes, u.translatedLang = "", ""
	return true, translateErr != nil || hadTranslation
}

func (u *Updater) translateReleaseNotes(ctx context.Context, original, language string) (string, error) {
	body, err := json.Marshal(map[string]string{"source": "en", "target": language, "text": original})
	if err != nil {
		return "", err
	}
	response, err := u.http.Fetch(ctx, translationEndpoint, FetchOptions{
		Method:            http.MethodPost,
		Header:            http.Header{"Content-Type": []string{"application/json"}},
		Body:              bytes.NewReader(body),
		DisableHTTPErrors: true,
	})
	if err != nil {
		return "", err
	}
	defer func() { _ = response.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxTranslationPayload+1))
	if err != nil {
		return "", err
	}
	if len(raw) > maxTranslationPayload {
		return "", errors.New("release notes translation response is too large")
	}
	value, err := decodeTranslationBody(response.Header.Get("Content-Type"), raw)
	if err != nil {
		return "", err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("release notes translation failed with status %d: %v", response.StatusCode, value)
	}
	return extractTranslatedText(value), nil
}

func decodeTranslationBody(contentType string, raw []byte) (any, error) {
	var value any
	if strings.Contains(strings.ToLower(contentType), "cbor") {
		mode, err := (cbor.DecOptions{DefaultMapType: reflect.TypeOf(map[string]any(nil))}).DecMode()
		if err != nil {
			return nil, err
		}
		if err := mode.Unmarshal(raw, &value); err != nil {
			return nil, err
		}
		return value, nil
	}
	if err := json.Unmarshal(raw, &value); err != nil {
		if strings.Contains(strings.ToLower(contentType), "json") {
			return nil, fmt.Errorf("decode translation response: %w", err)
		}
		// Non-JSON bodies (text/plain) are valid translation payloads.
		return strings.TrimSpace(string(raw)), nil
	}
	return value, nil
}

func extractTranslatedText(value any) string {
	root, ok := value.(map[string]any)
	if !ok {
		return strings.TrimSpace(fmt.Sprint(value))
	}
	response := root["response"]
	if text, ok := response.(string); ok {
		return strings.TrimSpace(text)
	}
	responseMap, ok := response.(map[string]any)
	if !ok {
		return ""
	}
	choices, ok := responseMap["choices"].([]any)
	if !ok || len(choices) == 0 {
		return ""
	}
	choice, ok := choices[0].(map[string]any)
	if !ok {
		return ""
	}
	message, ok := choice["message"].(map[string]any)
	if !ok {
		return ""
	}
	content, _ := message["content"].(string)
	return strings.TrimSpace(content)
}

func normalizeReleaseNotes(notes string) string {
	notes = strings.ReplaceAll(notes, "\r\n", "\n")
	for strings.Contains(notes, "\n\n\n") {
		notes = strings.ReplaceAll(notes, "\n\n\n", "\n\n")
	}
	return strings.TrimSpace(notes)
}

func (u *Updater) clearTranslationLocked() {
	u.translatedNotes, u.translatedLang = "", ""
	u.translationSerial++
}

func (u *Updater) githubRateAllowsCheck(ctx context.Context, userInitiated bool) (bool, error) {
	u.mu.Lock()
	rate := u.rate
	u.mu.Unlock()
	if rate == nil {
		return true, nil
	}
	allowed, state, err := rate.CanUseGitHubAPI(ctx)
	if err != nil {
		return false, err
	}
	if allowed {
		return true, nil
	}
	if userInitiated {
		return false, fmt.Errorf("GitHub API rate limit is active until %s", formatGitHubRateReset(state))
	}
	return false, nil
}

func (u *Updater) mode(ctx context.Context) (string, error) {
	u.mu.Lock()
	settings := u.settings
	u.mu.Unlock()
	if settings == nil {
		return "auto", nil
	}
	return settings.GetAutoUpdateMode(ctx)
}

func (u *Updater) broadcastStatus(ctx context.Context) {
	status, err := u.GetStatus(ctx)
	if err != nil {
		u.logError(err, "updater.getStatus")
		return
	}
	u.broadcast("updater:status-changed", status)
}

// notifyReady announces the downloaded release. Callers claim
// notifiedVersion under u.mu before calling it.
func (u *Updater) notifyReady() {
	u.mu.Lock()
	ready := u.ready
	u.mu.Unlock()
	if ready != nil {
		ready()
		return
	}
	u.broadcast("updater:update-downloaded")
}

func (u *Updater) broadcast(name string, data ...any) {
	u.mu.Lock()
	emit := u.emit
	u.mu.Unlock()
	if emit != nil {
		emit(name, data...)
	}
}

func (u *Updater) runLogged(fn func() error, where string) {
	if err := fn(); err != nil {
		u.logError(err, where)
	}
}

func (u *Updater) logError(err error, where string) {
	if err == nil || IsCancellationError(err) {
		return
	}
	u.mu.Lock()
	log := u.log
	u.mu.Unlock()
	_ = ReportError(log, err, "Updater", Diagnostic{
		Severity: DiagnosticError, Operation: where, Stage: "background",
	})
}

func (u *Updater) ServiceShutdown() error {
	if u == nil {
		return nil
	}
	u.mu.Lock()
	cancel := u.cancel
	u.cancel, u.ctx = nil, nil
	u.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	u.loopWG.Wait()
	return nil
}
