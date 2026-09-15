package tools

import (
	"context"

	bodyshape "nahida.live/desktop/internal/tools/body_shape"
)

type (
	BodyShapeSessionDescriptor = bodyshape.BodyShapeSessionDescriptor
	BodyShapeMeshInput         = bodyshape.BodyShapeMeshInput
	BodyShapeMeshDescriptor    = bodyshape.BodyShapeMeshDescriptor
	BodyShapeMeshSummary       = bodyshape.BodyShapeMeshSummary
	BodyShapeChangeSummary     = bodyshape.BodyShapeChangeSummary
	BodyShapeBeginExportInput  = bodyshape.BodyShapeBeginExportInput
	BodyShapeExportUpload      = bodyshape.BodyShapeExportUpload
	BodyShapeCommitExportInput = bodyshape.BodyShapeCommitExportInput
	BodyShapeOK                = bodyshape.BodyShapeOK
	BodyShapeExportInput       = bodyshape.BodyShapeExportInput
	BodyShapeExportResult      = bodyshape.BodyShapeExportResult
)

func (t *Tools) BodyShapeLoadMod(ctx context.Context, modPath string) (BodyShapeSessionDescriptor, error) {
	return t.bodyShape.BodyShapeLoadMod(ctx, modPath)
}

//wails:ignore
func (t *Tools) BodyShapeExport(
	ctx context.Context,
	input BodyShapeExportInput,
) (BodyShapeExportResult, error) {
	return t.bodyShape.BodyShapeExport(ctx, input)
}

func (t *Tools) BodyShapeGetMesh(
	ctx context.Context,
	input BodyShapeMeshInput,
) (BodyShapeMeshDescriptor, error) {
	return t.bodyShape.BodyShapeGetMesh(ctx, input)
}

func (t *Tools) BodyShapeBeginExport(
	ctx context.Context,
	input BodyShapeBeginExportInput,
) (BodyShapeExportUpload, error) {
	return t.bodyShape.BodyShapeBeginExport(ctx, input)
}

func (t *Tools) BodyShapeCommitExport(
	ctx context.Context,
	input BodyShapeCommitExportInput,
) (BodyShapeExportResult, error) {
	return t.bodyShape.BodyShapeCommitExport(ctx, input)
}

func (t *Tools) BodyShapeCloseSession(ctx context.Context, sessionID string) (BodyShapeOK, error) {
	return t.bodyShape.BodyShapeCloseSession(ctx, sessionID)
}

func (t *Tools) shutdownBodyShape() error {
	if t == nil || t.bodyShape == nil {
		return nil
	}
	return t.bodyShape.Shutdown()
}
