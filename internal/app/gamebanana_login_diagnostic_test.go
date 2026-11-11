package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/myparsleycat/fairy"

	"nahida.live/desktop/internal/gamebanana"
	"nahida.live/desktop/internal/infra"
)

func TestGameBananaLoginClearsCookiesWhenLogoutPageStalls(t *testing.T) {
	var output bytes.Buffer
	logout := newFakeLoginWindow(fairy.Cookie{Name: "rmc", Value: "private-token"})
	logout.autoReady = false
	win := newFakeLoginWindow(fairy.Cookie{Name: "rmc", Value: "candidate"})
	login := newGameBananaLogin()
	login.log = infra.NewLogWithOptions(infra.LogOptions{Writer: &output, DisableFile: true})
	login.logoutWait = 5 * time.Millisecond
	login.cleanupWait = time.Second
	login.logoutFactory = func() (loginWindow, error) { return logout, nil }
	var created atomic.Int32
	login.factory = func() (loginWindow, error) {
		created.Add(1)
		if !logout.closed.Load() || len(logout.deletedNames()) == 0 {
			t.Error("login opened before the stalled logout was cleaned up")
		}
		return win, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cookie, err := login.Open(ctx, acceptCandidate)
	if err != nil || cookie != "rmc=candidate" || created.Load() != 1 {
		t.Fatalf("login failed: %v; windows=%d", err, created.Load())
	}
	for _, want := range []string{`"fallbackSucceeded":true`, `"navigationCompleted":false`, `"windowID":1`, `"elapsedMs":`} {
		if !strings.Contains(output.String(), want) {
			t.Errorf("missing %s in %s", want, output.String())
		}
	}
	if strings.Contains(output.String(), "private-token") {
		t.Fatal("secret leaked")
	}
}

func TestGameBananaLoginRejectsUnsafeReset(t *testing.T) {
	for _, kind := range []string{"close", "unsupported", "cookies", "delete", "cleanup-timeout", "cancel"} {
		t.Run(kind, func(t *testing.T) {
			logout := newFakeLoginWindow()
			login := newGameBananaLogin()
			login.logoutWait = time.Second
			login.cleanupWait = 50 * time.Millisecond
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			want := gamebanana.ErrLoginInitFailed
			switch kind {
			case "close":
				logout.waitCloseErr = errors.New("native window still exists")
			case "unsupported":
				logout.autoReady = false
				logout.runHook = func() { go logout.fail(errLoginWebViewUnavailable) }
				want = gamebanana.ErrAutoLoginUnsupported
			case "cookies":
				logout.getErr = errors.New("cookie read failed")
			case "delete":
				logout.deleteErr = errors.New("delete failed")
			case "cleanup-timeout":
				logout.holdGet = make(chan struct{})
			case "cancel":
				logout.autoReady = false
				logout.runHook = cancel
			}
			login.logoutFactory = func() (loginWindow, error) { return logout, nil }
			var created atomic.Int32
			login.factory = func() (loginWindow, error) { created.Add(1); return newFakeLoginWindow(), nil }
			_, err := login.Open(ctx, nil)
			if err == nil || created.Load() != 0 {
				t.Fatalf("unsafe reset opened login: %v", err)
			}
			if kind != "cancel" && !errors.Is(err, want) {
				t.Fatalf("err = %v, want %v", err, want)
			}
			if kind == "cancel" && logout.gets.Load() != 0 {
				t.Fatal("cleanup started after cancellation")
			}
		})
	}
}

func TestGameBananaCookieCleanupDiagnostic(t *testing.T) {
	var output bytes.Buffer
	win := newFakeLoginWindow(fairy.Cookie{Name: "rmc", Value: "candidate-secret"})
	win.deleteErr = errors.New("DeleteCookies E_INVALIDARG rmc=error-secret")
	login := newGameBananaLogin()
	login.window = win
	login.log = infra.NewLogWithOptions(infra.LogOptions{Writer: &output, DisableFile: true})
	validations := 0
	validate := func(context.Context, string) (bool, error) {
		validations++
		return false, nil
	}
	for range 2 {
		if !login.pollOnce(context.Background(), validate) {
			t.Fatal("cleanup failure stopped polling")
		}
	}
	for _, want := range []string{
		"DeleteCookies E_INVALIDARG", `"stage":"delete-invalid-cookie"`, `"cookieName":"rmc"`, gameBananaCookieURI,
		`"cleanupFailed":true`, `"candidateCount":1`, `"windowClosed":false`, `"settled":false`,
	} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("missing %q: %s", want, output.String())
		}
	}
	if strings.Contains(output.String(), "candidate-secret") || strings.Contains(output.String(), "error-secret") {
		t.Fatal("cookie value leaked")
	}
	if validations != 1 || win.closed.Load() || strings.Count(output.String(), "\n") != 1 {
		t.Fatalf("validations=%d closed=%v log=%s", validations, win.closed.Load(), output.String())
	}
}

func TestGameBananaCookiePollDiagnosticThrottle(t *testing.T) {
	var output bytes.Buffer
	now := time.Now()
	win := newFakeLoginWindow()
	win.getErr = errors.New("GetCookies E_FAIL Cookie: poll-secret")
	login := newGameBananaLogin()
	login.window = win
	login.log = infra.NewLogWithOptions(
		infra.LogOptions{Writer: &output, DisableFile: true, Now: func() time.Time { return now }},
	)
	poll := func() {
		t.Helper()
		if !login.pollOnce(context.Background(), nil) {
			t.Fatal("recoverable poll stopped")
		}
	}
	poll()
	poll()
	if strings.Count(output.String(), "\n") != 1 {
		t.Fatalf("identical failures were not throttled: %s", output.String())
	}
	now = now.Add(5 * time.Minute)
	poll()
	if !strings.Contains(output.String(), `"suppressedCount":1`) {
		t.Fatalf("missing suppressed count: %s", output.String())
	}
	if strings.Contains(output.String(), "poll-secret") {
		t.Fatal("cookie value leaked")
	}
}

func TestGameBananaCookieDiagnosticsIgnoreCancellation(t *testing.T) {
	for _, stage := range []string{"poll", "cleanup"} {
		t.Run(stage, func(t *testing.T) {
			var output bytes.Buffer
			win := newFakeLoginWindow(fairy.Cookie{Name: "rmc", Value: "secret"})
			if stage == "poll" {
				win.getErr = context.Canceled
			} else {
				win.deleteErr = context.Canceled
			}
			login := newGameBananaLogin()
			login.window = win
			login.log = infra.NewLogWithOptions(infra.LogOptions{Writer: &output, DisableFile: true})
			login.pollOnce(context.Background(), rejectCandidate)
			if output.Len() != 0 {
				t.Fatalf("normal cancellation logged: %s", output.String())
			}
		})
	}
}

func TestGameBananaLoginDiagnosticPreservesClassifiedCause(t *testing.T) {
	for _, stage := range []string{"validate-cookie", "create-login-window", "logout-shared-webview-profile", "poll-cookies"} {
		t.Run(stage, func(t *testing.T) {
			var output bytes.Buffer
			log := infra.NewLogWithOptions(infra.LogOptions{Writer: &output, DisableFile: true})
			login := newGameBananaLogin()
			login.log = log
			cause := infra.AnnotateError(
				errors.New("native failure rmc=private-value"),
				infra.Diagnostic{Fields: map[string]any{"status": 503}},
			)
			wantCode := gamebanana.ErrAuthFailed
			var err error
			switch stage {
			case "validate-cookie", "poll-cookies":
				win := newFakeLoginWindow(fairy.Cookie{Name: "rmc", Value: "candidate-secret"})
				login.window = win
				if stage == "poll-cookies" {
					win.getErr = fmt.Errorf("native failure: %w", errLoginWebViewUnavailable)
					wantCode = gamebanana.ErrAutoLoginUnsupported
				}
				login.pollOnce(
					context.Background(),
					func(context.Context, string) (bool, error) { return false, cause },
				)
				err = login.err
			case "create-login-window":
				login.factory = func() (loginWindow, error) { return nil, cause }
				_, err = login.Open(context.Background(), nil)
			case "logout-shared-webview-profile":
				wantCode = gamebanana.ErrLoginInitFailed
				login.factory = func() (loginWindow, error) { return newFakeLoginWindow(), nil }
				login.logoutFactory = func() (loginWindow, error) { return nil, cause }
				_, err = login.Open(context.Background(), nil)
			}
			if err == nil || err.Error() != wantCode.Error() || !errors.Is(err, wantCode) {
				t.Fatalf("public contract changed: %v", err)
			}
			_ = infra.ReportError(
				log,
				infra.WithCause(gamebanana.ClassifyLoginError(err), err),
				"GameBananaService.login",
				infra.Diagnostic{Severity: infra.DiagnosticWarn, Operation: "login", Stage: "open-login"},
			)
			for _, want := range []string{"native failure", stage} {
				if !strings.Contains(output.String(), want) {
					t.Fatalf("missing %q: %s", want, output.String())
				}
			}
			if stage != "poll-cookies" && !strings.Contains(output.String(), `"status":503`) {
				t.Fatalf("inner diagnostic lost: %s", output.String())
			}
			if strings.Contains(output.String(), "private-value") ||
				strings.Contains(output.String(), "candidate-secret") {
				t.Fatal("cookie value leaked")
			}
		})
	}
}
