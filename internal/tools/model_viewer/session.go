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
}

// CleanupModelViewerWindow also fences loads completing after the native window closes.
//
//wails:ignore
func (t *Service) CleanupModelViewerWindow(windowID uint) {
	t.modelViewerMu.Lock()
	t.modelViewerClosedWindows[windowID] = true
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
	if exists && t.protocol != nil {
		t.protocol.CleanupMemorySession(memorySessionID)
	}
	if lastSession {
		startedAt := time.Now()
		modelViewerFreeOSMemory()
		if t.log != nil {
			t.log.Info(
				fmt.Sprintf(
					"Released model viewer memory in %dms (session=%s)",
					time.Since(startedAt).Milliseconds(),
					memorySessionID,
				),
				"StaticGlb.cleanupViewer",
			)
		}
	}
	return exists, nil
}

func (t *Service) shutdownModelViewer() error {
	if t == nil {
		return nil
	}
	t.modelViewerMu.Lock()
	ids := make([]string, 0, len(t.modelViewerSessions))
	for id := range t.modelViewerSessions {
		if session := t.modelViewerSessions[id]; session.cancel != nil {
			session.cancel()
		}
		ids = append(ids, id)
	}
	clear(t.modelViewerSessions)
	t.modelViewerMu.Unlock()
	if t.protocol != nil {
		for _, id := range ids {
			t.protocol.CleanupMemorySession(id)
		}
	}
	return nil
}
