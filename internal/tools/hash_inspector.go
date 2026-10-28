package tools

import (
	"context"

	hashinspector "nahida.live/desktop/internal/tools/hash_inspector"
)

type (
	HashInspectorState   = hashinspector.State
	HashInspectorCapture = hashinspector.Capture
)

func (t *Tools) HashInspectorGetState(ctx context.Context, importer string) (HashInspectorState, error) {
	return t.hashInspector.State(ctx, importer)
}

func (t *Tools) HashInspectorPick(ctx context.Context, importer string) error {
	return t.hashInspector.Pick(ctx, importer)
}

func (t *Tools) HashInspectorGetCapture(ctx context.Context, importer, id string) (HashInspectorCapture, error) {
	return t.hashInspector.Capture(ctx, importer, id)
}

func (t *Tools) HashInspectorDeleteCaptures(ctx context.Context, importer string) error {
	return t.hashInspector.DeleteCaptures(ctx, importer)
}

// StartHashInspector subscribes the inspector to the desktop channel, which
// delivers picks started by the in-game key.
//
//wails:ignore
func (t *Tools) StartHashInspector(context.Context) error {
	t.hashInspector.Start()
	return nil
}

func (t *Tools) shutdownHashInspector() error {
	return t.hashInspector.Shutdown()
}
