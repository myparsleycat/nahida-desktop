package tools

import (
	"context"

	fixinspection "nahida.live/desktop/internal/tools/fix_inspection"
)

type (
	FixInspectionResult   = fixinspection.FixInspectionResult
	FixInspectionRecord   = fixinspection.FixInspectionRecord
	FixInspectionSnapshot = fixinspection.FixInspectionSnapshot
)

func (t *Tools) InspectModForFix(ctx context.Context, modPath, importer string) (*FixInspectionResult, error) {
	return t.fixInspection.InspectModForFix(ctx, modPath, importer)
}

func (t *Tools) RefreshFixInspections(ctx context.Context) FixInspectionSnapshot {
	return t.fixInspection.RefreshFixInspections(ctx)
}

func (t *Tools) DismissFixInspection(modPath string) {
	t.fixInspection.DismissFixInspection(modPath)
}

//wails:ignore
func (t *Tools) QueueFixInspections(paths []string) {
	t.fixInspection.QueueFixInspections(paths)
}

func (t *Tools) shutdownFixInspections() error {
	if t == nil || t.fixInspection == nil {
		return nil
	}
	return t.fixInspection.Shutdown()
}
