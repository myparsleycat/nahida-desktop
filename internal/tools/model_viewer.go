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
)

func (t *Tools) LoadModViewer(ctx context.Context, modPath string) (ModelViewerTransport, error) {
	return t.modelViewer.LoadModViewer(ctx, modPath)
}

func (t *Tools) CleanupModelViewer(ctx context.Context, memorySessionID string) (bool, error) {
	return t.modelViewer.CleanupModelViewer(ctx, memorySessionID)
}

func (t *Tools) GetModelViewerIneffectiveValues(
	ctx context.Context,
	sessionID string,
	state map[string]any,
) ([]ModelViewerIneffectiveValue, error) {
	return t.modelViewer.GetModelViewerIneffectiveValues(ctx, sessionID, state)
}

// CleanupModelViewerWindow also fences loads completing after the native window closes.
//
//wails:ignore
func (t *Tools) CleanupModelViewerWindow(windowID uint) {
	if t == nil || t.modelViewer == nil {
		return
	}
	t.modelViewer.CleanupModelViewerWindow(windowID)
}

// CleanupStaleModelViewerDirs removes directories left by the Electron static
// GLB viewer. The Go viewer no longer creates them, but existing installations
// can retain old directories in the Windows temp root.
//
//wails:ignore
func (t *Tools) CleanupStaleModelViewerDirs(ctx context.Context) error {
	if t == nil || t.modelViewer == nil {
		return nil
	}
	return t.modelViewer.CleanupStaleModelViewerDirs(ctx)
}

func (t *Tools) shutdownModelViewer() error {
	if t == nil || t.modelViewer == nil {
		return nil
	}
	return t.modelViewer.Shutdown()
}
