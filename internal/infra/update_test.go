package infra

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/fxamacker/cbor/v2"
	wailsupdater "github.com/wailsapp/wails/v3/pkg/updater"
	githubprovider "github.com/wailsapp/wails/v3/pkg/updater/providers/github"

	"nahida.live/desktop/internal/db"
)

type fakeUpdaterEngine struct {
	cfg         wailsupdater.Config
	release     *wailsupdater.Release
	checkErr    error
	downloadErr error
	restartErr  error
	checks      int
	downloads   int
	restarts    int
	stopped     bool
}

func (f *fakeUpdaterEngine) Init(cfg wailsupdater.Config) error {
	f.cfg = cfg
	return nil
}
func (f *fakeUpdaterEngine) Check(context.Context) (*wailsupdater.Release, error) {
	f.checks++
	return f.release, f.checkErr
}
func (f *fakeUpdaterEngine) DownloadAndInstall(context.Context) error {
	f.downloads++
	return f.downloadErr
}
func (f *fakeUpdaterEngine) Restart(context.Context) error {
	f.restarts++
	return f.restartErr
}
func (f *fakeUpdaterEngine) StopPeriodicCheck() { f.stopped = true }

type fakeUpdaterSettings struct {
	mode     string
	language string
}

func (s fakeUpdaterSettings) GetAutoUpdateMode(context.Context) (string, error) { return s.mode, nil }
func (s fakeUpdaterSettings) GetLanguage(context.Context) (string, error)       { return s.language, nil }

func TestGitHubProviderConfig(t *testing.T) {
	t.Parallel()
	cfg := githubProviderConfig("3.0.0-beta.1")
	if cfg.Repository != updaterRepository {
		t.Fatalf("Repository = %q, want %q", cfg.Repository, updaterRepository)
	}
	if cfg.ChecksumAsset != updaterChecksumAsset {
		t.Fatalf("ChecksumAsset = %q, want %q", cfg.ChecksumAsset, updaterChecksumAsset)
	}
	if !cfg.Prerelease {
		t.Fatal("Prerelease = false for beta version")
	}
	if githubProviderConfig("3.0.0").Prerelease {
		t.Fatal("Prerelease = true for stable version")
	}
	if githubProviderConfig("3.0.0-rc.1").Prerelease {
		t.Fatal("Prerelease = true for unsupported prerelease channel")
	}
}

func TestConfigureDefaultGitHubChecksumAsset(t *testing.T) {
	t.Parallel()
	engine := &fakeUpdaterEngine{}
	u := NewUpdater()
	if err := u.Configure(UpdaterOptions{Engine: engine}); err != nil {
		t.Fatalf("Configure: %v", err)
	}
	t.Cleanup(func() {
		if err := u.ServiceShutdown(); err != nil {
			t.Errorf("ServiceShutdown: %v", err)
		}
	})
	if engine.cfg.Window != wailsupdater.WindowNone {
		t.Fatalf("Window = %#v, want WindowNone", engine.cfg.Window)
	}
	if len(engine.cfg.Providers) != 1 {
		t.Fatalf("Providers = %d, want 1", len(engine.cfg.Providers))
	}
	provider, ok := engine.cfg.Providers[0].(*githubprovider.Provider)
	if !ok {
		t.Fatalf("provider type = %T, want *github.Provider", engine.cfg.Providers[0])
	}
	got := reflect.ValueOf(provider).Elem().FieldByName("cfg").FieldByName("ChecksumAsset").String()
	if got != updaterChecksumAsset {
		t.Fatalf("ChecksumAsset = %q, want %q", got, updaterChecksumAsset)
	}
}

func TestUpdaterNotifyFlow(t *testing.T) {
	t.Parallel()
	engine := &fakeUpdaterEngine{release: &wailsupdater.Release{Version: "1.2.3", Notes: "Changes"}}
	var eventMu sync.Mutex
	events := make([]string, 0)
	u := &Updater{
		engine:   engine,
		settings: fakeUpdaterSettings{mode: "notify", language: "en"},
		emit: func(name string, _ ...any) {
			eventMu.Lock()
			events = append(events, name)
			eventMu.Unlock()
		},
		ctx: context.Background(),
	}
	if err := u.CheckForUpdates(context.Background(), true); err != nil {
		t.Fatalf("CheckForUpdates: %v", err)
	}
	status, err := u.GetStatus(context.Background())
	if err != nil {
		t.Fatalf("GetStatus: %v", err)
	}
	if !status.UpdateAvailable || status.UpdateDownloaded || status.ReleaseVersion == nil || *status.ReleaseVersion != "1.2.3" {
		t.Fatalf("available status = %#v", status)
	}
	if engine.downloads != 0 {
		t.Fatalf("notify mode downloads = %d", engine.downloads)
	}
	if err := u.DownloadUpdate(context.Background()); err != nil {
		t.Fatalf("DownloadUpdate: %v", err)
	}
	status, _ = u.GetStatus(context.Background())
	if !status.UpdateDownloaded || !status.ShouldPromptForUpdate || engine.downloads != 1 {
		t.Fatalf("downloaded status = %#v, downloads=%d", status, engine.downloads)
	}
	u.DismissUpdateDialog()
	status, _ = u.GetStatus(context.Background())
	if status.ShouldPromptForUpdate {
		t.Fatalf("dismissed status = %#v", status)
	}
	if err := u.InstallUpdate(context.Background()); err != nil || engine.restarts != 1 {
		t.Fatalf("InstallUpdate = %v, restarts=%d", err, engine.restarts)
	}
	eventMu.Lock()
	defer eventMu.Unlock()
	if !containsString(events, "updater:update-available") || !containsString(events, "updater:update-downloaded") {
		t.Fatalf("events = %v", events)
	}
}

func TestUpdaterAutoModeDownloadsAvailableRelease(t *testing.T) {
	t.Parallel()
	engine := &fakeUpdaterEngine{release: &wailsupdater.Release{Version: "2.0.0"}}
	u := &Updater{engine: engine, settings: fakeUpdaterSettings{mode: "auto"}}
	if err := u.CheckForUpdates(context.Background(), false); err != nil {
		t.Fatalf("CheckForUpdates: %v", err)
	}
	status, _ := u.GetStatus(context.Background())
	if engine.downloads != 1 || !status.UpdateDownloaded {
		t.Fatalf("downloads=%d, status=%#v", engine.downloads, status)
	}
}

func TestDismissUpdateDialogBeforeDownloadIsNoOp(t *testing.T) {
	t.Parallel()

	var events []string
	u := &Updater{
		settings: fakeUpdaterSettings{mode: "notify"},
		emit: func(name string, _ ...any) {
			events = append(events, name)
		},
	}
	u.DismissUpdateDialog()
	if len(events) != 0 {
		t.Fatalf("events = %v, want none", events)
	}
	if u.dialogDismissed {
		t.Fatal("dialog was dismissed before an update was downloaded")
	}
}

func TestNotifyReadyUsesWindowScopedCallback(t *testing.T) {
	t.Parallel()

	readyCalls, focusCalls := 0, 0
	var events []string
	u := &Updater{
		ready: func() { readyCalls++ },
		focus: func() { focusCalls++ },
		emit: func(name string, _ ...any) {
			events = append(events, name)
		},
	}
	u.notifyReady()
	if readyCalls != 1 {
		t.Fatalf("ready calls = %d, want 1", readyCalls)
	}
	if focusCalls != 0 || len(events) != 0 {
		t.Fatalf("fallback focus/events = %d/%v, want none", focusCalls, events)
	}
}

func TestUnsupportedTranslationLanguageOnlyBroadcastsWhenClearingTranslation(t *testing.T) {
	t.Parallel()

	var events []string
	u := &Updater{
		settings:       fakeUpdaterSettings{language: "en"},
		http:           NewClient(),
		originalNotes:  "Changes",
		releaseVersion: "1.2.3",
		emit: func(name string, _ ...any) {
			events = append(events, name)
		},
	}
	u.translateCurrentReleaseNotes(context.Background())
	if len(events) != 0 {
		t.Fatalf("events without a translation = %v, want none", events)
	}

	u.translatedNotes, u.translatedLang = "Translated", "ko"
	u.translateCurrentReleaseNotes(context.Background())
	if !reflect.DeepEqual(events, []string{"updater:status-changed"}) {
		t.Fatalf("events while clearing = %v", events)
	}
	if u.translatedNotes != "" || u.translatedLang != "" {
		t.Fatalf("translation was not cleared: %q/%q", u.translatedNotes, u.translatedLang)
	}
}

func TestApplyTranslationResultBroadcastGuards(t *testing.T) {
	t.Parallel()

	const (
		serial   = uint64(7)
		original = "Changes"
		version  = "1.2.3"
	)
	tests := []struct {
		name             string
		translated       string
		language         string
		translateErr     error
		previousNotes    string
		previousLanguage string
		mutateSerial     bool
		wantCurrent      bool
		wantBroadcast    bool
		wantNotes        string
		wantLanguage     string
	}{
		{name: "success", translated: "변경 사항", language: "ko", wantCurrent: true, wantBroadcast: true, wantNotes: "변경 사항", wantLanguage: "ko"},
		{name: "empty without previous translation", language: "ko", wantCurrent: true},
		{name: "same as original without previous translation", translated: original, language: "ja", wantCurrent: true},
		{name: "empty clears previous translation", language: "zh", previousNotes: "旧内容", previousLanguage: "zh", wantCurrent: true, wantBroadcast: true},
		{name: "error always broadcasts", language: "ko", translateErr: errors.New("translation failed"), wantCurrent: true, wantBroadcast: true},
		{name: "stale request is ignored", translated: "stale", language: "ko", previousNotes: "current", previousLanguage: "ja", mutateSerial: true, wantNotes: "current", wantLanguage: "ja"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			u := &Updater{
				translationSerial: serial,
				originalNotes:     original,
				releaseVersion:    version,
				translatedNotes:   test.previousNotes,
				translatedLang:    test.previousLanguage,
			}
			if test.mutateSerial {
				u.translationSerial++
			}
			current, broadcast := u.applyTranslationResult(serial, original, version, test.translated, test.language, test.translateErr)
			if current != test.wantCurrent || broadcast != test.wantBroadcast {
				t.Fatalf("result = (%v, %v), want (%v, %v)", current, broadcast, test.wantCurrent, test.wantBroadcast)
			}
			if u.translatedNotes != test.wantNotes || u.translatedLang != test.wantLanguage {
				t.Fatalf("translation = %q/%q, want %q/%q", u.translatedNotes, u.translatedLang, test.wantNotes, test.wantLanguage)
			}
		})
	}
}

func TestGitHubRateCoordinatorCanUseGitHubAPI(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	client := openUpdaterTestDB(t)
	rate := NewGitHubRateCoordinator()
	rate.UseAppState(client.AppState)

	allowed, state, err := rate.CanUseGitHubAPI(ctx, GitHubRateCheckOptions{})
	if err != nil {
		t.Fatalf("CanUseGitHubAPI missing: %v", err)
	}
	if !allowed || state != nil {
		t.Fatalf("missing state allowed=%v state=%#v", allowed, state)
	}

	seedGitHubRateState(t, client, GitHubRateState{Limit: 60, Remaining: 0, Reset: time.Now().Add(time.Hour).Unix(), Used: 60, Resource: "core"})
	allowed, state, err = rate.CanUseGitHubAPI(ctx, GitHubRateCheckOptions{})
	if err != nil {
		t.Fatalf("CanUseGitHubAPI limited: %v", err)
	}
	if allowed || state == nil || state.Remaining != 0 {
		t.Fatalf("limited allowed=%v state=%#v", allowed, state)
	}

	seedGitHubRateState(t, client, GitHubRateState{Limit: 60, Remaining: 0, Reset: time.Now().Add(-time.Hour).Unix(), Used: 60, Resource: "core"})
	allowed, _, err = rate.CanUseGitHubAPI(ctx, GitHubRateCheckOptions{})
	if err != nil {
		t.Fatalf("CanUseGitHubAPI expired: %v", err)
	}
	if !allowed {
		t.Fatal("expired reset should allow GitHub API use")
	}

	if err := client.AppState.Delete(ctx, githubCoreRateKey); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	var requests int
	rate.UseHTTP(NewClientWithOptions(ClientOptions{
		HTTPClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			requests++
			if r.URL.String() != githubRateLimitURL {
				t.Fatalf("unexpected URL %s", r.URL)
			}
			if got := r.Header.Get("Accept"); got != "application/vnd.github+json" {
				t.Fatalf("Accept = %q", got)
			}
			header := make(http.Header)
			header.Set("X-RateLimit-Limit", "60")
			header.Set("X-RateLimit-Remaining", "12")
			header.Set("X-RateLimit-Reset", "2000000000")
			header.Set("X-RateLimit-Used", "48")
			header.Set("X-RateLimit-Resource", "core")
			return &http.Response{StatusCode: http.StatusOK, Header: header, Body: io.NopCloser(bytes.NewReader([]byte(`{}`)))}, nil
		})},
	}))
	allowed, state, err = rate.CanUseGitHubAPI(ctx, GitHubRateCheckOptions{RefreshIfMissing: true})
	if err != nil {
		t.Fatalf("CanUseGitHubAPI refresh: %v", err)
	}
	if requests != 1 || !allowed || state == nil || state.Remaining != 12 || state.Reset != 2000000000 {
		t.Fatalf("refresh allowed=%v state=%#v requests=%d", allowed, state, requests)
	}
	stored, err := rate.GetRateState(ctx)
	if err != nil || stored == nil || stored.Remaining != 12 {
		t.Fatalf("stored = %#v, %v", stored, err)
	}
}

func TestCheckForUpdatesGatesOnGitHubRateLimit(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	client := openUpdaterTestDB(t)
	reset := time.Now().Add(2 * time.Hour).Unix()
	seedGitHubRateState(t, client, GitHubRateState{Limit: 60, Remaining: 0, Reset: reset, Used: 60, Resource: "core"})
	rate := NewGitHubRateCoordinator()
	rate.UseAppState(client.AppState)

	engine := &fakeUpdaterEngine{release: &wailsupdater.Release{Version: "9.9.9"}}
	u := &Updater{
		engine:   engine,
		settings: fakeUpdaterSettings{mode: "notify"},
		rate:     rate,
		ctx:      ctx,
	}
	if err := u.CheckForUpdates(ctx, false); err != nil {
		t.Fatalf("automatic CheckForUpdates: %v", err)
	}
	if engine.checks != 0 {
		t.Fatalf("automatic check ran while rate limited: checks=%d", engine.checks)
	}

	err := u.CheckForUpdates(ctx, true)
	if err == nil {
		t.Fatal("user-initiated CheckForUpdates succeeded while rate limited")
	}
	want := "GitHub API rate limit is active until " + formatGitHubRateReset(&GitHubRateState{Reset: reset})
	if err.Error() != want {
		t.Fatalf("user-initiated error = %q, want %q", err.Error(), want)
	}
	if engine.checks != 0 {
		t.Fatalf("user-initiated check ran while rate limited: checks=%d", engine.checks)
	}
}

func TestCheckForUpdatesRefreshesMissingRateState(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	client := openUpdaterTestDB(t)
	var requests int
	rate := NewGitHubRateCoordinator()
	rate.UseAppState(client.AppState)
	rate.UseHTTP(NewClientWithOptions(ClientOptions{
		HTTPClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			requests++
			header := make(http.Header)
			header.Set("X-RateLimit-Limit", "60")
			header.Set("X-RateLimit-Remaining", "0")
			header.Set("X-RateLimit-Reset", "2000000000")
			header.Set("X-RateLimit-Used", "60")
			header.Set("X-RateLimit-Resource", "core")
			body := []byte(`{"rate":{"limit":60,"remaining":0,"reset":2000000000,"used":60,"resource":"core"}}`)
			return &http.Response{StatusCode: http.StatusOK, Header: header, Body: io.NopCloser(bytes.NewReader(body))}, nil
		})},
	}))
	engine := &fakeUpdaterEngine{release: &wailsupdater.Release{Version: "3.0.0"}}
	u := &Updater{engine: engine, settings: fakeUpdaterSettings{mode: "notify"}, rate: rate}
	if err := u.CheckForUpdates(ctx, false); err != nil {
		t.Fatalf("automatic CheckForUpdates: %v", err)
	}
	if requests != 1 {
		t.Fatalf("rate_limit requests = %d, want 1", requests)
	}
	if engine.checks != 0 {
		t.Fatalf("check ran after refreshed rate limit: checks=%d", engine.checks)
	}
}

func openUpdaterTestDB(t *testing.T) *db.Client {
	t.Helper()
	client, err := db.New(filepath.Join(t.TempDir(), "updater.db"))
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}
	if err := client.Reconcile(context.Background()); err != nil {
		_ = client.Close()
		t.Fatalf("Reconcile: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func seedGitHubRateState(t *testing.T, client *db.Client, state GitHubRateState) {
	t.Helper()
	if state.UpdatedAt == "" {
		state.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if err := client.AppState.Upsert(context.Background(), githubCoreRateKey, string(raw), time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
}

func TestExtractTranslatedTextShapes(t *testing.T) {
	t.Parallel()
	if got := extractTranslatedText(map[string]any{"response": " translated "}); got != "translated" {
		t.Fatalf("string response = %q", got)
	}
	value := map[string]any{"response": map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": " choice "}}}}}
	if got := extractTranslatedText(value); got != "choice" {
		t.Fatalf("choice response = %q", got)
	}
}

func TestDecodeTranslationCBOR(t *testing.T) {
	t.Parallel()
	raw, err := cbor.Marshal(map[string]any{"response": "translated"})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	value, err := decodeTranslationBody("application/cbor", raw)
	if err != nil {
		t.Fatalf("decodeTranslationBody: %v", err)
	}
	if got := extractTranslatedText(value); got != "translated" {
		t.Fatalf("translated text = %q (%#v)", got, value)
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
