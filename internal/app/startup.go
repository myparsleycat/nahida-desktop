package app

import (
	"context"
	"errors"
	"sync"
	"time"

	"nahida.live/desktop/internal/infra"
)

// startupWork owns deferred maintenance and its readiness signal.
type startupWork struct {
	ctx          context.Context
	cancel       context.CancelFunc
	once         sync.Once
	done         chan struct{}
	launched     time.Time
	prefetchDone <-chan struct{}
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
