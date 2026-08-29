package app

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/platform"
	"nahida.live/desktop/internal/setting"
)

type TitleBarOverlaySyncOptions struct {
	SymbolColor string `json:"symbolColor"`
}

type windowFilesDroppedEvent struct {
	Paths  []string                       `json:"paths"`
	Target *application.DropTargetDetails `json:"target"`
}

type Window struct {
	mu       sync.Mutex
	boundsMu sync.Mutex

	app          *application.App
	window       application.Window
	settings     *setting.Setting
	log          *infra.Log
	ready        bool
	startHidden  bool
	consoleOpen  bool
	currentRoute string
	pendingRoute string
	saveTimer    *time.Timer
	taskbar      *nativeTaskbar
	taskbarValue *float64
	taskbarMode  string
}

const screenCacheWait = time.Second

//wails:ignore
func (w *Window) SetStartHidden(hidden bool) {
	if w == nil {
		return
	}
	w.mu.Lock()
	w.startHidden = hidden
	w.mu.Unlock()
}

//wails:ignore
func (w *Window) SetInitialRoute(route string) {
	if w == nil || route == "" {
		return
	}
	w.mu.Lock()
	if w.window == nil {
		w.pendingRoute = route
		w.currentRoute = route
	}
	w.mu.Unlock()
}

func NewWindow() *Window {
	w := &Window{}
	w.taskbar = newNativeTaskbar(w.logTaskbarError)
	return w
}

//wails:ignore
func (w *Window) Configure(app *application.App, settings *setting.Setting, log *infra.Log) {
	if w == nil {
		return
	}
	w.mu.Lock()
	w.app, w.settings, w.log = app, settings, log
	w.mu.Unlock()
}

//wails:ignore
func (w *Window) Create() application.Window {
	if w == nil {
		return nil
	}
	w.mu.Lock()
	if w.window != nil {
		window := w.window
		w.mu.Unlock()
		w.bringToForeground(window)
		return window
	}
	app, settings := w.app, w.settings
	if app == nil {
		w.mu.Unlock()
		return nil
	}
	route := w.pendingRoute
	w.pendingRoute = ""
	consoleOpen := w.consoleOpen
	w.mu.Unlock()

	opts := application.WebviewWindowOptions{
		Name:             "main",
		Title:            "Nahida Desktop",
		Width:            1200,
		Height:           800,
		MinWidth:         800,
		MinHeight:        600,
		Hidden:           true,
		Frameless:        true,
		InitialPosition:  application.WindowCentered,
		BackgroundColour: application.NewRGB(6, 7, 15),
		URL:              "/",
		EnableFileDrop:   true,
		DevToolsEnabled:  consoleOpen,
		Mac: application.MacWindow{
			InvisibleTitleBarHeight: 50,
			Backdrop:                application.MacBackdropTranslucent,
			TitleBar:                application.MacTitleBarHiddenInset,
		},
		Windows: application.WindowsWindow{
			DisableMenu:            true,
			NonClientRegionSupport: true,
			// Composition hosting can miss file drops that occur before WebView2 dispatches dragover.
			// Keep HWND hosting so rapid file drops remain reliable.
			WebView2CompositionHosting: false,
		},
	}
	if route != "" {
		opts.URL = "/#" + route
	}
	if settings != nil {
		if enabled, err := settings.GetOpenConsole(context.Background()); err == nil {
			consoleOpen = enabled
			opts.DevToolsEnabled = enabled
		}
		screens := waitForScreens(app.Screen.GetAll, screenCacheWait)
		if bounds, err := settings.GetBounds(context.Background()); err == nil && validSavedBounds(bounds, screens) {
			opts.X, opts.Y = bounds.X, bounds.Y
			opts.Width, opts.Height = bounds.Width, bounds.Height
			opts.InitialPosition = application.WindowXY
		}
	}
	created := app.Window.NewWithOptions(opts)
	w.mu.Lock()
	w.window = created
	w.ready = false
	w.consoleOpen = consoleOpen
	w.currentRoute = route
	w.mu.Unlock()
	w.registerEvents(created)
	return created
}

func waitForScreens(get func() []*application.Screen, timeout time.Duration) []*application.Screen {
	screens := get()
	if len(screens) > 0 || timeout <= 0 {
		return screens
	}
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for range ticker.C {
		screens = get()
		if len(screens) > 0 || time.Now().After(deadline) {
			return screens
		}
	}
	return nil
}

func validSavedBounds(bounds *setting.Bounds, screens []*application.Screen) bool {
	if bounds == nil || bounds.Width < 800 || bounds.Height < 600 {
		return false
	}
	point := application.Point{X: bounds.X, Y: bounds.Y}
	for _, screen := range screens {
		if screen != nil && screen.WorkArea.Contains(point) {
			return true
		}
	}
	return false
}

func (w *Window) registerEvents(window application.Window) {
	window.OnWindowEvent(events.Common.WindowRuntimeReady, func(_ *application.WindowEvent) {
		w.mu.Lock()
		if w.window != window {
			w.mu.Unlock()
			return
		}
		w.ready = true
		consoleOpen := w.consoleOpen
		startHidden := w.startHidden
		w.startHidden = false
		route := w.pendingRoute
		w.pendingRoute = ""
		w.mu.Unlock()
		if !startHidden {
			window.Show()
		}
		w.syncTaskbarProgress(window)
		if consoleOpen {
			openWindowDevTools(window)
		}
		if route != "" {
			window.EmitEvent("fn:navi", route)
		}
	})
	window.OnWindowEvent(events.Common.WindowFocus, func(_ *application.WindowEvent) {
		window.EmitEvent("window:focus")
	})
	window.OnWindowEvent(events.Common.WindowLostFocus, func(_ *application.WindowEvent) {
		window.EmitEvent("window:blur")
	})
	window.OnWindowEvent(events.Common.WindowFilesDropped, func(event *application.WindowEvent) {
		paths := event.Context().DroppedFiles()
		target := event.Context().DropTargetDetails()
		if len(paths) == 0 || target == nil {
			return
		}
		emitAppEvent("window:files-dropped", windowFilesDroppedEvent{
			Paths:  paths,
			Target: target,
		})
	})
	window.OnWindowEvent(events.Common.WindowDidMove, func(_ *application.WindowEvent) { w.scheduleBoundsSave(window) })
	window.OnWindowEvent(events.Common.WindowDidResize, func(_ *application.WindowEvent) { w.scheduleBoundsSave(window) })
	// Closing listeners run concurrently, including Wails' internal listener
	// that destroys the native window. Use a hook so bounds are read before
	// teardown; Bounds returns an empty rectangle once the window is destroyed.
	window.RegisterHook(events.Common.WindowClosing, func(_ *application.WindowEvent) {
		w.clearTaskbarProgress(window)
		w.boundsMu.Lock()
		w.saveBoundsLocked(window)
		w.mu.Lock()
		app, settings, log := w.app, w.settings, w.log
		wasCurrent := w.window == window
		if wasCurrent {
			w.window = nil
			w.ready = false
		}
		if wasCurrent && w.saveTimer != nil {
			w.saveTimer.Stop()
			w.saveTimer = nil
		}
		w.mu.Unlock()
		w.boundsMu.Unlock()
		if !wasCurrent || settings == nil || app == nil {
			return
		}
		runInBackground, err := settings.GetRunInBackground(context.Background())
		if err != nil {
			if log != nil {
				log.Error(err.Error(), "MainWindow.runInBackground")
			}
			return
		}
		if !runInBackground {
			app.Quit()
		}
	})
}

func (w *Window) scheduleBoundsSave(window application.Window) {
	w.mu.Lock()
	if w.window != window {
		w.mu.Unlock()
		return
	}
	if w.saveTimer != nil {
		w.saveTimer.Stop()
	}
	w.saveTimer = time.AfterFunc(time.Second, func() { w.saveBounds(window) })
	w.mu.Unlock()
}

func (w *Window) saveBounds(window application.Window) {
	w.boundsMu.Lock()
	defer w.boundsMu.Unlock()
	w.saveBoundsLocked(window)
}

func (w *Window) saveBoundsLocked(window application.Window) {
	if window == nil {
		return
	}
	w.mu.Lock()
	current, settings, log := w.window, w.settings, w.log
	w.mu.Unlock()
	if current != window || settings == nil || window.IsMaximised() || window.IsMinimised() || window.IsFullscreen() {
		return
	}
	bounds := window.Bounds()
	err := settings.SetBounds(context.Background(), setting.Bounds{X: bounds.X, Y: bounds.Y, Width: bounds.Width, Height: bounds.Height})
	if err != nil && log != nil {
		log.Error(err.Error(), "MainWindow.saveBounds")
	}
}

//wails:ignore
func (w *Window) FocusAndNavigate(route string) {
	if w == nil {
		return
	}
	w.mu.Lock()
	if route != "" {
		w.pendingRoute = route
	}
	window, ready := w.window, w.ready
	w.mu.Unlock()
	if window == nil {
		window = w.Create()
	}
	if window == nil {
		return
	}
	w.bringToForeground(window)
	if route != "" && ready {
		window.EmitEvent("fn:navi", route)
		w.mu.Lock()
		if w.pendingRoute == route {
			w.pendingRoute = ""
		}
		w.mu.Unlock()
	}
}

//wails:ignore
func (w *Window) HandleArguments(args []string) {
	if route := nahidaDeepLinkRoute(args); route != "" {
		w.FocusAndNavigate(route)
		return
	}
	w.FocusAndNavigate("")
}

func (w *Window) OpenSetting() { w.FocusAndNavigate("/setting/gen") }

// SyncRoute keeps the SPA route available when the native window must be
// recreated to change WebView devtools availability.
func (w *Window) SyncRoute(route string) {
	if w == nil {
		return
	}
	route = normalizeWindowRoute(route)
	w.mu.Lock()
	w.currentRoute = route
	w.mu.Unlock()
}

//wails:ignore
func (w *Window) SetConsoleWindowEnabled(enabled bool) {
	if w == nil {
		return
	}
	w.mu.Lock()
	current := w.window
	previous := w.consoleOpen
	w.consoleOpen = enabled
	ready := w.ready
	route := w.currentRoute
	w.mu.Unlock()
	if current == nil {
		return
	}
	if previous == enabled {
		if enabled && ready {
			openWindowDevTools(current)
		}
		return
	}

	w.saveBounds(current)
	w.mu.Lock()
	if w.window != current {
		w.mu.Unlock()
		return
	}
	w.window = nil
	w.ready = false
	w.pendingRoute = route
	w.mu.Unlock()

	replacement := w.Create()
	if replacement == nil {
		w.mu.Lock()
		if w.window == nil {
			w.window = current
			w.ready = ready
			w.consoleOpen = previous
			w.pendingRoute = ""
		}
		w.mu.Unlock()
		return
	}
	current.Close()
}

func normalizeWindowRoute(route string) string {
	route = strings.TrimSpace(route)
	if route == "" || route == "/" {
		return ""
	}
	if !strings.HasPrefix(route, "/") || strings.Contains(route, "#") {
		return ""
	}
	return route
}

// SyncTitleBarOverlay is retained for renderer API compatibility. The main
// window is frameless, so Electron's title-bar overlay has no equivalent.
func (w *Window) SyncTitleBarOverlay(_ TitleBarOverlaySyncOptions) {}

//wails:ignore
func (w *Window) SetProgressBar(value *float64, mode string) {
	if w == nil {
		return
	}
	value = cloneProgress(value)
	w.mu.Lock()
	changed := !equalProgress(w.taskbarValue, value) || w.taskbarMode != mode
	w.taskbarValue = value
	w.taskbarMode = mode
	window, ready := w.window, w.ready
	w.mu.Unlock()
	if changed && ready && window != nil {
		w.syncTaskbarProgress(window)
	}
}

//wails:ignore
func (w *Window) CloseTaskbar() {
	if w == nil {
		return
	}
	w.mu.Lock()
	window, taskbar := w.window, w.taskbar
	w.taskbarValue = nil
	w.taskbarMode = ""
	w.mu.Unlock()
	if window != nil {
		w.clearTaskbarProgress(window)
	}
	if taskbar != nil {
		taskbar.Close()
	}
}

//wails:ignore
func (w *Window) Focus() { w.FocusAndNavigate("") }

// WaitReady waits for the current main window's Wails runtime to be ready. The
// boolean is true when it was already ready at entry, allowing Electron-style
// callers to add their post-load grace period only on a cold window.
//
//wails:ignore
func (w *Window) WaitReady(ctx context.Context) (bool, error) {
	if w == nil {
		return false, errors.New("window service is unavailable")
	}
	w.mu.Lock()
	ready := w.ready
	w.mu.Unlock()
	if ready {
		return true, nil
	}

	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-ticker.C:
			w.mu.Lock()
			ready = w.ready
			w.mu.Unlock()
			if ready {
				return false, nil
			}
		}
	}
}

// NotifyUpdateReady focuses or recreates the main window and emits the ready
// event only to that window, matching Electron's BrowserWindow.webContents.send.
//
//wails:ignore
func (w *Window) NotifyUpdateReady() {
	if w == nil {
		return
	}
	w.Focus()
	w.mu.Lock()
	window := w.window
	w.mu.Unlock()
	if window != nil {
		window.EmitEvent("updater:update-downloaded")
	}
}

func (w *Window) syncTaskbarProgress(window application.Window) {
	hwnd := webviewHWND(window)
	if hwnd == 0 {
		return
	}
	w.mu.Lock()
	taskbar, value, mode := w.taskbar, cloneProgress(w.taskbarValue), w.taskbarMode
	w.mu.Unlock()
	if taskbar != nil {
		taskbar.Set(hwnd, value, mode)
	}
}

func (w *Window) clearTaskbarProgress(window application.Window) {
	hwnd := webviewHWND(window)
	if hwnd == 0 {
		return
	}
	w.mu.Lock()
	taskbar := w.taskbar
	w.mu.Unlock()
	if taskbar != nil {
		taskbar.Set(hwnd, nil, "")
	}
}

func (w *Window) logTaskbarError(err error) {
	if err == nil || w == nil {
		return
	}
	w.mu.Lock()
	log := w.log
	w.mu.Unlock()
	if log != nil {
		log.Error(err.Error(), "MainWindow.taskbarProgress")
	}
}

func cloneProgress(value *float64) *float64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func equalProgress(a, b *float64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func (w *Window) bringToForeground(window application.Window) {
	if window == nil {
		return
	}
	application.InvokeSync(func() {
		window.Show()
		if hwnd := webviewHWND(window); hwnd != 0 {
			platform.ForceForegroundWindow(hwnd)
		}
		window.Focus()
	})
}

func webviewHWND(window application.Window) uintptr {
	host, ok := window.(*application.WebviewWindow)
	if !ok || host == nil {
		return 0
	}
	return uintptr(host.NativeWindow())
}

func (w *Window) native() *application.WebviewWindow {
	if w == nil {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.window == nil {
		return nil
	}
	host, _ := w.window.(*application.WebviewWindow)
	return host
}
