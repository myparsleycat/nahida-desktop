package app

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"nahida.live/desktop/internal/gamebanana"
	"nahida.live/desktop/internal/infra"
)

func TestGameBananaLoginRecoversBeforeOpening(t *testing.T) {
	for _, navigation := range []bool{false, true} {
		t.Run(map[bool]string{false: "navigation-timeout", true: "cookie-error"}[navigation], func(t *testing.T) {
			var output bytes.Buffer
			logout := newFakeLoginWindow()
			logout.autoReady = navigation
			if navigation {
				logout.getErr = errors.New("cookie read failed")
			}
			profile := newFakeLoginWindow(application.WebviewCookie{Name: "rmc", Value: "private-token"})
			win := newFakeLoginWindow(application.WebviewCookie{Name: "rmc", Value: "candidate"})
			login := newGameBananaLogin()
			login.log = infra.NewLogWithOptions(infra.LogOptions{Writer: &output, DisableFile: true})
			login.logoutWait = 5 * time.Millisecond
			login.cleanupWait = time.Second
			login.profile = profile
			login.logoutFactory = func() (loginWindow, error) { return logout, nil }
			var created atomic.Int32
			login.factory = func() (loginWindow, error) {
				created.Add(1)
				if !logout.closed.Load() || profile.gets.Load() != 1 {
					t.Error("login opened before fallback cleanup")
				}
				profile.mu.Lock()
				defer profile.mu.Unlock()
				if len(profile.cookies) != 0 {
					t.Error("profile cookies remain")
				}
				return win, nil
			}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			cookie, err := login.Open(ctx, func(context.Context, string) (bool, error) { return true, nil })
			if err != nil || cookie != "rmc=candidate" || created.Load() != 1 {
				t.Fatalf("login failed: %v; windows=%d", err, created.Load())
			}
			for _, want := range []string{`"fallbackSucceeded":true`, `"fallbackAttempted":true`, `"windowID":1`, `"elapsedMs":`, "fallback-clear-profile"} {
				if !strings.Contains(output.String(), want) {
					t.Errorf("missing %s in %s", want, output.String())
				}
			}
			if strings.Contains(output.String(), "private-token") {
				t.Fatal("secret leaked")
			}
		})
	}
}

func TestGameBananaLoginRejectsUnsafeFallback(t *testing.T) {
	for _, kind := range []string{"close", "missing-profile", "unsupported", "delete", "cleanup-timeout", "cancel"} {
		t.Run(kind, func(t *testing.T) {
			logout := newFakeLoginWindow()
			logout.autoReady = false
			profile := newFakeLoginWindow()
			login := newGameBananaLogin()
			login.profile = profile
			login.logoutWait = 5 * time.Millisecond
			login.cleanupWait = 20 * time.Millisecond
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			switch kind {
			case "close":
				logout.waitCloseErr = errors.New("native window still exists")
			case "missing-profile":
				login.profile = nil
			case "unsupported":
				profile.getErr = application.ErrWebviewCookiesUnsupported
			case "delete":
				profile.deleteErr = errors.New("delete failed")
			case "cleanup-timeout":
				profile.holdGet = make(chan struct{})
			case "cancel":
				logout.runHook = cancel
			}
			login.logoutFactory = func() (loginWindow, error) { return logout, nil }
			var created atomic.Int32
			login.factory = func() (loginWindow, error) { created.Add(1); return newFakeLoginWindow(), nil }
			_, err := login.Open(ctx, nil)
			if err == nil || created.Load() != 0 {
				t.Fatalf("unsafe fallback opened login: %v", err)
			}
			if kind == "close" || kind == "cancel" {
				if profile.gets.Load() != 0 {
					t.Fatal("cleanup started before safe close or after cancellation")
				}
			}
		})
	}
}

func TestGameBananaLoginDeadlineClosesWindow(t *testing.T) {
	win := newFakeLoginWindow()
	win.autoReady = false
	login := newGameBananaLogin()
	login.factory = func() (loginWindow, error) { return win, nil }
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := login.Open(ctx, nil)
	if !errors.Is(err, context.DeadlineExceeded) || !win.closed.Load() {
		t.Fatalf("deadline left window alive: %v", err)
	}
	win.emit(events.Windows.WebViewNavigationCompleted)
	if win.gets.Load() != 0 {
		t.Fatal("late navigation started polling")
	}
}

func TestGameBananaOldWindowCannotCloseNewLogin(t *testing.T) {
	first := newFakeLoginWindow(application.WebviewCookie{Name: "rmc", Value: "first"})
	second := newFakeLoginWindow()
	second.autoReady = false
	login := newGameBananaLogin()
	login.factory = func() (loginWindow, error) { return first, nil }
	if _, err := login.Open(context.Background(), func(context.Context, string) (bool, error) { return true, nil }); err != nil {
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
	first.emit(events.Common.WindowClosing)
	first.emit(events.Windows.WebViewNavigationCompleted)
	login.mu.Lock()
	closed := login.closed
	login.mu.Unlock()
	if closed || second.closed.Load() {
		t.Fatal("old event closed new login")
	}
	cancel()
	<-done
}

func TestGameBananaInitializationErrorCode(t *testing.T) {
	login := newGameBananaLogin()
	login.factory = func() (loginWindow, error) { return newFakeLoginWindow(), nil }
	login.logoutFactory = func() (loginWindow, error) { return nil, errors.New("native failure") }
	_, err := login.Open(context.Background(), nil)
	if !errors.Is(err, gamebanana.ErrLoginInitFailed) {
		t.Fatalf("err=%v", err)
	}
}
