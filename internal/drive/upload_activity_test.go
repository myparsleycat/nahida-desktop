package drive

import (
	"context"
	"slices"
	"testing"

	"nahida.live/desktop/internal/transfer"
)

func TestUploadActivityAggregatesConcurrentRequests(t *testing.T) {
	t.Parallel()
	phases := []transfer.UploadPhase{}
	activity := &uploadActivity{onProgress: func(progress UploadExecutionProgress) {
		phases = append(phases, progress.Phase)
	}}
	activity.setPreparing(true)
	ctx := context.WithValue(t.Context(), uploadActivityKey{}, activity)
	first := beginUploadRequest(ctx)
	second := beginUploadRequest(ctx)
	activity.setPreparing(false)
	first()
	first()
	if activity.phase != transfer.UploadTransferring {
		t.Fatalf("one waiting request hid the sending request: %s", activity.phase)
	}
	second()
	activity.setPreparing(true)
	activity.setPreparing(false)
	third := beginUploadRequest(ctx)
	third()
	want := []transfer.UploadPhase{
		transfer.UploadPreparing, transfer.UploadTransferring, transfer.UploadWaiting,
		transfer.UploadPreparing, transfer.UploadWaiting,
		transfer.UploadTransferring, transfer.UploadWaiting,
	}
	if !slices.Equal(phases, want) {
		t.Fatalf("upload phases = %v, want %v", phases, want)
	}
}
