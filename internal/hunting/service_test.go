package hunting

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"nahida.live/desktop/internal/platform"
	"nahida.live/desktop/internal/xxmi"
)

func TestServiceRunsUserControlledHuntingSession(t *testing.T) {
	input := &fakeWindowInput{windows: []platform.WindowInfo{{PID: 42, Title: "Game"}}}
	clipboard := &fakeClipboard{values: []platform.ClipboardText{
		{Sequence: 10, Text: "old"},
		{Sequence: 11, Text: "0xDEADBEEF"},
	}}
	service := newTestService(t, huntingTestConfig(t), input, clipboard)

	begin, err := service.Begin(context.Background(), "owner", "GIMI", 42, false)
	if err != nil {
		t.Fatalf("Begin = %v", err)
	}
	if begin.Snapshot.Status != StatusManual || string(begin.PNG) != "png" {
		t.Fatalf("Begin = %+v", begin)
	}

	first, err := service.Next(context.Background(), "owner", CategoryPixelShader)
	if err != nil {
		t.Fatalf("Next = %v", err)
	}
	second, err := service.Next(context.Background(), "owner", CategoryPixelShader)
	if err != nil {
		t.Fatalf("Next = %v", err)
	}
	if first.SelectedCategory != CategoryPixelShader || second.TotalSteps != 2 || second.Categories[1].Steps != 2 {
		t.Fatalf("Next snapshots = %+v, %+v", first, second)
	}

	completed, err := service.Found(context.Background(), "owner")
	if err != nil {
		t.Fatalf("Found = %v", err)
	}
	if completed.Status != StatusCompleted || completed.Hash != "deadbeef" || !completed.Cleanup.Complete {
		t.Fatalf("Found snapshot = %+v", completed)
	}
	wantKeys := []string{"vk_numpad0", "vk_f8", "vk_f8", "vk_f9", "vk_numpad0"}
	if got := input.sentKeys(); !slicesEqual(got, wantKeys) {
		t.Fatalf("sent keys = %v, want %v", got, wantKeys)
	}
}

func TestServiceFoundRequiresAUserSelectedCategory(t *testing.T) {
	service := newTestService(
		t,
		huntingTestConfig(t),
		&fakeWindowInput{windows: []platform.WindowInfo{{PID: 42, Title: "Game"}}},
		&fakeClipboard{},
	)
	if _, err := service.Begin(context.Background(), "owner", "GIMI", 42, false); err != nil {
		t.Fatalf("Begin = %v", err)
	}

	snapshot, err := service.Found(context.Background(), "owner")
	if !errors.Is(err, ErrCategoryRequired) || snapshot.Status != StatusManual {
		t.Fatalf("Found = %+v, %v", snapshot, err)
	}
}

func TestServiceFoundRejectsUnknownClipboardBaseline(t *testing.T) {
	input := &fakeWindowInput{windows: []platform.WindowInfo{{PID: 42, Title: "Game"}}}
	readErr := errors.New("clipboard is unavailable")
	clipboard := &fakeClipboard{
		values:   []platform.ClipboardText{{}, {Sequence: 11, Text: "deadbeef"}},
		failures: map[int]error{0: readErr},
	}
	service := newTestService(t, huntingTestConfig(t), input, clipboard)
	if _, err := service.Begin(context.Background(), "owner", "GIMI", 42, false); err != nil {
		t.Fatalf("Begin = %v", err)
	}
	if _, err := service.Next(context.Background(), "owner", CategoryPixelShader); err != nil {
		t.Fatalf("Next = %v", err)
	}

	snapshot, err := service.Found(context.Background(), "owner")
	if !errors.Is(err, readErr) || snapshot.Status != StatusManual || snapshot.Hash != "" {
		t.Fatalf("Found = %+v, %v", snapshot, err)
	}
	if got := input.sentKeys(); !slicesEqual(got, []string{"vk_numpad0", "vk_f8"}) {
		t.Fatalf("Found sent a mark key without a clipboard baseline: %v", got)
	}
}

func TestServiceFoundUsesSequenceWhenClipboardTextIsUnavailable(t *testing.T) {
	input := &fakeWindowInput{windows: []platform.WindowInfo{{PID: 42, Title: "Game"}}}
	clipboard := &fakeClipboard{
		values: []platform.ClipboardText{
			{Sequence: 10},
			{Sequence: 11, Text: "deadbeef"},
		},
		failures: map[int]error{0: errors.New("clipboard has no text")},
	}
	service := newTestService(t, huntingTestConfig(t), input, clipboard)
	if _, err := service.Begin(context.Background(), "owner", "GIMI", 42, false); err != nil {
		t.Fatalf("Begin = %v", err)
	}
	if _, err := service.Next(context.Background(), "owner", CategoryPixelShader); err != nil {
		t.Fatalf("Next = %v", err)
	}

	snapshot, err := service.Found(context.Background(), "owner")
	if err != nil || snapshot.Status != StatusCompleted || snapshot.Hash != "deadbeef" {
		t.Fatalf("Found = %+v, %v", snapshot, err)
	}
}

func TestServiceCancelRestoresTheInitialHuntingState(t *testing.T) {
	input := &fakeWindowInput{windows: []platform.WindowInfo{{PID: 42, Title: "Game"}}}
	service := newTestService(t, huntingTestConfig(t), input, &fakeClipboard{})

	begin, err := service.Begin(context.Background(), "owner", "GIMI", 42, true)
	if err != nil {
		t.Fatalf("Begin = %v", err)
	}
	cancelled, err := service.CancelOwner(context.Background(), "owner")
	if err != nil {
		t.Fatalf("CancelOwner = %v", err)
	}
	if cancelled.Status != StatusCancelled || !cancelled.Cleanup.Complete {
		t.Fatalf("CancelOwner snapshot = %+v", cancelled)
	}
	wantKeys := []string{"vk_numpad0", "vk_numpad0", "vk_numpad0", "vk_numpad0"}
	if got := input.sentKeys(); !slicesEqual(got, wantKeys) {
		t.Fatalf("sent keys = %v, want %v", got, wantKeys)
	}
	if current := service.Current("owner"); current == nil || current.Status != StatusCancelled {
		t.Fatalf("Current = %+v", current)
	}
	if current := service.Current("other"); current != nil {
		t.Fatalf("Current(other) = %+v", current)
	}

	again, err := service.Cancel(context.Background(), "owner", begin.Snapshot.ID)
	if err != nil || again.Status != StatusCancelled {
		t.Fatalf("idempotent Cancel = %+v, %v", again, err)
	}
}

func TestServiceRejectsControlsFromAnotherConversation(t *testing.T) {
	service := newTestService(
		t,
		huntingTestConfig(t),
		&fakeWindowInput{windows: []platform.WindowInfo{{PID: 42, Title: "Game"}}},
		&fakeClipboard{},
	)
	if _, err := service.Begin(context.Background(), "owner", "GIMI", 42, false); err != nil {
		t.Fatalf("Begin = %v", err)
	}
	if _, err := service.Next(context.Background(), "other", CategoryIndexBuffer); !errors.Is(
		err,
		ErrSessionOwnerMismatch,
	) {
		t.Fatalf("Next error = %v", err)
	}
}

func TestServiceWatchdogRestoresHuntingAfterIdleTimeout(t *testing.T) {
	root := huntingTestConfig(t)
	input := &fakeWindowInput{windows: []platform.WindowInfo{{PID: 42, Title: "Game"}}}
	service := New(Options{
		Importer: fakeImporter{runtime: xxmi.HuntingRuntime{
			ImporterKey: "GIMI", ImporterFolder: root, INIPath: filepath.Join(root, "d3dx.ini"),
			GameEXENames: []string{"game.exe"},
		}},
		Input: input, Screen: fakeScreen{}, Clipboard: &fakeClipboard{},
		IdleTimeout: 20 * time.Millisecond, SessionTimeout: time.Hour, WatchInterval: 5 * time.Millisecond,
		Wait: func(ctx context.Context, _ time.Duration) error { return ctx.Err() },
	})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := service.Shutdown(ctx); err != nil {
			t.Errorf("Shutdown = %v", err)
		}
	})

	begin, err := service.Begin(context.Background(), "owner", "GIMI", 42, false)
	if err != nil {
		t.Fatalf("Begin = %v", err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		snapshot, statusErr := service.Status("owner", begin.Snapshot.ID)
		if statusErr == nil && snapshot.Status == StatusExpired {
			if !snapshot.Cleanup.Complete {
				t.Fatalf("expired cleanup = %+v", snapshot.Cleanup)
			}
			if got := input.sentKeys(); !slicesEqual(got, []string{"vk_numpad0", "vk_numpad0"}) {
				t.Fatalf("sent keys = %v", got)
			}
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("hunting session did not expire")
}

func TestServiceBeginReportsTheActiveSession(t *testing.T) {
	input := &fakeWindowInput{windows: []platform.WindowInfo{{PID: 42, Title: "Game"}}}
	service := newTestService(t, huntingTestConfig(t), input, &fakeClipboard{})

	first, err := service.Begin(context.Background(), "owner", "GIMI", 42, false)
	if err != nil {
		t.Fatalf("Begin = %v", err)
	}
	_, err = service.Begin(context.Background(), "owner", "GIMI", 42, false)
	if !errors.Is(err, ErrAlreadyActive) || !strings.Contains(err.Error(), first.Snapshot.ID) {
		t.Fatalf("second Begin = %v", err)
	}
}

func TestServiceBeginDoesNotRegisterCancelledSession(t *testing.T) {
	root := huntingTestConfig(t)
	input := &fakeWindowInput{windows: []platform.WindowInfo{{PID: 42, Title: "Game"}}}
	entered := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	service := New(Options{
		Importer: &blockingImporter{root: root, entered: entered, release: release},
		Input:    input, Screen: fakeScreen{}, Clipboard: &fakeClipboard{},
		IdleTimeout: time.Hour, SessionTimeout: time.Hour,
		Wait: func(ctx context.Context, _ time.Duration) error { return ctx.Err() },
	})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := service.Shutdown(ctx); err != nil {
			t.Errorf("Shutdown = %v", err)
		}
	})
	// Release the blocked resolve before the service stops so a failed test
	// cannot leave the begin goroutine waiting forever.
	t.Cleanup(unblock)

	ctx, cancel := context.WithCancel(context.Background())
	beginDone := make(chan error, 1)
	go func() {
		_, err := service.Begin(ctx, "owner", "GIMI", 42, true)
		beginDone <- err
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("Begin did not start resolving")
	}
	cancel()
	unblock()
	if err := <-beginDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("Begin = %v, want %v", err, context.Canceled)
	}
	if current := service.Current("owner"); current != nil {
		t.Fatalf("Current after cancelled Begin = %+v", current)
	}
	if got := input.sentKeys(); len(got) != 0 {
		t.Fatalf("cancelled Begin sent keys = %v", got)
	}
}

func TestServiceFinishSessionRunsCleanupOnce(t *testing.T) {
	input := &fakeWindowInput{windows: []platform.WindowInfo{{PID: 42, Title: "Game"}}}
	service := newTestService(t, huntingTestConfig(t), input, &fakeClipboard{})
	if _, err := service.Begin(context.Background(), "owner", "GIMI", 42, true); err != nil {
		t.Fatalf("Begin = %v", err)
	}
	current, err := service.ownerSession("owner")
	if err != nil {
		t.Fatalf("ownerSession = %v", err)
	}

	first, err := service.finishSession(current, StatusCancelled, nil)
	if err != nil || !first.Cleanup.Complete {
		t.Fatalf("first finishSession = %+v, %v", first, err)
	}
	sent := input.sentKeys()

	second, err := service.finishSession(current, StatusCompleted, nil)
	if err != nil || second.Status != StatusCancelled || !second.Cleanup.Complete {
		t.Fatalf("second finishSession = %+v, %v", second, err)
	}
	if got := input.sentKeys(); !slicesEqual(got, sent) {
		t.Fatalf("cleanup ran twice: %v, then %v", sent, got)
	}
}

func TestServiceKeepsSessionRetryableWhenCleanupFails(t *testing.T) {
	root := huntingTestConfig(t)
	input := &fakeWindowInput{windows: []platform.WindowInfo{{PID: 42, Title: "Game"}}}
	now := time.Now()
	service := New(Options{
		Importer: fakeImporter{runtime: xxmi.HuntingRuntime{
			ImporterKey: "GIMI", ImporterFolder: root, INIPath: filepath.Join(root, "d3dx.ini"),
			GameEXENames: []string{"game.exe"},
		}},
		Input: input, Screen: fakeScreen{}, Clipboard: &fakeClipboard{},
		Now:         func() time.Time { return now },
		IdleTimeout: time.Hour, SessionTimeout: time.Hour, WatchInterval: time.Hour,
		Wait: func(ctx context.Context, _ time.Duration) error { return ctx.Err() },
	})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := service.Shutdown(ctx); err != nil {
			t.Errorf("Shutdown = %v", err)
		}
	})

	begin, err := service.Begin(context.Background(), "owner", "GIMI", 42, true)
	if err != nil {
		t.Fatalf("Begin = %v", err)
	}
	input.setAcquireError(errors.New("game window is gone"))

	failed, err := service.CancelOwner(context.Background(), "owner")
	if !errors.Is(err, ErrCleanupIncomplete) || failed.Cleanup.Complete || !failed.Cleanup.Pending {
		t.Fatalf("CancelOwner = %+v, %v", failed, err)
	}
	if _, err := service.Begin(context.Background(), "owner", "GIMI", 42, false); !errors.Is(
		err,
		ErrAlreadyActive,
	) {
		t.Fatalf("Begin while cleanup pending = %v", err)
	}
	if _, err := service.Next(context.Background(), "owner", CategoryPixelShader); !errors.Is(
		err,
		ErrCleanupIncomplete,
	) {
		t.Fatalf("Next while cleanup pending = %v", err)
	}
	if snapshot, err := service.Status("owner", begin.Snapshot.ID); err != nil || snapshot.Cleanup.Complete {
		t.Fatalf("Status while cleanup pending = %+v, %v", snapshot, err)
	}

	// The retry stays available even after the session deadline passed.
	now = now.Add(2 * time.Hour)
	input.setAcquireError(nil)
	retried, err := service.CancelOwner(context.Background(), "owner")
	if err != nil || !retried.Cleanup.Complete {
		t.Fatalf("retry CancelOwner = %+v, %v", retried, err)
	}
	if snapshot, err := service.Status("owner", begin.Snapshot.ID); err != nil ||
		!snapshot.Cleanup.Complete || snapshot.Status != StatusCancelled {
		t.Fatalf("Status after retry = %+v, %v", snapshot, err)
	}
	wantKeys := []string{"vk_numpad0", "vk_numpad0", "vk_numpad0", "vk_numpad0"}
	if got := input.sentKeys(); !slicesEqual(got, wantKeys) {
		t.Fatalf("sent keys = %v, want %v", got, wantKeys)
	}
}

func TestServiceReleasesSessionWhenTargetWindowDisappears(t *testing.T) {
	root := huntingTestConfig(t)
	windows := []platform.WindowInfo{{PID: 42, Title: "Game"}}
	// The game exited, so its window disappeared with its process.
	input := &fakeWindowInput{windows: windows, processAlive: false}
	service := New(Options{
		Importer: fakeImporter{runtime: xxmi.HuntingRuntime{
			ImporterKey: "GIMI", ImporterFolder: root, INIPath: filepath.Join(root, "d3dx.ini"),
			GameEXENames: []string{"game.exe"},
		}},
		Input: input, Screen: fakeScreen{}, Clipboard: &fakeClipboard{},
		IdleTimeout: time.Hour, SessionTimeout: time.Hour,
		Wait: func(ctx context.Context, _ time.Duration) error { return ctx.Err() },
	})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := service.Shutdown(ctx); err != nil {
			t.Errorf("Shutdown = %v", err)
		}
	})

	begin, err := service.Begin(context.Background(), "owner", "GIMI", 42, true)
	if err != nil {
		t.Fatalf("Begin = %v", err)
	}
	input.setWindows(nil)
	input.setAcquireError(fmt.Errorf("%w: game window is gone", platform.ErrWindowNotFound))

	released, err := service.CancelOwner(context.Background(), "owner")
	if !errors.Is(err, ErrCleanupIncomplete) || released.Cleanup.Complete || released.Cleanup.Pending {
		t.Fatalf("CancelOwner = %+v, %v", released, err)
	}
	if current := service.Current("owner"); current == nil || current.Cleanup.Pending {
		t.Fatalf("Current after release = %+v", current)
	}

	input.setWindows(windows)
	input.setAcquireError(nil)
	next, err := service.Begin(context.Background(), "owner", "GIMI", 42, false)
	if err != nil {
		t.Fatalf("Begin after release = %v", err)
	}
	if next.Snapshot.ID == begin.Snapshot.ID {
		t.Fatalf("Begin reused the released session %s", begin.Snapshot.ID)
	}
}

func TestServiceKeepsSessionRetryableWhenWindowIsHidden(t *testing.T) {
	windows := []platform.WindowInfo{{PID: 42, Title: "Game"}}
	// The game hid its window, but the process is still running.
	input := &fakeWindowInput{windows: windows, processAlive: true}
	service := newTestService(t, huntingTestConfig(t), input, &fakeClipboard{})

	if _, err := service.Begin(context.Background(), "owner", "GIMI", 42, true); err != nil {
		t.Fatalf("Begin = %v", err)
	}
	input.setWindows(nil)
	input.setAcquireError(errors.New("game window is hidden"))

	failed, err := service.CancelOwner(context.Background(), "owner")
	if !errors.Is(err, ErrCleanupIncomplete) || failed.Cleanup.Complete || !failed.Cleanup.Pending {
		t.Fatalf("CancelOwner = %+v, %v", failed, err)
	}

	input.setWindows(windows)
	input.setAcquireError(nil)
	retried, err := service.CancelOwner(context.Background(), "owner")
	if err != nil || !retried.Cleanup.Complete || retried.Cleanup.Pending {
		t.Fatalf("retry CancelOwner = %+v, %v", retried, err)
	}
}

func TestServiceFoundKeepsCompletedStatusAcrossCleanupRetry(t *testing.T) {
	input := &fakeWindowInput{windows: []platform.WindowInfo{{PID: 42, Title: "Game"}}}
	clipboard := &fakeClipboard{values: []platform.ClipboardText{
		{Sequence: 10, Text: "old"},
		{Sequence: 11, Text: "deadbeef"},
	}}
	service := newTestService(t, huntingTestConfig(t), input, clipboard)
	if _, err := service.Begin(context.Background(), "owner", "GIMI", 42, true); err != nil {
		t.Fatalf("Begin = %v", err)
	}
	if _, err := service.Next(context.Background(), "owner", CategoryPixelShader); err != nil {
		t.Fatalf("Next = %v", err)
	}

	acquires := 0
	input.setAcquireHook(func() error {
		acquires++
		if acquires > 1 {
			return errors.New("game window is gone")
		}
		return nil
	})
	found, err := service.Found(context.Background(), "owner")
	if !errors.Is(err, ErrCleanupIncomplete) || found.Status != StatusCompleted ||
		found.Hash != "deadbeef" || !found.Cleanup.Pending {
		t.Fatalf("Found = %+v, %v", found, err)
	}

	input.setAcquireHook(nil)
	retried, err := service.CancelOwner(context.Background(), "owner")
	if err != nil || !retried.Cleanup.Complete || retried.Cleanup.Pending ||
		retried.Status != StatusCompleted || retried.Hash != "deadbeef" {
		t.Fatalf("retry CancelOwner = %+v, %v", retried, err)
	}
}

func newTestService(
	t *testing.T,
	root string,
	input *fakeWindowInput,
	clipboard ClipboardReader,
) *Service {
	t.Helper()
	service := New(Options{
		Importer: fakeImporter{runtime: xxmi.HuntingRuntime{
			ImporterKey: "GIMI", ImporterFolder: root, INIPath: filepath.Join(root, "d3dx.ini"),
			GameEXENames: []string{"game.exe"},
		}},
		Input: input, Screen: fakeScreen{}, Clipboard: clipboard,
		IdleTimeout: time.Hour, SessionTimeout: time.Hour,
		Wait: func(ctx context.Context, _ time.Duration) error { return ctx.Err() },
	})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := service.Shutdown(ctx); err != nil {
			t.Errorf("Shutdown = %v", err)
		}
	})
	return service
}

func huntingTestConfig(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	contents := `[Hunting]
hunting = 2
marking_mode = skip
marking_actions = clipboard
toggle_hunting = no_modifiers VK_NUMPAD0
next_indexbuffer = no_modifiers VK_NUMPAD8
mark_indexbuffer = no_modifiers VK_NUMPAD9
next_pixelshader = no_modifiers VK_F8
mark_pixelshader = no_modifiers VK_F9
`
	if err := os.WriteFile(filepath.Join(root, "d3dx.ini"), []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	return root
}

type fakeImporter struct {
	runtime xxmi.HuntingRuntime
}

func (f fakeImporter) ResolveHuntingRuntime(context.Context, string) (xxmi.HuntingRuntime, error) {
	return f.runtime, nil
}

type blockingImporter struct {
	root    string
	entered chan struct{}
	release chan struct{}
}

// ResolveHuntingRuntime blocks past the caller's cancellation, like a config
// read that is already in progress when the session is deleted.
func (f *blockingImporter) ResolveHuntingRuntime(context.Context, string) (xxmi.HuntingRuntime, error) {
	close(f.entered)
	<-f.release
	return xxmi.HuntingRuntime{
		ImporterKey: "GIMI", ImporterFolder: f.root, INIPath: filepath.Join(f.root, "d3dx.ini"),
		GameEXENames: []string{"game.exe"},
	}, nil
}

type fakeWindowInput struct {
	mu           sync.Mutex
	windows      []platform.WindowInfo
	keys         []string
	acquireErr   error
	acquireHook  func() error
	processAlive bool
}

func (f *fakeWindowInput) ListWindows(
	_ context.Context,
	filter platform.WindowFilter,
) ([]platform.WindowInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	result := make([]platform.WindowInfo, 0, len(f.windows))
	for _, window := range f.windows {
		if filter.PID == 0 || filter.PID == window.PID {
			result = append(result, window)
		}
	}
	return result, nil
}

func (f *fakeWindowInput) AcquireForeground(
	_ context.Context,
	target platform.WindowTarget,
) (platform.ForegroundController, error) {
	f.mu.Lock()
	hook := f.acquireHook
	acquireErr := f.acquireErr
	f.mu.Unlock()
	if hook != nil {
		if err := hook(); err != nil {
			return nil, err
		}
	}
	if acquireErr != nil {
		return nil, acquireErr
	}
	return &fakeForeground{input: f, window: platform.WindowInfo{PID: target.PID, Title: "Game"}}, nil
}

func (f *fakeWindowInput) ValidateKeys([]string, []string) error { return nil }

func (f *fakeWindowInput) ProcessAlive(uint32) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.processAlive
}

func (f *fakeWindowInput) setAcquireError(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.acquireErr = err
}

func (f *fakeWindowInput) setAcquireHook(hook func() error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.acquireHook = hook
}

func (f *fakeWindowInput) setWindows(windows []platform.WindowInfo) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.windows = windows
}

func (f *fakeWindowInput) sentKeys() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.keys...)
}

type fakeForeground struct {
	input  *fakeWindowInput
	window platform.WindowInfo
}

func (f *fakeForeground) Window() platform.WindowInfo { return f.window }

func (f *fakeForeground) SendKeys(_ context.Context, keys []string) (platform.KeyResult, error) {
	f.input.mu.Lock()
	f.input.keys = append(f.input.keys, keys...)
	f.input.mu.Unlock()
	return platform.KeyResult{Window: f.window, Keys: keys}, nil
}

func (f *fakeForeground) PressedKeys([]string) ([]string, error) { return nil, nil }
func (f *fakeForeground) Close() error                           { return nil }

type fakeScreen struct{}

func (fakeScreen) CaptureWindow(
	_ context.Context,
	request platform.CaptureRequest,
) (platform.CaptureResult, error) {
	return platform.CaptureResult{
		Window: platform.WindowInfo{PID: request.Target.PID, Title: "Game"},
		Width:  100, Height: 100, Scale: 1, PNG: []byte("png"),
	}, nil
}

type fakeClipboard struct {
	mu       sync.Mutex
	values   []platform.ClipboardText
	failures map[int]error
	index    int
}

func (f *fakeClipboard) ReadClipboardText() (platform.ClipboardText, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.values) == 0 {
		return platform.ClipboardText{}, errors.New("clipboard unavailable")
	}
	index := min(f.index, len(f.values)-1)
	f.index++
	return f.values[index], f.failures[index]
}

func slicesEqual(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
