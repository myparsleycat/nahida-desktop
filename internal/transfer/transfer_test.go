package transfer

import (
	"context"
	"errors"
	"math"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type testSettings struct {
	enabled bool
	mibps   int
	move    bool
	err     error
}

func (s testSettings) GetDownloadBandwidthLimitMibps(context.Context) (int, error) {
	return s.mibps, s.err
}

func (s testSettings) GetPowerSaveBlockInTransfer(context.Context) (bool, error) {
	return s.enabled, s.err
}

func (s testSettings) GetMoveTransferPageWhenStartTransfer(context.Context) (bool, error) {
	return s.move, s.err
}

func createTestTransfer(t *testing.T, service *Transfer, pid string, status Status, manual bool) Record {
	t.Helper()
	record, err := service.Create(CreateParams{
		PID:           pid,
		Type:          "download",
		Name:          pid,
		InitialStatus: status,
		ManualStart:   manual,
		Data: Data{Files: []DownloadFile{
			{ID: pid + "-file", FileID: pid + "-file", Name: "file", Size: 100},
		}},
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	return record
}

func TestCreateBuildsRecordAndListOmitsData(t *testing.T) {
	service := New()
	first := createTestTransfer(t, service, "first", StatusPreparing, true)
	second := createTestTransfer(t, service, "second", StatusPreparing, true)

	if first.TotalSize != 100 || first.TotalFiles != 1 || first.Status != StatusPreparing {
		t.Fatalf("first transfer = %#v", first)
	}
	list := service.List()
	if len(list) != 2 || list[0].PID != second.PID || list[1].PID != first.PID {
		t.Fatalf("List() = %#v", list)
	}
}

func TestCreateEmitsStartToastAndOptionalNavigation(t *testing.T) {
	var events []struct {
		name string
		data []any
	}
	service := NewWithOptions(Options{
		Settings: testSettings{move: true},
		EventEmit: func(name string, data ...any) {
			events = append(events, struct {
				name string
				data []any
			}{name: name, data: data})
		},
	})
	createTestTransfer(t, service, "notified", StatusPreparing, true)
	if len(events) != 2 || events[0].name != "fn:toast" || events[1].name != "fn:navi" {
		t.Fatalf("events = %+v", events)
	}
	if got := events[1].data; len(got) != 1 || got[0] != "/transfer" {
		t.Fatalf("navigation data = %#v", got)
	}
}

func TestAttachedCancelStopsExternalWork(t *testing.T) {
	service := New()
	createTestTransfer(t, service, "external", StatusProgress, true)
	canceled := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	if err := service.AttachCancel("external", cancel); err != nil {
		t.Fatal(err)
	}
	go func() {
		<-ctx.Done()
		close(canceled)
	}()
	if err := service.Cancel("external"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("external cancel was not called")
	}
	record, ok := service.Get("external")
	if !ok || record.Status != StatusCanceled {
		t.Fatalf("record = %+v, ok=%v", record, ok)
	}
}

func TestPauseStopsAttachedExternalWork(t *testing.T) {
	service := New()
	createTestTransfer(t, service, "external-pause", StatusProgress, true)
	service.mu.Lock()
	service.entries["external-pause"].record.Speed = 12
	service.entries["external-pause"].record.ETA = 8
	service.mu.Unlock()
	ctx, cancel := context.WithCancel(context.Background())
	if err := service.AttachCancel("external-pause", cancel); err != nil {
		t.Fatal(err)
	}
	if err := service.Pause("external-pause"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("pause did not cancel external work")
	}
	record, ok := service.Get("external-pause")
	if !ok || record.Status != StatusPaused || record.Speed != 12 || record.ETA != 8 {
		t.Fatalf("record = %+v, ok=%v", record, ok)
	}
}

func TestCancelAndRunErrorPreserveElectronDisplayMetrics(t *testing.T) {
	for _, test := range []struct {
		name string
		run  func(*Transfer)
		want Status
	}{
		{name: "cancel", run: func(service *Transfer) { _ = service.Cancel("metrics") }, want: StatusCanceled},
		{name: "error", run: func(service *Transfer) { service.finishRun("metrics", errors.New("failed")) }, want: StatusError},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := New()
			createTestTransfer(t, service, "metrics", StatusProgress, true)
			phase := PlanProcessing
			progress := 25.0
			service.mu.Lock()
			item := service.entries["metrics"]
			item.record.Speed = 12
			item.record.ETA = 8
			item.record.PlanPhase = &phase
			item.record.PlanProgress = &progress
			service.mu.Unlock()

			test.run(service)
			record, ok := service.Get("metrics")
			if !ok || record.Status != test.want || record.Speed != 12 || record.ETA != 8 ||
				record.PlanPhase == nil || record.PlanProgress == nil {
				t.Fatalf("record = %+v, ok=%v", record, ok)
			}
		})
	}
}

func TestCreateSharesQueueGroupWhileQueueIsOpen(t *testing.T) {
	service := New()
	first := createTestTransfer(t, service, "first", StatusPending, true)
	second := createTestTransfer(t, service, "second", StatusPreparing, true)
	if first.QueueGroupID == nil || second.QueueGroupID == nil || *first.QueueGroupID != *second.QueueGroupID {
		t.Fatalf("open queue groups = %v, %v", first.QueueGroupID, second.QueueGroupID)
	}

	completed := StatusCompleted
	if err := service.Update("first", Updates{Status: &completed}); err != nil {
		t.Fatal(err)
	}
	if err := service.Update("second", Updates{Status: &completed}); err != nil {
		t.Fatal(err)
	}
	third := createTestTransfer(t, service, "third", StatusPending, true)
	if third.QueueGroupID == nil || *third.QueueGroupID == *first.QueueGroupID {
		t.Fatalf("new queue group = %v, previous = %v", third.QueueGroupID, first.QueueGroupID)
	}
}

func TestProcessQueueContinuesAfterRunnerError(t *testing.T) {
	service := New()
	createTestTransfer(t, service, "failed", StatusPending, true)
	createTestTransfer(t, service, "completed", StatusPending, true)

	wantErr := errors.New("download failed")
	if err := service.RegisterRunner("failed", func(context.Context, *Transfer, string) error {
		return wantErr
	}); err != nil {
		t.Fatal(err)
	}
	if err := service.RegisterRunner("completed", func(_ context.Context, transfers *Transfer, pid string) error {
		status := StatusCompleted
		progress := 100.0
		transferred := int64(100)
		files := 1
		return transfers.Update(pid, Updates{Status: &status, Progress: &progress, TransferredSize: &transferred, TransferredFiles: &files})
	}); err != nil {
		t.Fatal(err)
	}
	if err := service.ProcessQueue(context.Background()); err != nil {
		t.Fatalf("ProcessQueue() error = %v", err)
	}

	failed, ok := service.Get("failed")
	if !ok || failed.Status != StatusError || failed.Error != wantErr.Error() {
		t.Fatalf("failed transfer = %#v, ok = %v", failed, ok)
	}
	completed, ok := service.Get("completed")
	if !ok || completed.Status != StatusCompleted || completed.Progress != 100 {
		t.Fatalf("completed transfer = %#v, ok = %v", completed, ok)
	}
}

func TestPauseAndResumeUseFreshContext(t *testing.T) {
	service := New()
	createTestTransfer(t, service, "resume", StatusPending, true)
	service.mu.Lock()
	service.entries["resume"].restartData = struct{}{}
	service.mu.Unlock()

	started := make(chan struct{})
	var attempts atomic.Int32
	if err := service.RegisterRunner("resume", func(ctx context.Context, _ *Transfer, _ string) error {
		if attempts.Add(1) == 1 {
			close(started)
			<-ctx.Done()
			return ctx.Err()
		}
		if err := ctx.Err(); err != nil {
			t.Fatalf("resumed runner received canceled context: %v", err)
		}
		status := StatusCompleted
		return service.Update("resume", Updates{Status: &status})
	}); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() { done <- service.ProcessQueue(context.Background()) }()
	<-started
	if err := service.Pause("resume"); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatalf("first ProcessQueue() error = %v", err)
	}
	record, _ := service.Get("resume")
	if record.Status != StatusPaused {
		t.Fatalf("paused status = %q", record.Status)
	}

	if err := service.Resume("resume"); err != nil {
		t.Fatal(err)
	}
	if err := service.ProcessQueue(context.Background()); err != nil {
		t.Fatalf("resumed ProcessQueue() error = %v", err)
	}
	record, _ = service.Get("resume")
	if record.Status != StatusCompleted || attempts.Load() != 2 {
		t.Fatalf("resumed transfer = %#v, attempts = %d", record, attempts.Load())
	}
}

func TestResumeAndRetryIgnoreTransfersWithoutRestartData(t *testing.T) {
	for _, operation := range []struct {
		name string
		run  func(*Transfer) error
	}{
		{name: "resume", run: func(service *Transfer) error { return service.Resume("custom") }},
		{name: "retry", run: func(service *Transfer) error { return service.Retry("custom") }},
	} {
		t.Run(operation.name, func(t *testing.T) {
			service := New()
			createTestTransfer(t, service, "custom", StatusCanceled, true)
			var attempts atomic.Int32
			if err := service.RegisterRunner("custom", func(context.Context, *Transfer, string) error {
				attempts.Add(1)
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if err := operation.run(service); err != nil {
				t.Fatal(err)
			}
			if err := service.ProcessQueue(context.Background()); err != nil {
				t.Fatal(err)
			}
			record, _ := service.Get("custom")
			if record.Status != StatusCanceled || attempts.Load() != 0 {
				t.Fatalf("transfer = %#v, attempts = %d", record.Snapshot, attempts.Load())
			}
		})
	}
}

func TestUpdateCalculatesProgressSpeedAndClearsPlan(t *testing.T) {
	now := time.Unix(100, 0)
	service := NewWithOptions(Options{Now: func() time.Time { return now }})
	createTestTransfer(t, service, "progress", StatusProgress, true)
	phase := PlanProcessing
	planProgress := 25.0
	transferred := int64(10)
	if err := service.Update("progress", Updates{
		TransferredSize: &transferred,
		PlanPhase:       &phase,
		PlanProgress:    &planProgress,
	}); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Second)
	transferred = 60
	if err := service.Update("progress", Updates{
		TransferredSize:   &transferred,
		ClearPlanPhase:    true,
		ClearPlanProgress: true,
	}); err != nil {
		t.Fatal(err)
	}
	record, _ := service.Get("progress")
	if record.Progress != 60 || record.Speed != 50 || record.ETA != 1 {
		t.Fatalf("progress metrics = %#v", record.Snapshot)
	}
	if record.PlanPhase != nil || record.PlanProgress != nil {
		t.Fatalf("plan fields were not cleared: %#v", record.Snapshot)
	}
}

func TestUpdateTracksUnknownSizeProgress(t *testing.T) {
	now := time.Unix(100, 0)
	service := NewWithOptions(Options{Now: func() time.Time { return now }})
	if _, err := service.Create(CreateParams{
		PID: "unknown-size", Type: "download", Name: "unknown-size", InitialStatus: StatusProgress, ManualStart: true,
		Data: Data{Files: []DownloadFile{{ID: "file", FileID: "file", Name: "file"}}},
	}); err != nil {
		t.Fatal(err)
	}

	transferred := int64(10)
	if err := service.Update("unknown-size", Updates{TransferredSize: &transferred}); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Second)
	transferred = 60
	if err := service.Update("unknown-size", Updates{TransferredSize: &transferred}); err != nil {
		t.Fatal(err)
	}
	record, _ := service.Get("unknown-size")
	if record.TransferredSize != 60 || record.Progress != 100 || record.Speed != 50 || record.ETA != 0 {
		t.Fatalf("unknown-size metrics = %#v", record.Snapshot)
	}
}

func TestPowerSaveBlockTracksActiveTransfers(t *testing.T) {
	var mu sync.Mutex
	var calls []bool
	service := NewWithOptions(Options{
		Settings: testSettings{enabled: true},
		PreventSuspension: func(block bool) error {
			mu.Lock()
			calls = append(calls, block)
			mu.Unlock()
			return nil
		},
	})
	createTestTransfer(t, service, "power", StatusPreparing, true)
	paused := StatusPaused
	if err := service.Update("power", Updates{Status: &paused}); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(calls) != 2 || !calls[0] || calls[1] {
		t.Fatalf("prevent suspension calls = %v", calls)
	}
}

func TestAggregateProgressScopesCompletedTransfersToOpenGroups(t *testing.T) {
	groupOne := uint64(1)
	groupTwo := uint64(2)
	progress := AggregateProgress([]Snapshot{
		{Status: StatusCompleted, TotalSize: 100, QueueGroupID: &groupOne},
		{Status: StatusProgress, TotalSize: 100, TransferredSize: 50, QueueGroupID: &groupOne},
		{Status: StatusCompleted, TotalSize: 900, QueueGroupID: &groupTwo},
	})
	if progress == nil || math.Abs(*progress-75) > 0.0001 {
		t.Fatalf("AggregateProgress() = %v, want 75", progress)
	}
}

func TestCalculateWindowProgressMatchesElectronModes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		transfers []Snapshot
		wantValue *float64
		wantMode  WindowProgressMode
	}{
		{
			name:      "completed and canceled are hidden",
			transfers: []Snapshot{{Status: StatusCompleted}, {Status: StatusCanceled}},
		},
		{
			name: "progress takes priority",
			transfers: []Snapshot{
				{Status: StatusProgress, TotalSize: 100, TransferredSize: 50},
				{Status: StatusPaused, TotalSize: 100, TransferredSize: 25},
			},
			wantValue: float64Ptr(50),
			wantMode:  WindowProgressNormal,
		},
		{
			name:      "pending is indeterminate",
			transfers: []Snapshot{{Status: StatusPending, Progress: 10}},
			wantValue: float64Ptr(10),
			wantMode:  WindowProgressIndeterminate,
		},
		{
			name:      "paused uses fallback bytes",
			transfers: []Snapshot{{Status: StatusPaused, TotalSize: 200, TransferredSize: 80}},
			wantValue: float64Ptr(40),
			wantMode:  WindowProgressPaused,
		},
		{
			name:      "error uses fallback progress",
			transfers: []Snapshot{{Status: StatusError, Progress: 35}},
			wantValue: float64Ptr(35),
			wantMode:  WindowProgressError,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := CalculateWindowProgress(tt.transfers)
			if tt.wantValue == nil {
				if got != nil {
					t.Fatalf("CalculateWindowProgress() = %#v, want nil", got)
				}
				return
			}
			if got == nil || math.Abs(got.Value-*tt.wantValue) > 0.0001 || got.Mode != tt.wantMode {
				t.Fatalf("CalculateWindowProgress() = %#v, want value %v mode %q", got, *tt.wantValue, tt.wantMode)
			}
		})
	}
}

func TestEmitSyncsWindowProgress(t *testing.T) {
	t.Parallel()
	var got *WindowProgress
	service := NewWithOptions(Options{
		SyncWindowProgress: func(progress *WindowProgress) { got = progress },
	})
	createTestTransfer(t, service, "taskbar", StatusProgress, true)
	if got == nil || got.Mode != WindowProgressNormal {
		t.Fatalf("synced progress = %#v", got)
	}
}

func TestClearRemovesOnlyTerminalTransfers(t *testing.T) {
	service := New()
	createTestTransfer(t, service, "active", StatusPreparing, true)
	createTestTransfer(t, service, "terminal", StatusPreparing, true)
	canceled := StatusCanceled
	if err := service.Update("terminal", Updates{Status: &canceled}); err != nil {
		t.Fatal(err)
	}
	if err := service.Clear(); err != nil {
		t.Fatal(err)
	}
	if _, ok := service.Get("terminal"); ok {
		t.Fatal("terminal transfer was not cleared")
	}
	if _, ok := service.Get("active"); !ok {
		t.Fatal("active transfer was cleared")
	}
}

func TestLogicalFileBytes(t *testing.T) {
	t.Parallel()
	uncomp := int64(40)
	if got := LogicalFileBytes(DownloadFile{Size: 12, UncompSize: &uncomp}); got != 40 {
		t.Fatalf("uncomp = %d", got)
	}
	if got := LogicalFileBytes(DownloadFile{Size: 12}); got != 12 {
		t.Fatalf("missing uncomp = %d", got)
	}
	if got := LogicalFileBytes(DownloadFile{Size: 12, UncompSize: nil}); got != 12 {
		t.Fatalf("nil uncomp = %d", got)
	}
}

func float64Ptr(value float64) *float64 { return &value }
