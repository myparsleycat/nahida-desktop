package transfer

import (
	"testing"
	"time"
)

func TestHeartbeatContinuesDuringPreparationAndStopsWhenCompleted(t *testing.T) {
	service, emissions := newRecordingTransfer(t, 25*time.Millisecond)
	createTestTransfer(t, service, "preparing", StatusPreparing, true)
	awaitTransferEmission(t, emissions)
	for range 3 {
		got := emittedTransfer(t, awaitTransferEmission(t, emissions), "preparing")
		if got.Status != StatusPreparing || got.TransferredSize != 0 || got.Progress != 0 {
			t.Fatalf("preparation heartbeat = %#v", got)
		}
	}
	completed := StatusCompleted
	if err := service.Update("preparing", Updates{Status: &completed}); err != nil {
		t.Fatal(err)
	}
	awaitTransferEmission(t, emissions)
	assertNoTransferEmission(t, emissions, 100*time.Millisecond)
}

func TestHeartbeatKeepsRefreshingOtherActiveTransfersAfterPause(t *testing.T) {
	service, emissions := newRecordingTransfer(t, 25*time.Millisecond)
	createTestTransfer(t, service, "paused", StatusProgress, true)
	awaitTransferEmission(t, emissions)
	createTestTransfer(t, service, "active", StatusProgress, true)
	awaitTransferEmission(t, emissions)
	paused := StatusPaused
	if err := service.Update("paused", Updates{Status: &paused}); err != nil {
		t.Fatal(err)
	}
	awaitTransferEmission(t, emissions)
	got := emittedTransfer(t, awaitTransferEmission(t, emissions), "active")
	if got.Status != StatusProgress {
		t.Fatalf("remaining active transfer = %#v", got)
	}
}
