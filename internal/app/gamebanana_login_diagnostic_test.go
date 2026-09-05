package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"nahida.live/desktop/internal/gamebanana"
	"nahida.live/desktop/internal/infra"
)

func TestGameBananaCookieCleanupDiagnostic(t *testing.T) {
	var output bytes.Buffer
	win := newFakeLoginWindow(application.WebviewCookie{Name: "rmc", Value: "candidate-secret"})
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
	for _, want := range []string{"DeleteCookies E_INVALIDARG", `"stage":"delete-invalid-cookie"`, `"operation":"login"`, `"cookieName":"rmc"`, gameBananaCookieURI, `"cleanupFailed":true`, `"candidateCount":1`, `"windowClosed":false`, `"settled":false`} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("missing %q: %s", want, output.String())
		}
	}
	if strings.Contains(output.String(), "candidate-secret") || strings.Contains(output.String(), "error-secret") {
		t.Fatal("cookie value leaked")
	}
	if validations != 1 || win.closed.Load() || strings.Count(output.String(), "\n") != 1 {
		t.Fatalf("cleanup behavior changed: validations=%d, closed=%v, log=%s", validations, win.closed.Load(), output.String())
	}
}

func TestGameBananaCookiePollDiagnosticThrottle(t *testing.T) {
	var output bytes.Buffer
	now := time.Now()
	win := newFakeLoginWindow()
	win.getErr = errors.New("GetCookies E_FAIL Cookie: poll-secret")
	login := newGameBananaLogin()
	login.window = win
	login.log = infra.NewLogWithOptions(infra.LogOptions{Writer: &output, DisableFile: true, Now: func() time.Time { return now }})
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
	win.getErr = errors.New("GetCookies changed failure rmc=changed-secret")
	poll()
	win.getErr = nil
	poll()
	win.getErr = errors.New("GetCookies changed failure rmc=changed-secret")
	poll()
	if strings.Count(output.String(), "\n") != 4 {
		t.Fatalf("changed/recovered failure was suppressed: %s", output.String())
	}
	for _, want := range []string{"GetCookies E_FAIL", `"stage":"poll-cookies"`, gameBananaCookieURI} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("missing %q: %s", want, output.String())
		}
	}
	if strings.Contains(output.String(), "poll-secret") || strings.Contains(output.String(), "changed-secret") {
		t.Fatal("cookie value leaked")
	}
}

func TestGameBananaCookieDiagnosticsIgnoreCancellation(t *testing.T) {
	for _, stage := range []string{"poll", "cleanup"} {
		t.Run(stage, func(t *testing.T) {
			var output bytes.Buffer
			win := newFakeLoginWindow(application.WebviewCookie{Name: "rmc", Value: "secret"})
			if stage == "poll" {
				win.getErr = context.Canceled
			} else {
				win.deleteErr = context.Canceled
			}
			login := newGameBananaLogin()
			login.window = win
			login.log = infra.NewLogWithOptions(infra.LogOptions{Writer: &output, DisableFile: true})
			login.pollOnce(context.Background(), func(context.Context, string) (bool, error) { return false, nil })
			if output.Len() != 0 {
				t.Fatalf("normal cancellation logged: %s", output.String())
			}
		})
	}
}

func TestGameBananaCookiePollDeadlineDiagnostic(t *testing.T) {
	var output bytes.Buffer
	win := newFakeLoginWindow()
	win.getErr = context.DeadlineExceeded
	login := newGameBananaLogin()
	login.window = win
	login.log = infra.NewLogWithOptions(infra.LogOptions{Writer: &output, DisableFile: true})
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	if login.pollOnce(ctx, nil) {
		t.Fatal("deadline did not stop polling")
	}
	if !strings.Contains(output.String(), `"contextError":"context deadline exceeded"`) {
		t.Fatalf("deadline context missing: %s", output.String())
	}
}

func TestGameBananaLoginDiagnosticPreservesClassifiedCause(t *testing.T) {
	for _, stage := range []string{"validate-cookie", "create-login-window", "logout-shared-webview-profile", "poll-cookies"} {
		t.Run(stage, func(t *testing.T) {
			var output bytes.Buffer
			log := infra.NewLogWithOptions(infra.LogOptions{Writer: &output, DisableFile: true})
			login := newGameBananaLogin()
			login.log = log
			cause := infra.AnnotateError(errors.New("native failure rmc=private-value"), infra.Diagnostic{Fields: map[string]any{"status": 503}})
			wantCode := gamebanana.ErrAuthFailed
			var err error
			switch stage {
			case "validate-cookie", "poll-cookies":
				win := newFakeLoginWindow(application.WebviewCookie{Name: "rmc", Value: "candidate-secret"})
				login.window = win
				if stage == "poll-cookies" {
					win.getErr = fmt.Errorf("native failure: %w", application.ErrWebviewCookiesUnsupported)
					wantCode = gamebanana.ErrAutoLoginUnsupported
				}
				login.pollOnce(context.Background(), func(context.Context, string) (bool, error) { return false, cause })
				err = login.err
			case "create-login-window":
				login.factory = func() (loginWindow, error) { return nil, cause }
				_, err = login.Open(context.Background(), nil)
			case "logout-shared-webview-profile":
				login.factory = func() (loginWindow, error) { return newFakeLoginWindow(), nil }
				login.logoutFactory = func() (loginWindow, error) { return nil, cause }
				_, err = login.Open(context.Background(), nil)
			}
			if err == nil || err.Error() != wantCode.Error() || !errors.Is(err, wantCode) {
				t.Fatalf("public contract changed: %v", err)
			}
			_ = infra.ReportError(log, infra.WithCause(gamebanana.ClassifyLoginError(err), err), "GameBananaService.login", infra.Diagnostic{Severity: infra.DiagnosticWarn, Operation: "login", Stage: "open-login"})
			for _, want := range []string{"native failure", stage} {
				if !strings.Contains(output.String(), want) {
					t.Fatalf("missing %q: %s", want, output.String())
				}
			}
			if stage != "poll-cookies" && !strings.Contains(output.String(), `"status":503`) {
				t.Fatalf("inner diagnostic lost: %s", output.String())
			}
			if strings.Contains(output.String(), "private-value") || strings.Contains(output.String(), "candidate-secret") {
				t.Fatal("cookie value leaked")
			}
		})
	}
}
