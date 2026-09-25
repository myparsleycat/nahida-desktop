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
	t.Cleanup(func() { closeFinish(finish) })
	s.start(func(ctx context.Context) {
		close(started)
		<-ctx.Done()
		<-finish
	})
	waitClosed(t, started, finish, "startup work did not start")
	stopped := make(chan struct{})
	go func() { s.stop(); close(stopped) }()
	waitClosed(t, s.ctx.Done(), finish, "shutdown did not cancel maintenance")
	select {
	case <-stopped:
		t.Fatal("shutdown returned while maintenance still owned dependencies")
	default:
	}
	closeFinish(finish)
	waitClosed(t, stopped, nil, "shutdown did not finish after maintenance released dependencies")
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
	t.Cleanup(func() { closeFinish(finish) })
	s.start(func(ctx context.Context) {
		s.prefetchDone = prefetchDone
		go func() {
			defer close(prefetchDone)
			<-ctx.Done()
			<-finish
		}()
	})
	waitStartupReady(t, s, finish)
	stopped := make(chan struct{})
	go func() { s.stop(); close(stopped) }()
	waitClosed(t, s.ctx.Done(), finish, "shutdown did not cancel prefetch")
	select {
	case <-stopped:
		t.Fatal("shutdown did not join release prefetch")
	default:
	}
	closeFinish(finish)
	waitClosed(t, stopped, nil, "shutdown did not finish after prefetch released dependencies")
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
	rt.http.UseTransport(startupTestTransport(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("network disabled during startup test")
	}))
	t.Cleanup(func() { _ = rt.Close() })
	if err := rt.Init(context.Background(), path, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(disabled); err != nil {
		t.Fatal("Init ran recovery before the application could create a window")
	}
	if _, err := os.Stat(original); !os.IsNotExist(err) {
		t.Fatalf("original exists before startup recovery: %v", err)
	}
	rt.startup.start(rt.runStartupWork)
	if err := rt.startup.wait(t.Context()); err != nil {
		t.Fatal(err)
	}
	if content, err := os.ReadFile(original); err != nil || string(content) != "restore" {
		t.Fatalf("deferred recovery = %q, %v", content, err)
	}
	if _, err := os.Stat(disabled); !os.IsNotExist(err) {
		t.Fatalf("disabled file remains after recovery: %v", err)
	}
}

type startupTestTransport func(*http.Request) (*http.Response, error)

func (f startupTestTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestStartupMiddlewareProtectsFileServices(t *testing.T) {
	// Wails' binding registry uses the application singleton; do not parallelise.
	application.New(application.Options{Name: "startup-binding-test"})
	rt := newRuntime()
	t.Cleanup(rt.startup.stop)
	if err := rt.configureStartupBindings(); err != nil {
		t.Fatal(err)
	}
	for _, registration := range rt.serviceRegistrations() {
		if registration.waitForMaintenance == nil {
			continue
		}
		instanceType := reflect.TypeOf(registration.service.Instance())
		for index := range instanceType.NumMethod() {
			methodName := instanceType.Method(index).Name
			options := application.CallOptions{MethodName: instanceType.Elem().PkgPath() + "." +
				instanceType.Elem().Name() + "." + methodName}
			binding := rt.startup.bindings.Get(&options)
			if binding == nil {
				continue
			}
			want := registration.waitForMaintenance(methodName)
			if got := rt.startup.needsWait(options); got != want {
				t.Errorf("%s gate = %v, want %v", options.MethodName, got, want)
			}
			if got := rt.startup.needsWait(application.CallOptions{MethodID: binding.ID}); got != want {
				t.Errorf("numeric ID for %s gate = %v, want %v", options.MethodName, got, want)
			}
		}
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
	waitStartupPending(t, rt.startup, "", "startup-toggle")
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
			waitStartupPending(t, rt.startup, "7", "cancel-me")
			switch kind {
			case "call":
				cancel := httptest.NewRequest(http.MethodPost, "/wails/runtime", strings.NewReader(
					`{"object":10,"method":0,"args":{"call-id":"cancel-me"}}`,
				))
				cancel.Header.Set("x-wails-window-id", "7")
				handler.ServeHTTP(httptest.NewRecorder(), cancel)
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

func windowRequest(windowID string) *http.Request {
	request := httptest.NewRequest(http.MethodPost, "/wails/runtime", nil)
	if windowID != "" {
		request.Header.Set("x-wails-window-id", windowID)
	}
	return request
}

func waitStartupPending(t *testing.T, s *startupWork, window, id string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	key := requestCallKey(windowRequest(window), id)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		pending := s.pending[key] != nil
		s.mu.Unlock()
		if pending {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("request did not enter startup gate")
}

func waitClosed(t *testing.T, ch <-chan struct{}, finish chan struct{}, msg string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		closeFinish(finish)
		t.Fatal(msg)
	}
}

func waitStartupReady(t *testing.T, s *startupWork, finish chan struct{}) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := s.wait(ctx); err != nil {
		closeFinish(finish)
		t.Fatal(err)
	}
}

func closeFinish(ch chan struct{}) {
	if ch == nil {
		return
	}
	select {
	case <-ch:
	default:
		close(ch)
	}
}

func TestStartupMiddlewareRejectsWhenBodyBudgetExhausted(t *testing.T) {
	t.Parallel()
	s := newStartupWork()
	t.Cleanup(s.stop)
	var calls int
	handler := s.middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls++ }))
	s.mu.Lock()
	s.heldBytes = maxRuntimeBodyBytes
	s.mu.Unlock()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/wails/runtime", strings.NewReader(
		`{"object":6,"method":0,"args":{}}`,
	)))
	if calls != 0 || response.Code != http.StatusServiceUnavailable {
		t.Fatal("request ran after startup body budget was exhausted")
	}
	s.mu.Lock()
	s.heldBytes = 0
	s.mu.Unlock()
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/wails/runtime", strings.NewReader(
		`{"object":6,"method":0,"args":{}}`,
	)))
	if calls != 1 {
		t.Fatal("request stayed blocked after startup body budget was released")
	}
}

type startupSpaceReader struct{ remaining int64 }

func (r *startupSpaceReader) Read(p []byte) (int, error) {
	if r.remaining == 0 {
		return 0, io.EOF
	}
	n := int(min(int64(len(p)), r.remaining))
	for i := range p[:n] {
		p[i] = ' '
	}
	r.remaining -= int64(n)
	return n, nil
}

func TestStartupMiddlewareBodySizeBoundaries(t *testing.T) {
	t.Parallel()
	s := newStartupWork()
	t.Cleanup(s.stop)
	var calls int
	var forwarded int64
	handler := s.middleware(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		calls++
		var err error
		forwarded, err = io.Copy(io.Discard, request.Body)
		if err != nil {
			t.Error(err)
		}
	}))
	const envelope = `{"object":6,"method":0,"args":{}}`
	request := func(size int64, unknownLength bool) *http.Request {
		body := io.MultiReader(strings.NewReader(envelope), &startupSpaceReader{remaining: size - int64(len(envelope))})
		r := httptest.NewRequest(http.MethodPost, "/wails/runtime", body)
		r.ContentLength = size
		if unknownLength {
			r.ContentLength = -1
		}
		return r
	}

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request(int64(len(envelope)), true))
	if response.Code != http.StatusOK || calls != 1 || forwarded != int64(len(envelope)) {
		t.Fatalf("unknown length = status %d, calls %d, forwarded %d", response.Code, calls, forwarded)
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request(maxRuntimeBodyBytes, false))
	if response.Code != http.StatusOK || calls != 2 || forwarded != maxRuntimeBodyBytes {
		t.Fatalf("maximum length = status %d, calls %d, forwarded %d", response.Code, calls, forwarded)
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request(maxRuntimeBodyBytes+1, false))
	if response.Code != http.StatusRequestEntityTooLarge || calls != 2 {
		t.Fatalf("oversized body = status %d, calls %d", response.Code, calls)
	}
}

func TestStartupMiddlewareRejectsDuplicateCallIDInOneWindow(t *testing.T) {
	application.New(application.Options{Name: "startup-duplicate-call-test"})
	rt := newRuntime()
	t.Cleanup(rt.startup.stop)
	if err := rt.configureStartupBindings(); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	handler := rt.startup.middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	request := func() *http.Request {
		r := httptest.NewRequest(http.MethodPost, "/wails/runtime", strings.NewReader(
			`{"object":0,"method":0,"args":{"methodName":"nahida.live/desktop/internal/mod.Mod.Toggle","call-id":"duplicate","args":["mod"]}}`,
		))
		r.Header.Set("x-wails-window-id", "7")
		return r
	}
	first := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { handler.ServeHTTP(first, request()); close(done) }()
	waitStartupPending(t, rt.startup, "7", "duplicate")
	second := httptest.NewRecorder()
	secondDone := make(chan struct{})
	go func() { handler.ServeHTTP(second, request()); close(secondDone) }()
	select {
	case <-secondDone:
	case <-time.After(2 * time.Second):
		rt.startup.start(func(context.Context) {})
		waitClosed(t, secondDone, nil, "second request remained blocked after maintenance")
		waitClosed(t, done, nil, "first request remained blocked after maintenance")
		t.Fatal("duplicate request blocked during maintenance")
	}
	if second.Code != http.StatusUnprocessableEntity || calls.Load() != 0 {
		t.Fatalf("duplicate = status %d, calls %d", second.Code, calls.Load())
	}
	rt.startup.start(func(context.Context) {})
	waitClosed(t, done, nil, "first request did not leave startup gate")
	if first.Code != http.StatusOK || calls.Load() != 1 {
		t.Fatalf("original = status %d, calls %d", first.Code, calls.Load())
	}
}

func TestStartupMiddlewareScopesCallIDsByWindow(t *testing.T) {
	application.New(application.Options{Name: "startup-call-id-scope-test"})
	rt := newRuntime()
	t.Cleanup(rt.startup.stop)
	if err := rt.configureStartupBindings(); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	handler := rt.startup.middleware(
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }),
	)
	startGated := func(windowID, callID string) (<-chan struct{}, *httptest.ResponseRecorder) {
		request := httptest.NewRequest(http.MethodPost, "/wails/runtime", strings.NewReader(
			`{"object":0,"method":0,"args":{"methodName":"nahida.live/desktop/internal/mod.Mod.Toggle","call-id":"`+
				callID+`","args":["mod"]}}`,
		))
		request.Header.Set("x-wails-window-id", windowID)
		response := httptest.NewRecorder()
		done := make(chan struct{})
		go func() { handler.ServeHTTP(response, request); close(done) }()
		waitStartupPending(t, rt.startup, windowID, callID)
		return done, response
	}
	doneA, responseA := startGated("1", "shared")
	doneB, _ := startGated("2", "shared")

	cancel := httptest.NewRequest(http.MethodPost, "/wails/runtime", strings.NewReader(
		`{"object":10,"method":0,"args":{"call-id":"shared"}}`,
	))
	cancel.Header.Set("x-wails-window-id", "1")
	handler.ServeHTTP(httptest.NewRecorder(), cancel)
	select {
	case <-doneA:
	case <-time.After(2 * time.Second):
		t.Fatal("same-window cancel did not release the queued call")
	}
	if responseA.Code != http.StatusServiceUnavailable {
		t.Fatal("cancelled call reached the service")
	}
	rt.startup.mu.Lock()
	otherPending := rt.startup.pending[requestCallKey(windowRequest("2"), "shared")] != nil
	rt.startup.mu.Unlock()
	if !otherPending {
		t.Fatal("cancel from another window released the queued call")
	}

	rt.startup.start(func(context.Context) {})
	select {
	case <-doneB:
	case <-time.After(2 * time.Second):
		t.Fatal("other window's call was not released after maintenance")
	}
	if calls.Load() != 1 {
		t.Fatal("expected only the uncancelled window's call to run")
	}
}

func TestStartupMiddlewareWindowIDAndNameDoNotShareCallKeys(t *testing.T) {
	application.New(application.Options{Name: "startup-call-id-namespace-test"})
	rt := newRuntime()
	t.Cleanup(rt.startup.stop)
	if err := rt.configureStartupBindings(); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	handler := rt.startup.middleware(
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }),
	)
	startGated := func(setHeader func(*http.Request), window, callID string) <-chan struct{} {
		request := httptest.NewRequest(http.MethodPost, "/wails/runtime", strings.NewReader(
			`{"object":0,"method":0,"args":{"methodName":"nahida.live/desktop/internal/mod.Mod.Toggle","call-id":"`+
				callID+`","args":["mod"]}}`,
		))
		setHeader(request)
		done := make(chan struct{})
		go func() { handler.ServeHTTP(httptest.NewRecorder(), request); close(done) }()
		waitStartupPending(t, rt.startup, window, callID)
		return done
	}
	doneID := startGated(func(r *http.Request) { r.Header.Set("x-wails-window-id", "1") }, "1", "shared")
	named := httptest.NewRequest(http.MethodPost, "/wails/runtime", strings.NewReader(
		`{"object":0,"method":0,"args":{"methodName":"nahida.live/desktop/internal/mod.Mod.Toggle","call-id":"shared","args":["mod"]}}`,
	))
	named.Header.Set("x-wails-window-name", "1")
	doneName := make(chan struct{})
	go func() { handler.ServeHTTP(httptest.NewRecorder(), named); close(doneName) }()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		rt.startup.mu.Lock()
		pending := rt.startup.pending[requestCallKey(named, "shared")] != nil
		rt.startup.mu.Unlock()
		if pending {
			break
		}
		time.Sleep(time.Millisecond)
	}
	rt.startup.mu.Lock()
	idPending := rt.startup.pending[requestCallKey(windowRequest("1"), "shared")] != nil
	namePending := rt.startup.pending[requestCallKey(named, "shared")] != nil
	rt.startup.mu.Unlock()
	if !idPending || !namePending {
		t.Fatal("window ID 1 and window name 1 must not share a call key")
	}

	cancel := httptest.NewRequest(http.MethodPost, "/wails/runtime", strings.NewReader(
		`{"object":10,"method":0,"args":{"call-id":"shared"}}`,
	))
	cancel.Header.Set("x-wails-window-id", "1")
	handler.ServeHTTP(httptest.NewRecorder(), cancel)
	select {
	case <-doneID:
	case <-time.After(2 * time.Second):
		t.Fatal("ID-scoped cancel did not release the ID-keyed call")
	}
	rt.startup.mu.Lock()
	namePending = rt.startup.pending[requestCallKey(named, "shared")] != nil
	rt.startup.mu.Unlock()
	if !namePending {
		t.Fatal("cancelling window ID 1 cancelled a call keyed by window name 1")
	}

	rt.startup.start(func(context.Context) {})
	select {
	case <-doneName:
	case <-time.After(2 * time.Second):
		t.Fatal("name-keyed call was not released after maintenance")
	}
	if calls.Load() != 1 {
		t.Fatal("expected only the name-keyed call to run")
	}
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

func TestServiceRegistrationsMatchBoundServices(t *testing.T) {
	t.Parallel()
	rt := newRuntime()
	registrations, services := rt.serviceRegistrations(), rt.services()
	if len(registrations) != len(services) {
		t.Fatalf("registrations = %d, bound services = %d", len(registrations), len(services))
	}
	seen := map[reflect.Type]bool{}
	maintenanceGates := 0
	for index, registration := range registrations {
		instance := registration.service.Instance()
		instanceType := reflect.TypeOf(instance)
		if seen[instanceType] {
			t.Errorf("duplicate service type %s", instanceType)
		}
		seen[instanceType] = true
		if services[index].Instance() != instance {
			t.Errorf("registration %d has a different bound instance", index)
		}
		if registration.waitForMaintenance != nil {
			maintenanceGates++
		}
	}
	if maintenanceGates != 10 {
		t.Errorf("maintenance gates = %d, want 10", maintenanceGates)
	}
}
