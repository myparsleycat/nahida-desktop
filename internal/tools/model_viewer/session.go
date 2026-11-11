package modelviewer

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type modelViewerSession struct {
	modPath   string
	evaluator *modelViewerEvaluator
	cancel    context.CancelFunc
	// drawSources maps transport mesh IDs to their INI draw call.
	drawSources map[string]DrawSource
}

type modelViewerLoad struct {
	cancel context.CancelFunc
}

// beginModelViewerLoad registers a load so shutdown cancels it and the deferred memory release waits for it.
func (t *Service) beginModelViewerLoad(ctx context.Context) (context.Context, func()) {
	ctx, cancel := context.WithCancel(ctx)
	load := &modelViewerLoad{cancel: cancel}
	t.modelViewerMu.Lock()
	defer t.modelViewerMu.Unlock()
	t.cancelModelViewerMemoryReleaseLocked()
	t.modelViewerLoads[load] = struct{}{}
	return ctx, func() {
		t.modelViewerMu.Lock()
		delete(t.modelViewerLoads, load)
		t.scheduleModelViewerMemoryReleaseLocked("load ended without a session")
		t.modelViewerMu.Unlock()
		cancel()
	}
}

// modelViewerMemoryReleaseDelay lets the next load of a preview batch start
// before memory is returned to the OS, so a busy viewer is not collected per load.
const modelViewerMemoryReleaseDelay = 3 * time.Second

// scheduleModelViewerMemoryReleaseLocked must be called with modelViewerMu held.
func (t *Service) scheduleModelViewerMemoryReleaseLocked(trigger string) {
	if t.modelViewerShutdown || len(t.modelViewerSessions) > 0 || len(t.modelViewerLoads) > 0 {
		return
	}
	t.cancelModelViewerMemoryReleaseLocked()
	generation := t.modelViewerReleaseGeneration
	t.modelViewerStopRelease = t.afterFunc(modelViewerMemoryReleaseDelay, func() {
		t.releaseModelViewerMemory(generation, trigger)
	})
}

// cancelModelViewerMemoryReleaseLocked must be called with modelViewerMu held.
// The generation also voids a release whose timer already fired.
func (t *Service) cancelModelViewerMemoryReleaseLocked() {
	t.modelViewerReleaseGeneration++
	if t.modelViewerStopRelease != nil {
		t.modelViewerStopRelease()
		t.modelViewerStopRelease = nil
	}
}

func (t *Service) releaseModelViewerMemory(generation uint64, trigger string) {
	t.modelViewerMu.Lock()
	if generation != t.modelViewerReleaseGeneration ||
		len(t.modelViewerSessions) > 0 ||
		len(t.modelViewerLoads) > 0 {
		t.modelViewerMu.Unlock()
		return
	}
	t.modelViewerStopRelease = nil
	t.modelViewerMu.Unlock()
	startedAt := time.Now()
	t.freeOSMemory()
	t.log.Info(
		fmt.Sprintf(
			"Released model viewer memory in %dms (%s)",
			time.Since(startedAt).Milliseconds(),
			trigger,
		),
		"StaticGlb.cleanupViewer",
	)
}

func (t *Service) CleanupModelViewer(_ context.Context, memorySessionID string) (bool, error) {
	memorySessionID = strings.TrimSpace(memorySessionID)
	if memorySessionID == "" {
		return false, nil
	}
	t.modelViewerMu.Lock()
	session, exists := t.modelViewerSessions[memorySessionID]
	if exists {
		if session.cancel != nil {
			session.cancel()
		}
		delete(t.modelViewerSessions, memorySessionID)
	}
	if exists {
		t.scheduleModelViewerMemoryReleaseLocked("session=" + memorySessionID)
	}
	t.modelViewerMu.Unlock()
	if exists {
		t.protocol.CleanupMemorySession(memorySessionID)
	}
	return exists, nil
}

func (t *Service) shutdownModelViewer() error {
	t.modelViewerMu.Lock()
	t.modelViewerShutdown = true
	t.cancelModelViewerMemoryReleaseLocked()
	for load := range t.modelViewerLoads {
		load.cancel()
	}
	clear(t.modelViewerLoads)
	ids := make([]string, 0, len(t.modelViewerSessions))
	for id := range t.modelViewerSessions {
		if session := t.modelViewerSessions[id]; session.cancel != nil {
			session.cancel()
		}
		ids = append(ids, id)
	}
	clear(t.modelViewerSessions)
	t.modelViewerMu.Unlock()
	for _, id := range ids {
		t.protocol.CleanupMemorySession(id)
	}
	return nil
}
