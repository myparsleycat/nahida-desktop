package tools

import (
	"context"

	liquidemitter "nahida.live/desktop/internal/tools/liquid_emitter"
)

type (
	LiquidEmitterInspection  = liquidemitter.Inspection
	LiquidEmitterApplyInput  = liquidemitter.ApplyInput
	LiquidEmitterRemoveInput = liquidemitter.RemoveInput
	LiquidEmitterLookInput   = liquidemitter.LookInput
)

func (t *Tools) LiquidEmitterImporters() []string {
	return liquidemitter.Importers()
}

func (t *Tools) LiquidEmitterInspect(ctx context.Context, modPath string) (LiquidEmitterInspection, error) {
	return t.liquidEmitter.Inspect(ctx, modPath)
}

func (t *Tools) LiquidEmitterInstallRuntime(ctx context.Context, modPath string) (LiquidEmitterInspection, error) {
	return t.liquidEmitter.InstallRuntime(ctx, modPath)
}

func (t *Tools) LiquidEmitterApply(
	ctx context.Context,
	input LiquidEmitterApplyInput,
) (LiquidEmitterInspection, error) {
	return t.liquidEmitter.Apply(ctx, input)
}

func (t *Tools) LiquidEmitterRemove(
	ctx context.Context,
	input LiquidEmitterRemoveInput,
) (LiquidEmitterInspection, error) {
	return t.liquidEmitter.Remove(ctx, input)
}

func (t *Tools) LiquidEmitterSetLook(
	ctx context.Context,
	input LiquidEmitterLookInput,
) (LiquidEmitterInspection, error) {
	return t.liquidEmitter.SetLook(ctx, input)
}

func (t *Tools) LiquidEmitterRestoreSource(ctx context.Context, modPath string) (LiquidEmitterInspection, error) {
	return t.liquidEmitter.RestoreSource(ctx, modPath)
}
