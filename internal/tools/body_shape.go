package tools

import (
	"context"

	bodyshape "nahida.live/desktop/internal/tools/body_shape"
)

type (
	BodyShapeSessionDescriptor    = bodyshape.BodyShapeSessionDescriptor
	BodyShapeMeshInput            = bodyshape.BodyShapeMeshInput
	BodyShapeMeshDescriptor       = bodyshape.BodyShapeMeshDescriptor
	BodyShapeMeshSummary          = bodyshape.BodyShapeMeshSummary
	BodyShapeChangeSummary        = bodyshape.BodyShapeChangeSummary
	BodyShapeExportedMesh         = bodyshape.BodyShapeExportedMesh
	BodyShapeOK                   = bodyshape.BodyShapeOK
	BodyShapeExportResult         = bodyshape.BodyShapeExportResult
	BodyShapeMeshData             = bodyshape.BodyShapeMeshData
	BodyShapeMeshPositions        = bodyshape.BodyShapeMeshPositions
	BodyShapeCommitPositionsInput = bodyshape.BodyShapeCommitPositionsInput
)

func (t *Tools) BodyShapeLoadMod(ctx context.Context, modPath string) (BodyShapeSessionDescriptor, error) {
	return t.bodyShape.BodyShapeLoadMod(ctx, modPath)
}

func (t *Tools) BodyShapeGetMesh(
	ctx context.Context,
	input BodyShapeMeshInput,
) (BodyShapeMeshDescriptor, error) {
	return t.bodyShape.BodyShapeGetMesh(ctx, input)
}

func (t *Tools) BodyShapeCloseSession(ctx context.Context, sessionID string) (BodyShapeOK, error) {
	return t.bodyShape.BodyShapeCloseSession(ctx, sessionID)
}

func (t *Tools) shutdownBodyShape() error {
	return t.bodyShape.Shutdown()
}

func (t *Tools) BodyShapeMeshData(ctx context.Context, input BodyShapeMeshInput) (BodyShapeMeshData, error) {
	return t.bodyShape.BodyShapeMeshData(ctx, input)
}

func (t *Tools) BodyShapeCommitPositions(
	ctx context.Context,
	input BodyShapeCommitPositionsInput,
) (BodyShapeExportResult, error) {
	return t.bodyShape.BodyShapeCommitPositions(ctx, input)
}
