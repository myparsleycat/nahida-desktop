package app

import (
	"context"
	"net/url"
	"reflect"
	"testing"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

func TestModelViewerArgument(t *testing.T) {
	for _, tt := range []struct {
		name string
		args []string
		want string
	}{
		{"absolute", []string{"--model-viewer", `C:\Mods\모드 (1)`}, `C:\Mods\모드 (1)`},
		{"relative", []string{"--model-viewer", `..\모드 (1)`}, `D:\모드 (1)`},
		{"slashes", []string{"--model-viewer", `C:/Mods/A/../B`}, `C:\Mods\B`},
		{"missing", []string{"--model-viewer"}, ""},
		{"empty", []string{"--model-viewer", " "}, ""},
		{"other flag", []string{"--model-viewer", "--hidden"}, ""},
		{"duplicate", []string{"--model-viewer", `C:\First`, "--model-viewer", `C:\Second`}, `C:\First`},
		{"skip invalid", []string{"--model-viewer", "--model-viewer", `C:\Second`}, `C:\Second`},
		{"deep link", []string{"nahida://gamebanana/42", "--model-viewer", `C:\Mods`}, `C:\Mods`},
		{"deep link is not folder", []string{"--model-viewer", "nahida://gamebanana/42"}, ""},
		{"exact flag", []string{"--model-viewer=C:\\Mods"}, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := modelViewerArgument(append([]string{"app.exe"}, tt.args...), `D:\Working`); got != tt.want {
				t.Fatalf("path = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestLaunchDispatcherPreservesColdAndForwardedLaunches(t *testing.T) {
	d := &launchDispatcher{}
	var got []string
	d.Enqueue(application.SecondInstanceData{Args: []string{"app", "--model-viewer", "queued"}, WorkingDir: `C:\Mods`})
	handler := newLaunchHandler(func(path string) { got = append(got, path) }, func() { got = append(got, "main") }, func([]string) { got = append(got, "arguments") })
	d.Start(application.SecondInstanceData{Args: []string{"app", "--model-viewer", "first", "nahida://gamebanana/42"}, WorkingDir: `C:\Mods`}, handler)
	d.Enqueue(application.SecondInstanceData{Args: []string{"app"}})
	if want := []string{`C:\Mods\first`, `C:\Mods\queued`, "arguments"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("launches = %v, want %v", got, want)
	}
}

func TestLaunchDispatcherAllowsReentrantLaunch(t *testing.T) {
	d := &launchDispatcher{}
	var got []string
	d.Start(application.SecondInstanceData{Args: []string{"first"}}, func(data application.SecondInstanceData) {
		got = append(got, data.Args[0])
		if data.Args[0] == "first" {
			d.Enqueue(application.SecondInstanceData{Args: []string{"second"}})
		}
	})
	if !reflect.DeepEqual(got, []string{"first", "second"}) {
		t.Fatal(got)
	}
}

func TestMissingModelViewerFolderOpensMain(t *testing.T) {
	main := 0
	handler := newLaunchHandler(func(string) { t.Fatal("unexpected viewer") }, func() { main++ }, func([]string) { t.Fatal("unexpected forward") })
	handler(application.SecondInstanceData{Args: []string{"app", "--model-viewer"}})
	if main != 1 {
		t.Fatalf("main windows = %d", main)
	}
}

type viewerTestWindow struct {
	application.Window
	options application.WebviewWindowOptions
	ready   func(*application.WindowEvent)
	close   func(*application.WindowEvent)
}

func (w *viewerTestWindow) OnWindowEvent(event events.WindowEventType, callback func(*application.WindowEvent)) func() {
	if event == events.Common.WindowRuntimeReady {
		w.ready = callback
	}
	return func() {}
}
func (w *viewerTestWindow) RegisterHook(event events.WindowEventType, callback func(*application.WindowEvent)) func() {
	if event == events.Common.WindowClosing {
		w.close = callback
	}
	return func() {}
}
func (w *viewerTestWindow) Name() string { return w.options.Name }

func TestModelViewerWindowsDeduplicateAndCloseIndependently(t *testing.T) {
	var created []*viewerTestWindow
	var focused []application.Window
	var closed []bool
	v := &modelViewerWindows{windows: make(map[string]*modelViewerWindow)}
	v.create = func(opts application.WebviewWindowOptions) application.Window {
		w := &viewerTestWindow{options: opts}
		created = append(created, w)
		v.attach(w)
		return w
	}
	v.focus = func(w application.Window) { focused = append(focused, w) }
	v.closed = func(last bool) { closed = append(closed, last) }
	path := `C:\Mods\한글 (1) & #+%`
	v.Open(path)
	first := created[0]
	v.Open(path)
	if len(focused) != 0 || !first.options.Hidden {
		t.Fatal("viewer was shown before runtime ready")
	}
	first.ready(nil)
	v.Open(`c:/mods/한글 (1) & #+%`)
	if len(created) != 1 || len(focused) != 2 || focused[1] != first {
		t.Fatal("same folder did not focus original window")
	}
	parsed, err := url.Parse(first.options.URL)
	if err != nil {
		t.Fatal(err)
	}
	route, err := url.Parse(parsed.EscapedFragment())
	if err != nil || route.Query().Get("path") != path {
		t.Fatalf("URL did not roundtrip: %s, %v", first.options.URL, err)
	}
	if first.options.Width != 1200 || first.options.Height != 800 || first.options.MinWidth != 800 || first.options.MinHeight != 600 {
		t.Fatal(first.options)
	}
	v.Open(`C:\Mods\Other`)
	if len(created) != 2 || first.options.Name == created[1].options.Name {
		t.Fatal("different folder reused window")
	}
	first.close(nil)
	first.close(nil)
	if len(v.windows) != 1 || !reflect.DeepEqual(closed, []bool{false}) {
		t.Fatalf("closing one affected other window: %v", closed)
	}
	created[1].close(nil)
	if len(v.windows) != 0 || !reflect.DeepEqual(closed, []bool{false, true}) {
		t.Fatal(closed)
	}
	v.Open(path)
	if len(created) != 3 || created[2].options.Name != first.options.Name {
		t.Fatal("reopen name was not stable")
	}
}

func TestModelViewerReadyBeforeCreateReturns(t *testing.T) {
	var focused []application.Window
	v := &modelViewerWindows{
		windows: make(map[string]*modelViewerWindow),
		focus:   func(w application.Window) { focused = append(focused, w) },
		closed:  func(bool) {},
	}
	created := 0
	v.create = func(opts application.WebviewWindowOptions) application.Window {
		created++
		w := &viewerTestWindow{options: opts}
		v.attach(w)
		if w.ready == nil || w.close == nil {
			t.Fatal("lifecycle callbacks must be registered before native startup")
		}
		w.ready(nil)
		return w
	}
	v.attach(&viewerTestWindow{options: application.WebviewWindowOptions{Name: "main"}})
	v.Open(`C:\Mods\Fast`)
	v.Open(`c:/mods/fast`)
	if created != 1 || len(focused) != 2 || focused[0] != focused[1] {
		t.Fatalf("ready window was lost: created=%d focused=%v", created, focused)
	}
	w := focused[0].(*viewerTestWindow)
	w.close(nil)
	w.ready(nil)
	if len(v.windows) != 0 || len(focused) != 2 {
		t.Fatal("late readiness resurrected a closed window")
	}
}

func TestSyncRouteIgnoresNonMainWindow(t *testing.T) {
	w := NewWindow()
	w.currentRoute = "/tools/model-viewer"
	viewer := &viewerTestWindow{options: application.WebviewWindowOptions{Name: "model-viewer-test"}}
	w.SyncRoute(context.WithValue(context.Background(), application.WindowKey, viewer), "/model-viewer-window?path=other")
	w.SyncRoute(context.Background(), "/other")
	if w.currentRoute != "/tools/model-viewer" {
		t.Fatal(w.currentRoute)
	}
}

func TestModelViewerLastWindowExitPolicy(t *testing.T) {
	for _, tt := range []struct {
		name                         string
		last, main, background, quit bool
	}{
		{"last standalone exits", true, false, false, true},
		{"background keeps tray", true, false, true, false},
		{"main remains", true, true, false, false},
		{"another viewer remains", false, false, false, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			main := NewWindow()
			if tt.main {
				main.window = &viewerTestWindow{}
			}
			quit := false
			handler := modelViewerCloseHandler(main, func(context.Context) (bool, error) { return tt.background, nil }, func() { quit = true }, nil)
			handler(tt.last)
			if quit != tt.quit {
				t.Fatalf("quit = %v, want %v", quit, tt.quit)
			}
		})
	}
}

func TestModelViewerCloseKeepsWindowOpenedDuringSettingsRead(t *testing.T) {
	var created []*viewerTestWindow
	v := &modelViewerWindows{
		windows: make(map[string]*modelViewerWindow),
		focus:   func(application.Window) {},
	}
	v.create = func(opts application.WebviewWindowOptions) application.Window {
		w := &viewerTestWindow{options: opts}
		created = append(created, w)
		v.attach(w)
		return w
	}
	quit := false
	v.closed = modelViewerCloseHandler(NewWindow(), func(context.Context) (bool, error) {
		// Reproduce a forwarded launch while the closing hook reads settings.
		v.Open(`C:\Mods\New`)
		return false, nil
	}, func() { v.quitIfEmpty(func() { quit = true }) }, nil)
	v.Open(`C:\Mods\Old`)
	created[0].close(nil)
	if quit || len(v.windows) != 1 || len(created) != 2 {
		t.Fatalf("new window lost: quit=%v windows=%d created=%d", quit, len(v.windows), len(created))
	}
}

func TestModelViewerShutdownCommitPreventsNewNativeWindows(t *testing.T) {
	v := &modelViewerWindows{windows: make(map[string]*modelViewerWindow), create: func(application.WebviewWindowOptions) application.Window {
		t.Fatal("created a native window after shutdown committed")
		return nil
	}}
	quits := 0
	v.quitIfEmpty(func() {
		quits++
		v.Open(`C:\Mods\Late`)
	})
	v.quitIfEmpty(func() { quits++ })
	if quits != 1 {
		t.Fatalf("quit calls = %d", quits)
	}
}
