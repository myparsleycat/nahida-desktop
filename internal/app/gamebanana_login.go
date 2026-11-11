package app

import (
	"context"
	"errors"
	"slices"
	"sync"
	"time"

	"github.com/myparsleycat/fairy"
	"github.com/samber/lo"

	"nahida.live/desktop/internal/gamebanana"
	"nahida.live/desktop/internal/infra"
)

const (
	gameBananaLoginURL       = "https://gamebanana.com/members/account/login"
	gameBananaLogoutURL      = "https://gamebanana.com/members/account/logout"
	gameBananaCookieURI      = gameBananaLoginURL
	gameBananaCookiePollWait = 350 * time.Millisecond
	gameBananaReadyWait      = 100 * time.Millisecond
	gameBananaLogoutWait     = 15 * time.Second
	gameBananaCleanupWait    = 5 * time.Second
)

var gameBananaAuthCookieNames = []string{"rmc", "sess"}

// errLoginWebViewUnavailable marks a WebView2 browser that could not be created, such as a missing runtime.
var errLoginWebViewUnavailable = errors.New("WebView2 is unavailable")

var errLogoutPageStalled = errors.New("GameBanana logout page did not finish loading")

// loginWindow is a window showing one GameBanana page in the shared WebView2 profile. Its events run on
// their own goroutines, never on the UI thread.
type loginWindow interface {
	Run()
	ID() uint
	WaitClosed(context.Context) error
	Show()
	Focus()
	Close()
	GetCookies(context.Context, string) ([]fairy.Cookie, error)
	DeleteCookies(context.Context, string, ...string) error
	OnNavigated(func()) func()
	OnClosing(func()) func()
	OnFailed(func(error)) func()
}

type loginWindowFactory func() (loginWindow, error)

type loginWindowResult struct {
	cookie string
	err    error
}

type gameBananaLogin struct {
	mu sync.Mutex

	log *infra.Log

	factory        loginWindowFactory
	logoutFactory  loginWindowFactory
	pollWait       time.Duration
	logoutWait     time.Duration
	cleanupWait    time.Duration
	pollDiagnostic infra.DiagnosticThrottle
	window         loginWindow
	cancel         context.CancelFunc

	pollInFlight   bool
	opening        bool
	lastCandidates map[string]struct{}
	settled        bool
	programmatic   bool
	closed         bool
	ready          bool
	result         string
	err            error
	done           chan struct{}
	call           *loginWindowResult
	unsubs         []func()
	readyUnsub     func()
}

func newGameBananaLogin() *gameBananaLogin {
	return &gameBananaLogin{}
}

// Configure sets the native windows up once the WebView2 profile folder, the proxy arguments and the theme
// are known.
func (l *gameBananaLogin) Configure(opts loginWindowOptions, log *infra.Log) {
	if l == nil {
		return
	}
	l.mu.Lock()
	l.log = log
	if l.factory == nil {
		l.factory = func() (loginWindow, error) { return newGUILoginWindow(opts, false), nil }
	}
	if l.logoutFactory == nil {
		l.logoutFactory = func() (loginWindow, error) { return newGUILoginWindow(opts, true), nil }
	}
	l.mu.Unlock()
}

func (l *gameBananaLogin) Open(ctx context.Context, validate gamebanana.CookieValidator) (string, error) {
	if l == nil {
		return "", gamebanana.ErrAutoLoginUnsupported
	}
	if ctx == nil {
		ctx = context.Background()
	}

	for {
		l.mu.Lock()
		if l.window != nil && !l.closed {
			window := l.window
			ready := l.ready
			done := l.done
			call := l.call
			l.mu.Unlock()
			if ready {
				window.Show()
				window.Focus()
			}
			if done == nil {
				return "", gamebanana.ErrAuthFailed
			}
			select {
			case <-done:
			case <-ctx.Done():
				return "", ctx.Err()
			}
			return call.cookie, call.err
		}
		if l.opening {
			l.mu.Unlock()
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(20 * time.Millisecond):
			}
			continue
		}

		factory := l.factory
		if factory == nil {
			l.mu.Unlock()
			return "", gamebanana.ErrAutoLoginUnsupported
		}
		l.opening = true
		l.mu.Unlock()

		// Prefer the browser's server logout transition; a bounded local cookie
		// cleanup can recover when that transition stalls.
		if err := l.resetWebSession(ctx); err != nil {
			l.mu.Lock()
			l.opening = false
			l.mu.Unlock()
			code := gamebanana.ErrLoginInitFailed
			if errors.Is(err, errLoginWebViewUnavailable) {
				code = gamebanana.ErrAutoLoginUnsupported
			}
			return "", infra.ReportError(
				l.log,
				infra.WithCause(code, err),
				"GameBananaLogin.Open",
				infra.Diagnostic{
					Severity: infra.DiagnosticWarn, Operation: "login", Stage: "logout-shared-webview-profile",
				},
			)
		}

		window, err := factory()
		if err != nil {
			l.mu.Lock()
			l.opening = false
			l.mu.Unlock()
			return "", infra.AnnotateError(classifyLoginWindowError(err), infra.Diagnostic{
				Severity: infra.DiagnosticWarn, Operation: "login", Stage: "create-login-window",
			})
		}
		if window == nil {
			l.mu.Lock()
			l.opening = false
			l.mu.Unlock()
			return "", gamebanana.ErrAutoLoginUnsupported
		}
		if err := ctx.Err(); err != nil {
			l.mu.Lock()
			l.opening = false
			l.mu.Unlock()
			return "", err
		}

		sessionCtx, cancel := context.WithCancel(ctx)
		done := make(chan struct{})
		call := &loginWindowResult{}
		l.mu.Lock()
		l.window = window
		l.cancel = cancel
		l.done = done
		l.call = call
		l.settled = false
		l.programmatic = false
		l.closed = false
		l.ready = false
		l.pollInFlight = false
		l.lastCandidates = nil
		l.pollDiagnostic.Report(l.log, nil, "GameBananaLogin", infra.Diagnostic{})
		l.result = ""
		l.err = nil
		l.opening = false
		l.unsubs = []func(){
			window.OnClosing(func() { l.handleWindowClosing(window) }),
			window.OnFailed(func(err error) {
				l.settle(window, "", infra.AnnotateError(classifyLoginWindowError(err), infra.Diagnostic{
					Severity: infra.DiagnosticWarn, Operation: "login", Stage: "create-login-webview",
				}))
			}),
		}
		l.readyUnsub = window.OnNavigated(func() { l.handleNavigationCompleted(sessionCtx, window, validate) })
		l.mu.Unlock()
		window.Run()

		select {
		case <-done:
			return call.cookie, call.err
		case <-ctx.Done():
			l.settle(window, "", ctx.Err())
			return "", ctx.Err()
		}
	}
}

func (l *gameBananaLogin) handleNavigationCompleted(
	ctx context.Context,
	window loginWindow,
	validate gamebanana.CookieValidator,
) {
	timer := time.NewTimer(gameBananaReadyWait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return
	case <-timer.C:
	}
	l.mu.Lock()
	if l.settled || l.closed || l.ready || l.window != window || ctx.Err() != nil {
		l.mu.Unlock()
		return
	}
	l.ready = true
	if l.readyUnsub != nil {
		l.readyUnsub()
		l.readyUnsub = nil
	}
	l.mu.Unlock()
	go l.poll(ctx, validate)
}

func (l *gameBananaLogin) ClearCookies(ctx context.Context) error {
	if l == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	l.Close()
	err := l.resetWebSession(ctx)
	if errors.Is(err, errLoginWebViewUnavailable) {
		// A profile that cannot be opened holds no session to clear.
		return nil
	}
	if err != nil {
		return infra.WithCause(gamebanana.ErrLoginInitFailed, err)
	}
	return nil
}

// resetWebSession opens the logout page in the shared profile and then deletes the GameBanana cookies there.
// When the logout page never finishes loading, the cookies are still deleted and the stall is logged.
func (l *gameBananaLogin) resetWebSession(ctx context.Context) error {
	l.mu.Lock()
	factory := l.logoutFactory
	navigationWait, cleanupWait := l.logoutWait, l.cleanupWait
	l.mu.Unlock()
	if navigationWait <= 0 {
		navigationWait = gameBananaLogoutWait
	}
	if cleanupWait <= 0 {
		cleanupWait = gameBananaCleanupWait
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if factory == nil {
		return nil
	}

	started := time.Now()
	stage := "create-logout-window"
	window, err := factory()
	if err != nil {
		return infra.AnnotateError(err, infra.Diagnostic{Operation: "login", Stage: stage})
	}
	if window == nil {
		return gamebanana.ErrAutoLoginUnsupported
	}
	defer window.Close()
	completed := make(chan struct{})
	failed := make(chan error, 1)
	var once sync.Once
	defer window.OnNavigated(func() { once.Do(func() { close(completed) }) })()
	defer window.OnFailed(func(err error) {
		select {
		case failed <- err:
		default:
		}
	})()

	waitCtx, cancel := context.WithTimeout(ctx, navigationWait)
	window.Run()
	stage = "wait-logout-navigation"
	navigated := false
	select {
	case <-completed:
		navigated = true
	case err = <-failed:
	case <-waitCtx.Done():
		err = ctx.Err()
	}
	cancel()
	fields := map[string]any{
		"windowID":            window.ID(),
		"navigationCompleted": navigated,
		"navigationTimeoutMs": navigationWait.Milliseconds(),
		"cleanupTimeoutMs":    cleanupWait.Milliseconds(),
	}
	annotate := func(err error) error {
		fields["elapsedMs"] = time.Since(started).Milliseconds()
		if ctx.Err() != nil {
			fields["contextError"] = ctx.Err().Error()
		}
		return infra.AnnotateError(err, infra.Diagnostic{Operation: "login", Stage: stage, Fields: fields})
	}
	if err != nil {
		return annotate(err)
	}

	stage = "clear-webview-cookies"
	cleanCtx, cleanCancel := context.WithTimeout(ctx, cleanupWait)
	timer := time.NewTimer(gameBananaReadyWait)
	select {
	case <-cleanCtx.Done():
		err = cleanCtx.Err()
	case err = <-failed:
	case <-timer.C:
		err = clearGameBananaCookies(cleanCtx, window)
	}
	timer.Stop()
	cleanCancel()
	if err != nil {
		return annotate(err)
	}

	stage = "close-logout-window"
	closeCtx, closeCancel := context.WithTimeout(ctx, cleanupWait)
	defer closeCancel()
	window.Close()
	if err := window.WaitClosed(closeCtx); err != nil {
		return annotate(err)
	}
	if !navigated {
		stage = "wait-logout-navigation"
		fields["fallbackSucceeded"] = true
		fields["elapsedMs"] = time.Since(started).Milliseconds()
		_ = infra.ReportError(l.log, errLogoutPageStalled, "GameBananaLogin.recover", infra.Diagnostic{
			Severity: infra.DiagnosticWarn, Operation: "login", Stage: stage, Fields: fields,
		})
	}
	return nil
}

func clearGameBananaCookies(ctx context.Context, window loginWindow) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	cookies, err := window.GetCookies(ctx, gameBananaCookieURI)
	if err != nil {
		return err
	}
	// WebView2 rejects the documented empty-name wildcard with E_INVALIDARG,
	// so always delete GameBanana's known auth cookies by name and include any
	// additional cookies visible to the login page.
	names := lo.Uniq(slices.Concat(
		gameBananaAuthCookieNames,
		lo.FilterMap(cookies, func(cookie fairy.Cookie, _ int) (string, bool) {
			return cookie.Name, cookie.Name != ""
		}),
	))
	return window.DeleteCookies(ctx, gameBananaCookieURI, names...)
}

func (l *gameBananaLogin) Close() {
	if l == nil {
		return
	}
	l.mu.Lock()
	if l.cancel != nil {
		l.cancel()
	}
	window := l.window
	l.programmatic = true
	l.mu.Unlock()
	if window != nil {
		window.Close()
	}
}

func (l *gameBananaLogin) unsubscribeLocked() {
	for _, unsub := range l.unsubs {
		unsub()
	}
	l.unsubs = nil
	if l.readyUnsub != nil {
		l.readyUnsub()
		l.readyUnsub = nil
	}
}

func (l *gameBananaLogin) handleWindowClosing(window loginWindow) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed || l.window != window {
		return
	}
	l.closed = true
	l.unsubscribeLocked()
	if l.cancel != nil {
		l.cancel()
	}
	if !l.settled {
		l.settled = true
		if l.programmatic {
			l.err = gamebanana.ErrAuthFailed
		} else {
			l.err = gamebanana.ErrLoginCancelled
		}
		l.finishLocked()
	}
	l.window = nil
}

func (l *gameBananaLogin) finishLocked() {
	if l.done == nil {
		return
	}
	select {
	case <-l.done:
	default:
		if l.call != nil {
			l.call.cookie, l.call.err = l.result, l.err
		}
		close(l.done)
	}
}

func (l *gameBananaLogin) settle(window loginWindow, cookie string, err error) {
	l.mu.Lock()
	if l.settled || l.window != window {
		l.mu.Unlock()
		return
	}
	l.settled = true
	l.result = cookie
	l.err = err
	l.programmatic = true
	l.window = nil
	l.closed = true
	l.unsubscribeLocked()
	if l.cancel != nil {
		l.cancel()
	}
	done, call := l.done, l.call
	if call != nil {
		call.cookie, call.err = cookie, err
	}
	l.mu.Unlock()

	// Close before releasing waiters so Open never returns while its window is still open. Close does not
	// block, and the closing event it triggers sees l.window == nil and does nothing.
	if window != nil {
		window.Close()
	}
	if done != nil {
		close(done)
	}
}

func (l *gameBananaLogin) poll(ctx context.Context, validate gamebanana.CookieValidator) {
	if !l.pollOnce(ctx, validate) {
		return
	}
	ticker := time.NewTicker(l.pollInterval())
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !l.pollOnce(ctx, validate) {
				return
			}
		}
	}
}

func (l *gameBananaLogin) pollOnce(ctx context.Context, validate gamebanana.CookieValidator) bool {
	l.mu.Lock()
	if l.settled || l.closed || ctx.Err() != nil {
		l.mu.Unlock()
		return false
	}
	if l.pollInFlight {
		l.mu.Unlock()
		return true
	}
	window := l.window
	l.pollInFlight = true
	l.mu.Unlock()
	if window == nil {
		l.mu.Lock()
		l.pollInFlight = false
		l.mu.Unlock()
		return false
	}

	cookies, err := window.GetCookies(ctx, gameBananaCookieURI)
	l.mu.Lock()
	if l.window == window && ctx.Err() == nil {
		l.pollInFlight = false
	}
	if l.settled || l.closed || ctx.Err() != nil {
		l.mu.Unlock()
		return false
	}
	l.mu.Unlock()
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return false
		}
		if errors.Is(err, errLoginWebViewUnavailable) {
			l.settle(
				window,
				"",
				infra.AnnotateError(classifyLoginWindowError(err), l.cookieDiagnostic(ctx, "poll-cookies")),
			)
			return false
		}
		l.pollDiagnostic.Report(l.log, err, "GameBananaLogin", l.cookieDiagnostic(ctx, "poll-cookies"))
		return !errors.Is(err, context.DeadlineExceeded)
	}
	l.pollDiagnostic.Report(l.log, nil, "GameBananaLogin", infra.Diagnostic{})

	candidates := make([]string, 0, len(cookies))
	unique := make(map[string]struct{}, len(cookies))
	for _, cookie := range cookies {
		if cookie.Name == "rmc" && cookie.Value != "" {
			candidate := "rmc=" + cookie.Value
			if _, ok := unique[candidate]; ok {
				continue
			}
			unique[candidate] = struct{}{}
			candidates = append(candidates, candidate)
		}
	}
	if len(candidates) == 0 {
		return true
	}

	l.mu.Lock()
	if ctx.Err() != nil || l.window != window {
		l.mu.Unlock()
		return false
	}
	if l.lastCandidates == nil {
		l.lastCandidates = make(map[string]struct{})
	}
	untried := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if _, seen := l.lastCandidates[candidate]; seen {
			continue
		}
		l.lastCandidates[candidate] = struct{}{}
		untried = append(untried, candidate)
	}
	l.mu.Unlock()
	if len(untried) == 0 {
		return true
	}

	if validate == nil {
		l.settle(window, "", gamebanana.ErrAuthFailed)
		return false
	}
	for _, candidate := range untried {
		valid, verr := validate(ctx, candidate)
		l.mu.Lock()
		alreadyDone := l.settled || l.closed || ctx.Err() != nil
		l.mu.Unlock()
		if alreadyDone {
			return false
		}
		if verr != nil {
			if errors.Is(verr, context.Canceled) {
				return false
			}
			l.settle(window, "", infra.AnnotateError(
				infra.WithCause(gamebanana.ClassifyLoginError(verr), verr),
				l.cookieDiagnostic(ctx, "validate-cookie"),
			))
			return false
		}
		if valid {
			l.settle(window, candidate, nil)
			return false
		}
	}
	if err := window.DeleteCookies(ctx, gameBananaCookieURI, "rmc"); err == nil {
		l.mu.Lock()
		if ctx.Err() == nil && l.window == window {
			l.lastCandidates = nil
		}
		l.mu.Unlock()
	} else if !errors.Is(err, context.Canceled) {
		diagnostic := l.cookieDiagnostic(ctx, "delete-invalid-cookie")
		diagnostic.Fields["cleanupFailed"] = true
		diagnostic.Fields["candidateCount"] = len(untried)
		_ = infra.ReportError(l.log, err, "GameBananaLogin", diagnostic)
	}
	return true
}

func (l *gameBananaLogin) cookieDiagnostic(ctx context.Context, stage string) infra.Diagnostic {
	l.mu.Lock()
	fields := map[string]any{
		"uri": gameBananaCookieURI, "cookieName": "rmc",
		"windowClosed": l.closed, "settled": l.settled,
	}
	l.mu.Unlock()
	if err := ctx.Err(); err != nil {
		fields["contextError"] = err.Error()
	}
	return infra.Diagnostic{Severity: infra.DiagnosticWarn, Operation: "login", Stage: stage, Fields: fields}
}

func (l *gameBananaLogin) pollInterval() time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.pollWait > 0 {
		return l.pollWait
	}
	return gameBananaCookiePollWait
}

func classifyLoginWindowError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, errLoginWebViewUnavailable) {
		return infra.WithCause(gamebanana.ErrAutoLoginUnsupported, err)
	}
	return infra.WithCause(gamebanana.ClassifyLoginError(err), err)
}
