package app

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"nahida.live/desktop/internal/infra"
)

func modelViewerArgument(args []string, workingDir string) string {
	for i := 1; i+1 < len(args); i++ {
		if args[i] != "--model-viewer" || strings.TrimSpace(args[i+1]) == "" || strings.HasPrefix(args[i+1], "--") || strings.HasPrefix(strings.ToLower(args[i+1]), "nahida:") {
			continue
		}
		path := args[i+1]
		if !filepath.IsAbs(path) {
			path = filepath.Join(workingDir, path)
		}
		if absolute, err := filepath.Abs(path); err == nil {
			return filepath.Clean(absolute)
		}
	}
	return ""
}

func newLaunchHandler(openViewer func(string), openMain func(), handleArguments func([]string)) func(application.SecondInstanceData) {
	initial := true
	return func(data application.SecondInstanceData) {
		first := initial
		initial = false
		if path := modelViewerArgument(data.Args, data.WorkingDir); path != "" {
			openViewer(path)
			return
		}
		if first {
			openMain()
			return
		}
		handleArguments(data.Args)
	}
}

// Serialise startup and forwarded launches, including requests arriving during backend boot.
type launchDispatcher struct {
	mu       sync.Mutex
	queue    []application.SecondInstanceData
	handle   func(application.SecondInstanceData)
	draining bool
}

func (d *launchDispatcher) Enqueue(data application.SecondInstanceData) {
	d.mu.Lock()
	d.queue = append(d.queue, data)
	d.drain()
}

func (d *launchDispatcher) Start(initial application.SecondInstanceData, handle func(application.SecondInstanceData)) {
	d.mu.Lock()
	d.handle = handle
	d.queue = append([]application.SecondInstanceData{initial}, d.queue...)
	d.drain()
}

// drain takes ownership of the locked mutex and releases it around native calls.
func (d *launchDispatcher) drain() {
	if d.handle == nil || d.draining {
		d.mu.Unlock()
		return
	}
	d.draining = true
	for len(d.queue) > 0 {
		data := d.queue[0]
		d.queue = d.queue[1:]
		handle := d.handle
		d.mu.Unlock()
		handle(data)
		d.mu.Lock()
	}
	d.draining = false
	d.mu.Unlock()
}

type modelViewerWindows struct {
	mu       sync.Mutex
	windows  map[string]*modelViewerWindow
	create   func(application.WebviewWindowOptions) application.Window
	focus    func(application.Window)
	closed   func(bool)
	stopping bool
}

type modelViewerWindow struct {
	window application.Window
	ready  bool
}

func newModelViewerWindows(app *application.App, rt *runtime) *modelViewerWindows {
	v := &modelViewerWindows{
		windows: make(map[string]*modelViewerWindow),
		create: func(opts application.WebviewWindowOptions) application.Window {
			if enabled, err := rt.setting.GetOpenConsole(context.Background()); err == nil {
				opts.DevToolsEnabled = enabled
			}
			return app.Window.NewWithOptions(opts)
		},
		focus: func(window application.Window) {
			if window.IsMinimised() {
				window.Restore()
			}
			rt.window.bringToForeground(window)
		},
	}
	v.closed = modelViewerCloseHandler(rt.window, rt.setting.GetRunInBackground, func() { v.quitIfEmpty(app.Quit) }, rt.log)
	app.Window.OnCreate(v.attach)
	return v
}

// Commit shutdown under the same lock as Open, but never hold it during native teardown.
func (v *modelViewerWindows) quitIfEmpty(quit func()) {
	v.mu.Lock()
	if len(v.windows) != 0 || v.stopping {
		v.mu.Unlock()
		return
	}
	v.stopping = true
	v.mu.Unlock()
	quit()
}

func modelViewerCloseHandler(main *Window, runInBackground func(context.Context) (bool, error), quit func(), log *infra.Log) func(bool) {
	return func(last bool) {
		main.mu.Lock()
		hasMain := main.window != nil
		main.mu.Unlock()
		if !last || hasMain {
			return
		}
		background, err := runInBackground(context.Background())
		if err != nil {
			_ = infra.ReportError(log, err, "ModelViewerWindow", infra.Diagnostic{Operation: "close", Stage: "run-in-background"})
			return
		}
		if !background {
			main.mu.Lock()
			hasMain = main.window != nil
			main.mu.Unlock()
			if hasMain {
				return
			}
			quit()
		}
	}
}

func (v *modelViewerWindows) Open(path string) {
	key := fmt.Sprintf("model-viewer-%x", sha256.Sum256([]byte(strings.ToLower(filepath.Clean(path)))))
	v.mu.Lock()
	if v.stopping {
		v.mu.Unlock()
		return
	}
	if existing := v.windows[key]; existing != nil {
		window, ready := existing.window, existing.ready
		v.mu.Unlock()
		if ready {
			v.focus(window)
		}
		return
	}
	opts := application.WebviewWindowOptions{
		Name:  key,
		Title: filepath.Base(path) + " — Nahida Model Viewer",
		URL:   "/#/model-viewer-window?" + url.Values{"path": {path}}.Encode(),
		Width: 1200, Height: 800, MinWidth: 800, MinHeight: 600,
		Hidden: true, Frameless: true, InitialPosition: application.WindowCentered,
		BackgroundColour: application.NewRGB(6, 7, 15),
		Windows:          application.WindowsWindow{DisableMenu: true, NonClientRegionSupport: true, WebView2CompositionHosting: true},
	}
	v.windows[key] = &modelViewerWindow{}
	v.mu.Unlock()
	v.create(opts)
}

// OnCreate runs before native startup, so even an immediately ready window is observed.
func (v *modelViewerWindows) attach(window application.Window) {
	key := window.Name()
	v.mu.Lock()
	entry := v.windows[key]
	if entry == nil || entry.window != nil {
		v.mu.Unlock()
		return
	}
	entry.window = window
	v.mu.Unlock()
	window.OnWindowEvent(events.Common.WindowRuntimeReady, func(*application.WindowEvent) {
		v.mu.Lock()
		live := v.windows[key] == entry
		entry.ready = true
		v.mu.Unlock()
		if live {
			v.focus(window)
		}
	})
	window.RegisterHook(events.Common.WindowClosing, func(*application.WindowEvent) {
		v.mu.Lock()
		if v.windows[key] != entry {
			v.mu.Unlock()
			return
		}
		delete(v.windows, key)
		last := len(v.windows) == 0
		v.mu.Unlock()
		v.closed(last)
	})
}
