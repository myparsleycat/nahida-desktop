package tools

import (
	"context"

	togglepersist "nahida.live/desktop/internal/tools/toggle_persist"
)

type (
	PersistModelViewerResult = togglepersist.PersistModelViewerResult
	GameApplyResult          = togglepersist.GameApplyResult
	GamePreview              = togglepersist.GamePreview
	GamePreviewINI           = togglepersist.GamePreviewINI
	CapturedToggleState      = togglepersist.CapturedState
	ToggleStateApplyResult   = togglepersist.StateApplyResult
	ToggleINIValues          = togglepersist.INIValues
)

// CaptureModToggleState reads the persist variables of a mod from a linked running game, or else from its INIs
// and d3dx_user.ini.
func (t *Tools) CaptureModToggleState(ctx context.Context, modPath string) (CapturedToggleState, error) {
	return t.persist.CaptureModState(ctx, modPath)
}

// PersistModToggleValues keeps the persist variables of the model viewer state of each INI of a mod.
func (t *Tools) PersistModToggleValues(modPath string, inis map[string]map[string]any) (ToggleINIValues, error) {
	return t.persist.PersistValuesFromState(modPath, inis)
}

// ApplyModToggleState writes values to the mod INIs and d3dx_user.ini and sets them in the linked running games.
func (t *Tools) ApplyModToggleState(
	ctx context.Context,
	modPath string,
	values ToggleINIValues,
) (ToggleStateApplyResult, error) {
	return t.persist.ApplyModState(ctx, modPath, values)
}

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

// NewModelViewerGamePreview starts showing a model viewer's previewed toggle values in the running games.
func (t *Tools) NewModelViewerGamePreview() *GamePreview {
	return t.persist.NewGamePreview()
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
