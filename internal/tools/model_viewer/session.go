package modelviewer

import (
	"context"
	"fmt"
	"runtime/debug"
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

var modelViewerFreeOSMemory = debug.FreeOSMemory

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
	lastSession := exists && len(t.modelViewerSessions) == 0
	t.modelViewerMu.Unlock()
	if exists {
		t.protocol.CleanupMemorySession(memorySessionID)
	}
	if lastSession {
		startedAt := time.Now()
		modelViewerFreeOSMemory()
		t.log.Info(
			fmt.Sprintf(
				"Released model viewer memory in %dms (session=%s)",
				time.Since(startedAt).Milliseconds(),
				memorySessionID,
			),
			"StaticGlb.cleanupViewer",
		)
	}
	return exists, nil
}

func (t *Service) shutdownModelViewer() error {
	t.modelViewerMu.Lock()
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
