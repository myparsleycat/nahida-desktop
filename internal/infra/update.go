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
	wailsupdater "github.com/wailsapp/wails/v3/pkg/updater"
	githubprovider "github.com/wailsapp/wails/v3/pkg/updater/providers/github"

	"nahida.live/desktop/internal/platform"
)

const (
	updaterRepository     = "myparsleycat/nahida-desktop"
	updaterChecksumAsset  = "SHA256SUMS"
	updaterCheckInterval  = time.Hour
	translationEndpoint   = "https://api.nahida.live/translate"
	maxTranslationPayload = 8 << 20
)

type UpdaterSettings interface {
	GetAutoUpdateMode(context.Context) (string, error)
	GetLanguage(context.Context) (string, error)
}

type updaterEngine interface {
	Init(wailsupdater.Config) error
	Check(context.Context) (*wailsupdater.Release, error)
	DownloadAndInstall(context.Context) error
	Restart(context.Context) error
	StopPeriodicCheck()
}

type UpdaterOptions struct {
	Engine   updaterEngine
	Settings UpdaterSettings
	HTTP     *Client
	Log      *Log
	Emit     func(name string, data ...any)
	Focus    func()
	Ready    func()
	Version  string
	Provider wailsupdater.Provider
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
}

type Updater struct {
	mu sync.Mutex

	engine   updaterEngine
	settings UpdaterSettings
	http     *Client
	log      *Log
	rate     *GitHubRateCoordinator
	emit     func(string, ...any)
	focus    func()
	ready    func()

	available         bool
	downloaded        bool
	releaseVersion    string
	originalNotes     string
	translatedNotes   string
	translatedLang    string
	dialogDismissed   bool
	checking          bool
	downloading       bool
	translationSerial uint64

	ctx      context.Context
	cancel   context.CancelFunc
	interval time.Duration
	loopWG   sync.WaitGroup
}

func NewUpdater() *Updater { return &Updater{interval: updaterCheckInterval} }

// AttachGitHubRate binds the shared GitHub core-rate coordinator used to gate
// CheckForUpdates, matching Electron's GitHubRateCoordinator.
//
//wails:ignore
func (u *Updater) AttachGitHubRate(store githubRateStore, httpClient *Client, log *Log) {
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
	rate.UseHTTP(httpClient)
	rate.UseLog(log)
}

func githubProviderConfig(version string) githubprovider.Config {
	return githubprovider.Config{
		Repository:    updaterRepository,
		ChecksumAsset: updaterChecksumAsset,
		Prerelease:    strings.Contains(version, "-beta."),
	}
}

// Configure binds the Wails v3 updater after application.New has created it.
//
//wails:ignore
func (u *Updater) Configure(opts UpdaterOptions) error {
	if u == nil || opts.Engine == nil {
		return errors.New("updater engine is not configured")
	}
	version := strings.TrimPrefix(strings.TrimSpace(opts.Version), "v")
	if version == "" {
		version = platform.AppVersion
	}
	provider := opts.Provider
	if provider == nil {
		var err error
		provider, err = githubprovider.New(githubProviderConfig(version))
		if err != nil {
			return err
		}
	}
	if err := opts.Engine.Init(wailsupdater.Config{
		CurrentVersion: version,
		Providers:      []wailsupdater.Provider{provider},
		Window:         wailsupdater.WindowNone,
	}); err != nil {
		return err
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
	u.emit, u.focus, u.ready, u.interval = opts.Emit, opts.Focus, opts.Ready, interval
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
	if u.checking {
		u.mu.Unlock()
		return nil
	}
	u.mu.Unlock()
	allowed, err := u.githubRateAllowsCheck(ctx, userInitiated)
	if err != nil {
		return err
	}
	if !allowed {
		return nil
	}

	u.mu.Lock()
	if u.checking {
		u.mu.Unlock()
		return nil
	}
	engine := u.engine
	if u.downloaded {
		if userInitiated {
			u.dialogDismissed = false
		}
		u.mu.Unlock()
		u.notifyReady()
		u.broadcastStatus(ctx)
		return nil
	}
	available, downloading := u.available, u.downloading
	u.mu.Unlock()
	if engine == nil {
		u.mu.Lock()
		u.downloading = false
		u.mu.Unlock()
		return errors.New("updater is not configured")
	}
	mode, err := u.mode(ctx)
	if err != nil {
		return err
	}
	if available {
		if mode == "auto" && !downloading {
			return u.DownloadUpdate(ctx)
		}
		return nil
	}

	u.mu.Lock()
	u.checking = true
	u.mu.Unlock()
	u.broadcastStatus(ctx)
	release, err := engine.Check(ctx)
	if err != nil {
		u.mu.Lock()
		u.checking, u.downloading = false, false
		u.available, u.downloaded = false, false
		u.releaseVersion, u.originalNotes = "", ""
		u.clearTranslationLocked()
		u.dialogDismissed = false
		u.mu.Unlock()
		u.broadcastStatus(ctx)
		return err
	}
	if release == nil {
		u.mu.Lock()
		u.checking, u.downloading = false, false
		u.available, u.downloaded = false, false
		u.releaseVersion, u.originalNotes = "", ""
		u.clearTranslationLocked()
		u.dialogDismissed = false
		u.mu.Unlock()
		u.broadcastStatus(ctx)
		return nil
	}
	u.mu.Lock()
	u.checking = false
	u.available = true
	u.releaseVersion = release.Version
	u.originalNotes = normalizeReleaseNotes(release.Notes)
	u.clearTranslationLocked()
	u.mu.Unlock()
	u.broadcast("updater:update-available")
	u.broadcastStatus(ctx)
	u.translateCurrentReleaseNotes(ctx)
	if mode == "auto" {
		return u.DownloadUpdate(ctx)
	}
	return nil
}

func (u *Updater) DownloadUpdate(ctx context.Context) error {
	u.mu.Lock()
	if u.downloaded || !u.available || u.downloading {
		u.mu.Unlock()
		return nil
	}
	engine := u.engine
	u.downloading = true
	u.mu.Unlock()
	if engine == nil {
		return errors.New("updater is not configured")
	}
	u.broadcastStatus(ctx)
	if err := engine.DownloadAndInstall(ctx); err != nil {
		u.mu.Lock()
		u.downloading = false
		u.mu.Unlock()
		u.broadcastStatus(ctx)
		return err
	}
	u.mu.Lock()
	u.downloading = false
	u.downloaded = true
	u.dialogDismissed = false
	u.mu.Unlock()
	u.broadcastStatus(ctx)
	u.notifyReady()
	return nil
}

func (u *Updater) InstallUpdate(ctx context.Context) error {
	u.mu.Lock()
	ready := u.downloaded && u.available
	engine := u.engine
	u.mu.Unlock()
	if !ready {
		return errors.New("no update available to install")
	}
	if engine == nil {
		return errors.New("updater is not configured")
	}
	return engine.Restart(ctx)
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
	}
	if u.releaseVersion != "" {
		status.ReleaseVersion = stringPointer(u.releaseVersion)
	}
	if u.originalNotes != "" || u.translatedNotes != "" {
		status.ReleaseNotes = &UpdaterReleaseNotes{}
		if u.originalNotes != "" {
			status.ReleaseNotes.Original = stringPointer(u.originalNotes)
		}
		if u.translatedNotes != "" {
			status.ReleaseNotes.Translated = stringPointer(u.translatedNotes)
		}
		if u.translatedLang != "" {
			status.ReleaseNotes.TranslatedLanguage = stringPointer(u.translatedLang)
		}
	}
	return status, nil
}

//wails:ignore
func (u *Updater) HandleAutoUpdateModeChanged(mode string) {
	if u == nil {
		return
	}
	u.mu.Lock()
	ctx := u.ctx
	available, downloaded, downloading, checking := u.available, u.downloaded, u.downloading, u.checking
	u.mu.Unlock()
	if ctx == nil {
		ctx = context.Background()
	}
	switch {
	case mode == "auto" && available && !downloaded && !downloading:
		go u.runLogged(func() error { return u.DownloadUpdate(ctx) }, "updater.modeChanged")
	case mode != "off" && !available && !downloaded && !checking:
		go u.runLogged(func() error { return u.CheckForUpdates(ctx, false) }, "updater.modeChanged")
	default:
		u.broadcastStatus(ctx)
	}
}

//wails:ignore
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
		current, shouldBroadcast := u.applyTranslationResult(serial, original, version, translated, language, translateErr)
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
		return strings.TrimSpace(string(raw)), nil //nolint:nilerr // text/plain is a supported response shape.
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
	allowed, state, err := rate.CanUseGitHubAPI(ctx, GitHubRateCheckOptions{RefreshIfMissing: true})
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

func (u *Updater) notifyReady() {
	u.mu.Lock()
	ready, focus := u.ready, u.focus
	u.mu.Unlock()
	if ready != nil {
		ready()
		return
	}
	if focus != nil {
		focus()
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
	if err == nil {
		return
	}
	u.mu.Lock()
	log := u.log
	u.mu.Unlock()
	if log != nil {
		log.Error(err.Error(), where)
	}
}

func (u *Updater) ServiceShutdown() error {
	if u == nil {
		return nil
	}
	u.mu.Lock()
	cancel, engine := u.cancel, u.engine
	u.cancel, u.ctx = nil, nil
	u.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	u.loopWG.Wait()
	if engine != nil {
		engine.StopPeriodicCheck()
	}
	return nil
}

func stringPointer(value string) *string { return &value }
