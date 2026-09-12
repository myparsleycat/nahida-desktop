package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"nahida.live/desktop/internal/db"
	"nahida.live/desktop/internal/infra"
)

func TestStartupWorkStopsBeforeReleasingDependencies(t *testing.T) {
	t.Parallel()
	s := newStartupWork()
	started := make(chan struct{})
	finish := make(chan struct{})
	s.start(func(ctx context.Context) {
		close(started)
		<-ctx.Done()
		<-finish
	})
	<-started
	stopped := make(chan struct{})
	go func() { s.stop(); close(stopped) }()
	<-s.ctx.Done()
	select {
	case <-stopped:
		t.Fatal("shutdown returned while maintenance still owned dependencies")
	default:
	}
	close(finish)
	<-stopped
	s.stop()
	if !errors.Is(s.wait(context.Background()), context.Canceled) {
		t.Fatal("stopped startup released a waiting operation")
	}
}

func TestStartupWorkCanStopBeforeApplicationStarted(t *testing.T) {
	t.Parallel()
	s := newStartupWork()
	s.stop()
	s.start(func(context.Context) { t.Error("maintenance ran after startup was abandoned") })
}

func TestStartupPrefetchDoesNotHoldReadinessButIsJoinedOnStop(t *testing.T) {
	t.Parallel()
	s := newStartupWork()
	finish := make(chan struct{})
	prefetchDone := make(chan struct{})
	s.start(func(ctx context.Context) {
		s.prefetchDone = prefetchDone
		go func() {
			defer close(prefetchDone)
			<-ctx.Done()
			<-finish
		}()
	})
	if err := s.wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	stopped := make(chan struct{})
	go func() { s.stop(); close(stopped) }()
	<-s.ctx.Done()
	select {
	case <-stopped:
		t.Fatal("shutdown did not join release prefetch")
	default:
	}
	close(finish)
	<-stopped
}

func TestRuntimeInitDefersBisectRecovery(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	path := filepath.Join(root, "data.db")
	store, err := infra.OpenStore(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	modRoot := filepath.Join(root, "mods")
	if err := os.Mkdir(modRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := store.DB.GamePaths.Insert(
		context.Background(),
		db.GamePathRow{Game: "test", ModFolderPath: modRoot},
	); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	original := filepath.Join(modRoot, "mod.ini")
	disabled := original + ".mod-bisect-disabled"
	if err := os.WriteFile(disabled, []byte("restore"), 0o600); err != nil {
		t.Fatal(err)
	}
	rt := newRuntime()
	t.Cleanup(func() { _ = rt.Close() })
	if err := rt.Init(context.Background(), path, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(disabled); err != nil {
		t.Fatal("Init ran recovery before the application could create a window")
	}
	rt.startup.start(func(ctx context.Context) {
		if err := rt.tools.RecoverBisects(ctx); err != nil {
			t.Error(err)
		}
	})
	if err := rt.startup.wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	if content, err := os.ReadFile(original); err != nil || string(content) != "restore" {
		t.Fatalf("deferred recovery = %q, %v", content, err)
	}
}

func TestStartupMiddlewareProtectsFileServices(t *testing.T) {
	// Wails' binding registry uses the application singleton; do not parallelise.
	application.New(application.Options{Name: "startup-binding-test"})
	rt := newRuntime()
	t.Cleanup(rt.startup.stop)
	if err := rt.configureStartupBindings(); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		method string
		wait   bool
	}{
		{"mod.Mod.Toggle", true},
		{"tools.Tools.BisectStart", true},
		{"xxmi.XXMI.StartGame", true},
		{"drive.Drive.StartDownload", true},
		{"transfer.Transfer.Create", true},
		{"platform.FS.Mkdir", true},
		{"platform.Shell.Trash", true},
		{"setting.Setting.SetPersistToggles", true},
		{"setting.Setting.Set", true},
		{"setting.Setting.ClearImageCache", true},
		{"setting.Setting.Get", false},
		{"platform.Shell.GetAppStatus", false},
		{"platform.Shell.OpenExternal", false},
		{"auth.Auth.GetSession", false},
		{"app.Window.SyncRoute", false},
	} {
		options := application.CallOptions{MethodName: "nahida.live/desktop/internal/" + tc.method}
		if got := rt.startup.needsWait(options); got != tc.wait {
			t.Errorf("%s gate = %v, want %v", tc.method, got, tc.wait)
		}
		binding := rt.startup.bindings.Get(&options)
		if binding != nil && rt.startup.needsWait(application.CallOptions{MethodID: binding.ID}) != tc.wait {
			t.Errorf("numeric ID for %s has a different gate", tc.method)
		}
	}

	method := rt.startup.bindings.Get(
		&application.CallOptions{MethodName: "nahida.live/desktop/internal/mod.Mod.Toggle"},
	)
	if method == nil {
		t.Fatal("missing Mod.Toggle binding")
	}
	body := fmt.Sprintf(
		`{"object":0,"method":0,"args":{"methodID":%d,"call-id":"startup-toggle","args":["mod"]}}`,
		method.ID,
	)
	var calls atomic.Int32
	handler := rt.startup.middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = io.Copy(w, r.Body)
	}))
	response := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/wails/runtime", strings.NewReader(body)))
		close(done)
	}()
	waitStartupPending(t, rt.startup, "startup-toggle")
	if calls.Load() != 0 {
		t.Fatal("file operation ran before maintenance")
	}
	// Runtime/window controls and assets cannot be held behind recovery.
	for _, path := range []string{"/wails/runtime", "/wails/runtime.js", "/"} {
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, path,
			strings.NewReader(`{"object":6,"method":0,"args":{}}`)))
	}
	if calls.Load() != 3 {
		t.Fatal("window or asset request was blocked")
	}
	rt.startup.start(func(context.Context) {})
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("file operation was not released after maintenance")
	}
	if response.Body.String() != body || calls.Load() != 4 {
		t.Fatal("queued request body was not forwarded intact")
	}
}

func TestStartupMiddlewareCancellationDoesNotRunQueuedActions(t *testing.T) {
	application.New(application.Options{Name: "startup-cancel-test"})
	for _, kind := range []string{"call", "window", "request", "shutdown"} {
		t.Run(kind, func(t *testing.T) {
			rt := newRuntime()
			t.Cleanup(rt.startup.stop)
			if err := rt.configureStartupBindings(); err != nil {
				t.Fatal(err)
			}
			var calls atomic.Int32
			handler := rt.startup.middleware(
				http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }),
			)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			request := httptest.NewRequest(http.MethodPost, "/wails/runtime", strings.NewReader(
				`{"object":0,"method":0,"args":{"methodName":"nahida.live/desktop/internal/mod.Mod.Toggle","call-id":"cancel-me","args":["mod"]}}`,
			)).WithContext(ctx)
			request.Header.Set("x-wails-window-id", "7")
			response := httptest.NewRecorder()
			done := make(chan struct{})
			go func() { handler.ServeHTTP(response, request); close(done) }()
			waitStartupPending(t, rt.startup, "cancel-me")
			switch kind {
			case "call":
				handler.ServeHTTP(
					httptest.NewRecorder(),
					httptest.NewRequest(http.MethodPost, "/wails/runtime", strings.NewReader(
						`{"object":10,"method":0,"args":{"call-id":"cancel-me"}}`,
					)),
				)
			case "window":
				rt.startup.cancelWindow(7, "main")
			case "request":
				cancel()
			case "shutdown":
				rt.startup.stop()
			}
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Fatal("cancelled request remained queued")
			}
			rt.startup.start(func(context.Context) {})
			if calls.Load() != 0 || response.Code != http.StatusServiceUnavailable {
				t.Fatal("cancelled file operation reached the service")
			}
		})
	}
}

func waitStartupPending(t *testing.T, s *startupWork, id string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		pending := s.pending[id] != nil
		s.mu.Unlock()
		if pending {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("request did not enter startup gate")
}

func TestStartupMiddlewareDoesNotForwardUninspectableSubmissions(t *testing.T) {
	t.Parallel()
	s := newStartupWork()
	t.Cleanup(s.stop)
	var calls int
	handler := s.middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls++ }))
	chunk := httptest.NewRequest(http.MethodPost, "/wails/runtime", strings.NewReader("partial JSON"))
	chunk.Header.Set("x-wails-chunk-id", "large-call")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, chunk)
	if calls != 0 || response.Code != http.StatusServiceUnavailable {
		t.Fatal("chunked call bypassed maintenance")
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/wails/runtime?object=0&method=0", nil))
	if calls != 0 || response.Code != http.StatusServiceUnavailable {
		t.Fatal("query-string call bypassed maintenance")
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/wails/runtime", strings.NewReader("{not json")))
	if calls != 0 || response.Code != http.StatusServiceUnavailable {
		t.Fatal("malformed envelope bypassed maintenance")
	}
	s.start(func(context.Context) {})
	if err := s.wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	handler.ServeHTTP(httptest.NewRecorder(), chunk)
	if calls != 1 {
		t.Fatal("chunked transport remained blocked after startup")
	}
}

func TestFileMutatingServicesAreBound(t *testing.T) {
	t.Parallel()
	rt := newRuntime()
	bound := map[reflect.Type]bool{}
	for _, service := range rt.services() {
		bound[reflect.TypeOf(service.Instance())] = true
	}
	if len(rt.fileMutatingServices()) == 0 {
		t.Fatal("file-mutating services are empty")
	}
	for _, service := range rt.fileMutatingServices() {
		if !bound[reflect.TypeOf(service.Instance())] {
			t.Errorf("file-mutating %T is not registered in services()", service.Instance())
		}
	}
}
