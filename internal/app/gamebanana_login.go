package app

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"nahida.live/desktop/internal/gamebanana"
	"nahida.live/desktop/internal/infra"
)

const (
	gameBananaLoginName      = "gamebanana-login"
	gameBananaLoginURL       = "https://gamebanana.com/members/account/login"
	gameBananaLogoutName     = "gamebanana-logout"
	gameBananaLogoutURL      = "https://gamebanana.com/members/account/logout"
	gameBananaCookieURI      = gameBananaLoginURL
	gameBananaCookiePollWait = 350 * time.Millisecond
	gameBananaReadyWait      = 100 * time.Millisecond
	gameBananaLogoutWait     = 15 * time.Second
	gameBananaCleanupWait    = 5 * time.Second
)

var gameBananaAuthCookieNames = []string{"rmc", "sess"}

type loginWindow interface {
	Run()
	ID() uint
	WaitClosed(context.Context) error
	Show() application.Window
	Focus()
	Close()
	GetCookies(context.Context, string) ([]application.WebviewCookie, error)
	DeleteCookies(context.Context, string, ...string) error
	OnWindowEvent(events.WindowEventType, func(*application.WindowEvent)) func()
}

type loginWindowFactory func() (loginWindow, error)

type loginWindowResult struct {
	cookie string
	err    error
}

type gameBananaLogin struct {
	mu sync.Mutex

	app    *application.App
	parent *Window
	log    *infra.Log

	factory        loginWindowFactory
	logoutFactory  loginWindowFactory
	pollWait       time.Duration
	logoutWait     time.Duration
	cleanupWait    time.Duration
	pollDiagnostic infra.DiagnosticThrottle
	profile        loginWindow
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
	unsub          func()
	readyUnsub     func()
}

func newGameBananaLogin() *gameBananaLogin {
	return &gameBananaLogin{}
}

func (l *gameBananaLogin) Configure(
	app *application.App,
	parent *Window,
	log *infra.Log,
) {
	if l == nil {
		return
	}
	l.mu.Lock()
	l.app = app
	l.parent = parent
	l.log = log
	if l.factory == nil {
		l.factory = l.createNativeWindow
	}
	if l.logoutFactory == nil {
		l.logoutFactory = l.createNativeLogoutWindow
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
			return "", infra.ReportError(l.log, infra.WithCause(gamebanana.ErrLoginInitFailed, err), "GameBananaLogin.Open", infra.Diagnostic{
				Severity: infra.DiagnosticWarn, Operation: "login", Stage: "logout-shared-webview-profile",
			})
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
		l.unsub = window.OnWindowEvent(events.Common.WindowClosing, func(*application.WindowEvent) {
			l.handleWindowClosing(window)
		})
		l.readyUnsub = window.OnWindowEvent(events.Windows.WebViewNavigationCompleted, func(*application.WindowEvent) {
			l.handleNavigationCompleted(sessionCtx, validate)
		})
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
	if l.settled || l.closed || l.ready || ctx.Err() != nil {
		l.mu.Unlock()
		return
	}
	l.ready = true
	window := l.window
	if l.readyUnsub != nil {
		l.readyUnsub()
		l.readyUnsub = nil
	}
	l.mu.Unlock()
	if window != nil {
		go l.poll(ctx, validate)
	}
}

func (l *gameBananaLogin) ClearCookies(ctx context.Context) error {
	if l == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	l.Close()
	if err := l.resetWebSession(ctx); err != nil {
		return infra.WithCause(gamebanana.ErrLoginInitFailed, err)
	}
	return nil
}

func (l *gameBananaLogin) resetWebSession(ctx context.Context) error {
	l.mu.Lock()
	factory, profile, parent := l.logoutFactory, l.profile, l.parent
	navigationWait, cleanupWait := l.logoutWait, l.cleanupWait
	l.mu.Unlock()
	if navigationWait <= 0 {
		navigationWait = gameBananaLogoutWait
	}
	if cleanupWait <= 0 {
		cleanupWait = gameBananaCleanupWait
	}
	if profile == nil && parent != nil {
		if host := parent.native(); host != nil {
			profile = &nativeLoginWindow{window: host}
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if factory == nil {
		if profile == nil {
			return nil
		}
		return clearGameBananaCookies(ctx, profile)
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
	var once sync.Once
	unsub := window.OnWindowEvent(events.Windows.WebViewNavigationCompleted, func(*application.WindowEvent) {
		once.Do(func() { close(completed) })
	})
	defer unsub()
	waitCtx, cancel := context.WithTimeout(ctx, navigationWait)
	window.Run()
	stage = "wait-logout-navigation"
	navigated := false
	select {
	case <-waitCtx.Done():
		err = waitCtx.Err()
	case <-completed:
		navigated = true
	}
	cancel()
	if err == nil {
		stage = "clear-webview-cookies"
		cleanCtx, cleanCancel := context.WithTimeout(ctx, cleanupWait)
		timer := time.NewTimer(gameBananaReadyWait)
		select {
		case <-cleanCtx.Done():
			err = cleanCtx.Err()
		case <-timer.C:
			err = clearGameBananaCookies(cleanCtx, window)
		}
		timer.Stop()
		cleanCancel()
	}
	if err == nil {
		closeCtx, closeCancel := context.WithTimeout(ctx, cleanupWait)
		defer closeCancel()
		window.Close()
		return infra.AnnotateError(window.WaitClosed(closeCtx), infra.Diagnostic{
			Operation: "login", Stage: "close-logout-window",
			Fields: map[string]any{"windowID": window.ID(), "elapsedMs": time.Since(started).Milliseconds(), "cleanupTimeoutMs": cleanupWait.Milliseconds()},
		})
	}
	original := infra.AnnotateError(err, infra.Diagnostic{Operation: "login", Stage: stage})
	fields := map[string]any{
		"windowID": window.ID(), "navigationCompleted": navigated,
		"navigationTimeoutMs": navigationWait.Milliseconds(), "cleanupTimeoutMs": cleanupWait.Milliseconds(),
		"fallbackAttempted": false,
	}
	if ctx.Err() != nil {
		fields["contextError"] = ctx.Err().Error()
		fields["elapsedMs"] = time.Since(started).Milliseconds()
		return infra.AnnotateError(original, infra.Diagnostic{Operation: "login", Stage: stage, Fields: fields})
	}
	fields["fallbackAttempted"] = true
	fallbackCtx, fallbackCancel := context.WithTimeout(ctx, cleanupWait)
	defer fallbackCancel()
	stage = "close-logout-window"
	window.Close()
	err = window.WaitClosed(fallbackCtx)
	if err == nil {
		stage = "fallback-clear-profile"
		if profile == nil {
			err = errors.New("shared WebView profile is unavailable")
		} else {
			err = clearGameBananaCookies(fallbackCtx, profile)
		}
	}
	fields["elapsedMs"] = time.Since(started).Milliseconds()
	fields["fallbackSucceeded"] = err == nil
	if fallbackCtx.Err() != nil {
		fields["contextError"] = fallbackCtx.Err().Error()
	}
	diagnostic := infra.Diagnostic{Severity: infra.DiagnosticWarn, Operation: "login", Stage: stage, Fields: fields, Causes: []error{original}}
	if err != nil {
		return infra.AnnotateError(err, diagnostic)
	}
	_ = infra.ReportError(l.log, original, "GameBananaLogin.recover", diagnostic)
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
	names := append([]string(nil), gameBananaAuthCookieNames...)
	seen := make(map[string]struct{}, len(cookies)+len(names))
	for _, name := range names {
		seen[name] = struct{}{}
	}
	for _, cookie := range cookies {
		if cookie.Name == "" {
			continue
		}
		if _, ok := seen[cookie.Name]; ok {
			continue
		}
		seen[cookie.Name] = struct{}{}
		names = append(names, cookie.Name)
	}
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

func (l *gameBananaLogin) handleWindowClosing(window loginWindow) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed || l.window != window {
		return
	}
	l.closed = true
	if l.unsub != nil {
		l.unsub()
		l.unsub = nil
	}
	if l.readyUnsub != nil {
		l.readyUnsub()
		l.readyUnsub = nil
	}
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
	if l.unsub != nil {
		l.unsub()
		l.unsub = nil
	}
	if l.readyUnsub != nil {
		l.readyUnsub()
		l.readyUnsub = nil
	}
	if l.cancel != nil {
		l.cancel()
	}
	l.finishLocked()
	l.mu.Unlock()
	if window != nil {
		window.Close()
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
		if errors.Is(err, application.ErrWebviewCookiesUnsupported) {
			l.settle(window, "", infra.AnnotateError(classifyLoginWindowError(err), l.cookieDiagnostic(ctx, "poll-cookies")))
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

func (l *gameBananaLogin) createNativeWindow() (loginWindow, error) {
	l.mu.Lock()
	app := l.app
	parent := l.parent
	l.mu.Unlock()
	if app == nil || app.Window == nil {
		return nil, gamebanana.ErrAutoLoginUnsupported
	}
	created := application.NewWindow(application.WebviewWindowOptions{
		Name:                       gameBananaLoginName,
		Title:                      "GameBanana 로그인",
		Width:                      540,
		Height:                     760,
		MinWidth:                   540,
		MinHeight:                  760,
		MaxWidth:                   540,
		MaxHeight:                  760,
		DisableResize:              true,
		InitialPosition:            application.WindowCentered,
		URL:                        gameBananaLoginURL,
		DisableWailsRuntime:        true,
		DevToolsEnabled:            false,
		DefaultContextMenuDisabled: false,
		MinimiseButtonState:        application.ButtonDisabled,
		MaximiseButtonState:        application.ButtonDisabled,
		Windows:                    application.WindowsWindow{DisableMenu: true},
	})
	if created == nil {
		return nil, gamebanana.ErrAutoLoginUnsupported
	}
	return &nativeLoginWindow{window: created, app: app, parent: parent}, nil
}

func (l *gameBananaLogin) createNativeLogoutWindow() (loginWindow, error) {
	l.mu.Lock()
	app := l.app
	l.mu.Unlock()
	if app == nil || app.Window == nil {
		return nil, gamebanana.ErrAutoLoginUnsupported
	}
	created := application.NewWindow(application.WebviewWindowOptions{
		Name:                       gameBananaLogoutName,
		Title:                      "GameBanana 로그아웃",
		Width:                      1,
		Height:                     1,
		Hidden:                     true,
		URL:                        gameBananaLogoutURL,
		DisableWailsRuntime:        true,
		DevToolsEnabled:            false,
		DefaultContextMenuDisabled: true,
		MinimiseButtonState:        application.ButtonDisabled,
		MaximiseButtonState:        application.ButtonDisabled,
		Windows:                    application.WindowsWindow{DisableMenu: true},
	})
	if created == nil {
		return nil, gamebanana.ErrAutoLoginUnsupported
	}
	return &nativeLoginWindow{window: created, app: app}, nil
}

type nativeLoginWindow struct {
	window *application.WebviewWindow
	app    *application.App
	parent *Window
}

func (w *nativeLoginWindow) Run() {
	w.app.Window.Add(w.window)
	w.window.Run()
	if w.parent != nil {
		if host := w.parent.native(); host != nil {
			host.AttachModal(w.window)
		}
	}
}
func (w *nativeLoginWindow) ID() uint { return w.window.ID() }
func (w *nativeLoginWindow) WaitClosed(ctx context.Context) error {
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		if _, exists := w.app.Window.GetByID(w.window.ID()); !exists {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (w *nativeLoginWindow) Show() application.Window { return w.window.Show() }
func (w *nativeLoginWindow) Focus()                   { w.window.Focus() }
func (w *nativeLoginWindow) Close()                   { w.window.Close() }
func (w *nativeLoginWindow) GetCookies(ctx context.Context, uri string) ([]application.WebviewCookie, error) {
	return w.window.GetCookies(ctx, uri)
}
func (w *nativeLoginWindow) DeleteCookies(ctx context.Context, uri string, names ...string) error {
	return w.window.DeleteCookies(ctx, uri, names...)
}
func (w *nativeLoginWindow) OnWindowEvent(eventType events.WindowEventType, callback func(*application.WindowEvent)) func() {
	return w.window.OnWindowEvent(eventType, callback)
}

func classifyLoginWindowError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, application.ErrWebviewCookiesUnsupported) {
		return infra.WithCause(gamebanana.ErrAutoLoginUnsupported, err)
	}
	return infra.WithCause(gamebanana.ClassifyLoginError(err), err)
}
