package tools

import (
	"context"

	togglepersist "nahida.live/desktop/internal/tools/toggle_persist"
)

type (
	PersistModelViewerResult = togglepersist.PersistModelViewerResult
	GameApplyResult          = togglepersist.GameApplyResult
)

func (t *Tools) PersistModelViewerToggleState(iniPath string, state map[string]any) (PersistModelViewerResult, error) {
	return t.persist.PersistModelViewerToggleState(iniPath, state)
}

// ApplyModelViewerToggleStateToGame sets the variables of one model viewer INI in the running games that load
// it.
func (t *Tools) ApplyModelViewerToggleStateToGame(
	ctx context.Context,
	iniPath string,
	state map[string]any,
	persistentOnly bool,
) (GameApplyResult, error) {
	return t.persist.ApplyStateToGame(ctx, iniPath, state, persistentOnly)
}

// ModelViewerGameLinked reports whether a running game that loads the mod at path accepts toggle changes.
func (t *Tools) ModelViewerGameLinked(path string) bool {
	return t.persist.GameLinked(path)
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
