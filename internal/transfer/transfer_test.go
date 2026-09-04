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

type captureTransferLogger struct {
	records []any
}

func (l *captureTransferLogger) Error(msg any, _ string) {
	l.records = append(l.records, msg)
}

type reportedRunnerError struct{ error }

func (reportedRunnerError) DiagnosticReported() bool { return true }

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

func TestDestinationPathsAreClonedAcrossTransferBoundaries(t *testing.T) {
	service := New()
	createdPaths := []string{`C:\Mods\Legacy`}
	createdTargets := []DestinationTarget{{Path: `C:\Mods\First`, Kind: DestinationDirectory}}
	record, err := service.Create(CreateParams{
		PID: "paths", Type: "download", Name: "paths", InitialStatus: StatusPreparing,
		DestinationPaths: createdPaths, DestinationTargets: createdTargets, ManualStart: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	createdPaths[0] = `C:\Mods\Mutated`
	createdTargets[0].Path = `C:\Mods\MutatedTarget`
	record.DestinationPaths[0] = `C:\Mods\Returned`
	record.DestinationTargets[0].Path = `C:\Mods\ReturnedTarget`
	if got := service.List()[0].DestinationPaths[0]; got != `C:\Mods\First` {
		t.Fatalf("created destination path = %q", got)
	}
	if got := service.List()[0].DestinationTargets[0]; got.Path != `C:\Mods\First` || got.Kind != DestinationDirectory {
		t.Fatalf("created destination target = %#v", got)
	}

	updatedTargets := []DestinationTarget{
		{Path: `C:\Mods\Second`, Kind: DestinationDirectory},
		{Path: `C:\Mods\Third.zip`, Kind: DestinationFile},
	}
	if err := service.Update("paths", Updates{DestinationTargets: updatedTargets}); err != nil {
		t.Fatal(err)
	}
	updatedTargets[0].Path = `C:\Mods\MutatedAgain`
	listed := service.List()
	listed[0].DestinationPaths[1] = `C:\Mods\ReturnedAgain`
	listed[0].DestinationTargets[1].Path = `C:\Mods\ReturnedTargetAgain`
	got := service.List()[0].DestinationPaths
	if len(got) != 2 || got[0] != `C:\Mods\Second` || got[1] != `C:\Mods\Third.zip` {
		t.Fatalf("updated destination paths = %#v", got)
	}
	gotTargets := service.List()[0].DestinationTargets
	if len(gotTargets) != 2 || gotTargets[0].Path != `C:\Mods\Second` || gotTargets[1].Kind != DestinationFile {
		t.Fatalf("updated destination targets = %#v", gotTargets)
	}

	setDataTargets := []DestinationTarget{{Path: `C:\Mods\SetData`, Kind: DestinationDirectory}}
	if err := service.SetData("paths", Data{}, 0, "paths", setDataTargets); err != nil {
		t.Fatal(err)
	}
	setDataTargets[0].Path = `C:\Mods\MutatedSetData`
	gotRecord, ok := service.Get("paths")
	if !ok {
		t.Fatal("SetData transfer not found")
	}
	gotRecord.DestinationTargets[0].Path = `C:\Mods\ReturnedSetData`
	if got := service.List()[0]; len(got.DestinationPaths) != 1 || got.DestinationPaths[0] != `C:\Mods\SetData` ||
		got.DestinationTargets[0].Path != `C:\Mods\SetData` {
		t.Fatalf("SetData destinations = %#v, %#v", got.DestinationPaths, got.DestinationTargets)
	}

	legacyPaths := []string{`C:\Mods\LegacySecond`}
	if err := service.Update("paths", Updates{DestinationPaths: legacyPaths}); err != nil {
		t.Fatal(err)
	}
	legacyPaths[0] = `C:\Mods\MutatedLegacy`
	if got := service.List()[0]; len(got.DestinationPaths) != 1 || got.DestinationPaths[0] != `C:\Mods\LegacySecond` ||
		got.DestinationTargets != nil {
		t.Fatalf("legacy destination update = %#v, %#v", got.DestinationPaths, got.DestinationTargets)
	}
}

func TestIsActiveDownloadDestinationMatchesOverlappingOpenDownloadPaths(t *testing.T) {
	for _, test := range []struct {
		name   string
		type_  string
		status Status
		want   bool
	}{
		{name: "pending", type_: "download", status: StatusPending, want: true},
		{name: "preparing", type_: "download", status: StatusPreparing, want: true},
		{name: "progress", type_: "download", status: StatusProgress, want: true},
		{name: "paused", type_: "download", status: StatusPaused, want: true},
		{name: "completed", type_: "download", status: StatusCompleted},
		{name: "canceled", type_: "download", status: StatusCanceled},
		{name: "error", type_: "download", status: StatusError},
		{name: "upload", type_: "upload", status: StatusProgress},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := New()
			if _, err := service.Create(CreateParams{
				PID: "active", Type: test.type_, Name: "active", InitialStatus: test.status,
				DestinationPaths: []string{`C:\Mods\Character\Active`}, ManualStart: true,
			}); err != nil {
				t.Fatal(err)
			}
			for _, path := range []string{
				`c:\mods\character\active`,
				`C:\Mods\Character\Active\Nested`,
				`C:\Mods\Character`,
			} {
				if got := service.IsActiveDownloadDestination(path); got != test.want {
					t.Fatalf("IsActiveDownloadDestination(%q) = %v, want %v", path, got, test.want)
				}
			}
			if service.IsActiveDownloadDestination(`C:\Mods\Character\Other`) {
				t.Fatal("unrelated sibling matched active destination")
			}
		})
	}
}

func TestCanceledDownloadKeepsDestinationReservedUntilRunnerStops(t *testing.T) {
	service := New()
	path := `C:\Mods\Character\Active`
	if _, err := service.Create(CreateParams{
		PID: "canceling", Type: "download", Name: "canceling", InitialStatus: StatusPending,
		DestinationPaths: []string{path}, ManualStart: true,
	}); err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	canceled := make(chan struct{})
	releaseRunner := make(chan struct{})
	if err := service.RegisterRunner("canceling", func(ctx context.Context, _ *Transfer, _ string) error {
		close(started)
		<-ctx.Done()
		close(canceled)
		<-releaseRunner
		return ctx.Err()
	}); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- service.ProcessQueue(context.Background()) }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("download runner did not start")
	}
	if err := service.Cancel("canceling"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("download runner did not observe cancellation")
	}
	if !service.IsActiveDownloadDestination(path) {
		t.Fatal("destination reservation released before canceled runner stopped")
	}
	close(releaseRunner)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("canceled runner did not stop")
	}
	if service.IsActiveDownloadDestination(path) {
		t.Fatal("destination reservation remains after canceled runner stopped")
	}
}

func TestFailedDownloadReleasesDestinationAfterRunnerStops(t *testing.T) {
	service := New()
	path := `C:\Mods\Character\Failed`
	if _, err := service.Create(CreateParams{
		PID: "failing", Type: "download", Name: "failing", InitialStatus: StatusPending,
		DestinationPaths: []string{path}, ManualStart: true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := service.RegisterRunner("failing", func(context.Context, *Transfer, string) error {
		return errors.New("download failed")
	}); err != nil {
		t.Fatal(err)
	}
	if err := service.ProcessQueue(context.Background()); err != nil {
		t.Fatal(err)
	}
	record, ok := service.Get("failing")
	if !ok || record.Status != StatusError {
		t.Fatalf("failed transfer = %+v, ok=%v", record, ok)
	}
	if service.IsActiveDownloadDestination(path) {
		t.Fatal("destination reservation remains after failed runner stopped")
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

func TestFinishRunLogsUnreportedTerminalRunnerErrorOnce(t *testing.T) {
	t.Parallel()

	log := &captureTransferLogger{}
	service := NewWithOptions(Options{Log: log})
	createTestTransfer(t, service, "terminal", StatusPending, true)
	if err := service.RegisterRunner("terminal", func(_ context.Context, transfers *Transfer, pid string) error {
		failed := StatusError
		message := "owner failed"
		if err := transfers.Update(pid, Updates{Status: &failed, Error: &message}); err != nil {
			return err
		}
		return errors.New(message)
	}); err != nil {
		t.Fatal(err)
	}
	if err := service.ProcessQueue(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(log.records) != 1 {
		t.Fatalf("records = %#v, want one", log.records)
	}
	record, ok := log.records[0].(map[string]any)
	if !ok || record["pid"] != "terminal" || record["operation"] != "run" || record["error"] != "owner failed" {
		t.Fatalf("record = %#v", log.records[0])
	}
}

func TestFinishRunSkipsReportedRunnerError(t *testing.T) {
	t.Parallel()

	log := &captureTransferLogger{}
	service := NewWithOptions(Options{Log: log})
	createTestTransfer(t, service, "reported", StatusPending, true)
	if err := service.RegisterRunner("reported", func(context.Context, *Transfer, string) error {
		return reportedRunnerError{error: errors.New("already logged")}
	}); err != nil {
		t.Fatal(err)
	}
	if err := service.ProcessQueue(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(log.records) != 0 {
		t.Fatalf("reported failure logged again: %#v", log.records)
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

type recordedTransferEmission struct {
	at        time.Time
	transfers []Snapshot
}

func newRecordingTransfer(t *testing.T, interval time.Duration) (*Transfer, <-chan recordedTransferEmission) {
	t.Helper()
	emissions := make(chan recordedTransferEmission, 64)
	var service *Transfer
	service = NewWithOptions(Options{
		SyncWindowProgress: func(*WindowProgress) {
			emissions <- recordedTransferEmission{at: time.Now(), transfers: service.List()}
		},
	})
	service.emitEvery = interval
	t.Cleanup(func() {
		service.mu.Lock()
		service.emitStopped = true
		service.cancelProgressEmitLocked()
		service.mu.Unlock()
		service.emit()
	})
	return service, emissions
}

func awaitTransferEmission(t *testing.T, emissions <-chan recordedTransferEmission) recordedTransferEmission {
	t.Helper()
	select {
	case emission := <-emissions:
		return emission
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for transfer emission")
		return recordedTransferEmission{}
	}
}

func assertNoTransferEmission(t *testing.T, emissions <-chan recordedTransferEmission, wait time.Duration) {
	t.Helper()
	select {
	case emission := <-emissions:
		t.Fatalf("unexpected transfer emission: %#v", emission.transfers)
	case <-time.After(wait):
	}
}

func emittedTransfer(t *testing.T, emission recordedTransferEmission, pid string) Snapshot {
	t.Helper()
	for _, item := range emission.transfers {
		if item.PID == pid {
			return item
		}
	}
	t.Fatalf("transfer %q missing from emission: %#v", pid, emission.transfers)
	return Snapshot{}
}

func TestProgressEmitDeliversLatestTrailingSnapshot(t *testing.T) {
	const interval = 80 * time.Millisecond
	service, emissions := newRecordingTransfer(t, interval)
	createTestTransfer(t, service, "trailing", StatusProgress, true)
	awaitTransferEmission(t, emissions)

	transferred := int64(10)
	if err := service.Update("trailing", Updates{TransferredSize: &transferred}); err != nil {
		t.Fatal(err)
	}
	if got := emittedTransfer(t, awaitTransferEmission(t, emissions), "trailing"); got.TransferredSize != 10 {
		t.Fatalf("leading transfer = %#v", got)
	}

	time.Sleep(5 * time.Millisecond)
	transferred = 20
	if err := service.Update("trailing", Updates{TransferredSize: &transferred}); err != nil {
		t.Fatal(err)
	}
	transferred = 30
	if err := service.Update("trailing", Updates{TransferredSize: &transferred}); err != nil {
		t.Fatal(err)
	}
	assertNoTransferEmission(t, emissions, 10*time.Millisecond)

	got := emittedTransfer(t, awaitTransferEmission(t, emissions), "trailing")
	if got.TransferredSize != 30 || got.Progress != 30 || got.Speed <= 0 || got.ETA <= 0 {
		t.Fatalf("trailing transfer = %#v", got)
	}
	assertNoTransferEmission(t, emissions, interval+20*time.Millisecond)
}

func TestProgressEmitCoalescesTransfersIntoOneSnapshot(t *testing.T) {
	const interval = 60 * time.Millisecond
	service, emissions := newRecordingTransfer(t, interval)
	createTestTransfer(t, service, "first", StatusProgress, true)
	awaitTransferEmission(t, emissions)
	createTestTransfer(t, service, "second", StatusProgress, true)
	awaitTransferEmission(t, emissions)

	firstBytes := int64(10)
	if err := service.Update("first", Updates{TransferredSize: &firstBytes}); err != nil {
		t.Fatal(err)
	}
	awaitTransferEmission(t, emissions)
	firstBytes = 25
	if err := service.Update("first", Updates{TransferredSize: &firstBytes}); err != nil {
		t.Fatal(err)
	}
	secondBytes := int64(40)
	if err := service.Update("second", Updates{TransferredSize: &secondBytes}); err != nil {
		t.Fatal(err)
	}
	assertNoTransferEmission(t, emissions, 10*time.Millisecond)

	emission := awaitTransferEmission(t, emissions)
	if got := emittedTransfer(t, emission, "first"); got.TransferredSize != 25 {
		t.Fatalf("first transfer = %#v", got)
	}
	if got := emittedTransfer(t, emission, "second"); got.TransferredSize != 40 {
		t.Fatalf("second transfer = %#v", got)
	}
	assertNoTransferEmission(t, emissions, interval+20*time.Millisecond)
}

func TestProgressEmitMaintainsCadenceUnderContinuousUpdates(t *testing.T) {
	const interval = 40 * time.Millisecond
	service, emissions := newRecordingTransfer(t, interval)
	createTestTransfer(t, service, "continuous", StatusProgress, true)
	awaitTransferEmission(t, emissions)

	lastBytes := int64(1)
	if err := service.Update("continuous", Updates{TransferredSize: &lastBytes}); err != nil {
		t.Fatal(err)
	}
	first := awaitTransferEmission(t, emissions)
	recorded := []recordedTransferEmission{first}
	deadline := time.Now().Add(110 * time.Millisecond)
	for time.Now().Before(deadline) {
		lastBytes++
		if err := service.Update("continuous", Updates{TransferredSize: &lastBytes}); err != nil {
			t.Fatal(err)
		}
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(2 * interval)
	for {
		select {
		case emission := <-emissions:
			recorded = append(recorded, emission)
		default:
			goto drained
		}
	}

drained:
	if len(recorded) < 3 {
		t.Fatalf("continuous emissions = %d, want at least 3", len(recorded))
	}
	for index := 1; index < len(recorded); index++ {
		if elapsed := recorded[index].at.Sub(recorded[index-1].at); elapsed < interval-5*time.Millisecond {
			t.Fatalf("emission interval = %s, want at least %s", elapsed, interval-5*time.Millisecond)
		}
	}
	if got := emittedTransfer(t, recorded[len(recorded)-1], "continuous"); got.TransferredSize != lastBytes {
		t.Fatalf("last continuous transfer = %#v, want bytes %d", got, lastBytes)
	}
}

func TestImmediateStatusCancelsPendingProgressEmit(t *testing.T) {
	const interval = 60 * time.Millisecond
	service, emissions := newRecordingTransfer(t, interval)
	createTestTransfer(t, service, "paused", StatusProgress, true)
	awaitTransferEmission(t, emissions)

	transferred := int64(10)
	if err := service.Update("paused", Updates{TransferredSize: &transferred}); err != nil {
		t.Fatal(err)
	}
	awaitTransferEmission(t, emissions)
	transferred = 20
	if err := service.Update("paused", Updates{TransferredSize: &transferred}); err != nil {
		t.Fatal(err)
	}
	paused := StatusPaused
	if err := service.Update("paused", Updates{Status: &paused}); err != nil {
		t.Fatal(err)
	}
	got := emittedTransfer(t, awaitTransferEmission(t, emissions), "paused")
	if got.Status != StatusPaused || got.TransferredSize != 20 {
		t.Fatalf("paused transfer = %#v", got)
	}
	assertNoTransferEmission(t, emissions, 2*interval)
}

func TestRemovalDoesNotLeavePendingProgressEmit(t *testing.T) {
	const interval = 60 * time.Millisecond
	service, emissions := newRecordingTransfer(t, interval)
	createTestTransfer(t, service, "removed", StatusProgress, true)
	awaitTransferEmission(t, emissions)

	transferred := int64(10)
	if err := service.Update("removed", Updates{TransferredSize: &transferred}); err != nil {
		t.Fatal(err)
	}
	awaitTransferEmission(t, emissions)
	transferred = 20
	if err := service.Update("removed", Updates{TransferredSize: &transferred}); err != nil {
		t.Fatal(err)
	}
	completed := StatusCompleted
	hundred := 100.0
	if err := service.Update("removed", Updates{Status: &completed, Progress: &hundred}); err != nil {
		t.Fatal(err)
	}
	awaitTransferEmission(t, emissions)
	if err := service.Cancel("removed"); err != nil {
		t.Fatal(err)
	}
	if emission := awaitTransferEmission(t, emissions); len(emission.transfers) != 0 {
		t.Fatalf("removed emission = %#v", emission.transfers)
	}
	assertNoTransferEmission(t, emissions, 2*interval)
}

func TestShutdownCancelsPendingProgressEmit(t *testing.T) {
	const interval = 60 * time.Millisecond
	service, emissions := newRecordingTransfer(t, interval)
	createTestTransfer(t, service, "shutdown", StatusProgress, true)
	awaitTransferEmission(t, emissions)

	transferred := int64(10)
	if err := service.Update("shutdown", Updates{TransferredSize: &transferred}); err != nil {
		t.Fatal(err)
	}
	awaitTransferEmission(t, emissions)
	transferred = 20
	if err := service.Update("shutdown", Updates{TransferredSize: &transferred}); err != nil {
		t.Fatal(err)
	}
	if err := service.ServiceShutdown(); err != nil {
		t.Fatal(err)
	}
	awaitTransferEmission(t, emissions) // taskbar reset from ServiceShutdown
	assertNoTransferEmission(t, emissions, 2*interval)
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
