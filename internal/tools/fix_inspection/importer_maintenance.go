package fixinspection

import (
	"context"
	"errors"

	"nahida.live/desktop/internal/xxmi"
)

// SuspendImporterWatchers drains inspections and releases both mod and parent directory handles.
func (t *Service) SuspendImporterWatchers(ctx context.Context) (func([]xxmi.ImportedImporter) error, error) {
	t.fixInspectionRunMu.Lock()
	if err := ctx.Err(); err != nil {
		t.fixInspectionRunMu.Unlock()
		return nil, err
	}
	t.fixInspectionMu.Lock()
	tracked := make([]*trackedFixInspection, 0, len(t.fixInspections))
	for _, item := range t.fixInspections {
		tracked = append(tracked, item)
	}
	t.fixInspectionMu.Unlock()
	stopErr := closeFixInspectionWatchers(tracked)

	resume := func(moved []xxmi.ImportedImporter) error {
		defer t.fixInspectionRunMu.Unlock()
		t.fixInspectionMu.Lock()
		if t.fixInspectionClosed {
			t.fixInspectionMu.Unlock()
			return nil
		}
		// Snapshot dismissals at resume time so user choices made while paused are retained.
		tracked = tracked[:0]
		for _, item := range t.fixInspections {
			tracked = append(tracked, &trackedFixInspection{
				record: cloneFixInspectionRecord(item.record), dismissedResult: item.dismissedResult,
			})
		}
		t.fixInspections = make(map[string]*trackedFixInspection)
		t.fixInspectionMu.Unlock()
		var result error
		for _, item := range tracked {
			item.record.ModPath = xxmi.RelocateUserDataPath(item.record.ModPath, moved)
			_, err := t.storeFixInspection(item.record)
			result = errors.Join(result, err)
			t.carryFixInspectionDismissal(fixInspectionKey(item.record.ModPath), item.dismissedResult)
		}
		if len(tracked) > 0 {
			t.emitFixInspectionSnapshot()
		}
		return result
	}
	if stopErr != nil {
		return nil, errors.Join(stopErr, resume(nil))
	}
	return resume, nil
}
