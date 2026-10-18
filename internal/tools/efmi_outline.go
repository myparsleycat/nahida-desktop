package tools

import (
	"context"

	efmioutline "nahida.live/desktop/internal/tools/efmi_outline"
)

type (
	EFMIOutlineInspection = efmioutline.OutlineInspection
	EFMIOutlineApplyInput = efmioutline.OutlineApplyInput
)

func (t *Tools) EFMIOutlineInspect(ctx context.Context, modPath string) (EFMIOutlineInspection, error) {
	return t.efmiOutline.Inspect(ctx, modPath)
}

func (t *Tools) EFMIOutlineApply(ctx context.Context, input EFMIOutlineApplyInput) (EFMIOutlineInspection, error) {
	return t.efmiOutline.Apply(ctx, input)
}

func (t *Tools) EFMIOutlineRevert(ctx context.Context, modPath, file string) (EFMIOutlineInspection, error) {
	return t.efmiOutline.Revert(ctx, modPath, file)
}

func (t *Tools) EFMIOutlineAdopt(ctx context.Context, modPath, file string) (EFMIOutlineInspection, error) {
	return t.efmiOutline.Adopt(ctx, modPath, file)
}
