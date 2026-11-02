package modelviewer

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type modelViewerSession struct {
	modPath   string
	windowID  uint
	evaluator *modelViewerEvaluator
	cancel    context.CancelFunc
	// drawSources maps transport mesh IDs to their INI draw call.
	drawSources map[string]DrawSource
}

type modelViewerLoad struct {
	cancel context.CancelFunc
}

// beginModelViewerLoad registers a load so closing its window cancels it. It
// fails with context.Canceled when the window has already closed.
func (t *Service) beginModelViewerLoad(
	ctx context.Context,
	windowID uint,
) (context.Context, func(), error) {
	ctx, cancel := context.WithCancel(ctx)
	load := &modelViewerLoad{cancel: cancel}
	t.modelViewerMu.Lock()
	defer t.modelViewerMu.Unlock()
	if t.modelViewerClosedWindows[windowID] {
		cancel()
		return nil, nil, context.Canceled
	}
	t.cancelModelViewerMemoryReleaseLocked()
	if t.modelViewerLoads[windowID] == nil {
		t.modelViewerLoads[windowID] = make(map[*modelViewerLoad]struct{})
	}
	t.modelViewerLoads[windowID][load] = struct{}{}
	return ctx, func() {
		t.modelViewerMu.Lock()
		if loads := t.modelViewerLoads[windowID]; loads != nil {
			delete(loads, load)
			if len(loads) == 0 {
				delete(t.modelViewerLoads, windowID)
			}
		}
		t.scheduleModelViewerMemoryReleaseLocked(fmt.Sprintf("load ended without a session, window=%d", windowID))
		t.modelViewerMu.Unlock()
		cancel()
	}, nil
}

// cancelModelViewerLoadsLocked must be called with modelViewerMu held.
func (t *Service) cancelModelViewerLoadsLocked(windowID uint) {
	for load := range t.modelViewerLoads[windowID] {
		load.cancel()
	}
	delete(t.modelViewerLoads, windowID)
}

// CleanupModelViewerWindow cancels the window's loads in flight and fences
// loads completing after the native window closes.
//
//wails:ignore
func (t *Service) CleanupModelViewerWindow(windowID uint) {
	t.modelViewerMu.Lock()
	t.modelViewerClosedWindows[windowID] = true
	t.cancelModelViewerLoadsLocked(windowID)
	var ids []string
	for id, session := range t.modelViewerSessions {
		if session.windowID == windowID {
			ids = append(ids, id)
		}
	}
	t.modelViewerMu.Unlock()
	for _, id := range ids {
		_, _ = t.CleanupModelViewer(context.Background(), id)
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
	for windowID := range t.modelViewerLoads {
		t.cancelModelViewerLoadsLocked(windowID)
	}
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
