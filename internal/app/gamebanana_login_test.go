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

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"nahida.live/desktop/internal/gamebanana"
)

type fakeLoginWindow struct {
	mu        sync.Mutex
	cookies   []application.WebviewCookie
	gets      atomic.Int32
	inFlight  atomic.Int32
	maxFlight atomic.Int32
	holdGet   chan struct{}
	deleted   []string
	closed    atomic.Bool
	focuses   atomic.Int32
	shows     atomic.Int32
	listeners map[events.WindowEventType][]func(*application.WindowEvent)
	getDelay  time.Duration
}

func newFakeLoginWindow(cookies ...application.WebviewCookie) *fakeLoginWindow {
	return &fakeLoginWindow{
		cookies:   append([]application.WebviewCookie(nil), cookies...),
		listeners: make(map[events.WindowEventType][]func(*application.WindowEvent)),
	}
}

func (w *fakeLoginWindow) Show() application.Window {
	w.shows.Add(1)
	return nil
}
func (w *fakeLoginWindow) Focus() { w.focuses.Add(1) }
func (w *fakeLoginWindow) Close() {
	if w.closed.CompareAndSwap(false, true) {
		w.emit(events.Common.WindowClosing)
	}
}
func (w *fakeLoginWindow) GetCookies(ctx context.Context, uri string) ([]application.WebviewCookie, error) {
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
	return append([]application.WebviewCookie(nil), w.cookies...), nil
}
func (w *fakeLoginWindow) DeleteCookies(_ context.Context, _ string, names ...string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(names) == 0 {
		w.deleted = append(w.deleted, "*")
		w.cookies = nil
		return nil
	}
	w.deleted = append(w.deleted, names...)
	kept := w.cookies[:0]
	remove := map[string]struct{}{}
	for _, name := range names {
		remove[name] = struct{}{}
	}
	for _, cookie := range w.cookies {
		if _, ok := remove[cookie.Name]; !ok {
			kept = append(kept, cookie)
		}
	}
	w.cookies = kept
	return nil
}
func (w *fakeLoginWindow) OnWindowEvent(eventType events.WindowEventType, callback func(*application.WindowEvent)) func() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.listeners[eventType] = append(w.listeners[eventType], callback)
	return func() {}
}
func (w *fakeLoginWindow) emit(eventType events.WindowEventType) {
	w.mu.Lock()
	listeners := append([]func(*application.WindowEvent){}, w.listeners[eventType]...)
	w.mu.Unlock()
	for _, listener := range listeners {
		listener(&application.WindowEvent{})
	}
}

func (w *fakeLoginWindow) setCookies(cookies ...application.WebviewCookie) {
	w.mu.Lock()
	w.cookies = append([]application.WebviewCookie(nil), cookies...)
	w.mu.Unlock()
}

func TestGameBananaLoginWaiterCancelDoesNotCloseWindow(t *testing.T) {
	win := newFakeLoginWindow()
	login := newGameBananaLogin()
	var created atomic.Int32
	login.factory = func() (loginWindow, error) {
		created.Add(1)
		return win, nil
	}
	ownerDone := make(chan error, 1)
	go func() {
		_, err := login.Open(context.Background(), func(context.Context, string) (bool, error) {
			return false, nil
		})
		ownerDone <- err
	}()
	time.Sleep(30 * time.Millisecond)
	waitCtx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, waitErr := login.Open(waitCtx, func(context.Context, string) (bool, error) {
		return false, nil
	})
	if !errors.Is(waitErr, context.DeadlineExceeded) {
		t.Fatalf("waiter err = %v", waitErr)
	}
	if win.closed.Load() {
		t.Fatal("waiter cancel closed the shared login window")
	}
	if created.Load() != 1 {
		t.Fatalf("created = %d", created.Load())
	}
	login.Close()
	if err := <-ownerDone; !errors.Is(err, gamebanana.ErrAuthFailed) && err != nil && err.Error() != "GAMEBANANA_AUTH_FAILED" {
		t.Fatalf("owner err = %v", err)
	}
}

func TestGameBananaLoginReusesExistingWindow(t *testing.T) {
	win := newFakeLoginWindow()
	login := newGameBananaLogin()
	login.factory = func() (loginWindow, error) { return win, nil }
	started := make(chan struct{})
	go func() {
		close(started)
		_, _ = login.Open(context.Background(), func(context.Context, string) (bool, error) {
			return false, nil
		})
	}()
	<-started
	time.Sleep(20 * time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := login.Open(ctx, func(context.Context, string) (bool, error) {
		return false, nil
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v", err)
	}
	if win.focuses.Load() == 0 || win.shows.Load() < 2 {
		t.Fatalf("shows=%d focuses=%d", win.shows.Load(), win.focuses.Load())
	}
	login.Close()
}

func TestGameBananaLoginPollsDoNotOverlap(t *testing.T) {
	win := newFakeLoginWindow(application.WebviewCookie{Name: "keep", Value: "x"})
	win.getDelay = 80 * time.Millisecond
	login := newGameBananaLogin()
	login.pollWait = 10 * time.Millisecond
	login.factory = func() (loginWindow, error) { return win, nil }
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_, _ = login.Open(ctx, func(context.Context, string) (bool, error) {
		return false, nil
	})
	if win.maxFlight.Load() > 1 {
		t.Fatalf("overlapping polls: %d", win.maxFlight.Load())
	}
}

func TestGameBananaLoginDoesNotRevalidateSameCandidate(t *testing.T) {
	win := newFakeLoginWindow(application.WebviewCookie{Name: "rmc", Value: "same"})
	var validates atomic.Int32
	login := newGameBananaLogin()
	login.factory = func() (loginWindow, error) { return win, nil }
	ctx, cancel := context.WithTimeout(context.Background(), 800*time.Millisecond)
	defer cancel()
	_, _ = login.Open(ctx, func(context.Context, string) (bool, error) {
		validates.Add(1)
		return false, nil
	})
	if validates.Load() != 1 {
		t.Fatalf("validates = %d", validates.Load())
	}
}

func TestGameBananaLoginDeletesInvalidCandidateAndStaysOpen(t *testing.T) {
	win := newFakeLoginWindow(application.WebviewCookie{Name: "rmc", Value: "bad"})
	login := newGameBananaLogin()
	login.factory = func() (loginWindow, error) { return win, nil }
	ctx, cancel := context.WithTimeout(context.Background(), 600*time.Millisecond)
	defer cancel()
	go func() {
		time.Sleep(450 * time.Millisecond)
		login.Close()
	}()
	_, err := login.Open(ctx, func(context.Context, string) (bool, error) {
		return false, nil
	})
	if !errors.Is(err, gamebanana.ErrAuthFailed) && !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v", err)
	}
	win.mu.Lock()
	deleted := append([]string(nil), win.deleted...)
	win.mu.Unlock()
	if len(deleted) == 0 || deleted[0] != "rmc" {
		t.Fatalf("deleted = %v", deleted)
	}
}

func TestGameBananaLoginTriesAllRMCCandidatesBeforeDeleting(t *testing.T) {
	win := newFakeLoginWindow(
		application.WebviewCookie{Name: "rmc", Value: "stale", Domain: ".gamebanana.com", Path: "/members"},
		application.WebviewCookie{Name: "rmc", Value: "valid", Domain: ".gamebanana.com", Path: "/"},
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
	if len(candidates) != 2 || candidates[0] != "rmc=stale" || candidates[1] != "rmc=valid" {
		t.Fatalf("candidates = %v", candidates)
	}
	win.mu.Lock()
	defer win.mu.Unlock()
	if len(win.deleted) != 0 {
		t.Fatalf("deleted = %v", win.deleted)
	}
}

func TestGameBananaLoginAcceptsHTTPOnlyRMC(t *testing.T) {
	win := newFakeLoginWindow(application.WebviewCookie{Name: "rmc", Value: "hidden", HTTPOnly: true})
	login := newGameBananaLogin()
	login.factory = func() (loginWindow, error) { return win, nil }
	cookie, err := login.Open(context.Background(), func(_ context.Context, candidate string) (bool, error) {
		if candidate != "rmc=hidden" {
			t.Fatalf("candidate = %q", candidate)
		}
		return true, nil
	})
	if err != nil || cookie != "rmc=hidden" {
		t.Fatalf("cookie=%q err=%v", cookie, err)
	}
}

func TestGameBananaLoginClosesAfterValidCandidateCallbackReturns(t *testing.T) {
	win := newFakeLoginWindow(application.WebviewCookie{Name: "rmc", Value: "good"})
	returned := make(chan struct{})
	closedAfter := make(chan bool, 1)
	login := newGameBananaLogin()
	login.factory = func() (loginWindow, error) { return win, nil }
	go func() {
		cookie, err := login.Open(context.Background(), func(context.Context, string) (bool, error) {
			time.Sleep(30 * time.Millisecond)
			close(returned)
			return true, nil
		})
		if err != nil || cookie != "rmc=good" {
			t.Errorf("cookie=%q err=%v", cookie, err)
		}
	}()
	<-returned
	deadline := time.After(time.Second)
	for {
		if win.closed.Load() {
			closedAfter <- true
			break
		}
		select {
		case <-deadline:
			t.Fatal("window did not close after success")
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}
}

func TestGameBananaLoginUserCloseIsCancelled(t *testing.T) {
	win := newFakeLoginWindow()
	login := newGameBananaLogin()
	login.factory = func() (loginWindow, error) { return win, nil }
	errCh := make(chan error, 1)
	go func() {
		_, err := login.Open(context.Background(), func(context.Context, string) (bool, error) {
			return false, nil
		})
		errCh <- err
	}()
	time.Sleep(20 * time.Millisecond)
	win.emit(events.Common.WindowClosing)
	err := <-errCh
	if err == nil || err.Error() != "GAMEBANANA_LOGIN_CANCELLED" {
		t.Fatalf("err = %v", err)
	}
}

func TestGameBananaLoginProgrammaticCloseIsNotCancel(t *testing.T) {
	win := newFakeLoginWindow(application.WebviewCookie{Name: "rmc", Value: "good"})
	login := newGameBananaLogin()
	login.factory = func() (loginWindow, error) { return win, nil }
	cookie, err := login.Open(context.Background(), func(context.Context, string) (bool, error) {
		return true, nil
	})
	if err != nil || cookie != "rmc=good" {
		t.Fatalf("cookie=%q err=%v", cookie, err)
	}
}

func TestGameBananaLoginLateCallbackIgnored(t *testing.T) {
	win := newFakeLoginWindow()
	login := newGameBananaLogin()
	login.factory = func() (loginWindow, error) { return win, nil }
	errCh := make(chan error, 1)
	go func() {
		_, err := login.Open(context.Background(), func(context.Context, string) (bool, error) {
			return false, nil
		})
		errCh <- err
	}()
	time.Sleep(20 * time.Millisecond)
	win.emit(events.Common.WindowClosing)
	if err := <-errCh; err == nil || err.Error() != "GAMEBANANA_LOGIN_CANCELLED" {
		t.Fatalf("err = %v", err)
	}
	win.setCookies(application.WebviewCookie{Name: "rmc", Value: "late"})
	time.Sleep(400 * time.Millisecond)
	login.mu.Lock()
	result := login.result
	login.mu.Unlock()
	if result != "" {
		t.Fatalf("late result = %q", result)
	}
}

func TestGameBananaLoginErrorsDoNotIncludeCookieValues(t *testing.T) {
	win := newFakeLoginWindow(application.WebviewCookie{Name: "rmc", Value: "super-secret"})
	login := newGameBananaLogin()
	login.factory = func() (loginWindow, error) { return win, nil }
	_, err := login.Open(context.Background(), func(context.Context, string) (bool, error) {
		return false, errors.New("GAMEBANANA_AUTH_FAILED")
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), "super-secret") {
		t.Fatalf("error leaked cookie: %v", err)
	}
}

func TestGameBananaLoginClearCookiesUsesExistingWindow(t *testing.T) {
	win := newFakeLoginWindow(
		application.WebviewCookie{Name: "rmc", Value: "old"},
		application.WebviewCookie{Name: "PHPSESSID", Value: "session"},
	)
	login := newGameBananaLogin()
	var created atomic.Int32
	login.factory = func() (loginWindow, error) {
		created.Add(1)
		return win, nil
	}
	started := make(chan struct{})
	go func() {
		close(started)
		_, _ = login.Open(context.Background(), func(context.Context, string) (bool, error) {
			return false, nil
		})
	}()
	<-started
	time.Sleep(20 * time.Millisecond)
	if err := login.ClearCookies(context.Background()); err != nil {
		t.Fatal(err)
	}
	win.mu.Lock()
	deleted := append([]string(nil), win.deleted...)
	remaining := append([]application.WebviewCookie(nil), win.cookies...)
	win.mu.Unlock()
	if !slices.Contains(deleted, "*") {
		t.Fatalf("deleted = %v", deleted)
	}
	if len(remaining) != 0 {
		t.Fatalf("remaining = %v", remaining)
	}
	if created.Load() != 1 {
		t.Fatalf("created = %d", created.Load())
	}
	login.Close()
}

func TestRuntimeWiresGameBananaLogin(t *testing.T) {
	rt := newRuntime()
	if rt.gameBananaLogin == nil {
		t.Fatal("login controller missing")
	}
	if rt.gamebanana == nil {
		t.Fatal("gamebanana service missing")
	}
}
