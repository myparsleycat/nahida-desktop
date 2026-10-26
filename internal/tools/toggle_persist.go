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
	return t.persist.GetPersistLogs()
}

func (t *Tools) StartPersistWatcher(ctx context.Context) error {
	return t.persist.StartPersistWatcher(ctx)
}

func (t *Tools) StopPersistWatcher() bool {
	return t.persist.StopPersistWatcher()
}

func (t *Tools) shutdownPersistWatcher() error {
	return t.persist.Shutdown()
}
