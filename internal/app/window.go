package app

import (
	"context"
	"sync"
	"time"

	"github.com/myparsleycat/fairy"

	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/platform"
	"nahida.live/desktop/internal/setting"
	"nahida.live/desktop/internal/ui"
)

const (
	mainWindowWidth     = 1200
	mainWindowHeight    = 800
	mainWindowMinWidth  = 800
	mainWindowMinHeight = 600
)

// Window owns the main window, which shows the shell.
type Window struct {
	mu sync.Mutex

	settings *setting.Setting
	log      *infra.Log
	icon     []byte
	shell    *ui.Shell
	font     string
	opened   func()
	closed   func()

	window      *fairy.Window
	ready       bool
	startHidden bool
	everShown   bool
	progress    fairy.Progress
	value       float64
}

func NewWindow() *Window { return &Window{} }

func (w *Window) Configure(
	settings *setting.Setting,
	log *infra.Log,
	icon []byte,
	shell *ui.Shell,
	font string,
	opened func(),
	closed func(),
) {
	w.mu.Lock()
	w.settings, w.log, w.icon, w.shell, w.font = settings, log, icon, shell, font
	w.opened, w.closed = opened, closed
	w.mu.Unlock()
}

func (w *Window) SetStartHidden(hidden bool) {
	w.mu.Lock()
	w.startHidden = hidden
	w.mu.Unlock()
}

// Focus shows the main window, creating it when it is closed, and brings it to the foreground. It may be
// called from any goroutine.
func (w *Window) Focus() { fairy.Go(w.show) }

// open shows the main window at startup unless the launch asked to start hidden. It runs on the UI thread
// and creates the window before returning, so fairy.Main sees it.
func (w *Window) open() {
	w.mu.Lock()
	hidden := w.startHidden
	w.mu.Unlock()
	if !hidden {
		w.show()
	}
}

// show runs on the UI thread.
func (w *Window) show() {
	w.mu.Lock()
	w.startHidden = false
	window := w.window
	w.mu.Unlock()
	if window == nil {
		window = w.create()
	}
	if window == nil {
		return
	}
	w.mu.Lock()
	w.everShown = true
	w.mu.Unlock()
	window.Show()
	platform.ForceForegroundWindow(window.HWND())
}

// create runs on the UI thread.
func (w *Window) create() *fairy.Window {
	w.mu.Lock()
	settings, log, icon, shell, font := w.settings, w.log, w.icon, w.shell, w.font
	w.mu.Unlock()

	dark, background, text := shell.WindowColors()
	backdrop := fairy.BackdropNone
	if settings != nil {
		if enabled, err := settings.GetWindowBackdrop(context.Background()); err == nil {
			backdrop = mainWindowBackdrop(enabled)
		}
	}
	window, err := fairy.NewWindow(fairy.Options{
		Title:           "Nahida Desktop",
		Width:           mainWindowWidth,
		Height:          mainWindowHeight,
		MinWidth:        mainWindowMinWidth,
		MinHeight:       mainWindowMinHeight,
		Frameless:       true,
		Dark:            dark,
		Background:      background,
		Backdrop:        backdrop,
		DynamicBackdrop: true,
		TextColor:       text,
		Font:            font,
		Icon:            icon,
		StartHidden:     true,
		View:            shell.View,
		OnKey:           shell.OnKey,
		OnFocus:         shell.OnFocus,
		OnClose:         w.onClose,
		OnReady:         w.onReady,
	})
	if err != nil {
		_ = infra.ReportError(log, err, "MainWindow", infra.Diagnostic{Operation: "create"})
		return nil
	}
	if settings != nil {
		if bounds, err := settings.GetBounds(context.Background()); err == nil && bounds != nil &&
			bounds.Width >= mainWindowMinWidth && bounds.Height >= mainWindowMinHeight {
			window.SetBounds(fairy.Bounds{X: bounds.X, Y: bounds.Y, W: bounds.Width, H: bounds.Height})
		}
	}

	w.mu.Lock()
	w.window, w.ready = window, false
	opened := w.opened
	w.mu.Unlock()
	if opened != nil {
		opened()
	}
	return window
}

// mainWindowBackdrop is the backdrop the window backdrop setting asks for; Windows before 11 22H2 have none.
func mainWindowBackdrop(enabled bool) fairy.Backdrop {
	if !enabled || !fairy.BackdropSupported(fairy.BackdropMica) {
		return fairy.BackdropNone
	}
	return fairy.BackdropMica
}

// SetBackdropEnabled applies the window backdrop setting to the open main window. It may be called from any
// goroutine.
func (w *Window) SetBackdropEnabled(enabled bool) {
	fairy.Go(func() {
		w.mu.Lock()
		window, log := w.window, w.log
		w.mu.Unlock()
		if window == nil {
			return
		}
		if err := window.SetBackdrop(mainWindowBackdrop(enabled)); err != nil {
			_ = infra.ReportError(log, err, "MainWindow", infra.Diagnostic{
				Operation: "set-backdrop", Fields: map[string]any{"enabled": enabled},
			})
		}
	})
}

func (w *Window) onReady(window *fairy.Window) {
	w.mu.Lock()
	w.ready = true
	progress, value := w.progress, w.value
	w.mu.Unlock()
	window.SetProgress(progress, value)
}

func (w *Window) onClose() bool {
	w.mu.Lock()
	window, settings, log, shell, closed := w.window, w.settings, w.log, w.shell, w.closed
	w.window, w.ready = nil, false
	w.mu.Unlock()
	if shell != nil {
		shell.Closed()
	}
	saveBounds(window, settings, log)
	if closed != nil {
		closed()
	}
	return true
}

// SaveBounds stores the open window's position and size. Quitting through the tray, a restart or an update
// ends the message loop without closing the window, so the app calls it on the UI thread before it quits.
func (w *Window) SaveBounds() {
	w.mu.Lock()
	window, settings, log := w.window, w.settings, w.log
	w.mu.Unlock()
	saveBounds(window, settings, log)
}

func saveBounds(window *fairy.Window, settings *setting.Setting, log *infra.Log) {
	if window == nil || settings == nil || window.IsMaximized() || window.IsMinimized() {
		return
	}
	bounds := window.Bounds()
	if bounds.W <= 0 || bounds.H <= 0 {
		return
	}
	err := settings.SetBounds(
		context.Background(),
		setting.Bounds{X: bounds.X, Y: bounds.Y, Width: bounds.W, Height: bounds.H},
	)
	if err != nil {
		_ = infra.ReportError(log, err, "MainWindow", infra.Diagnostic{Operation: "save-bounds"})
	}
}

// SetProgressBar mirrors transfer progress, a percentage, on the taskbar button. A nil value clears it.
func (w *Window) SetProgressBar(value *float64, mode string) {
	progress := fairy.ProgressNone
	if value != nil {
		switch mode {
		case "indeterminate":
			progress = fairy.ProgressIndeterminate
		case "error":
			progress = fairy.ProgressError
		case "paused":
			progress = fairy.ProgressPaused
		default:
			progress = fairy.ProgressNormal
		}
	}
	w.mu.Lock()
	w.progress = progress
	w.value = 0
	if value != nil {
		w.value = *value / 100
	}
	window, ready, current := w.window, w.ready, w.value
	w.mu.Unlock()
	if window != nil && ready {
		window.SetProgress(progress, current)
	}
}

// WaitReady waits until the main window has shown its first frame. The boolean is true when it already had.
func (w *Window) WaitReady(ctx context.Context) (bool, error) {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for first := true; ; first = false {
		w.mu.Lock()
		ready := w.ready
		w.mu.Unlock()
		if ready {
			return first, nil
		}
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-ticker.C:
		}
	}
}

func (w *Window) hasShown() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.everShown
}

func (w *Window) native() *fairy.Window {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.window
}
