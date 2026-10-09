package transfer

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestLateRunnerUpdatePreservesInterruption(t *testing.T) {
	t.Parallel()
	for _, status := range []Status{StatusPaused, StatusCanceled} {
		service := New()
		service.emitStopped = true
		createTestTransfer(t, service, "late", StatusProgress, true)
		if err := service.Update("late", Updates{Status: &status}); err != nil {
			t.Fatal(err)
		}
		completed := StatusCompleted
		if err := service.UpdateRunning("late", Updates{Status: &completed}); !errors.Is(err, context.Canceled) {
			t.Fatalf("late update = %v; want cancellation", err)
		}
		if record, _ := service.Get("late"); record.Status != status {
			t.Fatalf("late update replaced %s with %s", status, record.Status)
		}
	}
}

func TestResumeBeforeCanceledRunnerReturns(t *testing.T) {
	t.Parallel()
	service := New()
	service.emitStopped = true
	if _, err := service.Create(CreateParams{
		PID: "resume", Type: "download", InitialStatus: StatusPending, RestartData: true,
	}); err != nil {
		t.Fatal(err)
	}
	started, release := make(chan struct{}), make(chan struct{})
	attempts := 0
	if err := service.RegisterRunner("resume", func(ctx context.Context, queue *Transfer, pid string) error {
		attempts++
		if attempts == 1 {
			close(started)
			<-release
			return ctx.Err()
		}
		completed := StatusCompleted
		return queue.UpdateRunning(pid, Updates{Status: &completed})
	}); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- service.ProcessQueue(t.Context()) }()
	<-started
	if err := service.Pause("resume"); err != nil {
		t.Fatal(err)
	}
	if err := service.Resume("resume"); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if record, _ := service.Get("resume"); record.Status != StatusCompleted || attempts != 2 {
		t.Fatalf("resumed record = %+v, attempts = %d", record, attempts)
	}
}

func TestProgressSamplesStayBoundedDuringFastTransfer(t *testing.T) {
	t.Parallel()
	now := time.Unix(100, 0)
	service := NewWithOptions(Options{Now: func() time.Time { return now }})
	service.emitStopped = true
	createTestTransfer(t, service, "fast", StatusProgress, true)
	item := service.entries["fast"]
	item.record.TotalSize = 1 << 40
	for index := range 100_000 {
		now = now.Add(100 * time.Microsecond)
		bytes := int64(index+1) * 32 * 1024
		if err := service.Update("fast", Updates{TransferredSize: &bytes}); err != nil {
			t.Fatal(err)
		}
	}
	if len(item.samples) > int(speedWindow/speedSampleInterval)+2 {
		t.Fatalf("speed samples grew with reads: %d", len(item.samples))
	}
	if item.record.TransferredSize != 100_000*32*1024 || item.record.Speed <= 0 {
		t.Fatalf("fast transfer metrics = %#v", item.record.Snapshot)
	}
}

func TestSpeedExpiresWithoutChangingTransferredBytes(t *testing.T) {
	t.Parallel()
	now := time.Unix(100, 0)
	item := &entry{record: Record{Snapshot: Snapshot{TotalSize: 100, TransferredSize: 10}}}
	updateSpeed(item, now, true)
	item.record.TransferredSize = 60
	updateSpeed(item, now.Add(time.Second), true)
	if item.record.Speed != 50 || item.record.ETA != 1 {
		t.Fatalf("initial metrics = %#v", item.record.Snapshot)
	}
	updateSpeed(item, now.Add(2*time.Second), false)
	if item.record.Speed != 25 || item.record.ETA != 2 {
		t.Fatalf("waiting metrics = %#v", item.record.Snapshot)
	}
	updateSpeed(item, now.Add(7*time.Second), false)
	if item.record.Speed != 0 || item.record.ETA != 0 || item.record.TransferredSize != 60 {
		t.Fatalf("expired metrics = %#v", item.record.Snapshot)
	}
}

func TestSpeedResetsAfterRetryRollback(t *testing.T) {
	t.Parallel()
	now := time.Unix(100, 0)
	item := &entry{record: Record{Snapshot: Snapshot{TotalSize: 100, TransferredSize: 60}}}
	updateSpeed(item, now, true)
	item.record.TransferredSize = 10
	updateSpeed(item, now.Add(time.Second), true)
	if item.record.Speed != 0 || item.record.ETA != 0 {
		t.Fatalf("rollback metrics = %#v", item.record.Snapshot)
	}
	item.record.TransferredSize = 30
	updateSpeed(item, now.Add(2*time.Second), true)
	if item.record.Speed != 20 {
		t.Fatalf("retry speed = %v, want 20", item.record.Speed)
	}
}

func TestUploadPhaseClearsOnPauseAndReset(t *testing.T) {
	t.Parallel()
	service := New()
	service.emitStopped = true
	createTestTransfer(t, service, "phase", StatusProgress, true)
	phase := UploadWaiting
	if err := service.Update("phase", Updates{UploadPhase: &phase}); err != nil {
		t.Fatal(err)
	}
	paused := StatusPaused
	if err := service.Update("phase", Updates{Status: &paused}); err != nil {
		t.Fatal(err)
	}
	if record, _ := service.Get("phase"); record.UploadPhase != "" {
		t.Fatalf("pause retained upload phase: %s", record.UploadPhase)
	}
	progress := StatusProgress
	if err := service.Update("phase", Updates{Status: &progress, UploadPhase: &phase}); err != nil {
		t.Fatal(err)
	}
	if err := service.ResetTransfer("phase"); err != nil {
		t.Fatal(err)
	}
	if record, _ := service.Get("phase"); record.UploadPhase != "" {
		t.Fatalf("reset retained upload phase: %s", record.UploadPhase)
	}
}
