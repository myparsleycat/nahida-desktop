package tools

import (
	"context"

	toggleviewer "nahida.live/desktop/internal/tools/toggle_viewer"
)

type (
	ToggleViewerState       = toggleviewer.State
	ToggleViewerBatchResult = toggleviewer.BatchResult
)

func (t *Tools) ToggleViewerGetLogs() []string {
	return t.toggleViewer.Logs()
}

func (t *Tools) ToggleViewerGetState() ToggleViewerState {
	return t.toggleViewer.State()
}

func (t *Tools) ToggleViewerCancelCurrentWork() bool {
	return t.toggleViewer.CancelCurrentWork()
}

func (t *Tools) ToggleViewerRunBatchGenerate(ctx context.Context) (ToggleViewerBatchResult, error) {
	return t.toggleViewer.RunBatchGenerate(ctx)
}

func (t *Tools) ToggleViewerRunBatchDelete(ctx context.Context) (ToggleViewerBatchResult, error) {
	return t.toggleViewer.RunBatchDelete(ctx)
}

//wails:ignore
func (t *Tools) ToggleViewerApplyHotkeyToArtifacts(ctx context.Context, hotkey string) error {
	return t.toggleViewer.ApplyHotkey(ctx, hotkey)
}

// StartToggleViewerWatcher starts or stops importer watching to match the stored auto-generate setting.
//
//wails:ignore
func (t *Tools) StartToggleViewerWatcher(ctx context.Context) error {
	if t == nil || t.toggleViewer == nil {
		return nil
	}
	return t.toggleViewer.StartWatcher(ctx)
}

func (t *Tools) shutdownToggleViewer() error {
	if t == nil || t.toggleViewer == nil {
		return nil
	}
	return t.toggleViewer.Shutdown()
}
