package app

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/kit"

	"nahida.live/desktop/internal/platform"
	"nahida.live/desktop/internal/ui"
)

// loginWindowOptions is what every GameBanana web window shares. All web views of one profile folder must
// use the same browser arguments.
type loginWindowOptions struct {
	UserDataFolder string
	BrowserArgs    string
	Icon           []byte
	Font           string
	Dark           func() bool
}

var loginWindowIDs atomic.Uint64

// loginEvents runs subscribed callbacks on their own goroutines, so state machine handlers never block
// the UI thread.
type loginEvents[T any] struct {
	mu    sync.Mutex
	next  int
	funcs map[int]func(T)
}

func (e *loginEvents[T]) on(f func(T)) func() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.funcs == nil {
		e.funcs = map[int]func(T){}
	}
	id := e.next
	e.next++
	e.funcs[id] = f
	return func() {
		e.mu.Lock()
		delete(e.funcs, id)
		e.mu.Unlock()
	}
}

func (e *loginEvents[T]) emit(value T) {
	e.mu.Lock()
	funcs := make([]func(T), 0, len(e.funcs))
	for _, f := range e.funcs {
		funcs = append(funcs, f)
	}
	e.mu.Unlock()
	for _, f := range funcs {
		go f(value)
	}
}

// guiLoginWindow shows a GameBanana page in a WebView2 child window. The logout variant keeps the browser
// at one pixel behind a progress indicator: a hidden window draws no frames, so its web view would never
// be created.
type guiLoginWindow struct {
	id     uint
	opts   loginWindowOptions
	logout bool

	navigated loginEvents[struct{}]
	closing   loginEvents[struct{}]
	failed    loginEvents[error]
	closeOnce sync.Once
	closed    chan struct{}

	// UI thread only.
	window  *fairy.Window
	view    *fairy.WebView
	failure error
}

func newGUILoginWindow(opts loginWindowOptions, logout bool) *guiLoginWindow {
	return &guiLoginWindow{
		id:     uint(loginWindowIDs.Add(1)),
		opts:   opts,
		logout: logout,
		closed: make(chan struct{}),
	}
}

func (w *guiLoginWindow) ID() uint { return w.id }

func (w *guiLoginWindow) Run() {
	fairy.Do(func() {
		select {
		case <-w.closed:
			return
		default:
		}
		url := gameBananaLoginURL
		if w.logout {
			url = gameBananaLogoutURL
		}
		w.view = fairy.NewWebView(fairy.WebViewOptions{
			URL:            url,
			UserDataFolder: w.opts.UserDataFolder,
			BrowserArgs:    w.opts.BrowserArgs,
			OnNavigated:    func(string, bool) { w.navigated.emit(struct{}{}) },
			OnError:        w.fail,
		})
		dark := w.opts.Dark != nil && w.opts.Dark()
		theme := ui.Theme(dark, w.opts.Font)
		title, width, height := ui.T("page.gamebanana.auth.sign_in"), float32(540), float32(760)
		if w.logout {
			title, width, height = ui.T("page.gamebanana.logout"), 320, 120
		}
		window, err := fairy.NewWindow(fairy.Options{
			Title:      "GameBanana - " + title,
			Width:      width,
			Height:     height,
			MinWidth:   width,
			MinHeight:  height,
			Dark:       dark,
			Background: theme.Card,
			TextColor:  theme.Foreground,
			Font:       w.opts.Font,
			Icon:       w.opts.Icon,
			View:       func(*fairy.Cx) *fairy.El { return w.build(theme) },
			OnClose: func() bool {
				w.window = nil
				w.view.Close()
				w.markClosed()
				return true
			},
		})
		if err != nil {
			w.view.Close()
			// Not a WebView2 failure, so the login reports an initialization error instead of an unsupported
			// browser, and the logout step does not treat the profile as having no session.
			w.failed.emit(fmt.Errorf("create login window: %w", err))
			w.markClosed()
			return
		}
		w.window = window
		window.Show()
		platform.ForceForegroundWindow(window.HWND())
	})
}

func (w *guiLoginWindow) build(theme kit.Theme) *fairy.El {
	kit.Use(theme)
	if w.logout {
		return fairy.Col(
			fairy.Row(kit.Spinner(16), fairy.Text(ui.T("page.gamebanana.logout"))).Gap(8).Align(fairy.Center),
			fairy.WebViewHost(w.view).Size(1, 1),
		).WFull().HFull().Center().Gap(4).Fill(theme.Card).Color(theme.Foreground)
	}
	if w.failure != nil {
		return fairy.Col(
			fairy.Text(ui.T("page.gamebanana.auth.unsupported_title")).SemiBold(),
			fairy.Text(ui.T("page.gamebanana.auth.unsupported_description")).Color(theme.MutedForeground),
		).WFull().HFull().Center().Gap(8).Pad(24).Fill(theme.Card).Color(theme.Foreground)
	}
	return fairy.WebViewHost(w.view).WFull().HFull()
}

// fail runs on the UI thread.
func (w *guiLoginWindow) fail(err error) {
	w.failure = err
	w.failed.emit(fmt.Errorf("%w: %w", errLoginWebViewUnavailable, err))
}

func (w *guiLoginWindow) markClosed() {
	w.closeOnce.Do(func() {
		close(w.closed)
		w.closing.emit(struct{}{})
	})
}

func (w *guiLoginWindow) WaitClosed(ctx context.Context) error {
	select {
	case <-w.closed:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (w *guiLoginWindow) Show() {
	fairy.Go(func() {
		if w.window != nil {
			w.window.Show()
			platform.ForceForegroundWindow(w.window.HWND())
		}
	})
}

func (w *guiLoginWindow) Focus() { w.Show() }

func (w *guiLoginWindow) Close() {
	fairy.Go(func() {
		if w.window != nil {
			w.window.Close()
			return
		}
		if w.view != nil {
			w.view.Close()
		}
		w.markClosed()
	})
}

var errLoginWindowClosed = errors.New("login window closed")

func (w *guiLoginWindow) GetCookies(ctx context.Context, uri string) ([]fairy.Cookie, error) {
	type result struct {
		cookies []fairy.Cookie
		err     error
	}
	done := make(chan result, 1)
	fairy.Go(func() {
		switch {
		case w.failure != nil:
			done <- result{err: fmt.Errorf("%w: %w", errLoginWebViewUnavailable, w.failure)}
		case w.view == nil || !w.view.Ready():
			done <- result{err: errLoginWindowClosed}
		default:
			w.view.Cookies(uri, func(cookies []fairy.Cookie, err error) { done <- result{cookies, err} })
		}
	})
	select {
	case r := <-done:
		return r.cookies, r.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (w *guiLoginWindow) DeleteCookies(ctx context.Context, uri string, names ...string) error {
	done := make(chan error, 1)
	fairy.Go(func() {
		if w.view == nil || !w.view.Ready() {
			done <- errLoginWindowClosed
			return
		}
		for _, name := range names {
			w.view.DeleteCookies(name, uri)
		}
		done <- nil
	})
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (w *guiLoginWindow) OnNavigated(f func()) func() {
	return w.navigated.on(func(struct{}) { f() })
}

func (w *guiLoginWindow) OnClosing(f func()) func() {
	return w.closing.on(func(struct{}) { f() })
}

func (w *guiLoginWindow) OnFailed(f func(error)) func() { return w.failed.on(f) }
