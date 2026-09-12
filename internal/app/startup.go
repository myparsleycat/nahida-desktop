package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"nahida.live/desktop/internal/infra"
)

// Wails objectNames.Call / objectNames.CancelCall. CancelCall is unexported in
// the v3 beta.20 application package.
const (
	wailsCallObject       = 0
	wailsCancelCallObject = 10
	maxRuntimeBodyBytes   = 64 << 20
)

// startupWork owns deferred maintenance and the IPC requests that depend on it.
// The gate is at the application boundary so internal recovery calls do not
// deadlock on their own readiness.
type startupWork struct {
	ctx          context.Context
	cancel       context.CancelFunc
	once         sync.Once
	done         chan struct{}
	bindings     *application.Bindings
	gatedIDs     map[uint32]struct{}
	gatedNames   map[string]struct{}
	mu           sync.Mutex
	pending      map[startupCallKey]*startupCall
	heldBytes    int64
	launched     time.Time
	prefetchDone <-chan struct{}
}

type startupCallKey struct {
	window string
	callID string
}

type startupCall struct {
	cancel     context.CancelFunc
	windowID   string
	windowName string
}

type startupStep struct {
	name string
	run  func(context.Context) error
}

func newStartupWork() *startupWork {
	ctx, cancel := context.WithCancel(context.Background())
	return &startupWork{
		ctx:      ctx,
		cancel:   cancel,
		done:     make(chan struct{}),
		pending:  make(map[startupCallKey]*startupCall),
		launched: time.Now(),
	}
}

func (s *startupWork) start(work func(context.Context)) {
	s.once.Do(func() {
		go func() {
			defer close(s.done)
			if s.ctx.Err() == nil {
				work(s.ctx)
			}
		}()
	})
}

func (s *startupWork) stop() {
	if s == nil {
		return
	}
	s.cancel()

	// Also finish a runtime that failed before ApplicationStarted.
	s.once.Do(func() { close(s.done) })
	<-s.done
	if s.prefetchDone != nil {
		<-s.prefetchDone
	}
}

func (s *startupWork) wait(ctx context.Context) error {
	if s == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-s.ctx.Done():
		return s.ctx.Err()
	case <-s.done:
		return errors.Join(ctx.Err(), s.ctx.Err())
	}
}

func (s *startupWork) cancelWindow(id uint, name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, call := range s.pending {
		if call.windowID == strconv.FormatUint(uint64(id), 10) || call.windowID == "" && call.windowName == name {
			call.cancel()
		}
	}
}

func (rt *runtime) configureStartupBindings() error {
	s := rt.startup
	s.bindings = application.NewBindings(nil, nil)
	s.gatedIDs = make(map[uint32]struct{})
	s.gatedNames = make(map[string]struct{})
	for _, service := range rt.fileMutatingServices() {
		if err := s.bindings.Add(service); err != nil {
			return err
		}
		s.gateMethods(service.Instance(), nil)
	}
	if err := s.bindings.Add(application.NewService(rt.setting)); err != nil {
		return err
	}
	if err := s.bindings.Add(application.NewService(rt.shell)); err != nil {
		return err
	}
	s.gateMethods(rt.setting, isSettingWrite)
	s.gateMethods(rt.shell, func(name string) bool { return name == "Trash" })
	return nil
}

func (s *startupWork) gateMethods(instance any, keep func(string) bool) {
	ptr := reflect.TypeOf(instance)
	if ptr == nil || ptr.Kind() != reflect.Pointer {
		return
	}
	named := ptr.Elem()
	for i := range ptr.NumMethod() {
		name := ptr.Method(i).Name
		if keep != nil && !keep(name) {
			continue
		}
		method := s.bindings.Get(&application.CallOptions{
			MethodName: named.PkgPath() + "." + named.Name() + "." + name,
		})
		if method == nil {
			continue
		}
		s.gatedNames[method.FQN] = struct{}{}
		s.gatedIDs[method.ID] = struct{}{}
	}
}

func isSettingWrite(name string) bool {
	return strings.HasPrefix(name, "Set") || name == "ClearImageCache" || name == "AdvancedSet"
}

func (s *startupWork) needsWait(options application.CallOptions) bool {
	if s.gatedIDs == nil {
		return true
	}
	if options.MethodName != "" {
		_, ok := s.gatedNames[options.MethodName]
		return ok
	}
	_, ok := s.gatedIDs[options.MethodID]
	return ok
}

func requestWindowKey(r *http.Request) string {
	if id := r.Header.Get("x-wails-window-id"); id != "" {
		return "id:" + id
	}
	return "name:" + r.Header.Get("x-wails-window-name")
}

func requestCallKey(r *http.Request, callID string) startupCallKey {
	return startupCallKey{window: requestWindowKey(r), callID: callID}
}

func (s *startupWork) holdRuntimeBody(n int64) (int64, bool) {
	if n < 0 || n > maxRuntimeBodyBytes {
		n = maxRuntimeBodyBytes
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if n == 0 {
		return 0, true
	}
	if s.heldBytes+n > maxRuntimeBodyBytes {
		return 0, false
	}
	s.heldBytes += n
	return n, true
}

func (s *startupWork) releaseRuntimeBody(n int64) {
	s.replaceRuntimeBody(n, 0)
}

func (s *startupWork) replaceRuntimeBody(from, to int64) bool {
	if to < 0 {
		to = 0
	}
	if to > maxRuntimeBodyBytes {
		to = maxRuntimeBodyBytes
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.heldBytes - from + to
	if next > maxRuntimeBodyBytes {
		return false
	}
	if next < 0 {
		next = 0
	}
	s.heldBytes = next
	return true
}

func (s *startupWork) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/wails/runtime" {
			next.ServeHTTP(w, r)
			return
		}
		select {
		case <-s.done:
			if s.ctx.Err() == nil {
				next.ServeHTTP(w, r)
				return
			}
		default:
		}

		// Chunks are assembled below this middleware, and the Windows runtime
		// always sends a JSON envelope. Unclassifiable submissions fail closed.
		if r.Header.Get("x-wails-chunk-id") != "" {
			http.Error(
				w,
				"Application maintenance is still running; retry after startup",
				http.StatusServiceUnavailable,
			)
			return
		}

		held, ok := s.holdRuntimeBody(r.ContentLength)
		if !ok {
			http.Error(
				w,
				"Application maintenance is still running; retry after startup",
				http.StatusServiceUnavailable,
			)
			return
		}
		defer func() { s.releaseRuntimeBody(held) }()

		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxRuntimeBodyBytes))
		if err != nil {
			http.Error(w, "Unable to read runtime request", http.StatusRequestEntityTooLarge)
			return
		}
		if actual := int64(len(body)); actual != held {
			if !s.replaceRuntimeBody(held, actual) {
				http.Error(
					w,
					"Application maintenance is still running; retry after startup",
					http.StatusServiceUnavailable,
				)
				return
			}
			held = actual
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		var request struct {
			Object *int `json:"object"`
			Method *int `json:"method"`
			Args   struct {
				application.CallOptions
				CallID string `json:"call-id"`
			} `json:"args"`
		}
		if (len(body) == 0 && r.URL.RawQuery != "") ||
			json.Unmarshal(body, &request) != nil || request.Object == nil || request.Method == nil {
			http.Error(
				w,
				"Application maintenance is still running; retry after startup",
				http.StatusServiceUnavailable,
			)
			return
		}

		key := requestCallKey(r, request.Args.CallID)
		if *request.Object == wailsCancelCallObject {
			s.mu.Lock()
			call := s.pending[key]
			if call != nil {
				call.cancel()
			}
			s.mu.Unlock()
			if call != nil {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte("null"))
				return
			}
		}
		if *request.Object != wailsCallObject || *request.Method != application.CallBinding ||
			request.Args.CallID == "" || !s.needsWait(request.Args.CallOptions) {
			next.ServeHTTP(w, r)
			return
		}

		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()
		call := &startupCall{
			cancel:     cancel,
			windowID:   r.Header.Get("x-wails-window-id"),
			windowName: r.Header.Get("x-wails-window-name"),
		}
		s.mu.Lock()
		if s.pending[key] != nil {
			s.mu.Unlock()
			http.Error(w, "Ambiguous runtime call ID", http.StatusUnprocessableEntity)
			return
		}
		s.pending[key] = call
		s.mu.Unlock()
		err = s.wait(ctx)
		s.mu.Lock()
		delete(s.pending, key)
		s.mu.Unlock()
		if err != nil || ctx.Err() != nil {
			http.Error(w, "Startup request cancelled", http.StatusServiceUnavailable)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (rt *runtime) logStartupMilestone(stage string) {
	fields := map[string]any{"stage": stage, "sinceLaunchMs": time.Since(rt.startup.launched).Milliseconds()}
	if time.Since(rt.startup.launched) >= time.Second {
		rt.log.Warn(fields, "Startup")
	} else {
		rt.log.Info(fields, "Startup")
	}
}

func (rt *runtime) runStartupWork(ctx context.Context) {
	rt.runStartupStepsParallel(ctx, []startupStep{
		{"zzmi-staging", rt.tools.CleanupZZMIAbandonedStaging},
		{"model-viewer-temp", rt.tools.CleanupStaleModelViewerDirs},
		{"d3d-builds", rt.tools.CleanupStaleD3DBuilds},
	})
	rt.runStartupSteps(ctx, []startupStep{
		{"bisect-recovery", rt.tools.RecoverBisects},
		{"compression", rt.runCompressionMaintenance},
		{"persist-watcher", rt.tools.StartPersistWatcher},
	})
	if ctx.Err() != nil {
		return
	}

	// Network warmup does not hold the readiness gate, but still belongs to
	// startup shutdown so it cannot access the store after it is closed.
	done := make(chan struct{})
	rt.startup.prefetchDone = done
	go func() {
		defer close(done)
		prefetchCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		if err := rt.tools.FourThousandOneFixerUpdateReleases(prefetchCtx); err != nil && ctx.Err() == nil {
			_ = infra.ReportError(rt.log, err, "Startup", infra.Diagnostic{
				Severity: infra.DiagnosticWarn, Operation: "maintenance", Stage: "release-prefetch",
			})
		}
	}()
	rt.tools.StartWuwaAutoUpdateCheck()
}

// runCompressionMaintenance starts continuous reconciliation and waits for the
// initial pass, so the readiness gate cannot open while mod files are being
// rewritten.
func (rt *runtime) runCompressionMaintenance(ctx context.Context) error {
	if err := rt.mod.StartCompression(ctx); err != nil {
		return err
	}
	return rt.mod.WaitCompressionPass(ctx)
}

func (rt *runtime) runStartupSteps(ctx context.Context, steps []startupStep) {
	for _, step := range steps {
		rt.runStartupStep(ctx, step)
	}
}

func (rt *runtime) runStartupStepsParallel(ctx context.Context, steps []startupStep) {
	if ctx.Err() != nil {
		return
	}
	var wg sync.WaitGroup
	for _, step := range steps {
		wg.Add(1)
		go func(step startupStep) {
			defer wg.Done()
			rt.runStartupStep(ctx, step)
		}(step)
	}
	wg.Wait()
}

func (rt *runtime) runStartupStep(ctx context.Context, step startupStep) {
	if ctx.Err() != nil {
		return
	}
	started := time.Now()
	err := step.run(ctx)
	elapsed := time.Since(started).Milliseconds()
	if err != nil && ctx.Err() == nil {
		_ = infra.ReportError(rt.log, err, "Startup", infra.Diagnostic{
			Operation: "maintenance", Stage: step.name, Fields: map[string]any{"elapsedMs": elapsed},
		})
	}
	fields := map[string]any{"stage": step.name, "elapsedMs": elapsed}
	if elapsed >= 1000 {
		rt.log.Warn(fields, "Startup")
	} else {
		rt.log.Info(fields, "Startup")
	}
}
