package app

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/myparsleycat/fairy"

	"nahida.live/desktop/internal/gamebanana"
)

type fakeLoginWindow struct {
	mu                 sync.Mutex
	cookies            []fairy.Cookie
	getErr             error
	deleteErr          error
	gets               atomic.Int32
	inFlight           atomic.Int32
	maxFlight          atomic.Int32
	holdGet            chan struct{}
	deleted            []string
	closed             atomic.Bool
	focuses            atomic.Int32
	shows              atomic.Int32
	navigated          []func()
	closing            []func()
	failed             []func(error)
	getDelay           time.Duration
	cookieScopeURI     string
	suppressCloseEvent bool
	autoReady          bool
	waitCloseErr       error
	runHook            func()
}

func newFakeLoginWindow(cookies ...fairy.Cookie) *fakeLoginWindow {
	return &fakeLoginWindow{cookies: append([]fairy.Cookie(nil), cookies...), autoReady: true}
}

func (w *fakeLoginWindow) Run() {
	if w.runHook != nil {
		w.runHook()
	}
	if w.autoReady {
		w.navigate()
	}
}

func (w *fakeLoginWindow) ID() uint { return 1 }

func (w *fakeLoginWindow) WaitClosed(ctx context.Context) error {
	if w.waitCloseErr != nil {
		return w.waitCloseErr
	}
	if w.closed.Load() {
		return nil
	}
	<-ctx.Done()
	return ctx.Err()
}

func (w *fakeLoginWindow) Show() { w.shows.Add(1) }

func (w *fakeLoginWindow) Focus() { w.focuses.Add(1) }

func (w *fakeLoginWindow) Close() {
	if w.closed.CompareAndSwap(false, true) && !w.suppressCloseEvent {
		w.close()
	}
}

func (w *fakeLoginWindow) GetCookies(ctx context.Context, uri string) ([]fairy.Cookie, error) {
	current := w.inFlight.Add(1)
	for {
		prev := w.maxFlight.Load()
		if current <= prev || w.maxFlight.CompareAndSwap(prev, current) {
			break
		}
	}
	defer w.inFlight.Add(-1)
	w.gets.Add(1)
	if w.holdGet != nil {
		select {
		case <-w.holdGet:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if w.getDelay > 0 {
		select {
		case <-time.After(w.getDelay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.getErr != nil {
		return nil, w.getErr
	}
	if w.cookieScopeURI != "" && uri != w.cookieScopeURI {
		return nil, nil
	}
	return append([]fairy.Cookie(nil), w.cookies...), nil
}

func (w *fakeLoginWindow) DeleteCookies(_ context.Context, uri string, names ...string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.deleteErr != nil {
		return w.deleteErr
	}
	if w.cookieScopeURI != "" && uri != w.cookieScopeURI {
		return nil
	}
	w.deleted = append(w.deleted, names...)
	w.cookies = slices.DeleteFunc(
		w.cookies,
		func(cookie fairy.Cookie) bool { return slices.Contains(names, cookie.Name) },
	)
	return nil
}

func (w *fakeLoginWindow) OnNavigated(f func()) func() {
	w.mu.Lock()
	w.navigated = append(w.navigated, f)
	w.mu.Unlock()
	return func() {}
}

func (w *fakeLoginWindow) OnClosing(f func()) func() {
	w.mu.Lock()
	w.closing = append(w.closing, f)
	w.mu.Unlock()
	return func() {}
}

func (w *fakeLoginWindow) OnFailed(f func(error)) func() {
	w.mu.Lock()
	w.failed = append(w.failed, f)
	w.mu.Unlock()
	return func() {}
}

func (w *fakeLoginWindow) navigate() {
	w.mu.Lock()
	listeners := slices.Clone(w.navigated)
	w.mu.Unlock()
	for _, f := range listeners {
		f()
	}
}

func (w *fakeLoginWindow) close() {
	w.mu.Lock()
	listeners := slices.Clone(w.closing)
	w.mu.Unlock()
	for _, f := range listeners {
		f()
	}
}

func (w *fakeLoginWindow) fail(err error) {
	w.mu.Lock()
	listeners := slices.Clone(w.failed)
	w.mu.Unlock()
	for _, f := range listeners {
		f(err)
	}
}

func (w *fakeLoginWindow) setCookies(cookies ...fairy.Cookie) {
	w.mu.Lock()
	w.cookies = append([]fairy.Cookie(nil), cookies...)
	w.mu.Unlock()
}

func (w *fakeLoginWindow) deletedNames() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return slices.Clone(w.deleted)
}

func rejectCandidate(context.Context, string) (bool, error) { return false, nil }

func acceptCandidate(context.Context, string) (bool, error) { return true, nil }

func TestGameBananaLoginWaiterCancelDoesNotCloseWindow(t *testing.T) {
	win := newFakeLoginWindow()
	opened := make(chan struct{})
	win.runHook = func() { close(opened) }
	login := newGameBananaLogin()
	t.Cleanup(login.Close)
	var created atomic.Int32
	login.factory = func() (loginWindow, error) {
		created.Add(1)
		return win, nil
	}
	ownerDone := make(chan error, 1)
	go func() {
		_, err := login.Open(context.Background(), rejectCandidate)
		ownerDone <- err
	}()
	select {
	case <-opened:
	case <-time.After(2 * time.Second):
		t.Fatal("owner login window did not open")
	}
	waitCtx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := login.Open(waitCtx, rejectCandidate); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("waiter err = %v", err)
	}
	if win.closed.Load() {
		t.Fatal("waiter cancel closed the shared login window")
	}
	if created.Load() != 1 {
		t.Fatalf("created = %d", created.Load())
	}
	login.Close()
	if err := <-ownerDone; !errors.Is(err, gamebanana.ErrAuthFailed) {
		t.Fatalf("owner err = %v", err)
	}
}

func TestGameBananaLoginReusesExistingWindow(t *testing.T) {
	win := newFakeLoginWindow()
	login := newGameBananaLogin()
	login.factory = func() (loginWindow, error) { return win, nil }
	go func() { _, _ = login.Open(context.Background(), rejectCandidate) }()
	time.Sleep(150 * time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := login.Open(ctx, rejectCandidate); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v", err)
	}
	if win.focuses.Load() == 0 || win.shows.Load() == 0 {
		t.Fatalf("shows=%d focuses=%d", win.shows.Load(), win.focuses.Load())
	}
	login.Close()
}

func TestGameBananaLoginWaitsForNavigationBeforeCookiePoll(t *testing.T) {
	win := newFakeLoginWindow()
	win.autoReady = false
	login := newGameBananaLogin()
	login.factory = func() (loginWindow, error) { return win, nil }
	done := make(chan error, 1)
	go func() {
		_, err := login.Open(context.Background(), rejectCandidate)
		done <- err
	}()
	time.Sleep(20 * time.Millisecond)
	if win.gets.Load() != 0 {
		t.Fatalf("cookies read before navigation: %d", win.gets.Load())
	}
	win.navigate()
	deadline := time.After(time.Second)
	for win.gets.Load() == 0 {
		select {
		case <-deadline:
			t.Fatal("cookie polling did not start after navigation")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	login.Close()
	if err := <-done; !errors.Is(err, gamebanana.ErrAuthFailed) {
		t.Fatalf("err = %v", err)
	}
}

func TestGameBananaLoginPollsDoNotOverlap(t *testing.T) {
	win := newFakeLoginWindow(fairy.Cookie{Name: "keep", Value: "x"})
	win.getDelay = 80 * time.Millisecond
	login := newGameBananaLogin()
	login.pollWait = 10 * time.Millisecond
	login.factory = func() (loginWindow, error) { return win, nil }
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_, _ = login.Open(ctx, rejectCandidate)
	if win.maxFlight.Load() > 1 {
		t.Fatalf("overlapping polls: %d", win.maxFlight.Load())
	}
}

func TestGameBananaLoginDeletesInvalidCandidateOnceAndStaysOpen(t *testing.T) {
	win := newFakeLoginWindow(fairy.Cookie{Name: "rmc", Value: "bad"})
	var validates atomic.Int32
	login := newGameBananaLogin()
	login.factory = func() (loginWindow, error) { return win, nil }
	ctx, cancel := context.WithTimeout(context.Background(), 600*time.Millisecond)
	defer cancel()
	_, err := login.Open(ctx, func(context.Context, string) (bool, error) {
		validates.Add(1)
		return false, nil
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v", err)
	}
	if validates.Load() != 1 {
		t.Fatalf("validates = %d", validates.Load())
	}
	if deleted := win.deletedNames(); !slices.Equal(deleted, []string{"rmc"}) {
		t.Fatalf("deleted = %v", deleted)
	}
}

func TestGameBananaLoginTriesAllRMCCandidatesBeforeDeleting(t *testing.T) {
	win := newFakeLoginWindow(
		fairy.Cookie{Name: "rmc", Value: "stale", Domain: ".gamebanana.com", Path: "/members"},
		fairy.Cookie{Name: "rmc", Value: "valid", Domain: ".gamebanana.com", Path: "/", HTTPOnly: true},
	)
	login := newGameBananaLogin()
	login.factory = func() (loginWindow, error) { return win, nil }
	var candidates []string
	cookie, err := login.Open(context.Background(), func(_ context.Context, candidate string) (bool, error) {
		candidates = append(candidates, candidate)
		return candidate == "rmc=valid", nil
	})
	if err != nil || cookie != "rmc=valid" {
		t.Fatalf("cookie=%q err=%v", cookie, err)
	}
	if !slices.Equal(candidates, []string{"rmc=stale", "rmc=valid"}) {
		t.Fatalf("candidates = %v", candidates)
	}
	if deleted := win.deletedNames(); len(deleted) != 0 {
		t.Fatalf("deleted = %v", deleted)
	}
	if !win.closed.Load() {
		t.Fatal("window stayed open after success")
	}
}

func TestGameBananaLoginUserCloseIsCancelledAndLateCookieIgnored(t *testing.T) {
	win := newFakeLoginWindow()
	login := newGameBananaLogin()
	login.factory = func() (loginWindow, error) { return win, nil }
	errCh := make(chan error, 1)
	go func() {
		_, err := login.Open(context.Background(), acceptCandidate)
		errCh <- err
	}()
	time.Sleep(20 * time.Millisecond)
	win.close()
	if err := <-errCh; !errors.Is(err, gamebanana.ErrLoginCancelled) {
		t.Fatalf("err = %v", err)
	}
	win.setCookies(fairy.Cookie{Name: "rmc", Value: "late"})
	time.Sleep(400 * time.Millisecond)
	login.mu.Lock()
	result := login.result
	login.mu.Unlock()
	if result != "" {
		t.Fatalf("late result = %q", result)
	}
}

func TestGameBananaLoginWebViewFailureIsUnsupported(t *testing.T) {
	win := newFakeLoginWindow()
	win.autoReady = false
	win.runHook = func() { go win.fail(errLoginWebViewUnavailable) }
	login := newGameBananaLogin()
	login.factory = func() (loginWindow, error) { return win, nil }
	_, err := login.Open(context.Background(), acceptCandidate)
	if !errors.Is(err, gamebanana.ErrAutoLoginUnsupported) || err.Error() != "GAMEBANANA_AUTO_LOGIN_UNSUPPORTED" {
		t.Fatalf("err = %v", err)
	}
	if !win.closed.Load() {
		t.Fatal("failed login window stayed open")
	}
}

func TestGameBananaLoginDoesNotReuseSettledResultWithoutCloseEvent(t *testing.T) {
	first := newFakeLoginWindow(fairy.Cookie{Name: "rmc", Value: "first"})
	first.suppressCloseEvent = true
	second := newFakeLoginWindow()
	windows := []loginWindow{first, second}
	var created atomic.Int32
	login := newGameBananaLogin()
	login.factory = func() (loginWindow, error) { return windows[int(created.Add(1))-1], nil }
	if cookie, err := login.Open(context.Background(), acceptCandidate); err != nil || cookie != "rmc=first" {
		t.Fatalf("cookie=%q err=%v", cookie, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	_, err := login.Open(ctx, acceptCandidate)
	login.Close()
	if !errors.Is(err, context.DeadlineExceeded) || created.Load() != 2 {
		t.Fatalf("err=%v created=%d", err, created.Load())
	}
}

func TestGameBananaOldWindowCannotCloseNewLogin(t *testing.T) {
	first := newFakeLoginWindow(fairy.Cookie{Name: "rmc", Value: "first"})
	second := newFakeLoginWindow()
	second.autoReady = false
	login := newGameBananaLogin()
	login.factory = func() (loginWindow, error) { return first, nil }
	if _, err := login.Open(context.Background(), acceptCandidate); err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	second.runHook = func() { close(started) }
	login.factory = func() (loginWindow, error) { return second, nil }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := login.Open(ctx, nil); done <- err }()
	<-started
	first.close()
	first.navigate()
	login.mu.Lock()
	closed := login.closed
	login.mu.Unlock()
	if closed || second.closed.Load() {
		t.Fatal("old event closed new login")
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled login returned %v", err)
	}
}

func TestGameBananaLoginDeadlineClosesWindow(t *testing.T) {
	win := newFakeLoginWindow()
	win.autoReady = false
	login := newGameBananaLogin()
	login.factory = func() (loginWindow, error) { return win, nil }
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := login.Open(ctx, nil); !errors.Is(err, context.DeadlineExceeded) || !win.closed.Load() {
		t.Fatalf("deadline left window alive: %v", err)
	}
	win.navigate()
	time.Sleep(2 * gameBananaReadyWait)
	if win.gets.Load() != 0 {
		t.Fatal("late navigation started polling")
	}
}

func TestGameBananaLoginPreparesSharedProfileBeforeCreatingWindow(t *testing.T) {
	logout := newFakeLoginWindow(fairy.Cookie{Name: "rmc", Value: "stale"}, fairy.Cookie{Name: "PHPSESSID", Value: "s"})
	logout.cookieScopeURI = gameBananaLoginURL
	win := newFakeLoginWindow()
	var stages []string
	login := newGameBananaLogin()
	login.logoutFactory = func() (loginWindow, error) {
		stages = append(stages, "logout")
		return logout, nil
	}
	login.factory = func() (loginWindow, error) {
		stages = append(stages, "create")
		return win, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	var validates atomic.Int32
	_, err := login.Open(ctx, func(context.Context, string) (bool, error) {
		validates.Add(1)
		return true, nil
	})
	login.Close()
	if !errors.Is(err, context.DeadlineExceeded) || validates.Load() != 0 {
		t.Fatalf("err=%v validates=%d", err, validates.Load())
	}
	if deleted := logout.deletedNames(); !slices.Equal(deleted, []string{"rmc", "sess", "PHPSESSID"}) {
		t.Fatalf("deleted = %v", deleted)
	}
	if !logout.closed.Load() || !slices.Equal(stages, []string{"logout", "create"}) {
		t.Fatalf("logout closed=%v stages=%v", logout.closed.Load(), stages)
	}
}

func TestGameBananaLoginClearCookiesWaitsForLogoutNavigation(t *testing.T) {
	win := newFakeLoginWindow(fairy.Cookie{Name: "rmc", Value: "old"}, fairy.Cookie{Name: "rmc", Value: "dup"})
	win.autoReady = false
	login := newGameBananaLogin()
	login.logoutFactory = func() (loginWindow, error) { return win, nil }
	done := make(chan error, 1)
	go func() { done <- login.ClearCookies(context.Background()) }()
	time.Sleep(20 * time.Millisecond)
	if deleted := win.deletedNames(); len(deleted) != 0 {
		t.Fatalf("deleted before navigation = %v", deleted)
	}
	win.navigate()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if deleted := win.deletedNames(); !slices.Equal(deleted, []string{"rmc", "sess"}) {
		t.Fatalf("deleted = %v", deleted)
	}
}

func TestGameBananaLoginClearCookiesWithoutWebView2Succeeds(t *testing.T) {
	win := newFakeLoginWindow()
	win.autoReady = false
	win.runHook = func() { go win.fail(errLoginWebViewUnavailable) }
	login := newGameBananaLogin()
	login.logoutFactory = func() (loginWindow, error) { return win, nil }
	if err := login.ClearCookies(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestGameBananaLoginErrorsDoNotIncludeCookieValues(t *testing.T) {
	win := newFakeLoginWindow(fairy.Cookie{Name: "rmc", Value: "super-secret"})
	login := newGameBananaLogin()
	login.factory = func() (loginWindow, error) { return win, nil }
	_, err := login.Open(context.Background(), func(context.Context, string) (bool, error) {
		return false, errors.New("GAMEBANANA_AUTH_FAILED")
	})
	if err == nil || strings.Contains(err.Error(), "super-secret") {
		t.Fatalf("err = %v", err)
	}
}
