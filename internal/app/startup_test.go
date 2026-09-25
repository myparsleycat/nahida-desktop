package app

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

func TestMaintenanceGuardWaitsForWork(t *testing.T) {
	t.Parallel()
	startup := newStartupWork()
	t.Cleanup(startup.stop)
	started := make(chan struct{})
	release := make(chan struct{})
	t.Cleanup(func() { closeFinish(release) })
	startup.start(func(context.Context) {
		close(started)
		<-release
	})
	<-started

	guard := maintenanceGuard(startup, waitForAllMethods)
	done := make(chan error, 1)
	go func() { done <- guard(context.Background(), "Toggle") }()
	select {
	case err := <-done:
		t.Fatalf("guard released before maintenance: %v", err)
	default:
	}
	closeFinish(release)
	waitCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-waitCtx.Done():
		t.Fatal("guard did not release after maintenance")
	}
}

func TestMaintenanceGuardPolicies(t *testing.T) {
	t.Parallel()
	startup := newStartupWork()
	t.Cleanup(startup.stop)
	for _, tc := range []struct {
		name      string
		predicate func(string) bool
		method    string
		wait      bool
	}{
		{"mod", waitForAllMethods, "Toggle", true},
		{"setting-write", isSettingWrite, "SetPersistToggles", true},
		{"setting-clear", isSettingWrite, "ClearImageCache", true},
		{"setting-read", isSettingWrite, "Get", false},
		{"shell-trash", waitForShellTrash, "Trash", true},
		{"shell-external", waitForShellTrash, "OpenExternal", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			err := maintenanceGuard(startup, tc.predicate)(ctx, tc.method)
			if tc.wait && !errors.Is(err, context.Canceled) {
				t.Fatalf("guard error = %v, want cancellation", err)
			}
			if !tc.wait && err != nil {
				t.Fatalf("unguarded call blocked: %v", err)
			}
		})
	}
}

func TestMaintenanceGuardCancelledCallDoesNotWait(t *testing.T) {
	t.Parallel()
	startup := newStartupWork()
	t.Cleanup(startup.stop)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- maintenanceGuard(startup, waitForAllMethods)(ctx, "Toggle") }()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("guard error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled call remained blocked")
	}
}

func TestMaintenanceFailureReleasesGuard(t *testing.T) {
	t.Parallel()
	rt := newRuntime()
	t.Cleanup(func() { _ = rt.Close() })
	var output bytes.Buffer
	rt.log.Configure(infra.LogOptions{Writer: &output, DisableFile: true})
	rt.startup.start(func(ctx context.Context) {
		rt.runStartupSteps(ctx, []startupStep{{name: "failed-step", run: func(context.Context) error {
			return errors.New("test maintenance failure")
		}}})
	})
	if err := maintenanceGuard(rt.startup, waitForAllMethods)(context.Background(), "Toggle"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "failed-step") ||
		!strings.Contains(output.String(), "test maintenance failure") {
		t.Fatalf("missing maintenance diagnostic: %s", output.String())
	}
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
