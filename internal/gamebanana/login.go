package gamebanana

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"nahida.live/desktop/internal/infra"
)

const loginSessionTimeout = 5 * time.Minute

// CookieValidator checks a candidate rmc cookie without persisting it.
type CookieValidator func(context.Context, string) (bool, error)

// OpenLoginFunc opens the GameBanana login UI and returns a validated cookie.
// The validator must succeed before the window is closed.
type OpenLoginFunc func(context.Context, CookieValidator) (string, error)

// ClearLoginCookiesFunc removes GameBanana cookies from the WebView profile.
type ClearLoginCookiesFunc func(context.Context) error

type loginCall struct {
	done   chan struct{}
	cancel context.CancelFunc
	cookie string
	err    error
}

func (g *GameBanana) openAuthenticatedSession(ctx context.Context) (string, error) {
	if g == nil {
		return "", ErrAuthFailed
	}

	g.loginMu.Lock()
	if g.loggingOut || g.shuttingDown {
		g.loginMu.Unlock()
		return "", ErrLoginCancelled
	}
	if call := g.login; call != nil {
		g.loginMu.Unlock()
		return waitLoginCall(ctx, call)
	}
	call := &loginCall{done: make(chan struct{})}
	loginCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), loginSessionTimeout)
	call.cancel = cancel
	g.login = call
	openLogin := g.openLogin
	g.loginMu.Unlock()

	go g.runLogin(loginCtx, call, openLogin)

	return waitLoginCall(ctx, call)
}

func (g *GameBanana) runLogin(ctx context.Context, call *loginCall, openLogin OpenLoginFunc) {
	defer func() {
		call.cancel()
		g.loginMu.Lock()
		if g.login == call {
			g.login = nil
		}
		g.loginMu.Unlock()
		close(call.done)
	}()
	var revision uint64
	for {
		if err := ctx.Err(); err != nil {
			call.err = err
			return
		}
		stored, current, err := g.cookieSnapshot(ctx)
		if err != nil {
			call.err = err
			return
		}
		revision = current
		if stored == "" {
			break
		}
		valid, merged, err := g.validateStoredRMCCookie(ctx, stored)
		_, latest, snapshotErr := g.cookieSnapshot(ctx)
		if snapshotErr != nil {
			call.err = snapshotErr
			return
		}
		if latest != revision {
			continue
		}
		if err != nil {
			call.err = infra.ReportError(
				g.log,
				infra.WithCause(classifyLoginError(err), err),
				"GameBananaService.login",
				infra.Diagnostic{Severity: infra.DiagnosticWarn, Operation: "login", Stage: "validate-stored-cookie"},
			)
			return
		}
		if !valid {
			merged = ""
		}
		applied, err := g.updateCookie(ctx, revision, merged)
		if err != nil {
			g.reportRecovery(err, "persist-validated-cookie")
			call.err = err
			return
		}
		if !applied {
			continue
		}
		if valid {
			call.cookie = merged
			return
		}
		// Read the new revision before opening; another accepted session wins.
	}
	if openLogin == nil {
		call.err = ErrAutoLoginUnsupported
		return
	}

	var validatedMu sync.Mutex
	validatedCookies := make(map[string]string)
	cookie, err := openLogin(ctx, func(vctx context.Context, candidate string) (bool, error) {
		valid, merged, verr := g.validateCandidateRMCCookie(vctx, candidate)
		if valid && verr == nil {
			if merged == "" {
				merged = candidate
			}
			if rmc := cookieValue(candidate, "rmc"); rmc != "" {
				validatedMu.Lock()
				validatedCookies[rmc] = merged
				validatedMu.Unlock()
			}
		}
		return valid, verr
	})
	if err != nil {
		call.err = infra.ReportError(
			g.log,
			infra.WithCause(classifyLoginError(err), err),
			"GameBananaService.login",
			infra.Diagnostic{Severity: infra.DiagnosticWarn, Operation: "login", Stage: "open-login"},
		)
		return
	}
	rmc := cookieValue(cookie, "rmc")
	validatedMu.Lock()
	merged, valid := validatedCookies[rmc]
	validatedMu.Unlock()
	if rmc == "" || !valid || cookieValue(merged, "rmc") == "" {
		g.warnLoginFailure("match-validated-cookie", nil)
		call.err = ErrAuthFailed
		return
	}
	applied, err := g.updateCookie(ctx, revision, merged)
	if err != nil {
		call.err = infra.ReportError(
			g.log,
			infra.WithCause(classifyLoginError(err), err),
			"GameBananaService.login",
			infra.Diagnostic{Severity: infra.DiagnosticWarn, Operation: "login", Stage: "persist-cookie"},
		)
		return
	}
	if !applied {
		// A concurrently accepted manual session satisfies the waiting callers.
		// An empty session means logout won, and must never reopen login.
		current, _, snapshotErr := g.cookieSnapshot(ctx)
		call.cookie, call.err = current, snapshotErr
		if snapshotErr == nil && current == "" {
			call.err = ErrLoginCancelled
		}
		return
	}
	call.cookie = merged
}

func (g *GameBanana) warnLoginFailure(stage string, err error) {
	if g == nil || g.log == nil || infra.IsCancellationError(err) {
		return
	}
	detail := map[string]any{"operation": "login", "stage": stage}
	if err != nil {
		detail["error"] = sanitizeLogMessage(err.Error())
	}
	g.log.Warn(detail, "GameBananaService.login")
}

func waitLoginCall(ctx context.Context, call *loginCall) (string, error) {
	select {
	case <-call.done:
		return call.cookie, call.err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func (g *GameBanana) cancelLogin() {
	if g == nil {
		return
	}
	g.loginMu.Lock()
	call := g.login
	g.login = nil
	g.loginMu.Unlock()
	if call != nil && call.cancel != nil {
		call.cancel()
	}
}

// ServiceShutdown cancels any in-flight login session.
func (g *GameBanana) ServiceShutdown() error {
	g.loginMu.Lock()
	g.shuttingDown = true
	g.loginMu.Unlock()
	g.cancelLogin()
	return nil
}

// ClassifyLoginError maps internal/network failures onto the stable
// GameBanana error codes the frontend understands.
func ClassifyLoginError(err error) error {
	return classifyLoginError(err)
}

func classifyLoginError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrLoginCancelled) || errorCode(err) == errCodeLoginCancelled {
		return ErrLoginCancelled
	}
	if errors.Is(err, ErrAutoLoginUnsupported) || errorCode(err) == errCodeAutoLoginUnsupported {
		return ErrAutoLoginUnsupported
	}
	if errors.Is(err, ErrServerUnreachable) || errorCode(err) == errCodeServerUnreachable {
		return ErrServerUnreachable
	}
	for _, classified := range []error{ErrLoginInitFailed, ErrAuthCheckFailed} {
		if errors.Is(err, classified) || errorCode(err) == classified.Error() {
			return classified
		}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return ErrServerUnreachable
	}
	var httpErr *gameBananaHTTPError
	if errors.As(err, &httpErr) && (httpErr.Status >= 500 || httpErr.Status == 429) {
		return ErrServerUnreachable
	}
	if infra.IsUnreachable(err) {
		return ErrServerUnreachable
	}
	return ErrAuthFailed
}

func errorCode(err error) string {
	if err == nil {
		return ""
	}
	return strings.TrimSpace(err.Error())
}
