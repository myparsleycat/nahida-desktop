package tools

import (
	"context"

	togglepersist "nahida.live/desktop/internal/tools/toggle_persist"
)

type PersistModelViewerResult = togglepersist.PersistModelViewerResult

func (t *Tools) PersistModelViewerToggleState(iniPath string, state map[string]any) (PersistModelViewerResult, error) {
	return t.persist.PersistModelViewerToggleState(iniPath, state)
}

func (t *Tools) GetPersistLogs() []string {
	if t == nil || t.persist == nil {
		return []string{}
	}
	return t.persist.GetPersistLogs()
}

func (t *Tools) StartPersistWatcher(ctx context.Context) error {
	if t == nil || t.persist == nil {
		return nil
	}
	return t.persist.StartPersistWatcher(ctx)
}

func (t *Tools) StopPersistWatcher() bool {
	if t != nil && t.persist != nil {
		return t.persist.StopPersistWatcher()
	}
	return true
}

func (t *Tools) shutdownPersistWatcher() error {
	if t == nil || t.persist == nil {
		return nil
	}
	return t.persist.Shutdown()
}
