package xxmi

import (
	"bytes"
	"context"
	"errors"
	"maps"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"nahida.live/desktop/internal/db"
	"nahida.live/desktop/internal/infra"
)

func TestRunningImportersUsesOneSnapshotAndPreservesLaunchingState(t *testing.T) {
	t.Parallel()
	service := New()
	calls := 0
	service.processSnapshot = func(context.Context) (map[string]bool, error) {
		calls++
		return map[string]bool{"yuanshen.exe": true, "client-win64-shipping.exe": true}, nil
	}
	service.setLaunching("SRMI", true)
	if !service.acquireImporter("ZZMI") {
		t.Fatal("could not acquire importer")
	}
	want := map[string]bool{"GIMI": true, "WWMI": true, "srmi": true, "ZZMI": false}
	running, err := service.runningImporters(t.Context(), []string{"GIMI", "WWMI", "srmi", "ZZMI"})
	if err != nil || !maps.Equal(running, want) || calls != 1 {
		t.Fatalf("running = %v, calls = %d, err = %v", running, calls, err)
	}
	service.setLaunching("SRMI", false)
	running, err = service.runningImporters(t.Context(), []string{"SRMI"})
	if err != nil || running["SRMI"] {
		t.Fatalf("finished launch = %v, err = %v", running, err)
	}
	calls = 0
	running, err = service.runningImporters(t.Context(), nil)
	if err != nil || len(running) != 0 || calls != 0 {
		t.Fatalf("empty importers = %v, calls = %d, err = %v", running, calls, err)
	}
}

func TestRunningImportersSkipsSnapshotWhileAllImportersAreLaunching(t *testing.T) {
	t.Parallel()
	service := New()
	service.processSnapshot = func(context.Context) (map[string]bool, error) {
		t.Error("launching importers should not require a process snapshot")
		return nil, errors.New("snapshot unavailable")
	}
	service.setLaunching("GIMI", true)
	running, err := service.runningImporters(t.Context(), []string{"GIMI"})
	if err != nil || !running["GIMI"] {
		t.Fatalf("launching importer = %v, err = %v", running, err)
	}
}

func TestBuiltinRunningStateSkipsExternalAndDisabledImporters(t *testing.T) {
	t.Parallel()
	service := newRunningStateTestService(t)
	calls := 0
	service.processSnapshot = func(context.Context) (map[string]bool, error) {
		calls++
		return map[string]bool{"genshinimpact.exe": true}, nil
	}
	for key, config := range map[string]string{
		"GIMI": `{"enabled":true,"importerFolder":"missing"}`,
		"SRMI": `{"enabled":false}`,
	} {
		if err := service.client.XXMIImporters.Upsert(t.Context(), key, config); err != nil {
			t.Fatal(err)
		}
	}
	running, err := service.builtinRunningState(t.Context())
	if err != nil || len(running) != 0 || calls != 0 {
		t.Fatalf("external state = %v, calls = %d, err = %v", running, calls, err)
	}
	useBuiltinLauncher(t, service)
	running, err = service.builtinRunningState(t.Context())
	if err != nil || !maps.Equal(running, map[string]bool{"GIMI": true}) || calls != 1 {
		t.Fatalf("builtin state = %v, calls = %d, err = %v", running, calls, err)
	}
	if err := service.client.XXMIImporters.Delete(t.Context(), "GIMI"); err != nil {
		t.Fatal(err)
	}
	running, err = service.builtinRunningState(t.Context())
	if err != nil || len(running) != 0 || calls != 1 {
		t.Fatalf("disabled state = %v, calls = %d, err = %v", running, calls, err)
	}
}

func TestWatchRunningEmitsOnlyChangesAndRetainsStateOnFailure(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		var output bytes.Buffer
		log := infra.NewLogWithOptions(infra.LogOptions{Writer: &output, DisableFile: true})
		t.Cleanup(func() { _ = log.Close() })
		var events atomic.Int32
		service := NewWithOptions(Options{Log: log, EventEmit: func(name string, _ ...any) {
			if name != "xxmi:running-changed" {
				t.Errorf("unexpected event %q", name)
			}
			events.Add(1)
		}})
		current := map[string]bool{"GIMI": false}
		var sampleErr error
		var sampleMu sync.Mutex
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		done := make(chan struct{})
		go func() {
			defer close(done)
			service.watchRunning(ctx, func(context.Context) (map[string]bool, error) {
				sampleMu.Lock()
				defer sampleMu.Unlock()
				return maps.Clone(current), sampleErr
			})
		}()
		synctest.Wait()
		if events.Load() != 1 {
			t.Fatalf("initial events = %d", events.Load())
		}
		time.Sleep(runningWatchInterval)
		synctest.Wait()
		if events.Load() != 1 {
			t.Fatalf("unchanged events = %d", events.Load())
		}
		sampleMu.Lock()
		current["GIMI"] = true
		sampleMu.Unlock()
		time.Sleep(runningWatchInterval)
		synctest.Wait()
		if events.Load() != 2 {
			t.Fatalf("start events = %d", events.Load())
		}

		sampleMu.Lock()
		sampleErr = errors.New("process enumeration failed")
		sampleMu.Unlock()
		time.Sleep(2 * runningWatchInterval)
		synctest.Wait()
		if events.Load() != 2 {
			t.Fatalf("failure events = %d", events.Load())
		}
		sampleMu.Lock()
		sampleErr = nil
		sampleMu.Unlock()
		time.Sleep(runningWatchInterval)
		synctest.Wait()
		if events.Load() != 2 {
			t.Fatalf("recovery changed the known state: %d events", events.Load())
		}
		sampleMu.Lock()
		current["GIMI"] = false
		sampleMu.Unlock()
		time.Sleep(runningWatchInterval)
		synctest.Wait()
		if events.Load() != 3 {
			t.Fatalf("exit events = %d", events.Load())
		}
		sampleMu.Lock()
		current = map[string]bool{}
		sampleMu.Unlock()
		time.Sleep(runningWatchInterval)
		synctest.Wait()
		if events.Load() != 4 {
			t.Fatalf("removed importer events = %d", events.Load())
		}
		cancel()
		<-done
		time.Sleep(2 * runningWatchInterval)
		if events.Load() != 4 {
			t.Fatalf("watcher emitted after cancellation: %d events", events.Load())
		}
		if strings.Count(output.String(), "process enumeration failed") != 1 {
			t.Fatalf("repeated error was not suppressed: %s", output.String())
		}
	})
}

func TestWatchRunningWakesForLaunchesWithoutReportingAnActiveGameAsStopped(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		var events atomic.Int32
		var gameRunning atomic.Bool
		var payload atomic.Value
		service := NewWithOptions(Options{EventEmit: func(_ string, data ...any) {
			payload.Store(data[0])
			events.Add(1)
		}})
		service.processSnapshot = func(context.Context) (map[string]bool, error) {
			return map[string]bool{"genshinimpact.exe": gameRunning.Load()}, nil
		}
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		go service.watchRunning(ctx, func(ctx context.Context) (map[string]bool, error) {
			return service.runningImporters(ctx, []string{"GIMI"})
		})
		synctest.Wait()

		// A maintenance lock on the importer is not a running game.
		if !service.acquireImporter("GIMI") {
			t.Fatal("could not acquire importer")
		}
		time.Sleep(runningWatchInterval)
		synctest.Wait()
		service.releaseImporter("GIMI")
		if events.Load() != 1 {
			t.Fatalf("importer lock events = %d", events.Load())
		}

		service.setLaunching("GIMI", true)
		synctest.Wait()
		if events.Load() != 2 || !maps.Equal(payload.Load().(map[string]bool), map[string]bool{"GIMI": true}) {
			t.Fatalf("launch events = %d, payload = %v", events.Load(), payload.Load())
		}
		gameRunning.Store(true)
		service.setLaunching("GIMI", false)
		synctest.Wait()
		if events.Load() != 2 {
			t.Fatalf("finished launch of a still-running game: %d events", events.Load())
		}
		gameRunning.Store(false)
		time.Sleep(runningWatchInterval)
		synctest.Wait()
		if events.Load() != 3 {
			t.Fatalf("exit events = %d", events.Load())
		}
	})
}

func TestWatchRunningIdlesWhileNothingIsWatched(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		var events, reads atomic.Int32
		var watching atomic.Bool
		service := NewWithOptions(Options{EventEmit: func(string, ...any) { events.Add(1) }})
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		go service.watchRunning(ctx, func(context.Context) (map[string]bool, error) {
			reads.Add(1)
			if !watching.Load() {
				return map[string]bool{}, nil
			}
			return map[string]bool{"GIMI": false}, nil
		})
		time.Sleep(2 * runningIdleInterval)
		synctest.Wait()
		if events.Load() != 0 || reads.Load() != 3 {
			t.Fatalf("idle watcher: %d events, %d reads", events.Load(), reads.Load())
		}

		watching.Store(true)
		service.wakeRunningWatch()
		synctest.Wait()
		if events.Load() != 1 || reads.Load() != 4 {
			t.Fatalf("woken watcher: %d events, %d reads", events.Load(), reads.Load())
		}
	})
}

func TestRunningWatchLifecycleIsIdempotentAndJoinsBeforeShutdown(t *testing.T) {
	t.Parallel()
	service := newRunningStateTestService(t)
	useBuiltinLauncher(t, service)
	if err := service.client.XXMIImporters.Upsert(t.Context(), "GIMI", `{"enabled":true}`); err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	service.processSnapshot = func(ctx context.Context) (map[string]bool, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	if err := service.ServiceStartup(ctx, application.ServiceOptions{}); err != nil {
		t.Fatal(err)
	}
	done := service.runningDone
	if err := service.ServiceStartup(ctx, application.ServiceOptions{}); err != nil || done != service.runningDone {
		t.Fatalf("duplicate startup: %v", err)
	}
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("watcher did not start")
	}
	for range 2 {
		if err := service.ServiceShutdown(); err != nil {
			t.Fatal(err)
		}
	}
	select {
	case <-done:
	default:
		t.Fatal("shutdown returned before watcher exited")
	}
}

func newRunningStateTestService(t *testing.T) *XXMI {
	t.Helper()
	client, err := db.New(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	if err := client.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	service := New()
	service.UseClient(client)
	t.Cleanup(func() { _ = service.ServiceShutdown() })
	return service
}
