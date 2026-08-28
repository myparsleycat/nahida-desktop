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
	gameBananaCookieURI      = "https://gamebanana.com/"
	gameBananaCookiePollWait = 350 * time.Millisecond
)

type loginWindow interface {
	Show() application.Window
	Focus()
	Close()
	GetCookies(context.Context, string) ([]application.WebviewCookie, error)
	DeleteCookies(context.Context, string, ...string) error
	OnWindowEvent(events.WindowEventType, func(*application.WindowEvent)) func()
}

type loginWindowFactory func() (loginWindow, error)

type gameBananaLogin struct {
	mu sync.Mutex

	app    *application.App
	parent *Window
	log    *infra.Log

	factory  loginWindowFactory
	pollWait time.Duration
	window   loginWindow
	cancel   context.CancelFunc

	pollInFlight   bool
	opening        bool
	lastCandidates map[string]struct{}
	settled        bool
	programmatic   bool
	closed         bool
	result         string
	err            error
	done           chan struct{}
	unsub          func()
}

func newGameBananaLogin() *gameBananaLogin {
	return &gameBananaLogin{}
}

func (l *gameBananaLogin) Configure(app *application.App, parent *Window, log *infra.Log) {
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
			l.window.Show()
			l.window.Focus()
			done := l.done
			l.mu.Unlock()
			if done == nil {
				return "", gamebanana.ErrAuthFailed
			}
			select {
			case <-done:
			case <-ctx.Done():
				return "", ctx.Err()
			}
			l.mu.Lock()
			cookie, err := l.result, l.err
			l.mu.Unlock()
			return cookie, err
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

		window, err := factory()
		if err != nil {
			l.mu.Lock()
			l.opening = false
			l.mu.Unlock()
			return "", classifyLoginWindowError(err)
		}
		if window == nil {
			l.mu.Lock()
			l.opening = false
			l.mu.Unlock()
			return "", gamebanana.ErrAutoLoginUnsupported
		}

		sessionCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
		done := make(chan struct{})
		l.mu.Lock()
		l.window = window
		l.cancel = cancel
		l.done = done
		l.settled = false
		l.programmatic = false
		l.closed = false
		l.pollInFlight = false
		l.lastCandidates = nil
		l.result = ""
		l.err = nil
		l.opening = false
		l.unsub = window.OnWindowEvent(events.Common.WindowClosing, func(*application.WindowEvent) {
			l.handleWindowClosing()
		})
		l.mu.Unlock()

		window.Show()
		window.Focus()
		go l.poll(sessionCtx, validate)

		select {
		case <-done:
			l.mu.Lock()
			cookie, loginErr := l.result, l.err
			l.mu.Unlock()
			return cookie, loginErr
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
}

func (l *gameBananaLogin) ClearCookies(ctx context.Context) error {
	if l == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	l.mu.Lock()
	window := l.window
	parent := l.parent
	l.mu.Unlock()
	if window != nil {
		return ignoreUnsupportedCookies(window.DeleteCookies(ctx, gameBananaCookieURI, "rmc"))
	}
	if parent != nil {
		if host := parent.native(); host != nil {
			return ignoreUnsupportedCookies(host.DeleteCookies(ctx, gameBananaCookieURI, "rmc"))
		}
	}
	return nil
}

func ignoreUnsupportedCookies(err error) error {
	if errors.Is(err, application.ErrWebviewCookiesUnsupported) {
		return nil
	}
	return err
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

func (l *gameBananaLogin) handleWindowClosing() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return
	}
	l.closed = true
	if l.unsub != nil {
		l.unsub()
		l.unsub = nil
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
		close(l.done)
	}
}

func (l *gameBananaLogin) settle(cookie string, err error) {
	l.mu.Lock()
	if l.settled {
		l.mu.Unlock()
		return
	}
	l.settled = true
	l.result = cookie
	l.err = err
	l.programmatic = true
	window := l.window
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
			l.mu.Lock()
			already := l.settled || l.closed
			l.mu.Unlock()
			if !already && ctx.Err() != nil && !errors.Is(ctx.Err(), context.Canceled) {
				l.settle("", gamebanana.ErrAuthFailed)
			}
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
	if l.settled || l.closed {
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
	l.pollInFlight = false
	if l.settled || l.closed {
		l.mu.Unlock()
		return false
	}
	l.mu.Unlock()
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return false
		}
		if errors.Is(err, application.ErrWebviewCookiesUnsupported) {
			l.settle("", gamebanana.ErrAutoLoginUnsupported)
			return false
		}
		if l.log != nil {
			l.log.Warn("gamebanana cookie poll failed", "GameBananaLogin")
		}
		return true
	}

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
		l.settle("", gamebanana.ErrAuthFailed)
		return false
	}
	for _, candidate := range untried {
		valid, verr := validate(ctx, candidate)
		l.mu.Lock()
		alreadyDone := l.settled || l.closed
		l.mu.Unlock()
		if alreadyDone {
			return false
		}
		if verr != nil {
			if errors.Is(verr, context.Canceled) {
				return false
			}
			l.settle("", gamebanana.ClassifyLoginError(verr))
			return false
		}
		if valid {
			l.settle(candidate, nil)
			return false
		}
	}
	if err := window.DeleteCookies(ctx, gameBananaCookieURI, "rmc"); err == nil {
		l.mu.Lock()
		l.lastCandidates = nil
		l.mu.Unlock()
	} else if l.log != nil {
		l.log.Warn("gamebanana invalid cookie cleanup failed", "GameBananaLogin")
	}
	return true
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
	created := app.Window.NewWithOptions(application.WebviewWindowOptions{
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
	if parent != nil {
		if host := parent.native(); host != nil {
			host.AttachModal(created)
		}
	}
	return &nativeLoginWindow{window: created}, nil
}

type nativeLoginWindow struct {
	window *application.WebviewWindow
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
		return gamebanana.ErrAutoLoginUnsupported
	}
	return gamebanana.ClassifyLoginError(err)
}
