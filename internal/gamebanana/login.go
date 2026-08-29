package gamebanana

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"
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
	if g.openLogin == nil {
		g.loginMu.Unlock()
		return "", ErrAutoLoginUnsupported
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
		g.warnLoginFailure("open-login", err)
		call.err = classifyLoginError(err)
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
	if err := g.saveCookie(ctx, merged); err != nil {
		g.warnLoginFailure("persist-cookie", err)
		call.err = classifyLoginError(err)
		return
	}
	call.cookie = merged
}

func (g *GameBanana) warnLoginFailure(stage string, err error) {
	if g == nil || g.log == nil {
		return
	}
	detail := map[string]any{"stage": stage}
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
	g.loginMu.Unlock()
	if call != nil && call.cancel != nil {
		call.cancel()
	}
}

// ServiceShutdown cancels any in-flight login session.
func (g *GameBanana) ServiceShutdown() error {
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
	if errors.Is(err, context.DeadlineExceeded) {
		return ErrAuthFailed
	}
	var httpErr *gameBananaHTTPError
	if errors.As(err, &httpErr) && httpErr.Status >= 500 {
		return ErrServerUnreachable
	}
	if isUnreachable(err) {
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

func isUnreachable(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "no such host") ||
		strings.Contains(msg, "network is unreachable") ||
		strings.Contains(msg, "i/o timeout") ||
		strings.Contains(msg, "timeout") ||
		strings.Contains(msg, "temporarily unavailable")
}
