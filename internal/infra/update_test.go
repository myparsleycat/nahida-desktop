package infra

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fxamacker/cbor/v2"

	"nahida.live/desktop/internal/db"
)

type fakeUpdaterEngine struct {
	release      *UpdateRelease
	checkErr     error
	downloadErr  error
	restartErr   error
	downloadDone func()
	checks       int
	includes     []bool
	downloads    int
	restarts     int
}

func (f *fakeUpdaterEngine) Check(_ context.Context, includePrerelease bool) (*UpdateRelease, error) {
	f.checks++
	f.includes = append(f.includes, includePrerelease)
	return f.release, f.checkErr
}

func (f *fakeUpdaterEngine) DownloadAndInstall(context.Context, func(int64, int64)) error {
	f.downloads++
	if f.downloadDone != nil {
		f.downloadDone()
	}
	return f.downloadErr
}

func (f *fakeUpdaterEngine) Restart(context.Context) error {
	f.restarts++
	return f.restartErr
}

type fakeUpdaterSettings struct {
	mode              string
	language          string
	includePrerelease bool
	includeErr        error
}

func (s fakeUpdaterSettings) GetAutoUpdateMode(context.Context) (string, error) { return s.mode, nil }

func (s fakeUpdaterSettings) GetLanguage(context.Context) (string, error) { return s.language, nil }

func (s fakeUpdaterSettings) GetIncludePrerelease(context.Context) (bool, error) {
	return s.includePrerelease, s.includeErr
}

func TestUpdaterChecksWithIncludePrereleaseSetting(t *testing.T) {
	t.Parallel()
	for _, include := range []bool{false, true} {
		engine := &fakeUpdaterEngine{}
		u := &Updater{engine: engine, settings: fakeUpdaterSettings{mode: "notify", includePrerelease: include}}
		if err := u.CheckForUpdates(context.Background(), true); err != nil {
			t.Fatalf("include=%t: %v", include, err)
		}
		if !slices.Equal(engine.includes, []bool{include}) {
			t.Fatalf("include=%t: engine checks = %v", include, engine.includes)
		}
	}

	readErr := errors.New("read setting")
	engine := &fakeUpdaterEngine{}
	u := &Updater{engine: engine, settings: fakeUpdaterSettings{mode: "notify", includeErr: readErr}}
	if err := u.CheckForUpdates(context.Background(), true); !errors.Is(err, readErr) {
		t.Fatalf("check error = %v, want %v", err, readErr)
	}
	status, _ := u.GetStatus(context.Background())
	if engine.checks != 0 || status.IsChecking {
		t.Fatalf("failed setting read checks=%d status=%#v", engine.checks, status)
	}
}
func TestUpdaterNotifyFlow(t *testing.T) {
	t.Parallel()
	engine := &fakeUpdaterEngine{release: &UpdateRelease{Version: "1.2.3", Notes: "Changes"}}
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
	if !status.UpdateAvailable || status.UpdateDownloaded || status.ReleaseVersion == nil ||
		*status.ReleaseVersion != "1.2.3" {
		t.Fatalf("available status = %#v", status)
	}
	if engine.downloads != 0 {
		t.Fatalf("notify mode downloads = %d", engine.downloads)
	}

	engine.release = &UpdateRelease{Version: "1.2.4", Notes: "Changes"}
	if err := u.CheckForUpdates(context.Background(), true); err != nil {
		t.Fatalf("user-initiated recheck: %v", err)
	}
	status, _ = u.GetStatus(context.Background())
	if status.ReleaseVersion == nil || *status.ReleaseVersion != "1.2.4" || engine.downloads != 0 {
		t.Fatalf("rechecked status = %#v, downloads=%d", status, engine.downloads)
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
	engine := &fakeUpdaterEngine{release: &UpdateRelease{Version: "2.0.0"}}
	u := &Updater{engine: engine, settings: fakeUpdaterSettings{mode: "auto"}}
	if err := u.CheckForUpdates(context.Background(), false); err != nil {
		t.Fatalf("CheckForUpdates: %v", err)
	}
	status, _ := u.GetStatus(context.Background())
	if engine.downloads != 1 || !status.UpdateDownloaded {
		t.Fatalf("downloads=%d, status=%#v", engine.downloads, status)
	}
}

func TestUpdaterAutomaticCheckNotifiesOncePerRelease(t *testing.T) {
	t.Parallel()

	engine := &fakeUpdaterEngine{release: &UpdateRelease{Version: "1.2.3", Notes: "Changes"}}
	notified := 0
	u := &Updater{
		engine:   engine,
		settings: fakeUpdaterSettings{mode: "auto"},
		ready:    func() { notified++ },
		ctx:      context.Background(),
	}
	if err := u.CheckForUpdates(context.Background(), false); err != nil {
		t.Fatalf("CheckForUpdates: %v", err)
	}
	if engine.downloads != 1 || notified != 1 {
		t.Fatalf("downloads=%d notified=%d, want 1/1", engine.downloads, notified)
	}

	if err := u.CheckForUpdates(context.Background(), false); err != nil {
		t.Fatalf("automatic recheck: %v", err)
	}
	if engine.downloads != 1 || notified != 1 {
		t.Fatalf("automatic recheck downloads=%d notified=%d, want 1/1", engine.downloads, notified)
	}

	u.DismissUpdateDialog()
	if err := u.CheckForUpdates(context.Background(), false); err != nil {
		t.Fatalf("automatic recheck after dismissal: %v", err)
	}
	if notified != 1 || !u.dialogDismissed {
		t.Fatalf("after dismissal notified=%d dismissed=%v, want 1/true", notified, u.dialogDismissed)
	}

	if err := u.CheckForUpdates(context.Background(), true); err != nil {
		t.Fatalf("user-initiated CheckForUpdates: %v", err)
	}
	if notified != 2 || u.dialogDismissed {
		t.Fatalf("user-initiated notified=%d dismissed=%v, want 2/false", notified, u.dialogDismissed)
	}
}

func TestUpdaterNotifiesAgainForNewRelease(t *testing.T) {
	t.Parallel()

	engine := &fakeUpdaterEngine{release: &UpdateRelease{Version: "1.2.3"}}
	notified := 0
	u := &Updater{
		engine:   engine,
		settings: fakeUpdaterSettings{mode: "auto"},
		ready:    func() { notified++ },
		ctx:      context.Background(),
	}
	if err := u.CheckForUpdates(context.Background(), false); err != nil {
		t.Fatalf("CheckForUpdates: %v", err)
	}
	if notified != 1 {
		t.Fatalf("notified=%d, want 1", notified)
	}

	// The first release stays downloaded, so the automatic check has to look
	// past its own candidate to find the newer one.
	engine.release = &UpdateRelease{Version: "1.2.4"}

	if err := u.CheckForUpdates(context.Background(), false); err != nil {
		t.Fatalf("check for the newer release: %v", err)
	}
	status, _ := u.GetStatus(context.Background())
	if notified != 2 || engine.downloads != 2 || status.ReleaseVersion == nil || *status.ReleaseVersion != "1.2.4" {
		t.Fatalf(
			"newer release notified=%d downloads=%d status=%#v, want 2/2/1.2.4",
			notified,
			engine.downloads,
			status,
		)
	}

	if err := u.CheckForUpdates(context.Background(), false); err != nil {
		t.Fatalf("automatic recheck: %v", err)
	}
	if notified != 2 {
		t.Fatalf("automatic recheck notified=%d, want 2", notified)
	}

	// The restart helper now owns the staged file, which a download of an even
	// newer release would delete.
	if err := u.InstallUpdate(context.Background()); err != nil {
		t.Fatalf("InstallUpdate: %v", err)
	}
	engine.release = &UpdateRelease{Version: "1.2.5"}
	if err := u.CheckForUpdates(context.Background(), false); err != nil {
		t.Fatalf("check during install: %v", err)
	}
	status, _ = u.GetStatus(context.Background())
	if engine.downloads != 2 || status.ReleaseVersion == nil || *status.ReleaseVersion != "1.2.4" {
		t.Fatalf("check during install downloads=%d status=%#v, want 2/1.2.4", engine.downloads, status)
	}
}

func TestUpdaterRefreshFinishingDuringInstallKeepsDownloadedRelease(t *testing.T) {
	t.Parallel()

	engine := &fakeUpdaterEngine{release: &UpdateRelease{Version: "1.2.3"}}
	u := &Updater{engine: engine, settings: fakeUpdaterSettings{mode: "auto"}, ctx: context.Background()}
	if err := u.CheckForUpdates(context.Background(), false); err != nil {
		t.Fatalf("CheckForUpdates: %v", err)
	}

	// The refresh announces its checking phase before it asks the engine, which
	// is where an install request can start ahead of the result.
	u.emit = func(string, ...any) {
		u.mu.Lock()
		u.installing = true
		u.mu.Unlock()
	}
	engine.release = nil
	if err := u.CheckForUpdates(context.Background(), false); err != nil {
		t.Fatalf("refresh during install: %v", err)
	}
	status, _ := u.GetStatus(context.Background())
	if !status.UpdateDownloaded || status.ReleaseVersion == nil || *status.ReleaseVersion != "1.2.3" {
		t.Fatalf("status after refresh during install = %#v, want downloaded 1.2.3", status)
	}

	// A failed restart releases the install, and the retry still has its release.
	u.mu.Lock()
	u.emit, u.installing = nil, false
	u.mu.Unlock()
	if err := u.InstallUpdate(context.Background()); err != nil || engine.restarts != 1 {
		t.Fatalf("InstallUpdate retry = %v, restarts=%d", err, engine.restarts)
	}
}

func TestUpdaterRecheckRacingWithDownloadCompletionNotifiesOnce(t *testing.T) {
	t.Parallel()

	var downloadReturned atomic.Bool
	engine := &fakeUpdaterEngine{
		release:      &UpdateRelease{Version: "4.0.0"},
		downloadDone: func() { downloadReturned.Store(true) },
	}
	var (
		notified    atomic.Int32
		gateClaimed atomic.Bool
	)
	gateEntered := make(chan struct{})
	releaseGate := make(chan struct{})
	var releaseOnce sync.Once
	releaseChecks := func() { releaseOnce.Do(func() { close(releaseGate) }) }
	// Release the gated check even when the test fails before doing so itself.
	defer releaseChecks()
	u := &Updater{
		engine:   engine,
		settings: fakeUpdaterSettings{mode: "auto"},
		ready:    func() { notified.Add(1) },
		emit: func(name string, _ ...any) {
			// Hold the completed download between its state update and its ready
			// prompt, where an automatic recheck used to decide to notify again.
			if name != "updater:status-changed" || !downloadReturned.Load() ||
				!gateClaimed.CompareAndSwap(false, true) {
				return
			}
			close(gateEntered)
			<-releaseGate
		},
		ctx: context.Background(),
	}

	checkDone := make(chan error, 1)
	go func() { checkDone <- u.CheckForUpdates(context.Background(), false) }()

	select {
	case <-gateEntered:
	case <-time.After(10 * time.Second):
		t.Fatal("download completion never reached its status broadcast")
	}
	if err := u.CheckForUpdates(context.Background(), false); err != nil {
		t.Fatalf("recheck while the download completed: %v", err)
	}
	releaseChecks()

	if err := <-checkDone; err != nil {
		t.Fatalf("CheckForUpdates: %v", err)
	}
	if got := notified.Load(); got != 1 {
		t.Fatalf("notifications = %d, want 1", got)
	}
}

func TestUpdaterOverlappingChecksNotifyOncePerRelease(t *testing.T) {
	t.Parallel()

	const (
		workers    = 32
		iterations = 100
	)
	engine := &fakeUpdaterEngine{release: &UpdateRelease{Version: "5.0.0"}}
	var notified atomic.Int32
	u := &Updater{
		engine:   engine,
		settings: fakeUpdaterSettings{mode: "notify"},
		ready:    func() { notified.Add(1) },
		ctx:      context.Background(),
	}
	for index := range iterations {
		// A downloaded release that no automatic check has announced yet. Until
		// the decision and the claim shared one critical section, overlapping
		// checks could all read the stale version and notify for it.
		u.mu.Lock()
		u.available, u.downloaded = true, true
		u.releaseVersion, u.notifiedVersion = "5.0.0", ""
		u.mu.Unlock()
		before := notified.Load()

		start := make(chan struct{})
		var waitGroup sync.WaitGroup
		for range workers {
			waitGroup.Add(1)
			go func() {
				defer waitGroup.Done()
				<-start
				if err := u.CheckForUpdates(context.Background(), false); err != nil {
					t.Errorf("CheckForUpdates: %v", err)
				}
			}()
		}
		close(start)
		waitGroup.Wait()

		if got := notified.Load() - before; got != 1 {
			t.Fatalf("iteration %d: notifications = %d, want 1", index, got)
		}
		u.mu.Lock()
		announced := u.notifiedVersion
		u.mu.Unlock()
		if announced != "5.0.0" {
			t.Fatalf("iteration %d: notifiedVersion = %q, want %q", index, announced, "5.0.0")
		}
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

	readyCalls := 0
	var events []string
	u := &Updater{
		ready: func() { readyCalls++ },
		emit: func(name string, _ ...any) {
			events = append(events, name)
		},
	}
	u.notifyReady()
	if readyCalls != 1 {
		t.Fatalf("ready calls = %d, want 1", readyCalls)
	}
	if len(events) != 0 {
		t.Fatalf("events = %v, want none", events)
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
		{
			name:          "success",
			translated:    "변경 사항",
			language:      "ko",
			wantCurrent:   true,
			wantBroadcast: true,
			wantNotes:     "변경 사항",
			wantLanguage:  "ko",
		},
		{name: "empty without previous translation", language: "ko", wantCurrent: true},
		{
			name:        "same as original without previous translation",
			translated:  original,
			language:    "ja",
			wantCurrent: true,
		},
		{
			name:             "empty clears previous translation",
			language:         "zh",
			previousNotes:    "旧内容",
			previousLanguage: "zh",
			wantCurrent:      true,
			wantBroadcast:    true,
		},
		{
			name:          "error always broadcasts",
			language:      "ko",
			translateErr:  errors.New("translation failed"),
			wantCurrent:   true,
			wantBroadcast: true,
		},
		{
			name:             "stale request is ignored",
			translated:       "stale",
			language:         "ko",
			previousNotes:    "current",
			previousLanguage: "ja",
			mutateSerial:     true,
			wantNotes:        "current",
			wantLanguage:     "ja",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
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
			current, broadcast := u.applyTranslationResult(
				serial,
				original,
				version,
				test.translated,
				test.language,
				test.translateErr,
			)
			if current != test.wantCurrent || broadcast != test.wantBroadcast {
				t.Fatalf("result = (%v, %v), want (%v, %v)", current, broadcast, test.wantCurrent, test.wantBroadcast)
			}
			if u.translatedNotes != test.wantNotes || u.translatedLang != test.wantLanguage {
				t.Fatalf(
					"translation = %q/%q, want %q/%q",
					u.translatedNotes,
					u.translatedLang,
					test.wantNotes,
					test.wantLanguage,
				)
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

	allowed, state, err := rate.CanUseGitHubAPI(ctx)
	if err != nil {
		t.Fatalf("CanUseGitHubAPI missing: %v", err)
	}
	if !allowed || state != nil {
		t.Fatalf("missing state allowed=%v state=%#v", allowed, state)
	}

	seedGitHubRateState(
		t,
		client,
		GitHubRateState{Limit: 60, Remaining: 0, Reset: time.Now().Add(time.Hour).Unix(), Used: 60, Resource: "core"},
	)
	rate.UseAppState(client.AppState)
	allowed, state, err = rate.CanUseGitHubAPI(ctx)
	if err != nil {
		t.Fatalf("CanUseGitHubAPI limited: %v", err)
	}
	if allowed || state == nil || state.Remaining != 0 {
		t.Fatalf("limited allowed=%v state=%#v", allowed, state)
	}

	seedGitHubRateState(
		t,
		client,
		GitHubRateState{Limit: 60, Remaining: 0, Reset: time.Now().Add(-time.Hour).Unix(), Used: 60, Resource: "core"},
	)
	rate.UseAppState(client.AppState)
	allowed, _, err = rate.CanUseGitHubAPI(ctx)
	if err != nil {
		t.Fatalf("CanUseGitHubAPI expired: %v", err)
	}
	if !allowed {
		t.Fatal("expired reset should allow GitHub API use")
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

	engine := &fakeUpdaterEngine{release: &UpdateRelease{Version: "9.9.9"}}
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

func TestDisablingPrereleaseDropsPrereleaseCandidate(t *testing.T) {
	t.Parallel()
	engine := &fakeUpdaterEngine{release: &UpdateRelease{Version: "3.0.0", Channel: "stable"}}
	u := &Updater{
		engine:         engine,
		settings:       fakeUpdaterSettings{mode: "notify"},
		ctx:            context.Background(),
		available:      true,
		downloaded:     true,
		releaseVersion: "3.1.0-beta.2",
		releaseChannel: UpdateChannelPrerelease,
	}
	if err := u.applyIncludePrereleaseChange(context.Background(), false, 0); err != nil {
		t.Fatalf("apply: %v", err)
	}
	status, err := u.GetStatus(context.Background())
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if status.UpdateDownloaded || status.ReleaseVersion == nil || *status.ReleaseVersion != "3.0.0" {
		t.Fatalf("status=%#v", status)
	}
	if u.releaseChannel != "stable" || engine.checks != 1 {
		t.Fatalf("channel=%s checks=%d", u.releaseChannel, engine.checks)
	}
}

func TestDisablingPrereleaseKeepsStableCandidate(t *testing.T) {
	t.Parallel()
	engine := &fakeUpdaterEngine{release: &UpdateRelease{Version: "9.0.0", Channel: "stable"}}
	u := &Updater{
		engine:         engine,
		settings:       fakeUpdaterSettings{mode: "notify"},
		ctx:            context.Background(),
		available:      true,
		releaseVersion: "3.0.0",
		releaseChannel: "stable",
	}
	if err := u.applyIncludePrereleaseChange(context.Background(), false, 0); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if engine.checks != 0 || u.releaseVersion != "3.0.0" || !u.available {
		t.Fatalf("checks=%d version=%s available=%v", engine.checks, u.releaseVersion, u.available)
	}
}

func TestDisablingPrereleaseWhileUpdatesOffClearsWithoutCheck(t *testing.T) {
	t.Parallel()
	engine := &fakeUpdaterEngine{release: &UpdateRelease{Version: "3.0.0", Channel: "stable"}}
	u := &Updater{
		engine:         engine,
		settings:       fakeUpdaterSettings{mode: "off"},
		ctx:            context.Background(),
		available:      true,
		downloaded:     true,
		releaseVersion: "3.1.0-beta.1",
		releaseChannel: UpdateChannelPrerelease,
	}
	if err := u.applyIncludePrereleaseChange(context.Background(), false, 0); err != nil {
		t.Fatalf("apply: %v", err)
	}
	status, err := u.GetStatus(context.Background())
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if engine.checks != 0 || status.UpdateAvailable || status.UpdateDownloaded || status.ReleaseVersion != nil {
		t.Fatalf("checks=%d status=%#v", engine.checks, status)
	}
}

func TestEnablingPrereleaseRechecksAndReplacesStable(t *testing.T) {
	t.Parallel()
	engine := &fakeUpdaterEngine{
		release: &UpdateRelease{Version: "3.1.0-beta.1", Channel: UpdateChannelPrerelease, Notes: "beta"},
	}
	u := &Updater{
		engine:         engine,
		settings:       fakeUpdaterSettings{mode: "notify", includePrerelease: true},
		ctx:            context.Background(),
		available:      true,
		downloaded:     true,
		releaseVersion: "3.0.0",
		releaseChannel: "stable",
	}
	if err := u.applyIncludePrereleaseChange(context.Background(), true, 0); err != nil {
		t.Fatalf("apply: %v", err)
	}
	status, err := u.GetStatus(context.Background())
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if status.UpdateDownloaded || status.ReleaseVersion == nil || *status.ReleaseVersion != "3.1.0-beta.1" {
		t.Fatalf("status=%#v", status)
	}
	if u.releaseChannel != UpdateChannelPrerelease || engine.downloads != 0 {
		t.Fatalf("channel=%s downloads=%d", u.releaseChannel, engine.downloads)
	}
}

func TestEnablingPrereleaseKeepsDownloadedStableWhenUnchanged(t *testing.T) {
	t.Parallel()
	engine := &fakeUpdaterEngine{release: &UpdateRelease{Version: "3.0.0", Channel: "stable"}}
	notified := 0
	u := &Updater{
		engine:          engine,
		settings:        fakeUpdaterSettings{mode: "auto", includePrerelease: true},
		ready:           func() { notified++ },
		ctx:             context.Background(),
		available:       true,
		downloaded:      true,
		releaseVersion:  "3.0.0",
		releaseChannel:  "stable",
		notifiedVersion: "3.0.0",
	}
	if err := u.applyIncludePrereleaseChange(context.Background(), true, 0); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if engine.downloads != 0 || notified != 0 || !u.downloaded || u.releaseVersion != "3.0.0" {
		t.Fatalf(
			"downloads=%d notified=%d downloaded=%v version=%s",
			engine.downloads,
			notified,
			u.downloaded,
			u.releaseVersion,
		)
	}
}

func TestEnablingPrereleaseRetriesFailedRefresh(t *testing.T) {
	t.Parallel()
	checkErr := errors.New("temporary GitHub failure")
	engine := &fakeUpdaterEngine{
		release:  &UpdateRelease{Version: "3.1.0-beta.1", Channel: UpdateChannelPrerelease},
		checkErr: checkErr,
	}
	u := &Updater{
		engine:         engine,
		settings:       fakeUpdaterSettings{mode: "notify", includePrerelease: true},
		available:      true,
		downloaded:     true,
		releaseVersion: "3.0.0",
		releaseChannel: "stable",
	}
	ctx := context.Background()
	if err := u.applyIncludePrereleaseChange(ctx, true, 0); !errors.Is(err, checkErr) {
		t.Fatalf("first refresh error = %v, want %v", err, checkErr)
	}
	if !u.channelRefreshPending || !u.downloaded || u.releaseVersion != "3.0.0" {
		t.Fatalf("pending=%v downloaded=%v version=%s", u.channelRefreshPending, u.downloaded, u.releaseVersion)
	}

	engine.checkErr = nil
	if err := u.CheckForUpdates(ctx, false); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if engine.checks != 2 || u.channelRefreshPending || u.downloaded || u.releaseVersion != "3.1.0-beta.1" {
		t.Fatalf(
			"checks=%d pending=%v downloaded=%v version=%s",
			engine.checks,
			u.channelRefreshPending,
			u.downloaded,
			u.releaseVersion,
		)
	}
}

func TestEnablingPrereleaseRetriesAfterRateLimit(t *testing.T) {
	t.Parallel()
	client := openUpdaterTestDB(t)
	reset := time.Now().Add(time.Hour).Unix()
	seedGitHubRateState(t, client, GitHubRateState{Limit: 60, Remaining: 0, Reset: reset, Resource: "core"})
	rate := NewGitHubRateCoordinator()
	rate.UseAppState(client.AppState)
	engine := &fakeUpdaterEngine{
		release: &UpdateRelease{Version: "3.1.0-beta.1", Channel: UpdateChannelPrerelease},
	}
	u := &Updater{
		engine:         engine,
		settings:       fakeUpdaterSettings{mode: "notify", includePrerelease: true},
		rate:           rate,
		available:      true,
		downloaded:     true,
		releaseVersion: "3.0.0",
		releaseChannel: "stable",
	}
	ctx := context.Background()
	if err := u.applyIncludePrereleaseChange(ctx, true, 0); err != nil {
		t.Fatalf("rate-limited refresh: %v", err)
	}
	if engine.checks != 0 || !u.channelRefreshPending || !u.downloaded {
		t.Fatalf("checks=%d pending=%v downloaded=%v", engine.checks, u.channelRefreshPending, u.downloaded)
	}
	if err := u.CheckForUpdates(ctx, true); err == nil {
		t.Fatal("manual retry did not report the GitHub rate limit")
	}
	if engine.checks != 0 || !u.channelRefreshPending {
		t.Fatalf("checks=%d pending=%v", engine.checks, u.channelRefreshPending)
	}

	seedGitHubRateState(t, client, GitHubRateState{Limit: 60, Remaining: 60, Reset: reset, Resource: "core"})
	rate.UseAppState(client.AppState)
	if err := u.CheckForUpdates(ctx, true); err != nil {
		t.Fatalf("retry after rate limit: %v", err)
	}
	if engine.checks != 1 || u.channelRefreshPending || u.releaseVersion != "3.1.0-beta.1" {
		t.Fatalf("checks=%d pending=%v version=%s", engine.checks, u.channelRefreshPending, u.releaseVersion)
	}
}

func TestEnablingPrereleaseWhileUpdatesOffDoesNotCheck(t *testing.T) {
	t.Parallel()
	engine := &fakeUpdaterEngine{
		release: &UpdateRelease{Version: "9.0.0-beta.1", Channel: UpdateChannelPrerelease},
	}
	u := &Updater{
		engine:         engine,
		settings:       fakeUpdaterSettings{mode: "off", includePrerelease: true},
		ctx:            context.Background(),
		available:      true,
		releaseVersion: "3.0.0",
		releaseChannel: "stable",
	}
	if err := u.applyIncludePrereleaseChange(context.Background(), true, 0); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if engine.checks != 0 || u.releaseVersion != "3.0.0" {
		t.Fatalf("checks=%d version=%s", engine.checks, u.releaseVersion)
	}
}

func TestHandleIncludePrereleaseChangedDefersWhileChecking(t *testing.T) {
	t.Parallel()
	u := &Updater{checking: true}
	u.HandleIncludePrereleaseChanged(true)
	u.HandleIncludePrereleaseChanged(false)
	if !u.recheckAfterBusy {
		t.Fatal("expected the change to wait until the check finishes")
	}
	if u.includePrereleaseGeneration != 2 {
		t.Fatalf("generation = %d, want 2", u.includePrereleaseGeneration)
	}
}

func TestApplyIncludePrereleaseChangeIgnoresStaleGeneration(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name    string
		enabled bool
		busy    bool
	}{
		{name: "stale enable", enabled: true},
		{name: "stale disable"},
		{name: "stale enable while busy", enabled: true, busy: true},
		{name: "stale disable while busy", busy: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			engine := &fakeUpdaterEngine{}
			emitted := 0
			u := &Updater{
				engine:                      engine,
				settings:                    fakeUpdaterSettings{mode: "off"},
				emit:                        func(string, ...any) { emitted++ },
				available:                   true,
				downloaded:                  true,
				releaseVersion:              "3.1.0-beta.2",
				releaseChannel:              UpdateChannelPrerelease,
				checking:                    tt.busy,
				channelRefreshPending:       !tt.enabled,
				includePrereleaseGeneration: 2,
			}
			before, err := u.GetStatus(context.Background())
			if err != nil {
				t.Fatalf("status before stale callback: %v", err)
			}

			if err := u.applyIncludePrereleaseChange(context.Background(), tt.enabled, 1); err != nil {
				t.Fatalf("stale callback: %v", err)
			}
			after, err := u.GetStatus(context.Background())
			if err != nil {
				t.Fatalf("status after stale callback: %v", err)
			}
			if !reflect.DeepEqual(after, before) {
				t.Fatalf("stale callback changed status: before=%#v after=%#v", before, after)
			}
			if u.channelRefreshPending != !tt.enabled || u.recheckAfterBusy ||
				u.releaseChannel != UpdateChannelPrerelease {
				t.Fatalf(
					"pending=%v deferred=%v channel=%s",
					u.channelRefreshPending,
					u.recheckAfterBusy,
					u.releaseChannel,
				)
			}
			if engine.checks != 0 || emitted != 0 {
				t.Fatalf("checks=%d emitted=%d, want no side effects", engine.checks, emitted)
			}
		})
	}
}

func TestDownloadedPrereleaseIsDiscardedWhenSwitchTurnsOff(t *testing.T) {
	t.Parallel()
	started := make(chan struct{})
	releaseDownload := make(chan struct{})
	settings := &fakeUpdaterSettings{mode: "notify", includePrerelease: true}
	engine := &fakeUpdaterEngine{
		release: &UpdateRelease{Version: "3.0.0", Channel: "stable"},
		downloadDone: func() {
			close(started)
			<-releaseDownload
		},
	}
	notified := 0
	u := &Updater{
		engine:         engine,
		settings:       settings,
		ready:          func() { notified++ },
		ctx:            context.Background(),
		available:      true,
		releaseVersion: "3.1.0-beta.2",
		releaseChannel: UpdateChannelPrerelease,
	}
	errCh := make(chan error, 1)
	go func() { errCh <- u.DownloadUpdate(context.Background()) }()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("download did not start")
	}
	settings.includePrerelease = false
	if err := u.applyIncludePrereleaseChange(context.Background(), false, 0); err != nil {
		t.Fatalf("defer channel change: %v", err)
	}
	if !u.recheckAfterBusy || engine.checks != 0 || !u.available || u.releaseVersion != "3.1.0-beta.2" {
		t.Fatalf(
			"pending=%v checks=%d available=%v version=%s",
			u.recheckAfterBusy,
			engine.checks,
			u.available,
			u.releaseVersion,
		)
	}
	close(releaseDownload)
	if err := <-errCh; err != nil {
		t.Fatalf("DownloadUpdate: %v", err)
	}
	if notified != 0 {
		t.Fatalf("notified=%d, want 0", notified)
	}
	status, err := u.GetStatus(context.Background())
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if status.UpdateDownloaded || status.ReleaseVersion == nil || *status.ReleaseVersion != "3.0.0" {
		t.Fatalf("status=%#v", status)
	}
	if u.releaseChannel != "stable" {
		t.Fatalf("channel=%s", u.releaseChannel)
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
	if err := client.AppState.Upsert(
		context.Background(),
		githubCoreRateKey,
		string(raw),
		time.Now().UTC().Format(time.RFC3339Nano),
	); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
}

func TestExtractTranslatedTextShapes(t *testing.T) {
	t.Parallel()
	if got := extractTranslatedText(map[string]any{"response": " translated "}); got != "translated" {
		t.Fatalf("string response = %q", got)
	}
	value := map[string]any{
		"response": map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": " choice "}}}},
	}
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
