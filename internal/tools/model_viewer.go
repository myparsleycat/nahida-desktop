package tools

import (
	"context"

	modelviewer "nahida.live/desktop/internal/tools/model_viewer"
)

type (
	ModelViewerDNFClause                = modelviewer.ModelViewerDNFClause
	ModelViewerDNF                      = modelviewer.ModelViewerDNF
	ModelViewerTextureVariant           = modelviewer.ModelViewerTextureVariant
	ModelViewerShapeTarget              = modelviewer.ModelViewerShapeTarget
	ModelViewerPositionVariant          = modelviewer.ModelViewerPositionVariant
	ModelViewerBounds                   = modelviewer.ModelViewerBounds
	ModelViewerMeshTransport            = modelviewer.ModelViewerMeshTransport
	ModelViewerTextureTransport         = modelviewer.ModelViewerTextureTransport
	ModelViewerVariableValue            = modelviewer.ModelViewerVariableValue
	ModelViewerMenuGuard                = modelviewer.ModelViewerMenuGuard
	ModelViewerMenuEffect               = modelviewer.ModelViewerMenuEffect
	ModelViewerVariable                 = modelviewer.ModelViewerVariable
	ModelViewerStateRule                = modelviewer.ModelViewerStateRule
	ModelViewerAnimationFrame           = modelviewer.ModelViewerAnimationFrame
	ModelViewerAnimationClip            = modelviewer.ModelViewerAnimationClip
	ModelViewerComputeBinarySource      = modelviewer.ModelViewerComputeBinarySource
	ModelViewerComputeShapePass         = modelviewer.ModelViewerComputeShapePass
	ModelViewerComputeShapeStage        = modelviewer.ModelViewerComputeShapeStage
	ModelViewerComputePoseSource        = modelviewer.ModelViewerComputePoseSource
	ModelViewerComputeDeformerTransport = modelviewer.ModelViewerComputeDeformerTransport
	ModelViewerTransport                = modelviewer.ModelViewerTransport
	ModelViewerSlider                   = modelviewer.ModelViewerSlider
	ModelViewerUIAssets                 = modelviewer.ModelViewerUIAssets
	ModelViewerBlockingVariable         = modelviewer.ModelViewerBlockingVariable
	ModelViewerResolutionChange         = modelviewer.ModelViewerResolutionChange
	ModelViewerResolutionSuggestion     = modelviewer.ModelViewerResolutionSuggestion
	ModelViewerIneffectiveValue         = modelviewer.ModelViewerIneffectiveValue
	GenshinToonAssetsTransport          = modelviewer.GenshinToonAssetsTransport
	GenshinToonRampTransport            = modelviewer.GenshinToonRampTransport
)

func (t *Tools) LoadModViewer(ctx context.Context, modPath string) (ModelViewerTransport, error) {
	return t.modelViewer.LoadModViewer(ctx, modPath)
}

// ReadModelViewerResource returns the bytes behind a URL of a transport LoadModViewer returned.
func (t *Tools) ReadModelViewerResource(ctx context.Context, url string) ([]byte, error) {
	return t.modelViewer.ReadResource(ctx, url)
}

func (t *Tools) LoadModGridPreview(ctx context.Context, modPath string) (ModelViewerTransport, error) {
	return t.modelViewer.LoadModGridPreview(ctx, modPath)
}

func (t *Tools) ListGenshinToonAssets(ctx context.Context, dir string) (GenshinToonAssetsTransport, error) {
	return t.modelViewer.ListGenshinToonAssets(ctx, dir)
}

func (t *Tools) CleanupModelViewer(ctx context.Context, memorySessionID string) (bool, error) {
	return t.modelViewer.CleanupModelViewer(ctx, memorySessionID)
}

func (t *Tools) GetModelViewerIneffectiveValues(
	ctx context.Context,
	sessionID, form string,
	state map[string]any,
) ([]ModelViewerIneffectiveValue, error) {
	return t.modelViewer.GetModelViewerIneffectiveValues(ctx, sessionID, form, state)
}

// CleanupStaleModelViewerDirs removes directories left by the Electron static
// GLB viewer. The Go viewer no longer creates them, but existing installations
// can retain old directories in the Windows temp root.
func (t *Tools) CleanupStaleModelViewerDirs(ctx context.Context) error {
	return t.modelViewer.CleanupStaleModelViewerDirs(ctx)
}

func (t *Tools) shutdownModelViewer() error {
	return t.modelViewer.Shutdown()
}
