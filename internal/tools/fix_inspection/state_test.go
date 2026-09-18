package fixinspection

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"nahida.live/desktop/internal/db"
	"nahida.live/desktop/internal/watcher"
)

type fixInspectionTestSettings struct {
	enabled bool
}

func (s fixInspectionTestSettings) GetAutoInspectFix(context.Context) (bool, error) {
	return s.enabled, nil
}

func (fixInspectionTestSettings) GetBisectPreserveD3dx(context.Context) (bool, error) {
	return false, nil
}

func (fixInspectionTestSettings) GetDisabledPrefixStyle(context.Context) (string, error) {
	return "space", nil
}

func TestQueueFixInspectionsResolvesImporterFromManagedPath(t *testing.T) {
	service := newMarkerInspectionService()
	service.UseClient(openToolsTestDB(t))
	service.settings = fixInspectionTestSettings{enabled: true}
	t.Cleanup(func() {
		if err := service.Shutdown(); err != nil {
			t.Errorf("shutdown tools service: %v", err)
		}
	})

	root := t.TempDir()
	importer := "TEST"
	if err := service.client.GamePaths.Insert(context.Background(), db.GamePathRow{
		Game:          "test",
		ModFolderPath: root,
		Importer:      &importer,
	}); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "NeedsFix")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "needs-fix"), []byte("pending"), 0o644); err != nil {
		t.Fatal(err)
	}

	service.QueueFixInspections([]string{target})
	waitForFixInspectionCount(t, service, 1)

	record := service.fixInspectionSnapshot().Inspections[0]
	if record.ModPath != target || record.Result.Importer != importer {
		t.Fatalf("queued inspection = %+v", record)
	}
}

func TestQueueFixInspectionsHonorsDisabledSetting(t *testing.T) {
	service := newMarkerInspectionService()
	service.UseClient(openToolsTestDB(t))
	service.settings = fixInspectionTestSettings{enabled: false}
	t.Cleanup(func() {
		if err := service.Shutdown(); err != nil {
			t.Errorf("shutdown tools service: %v", err)
		}
	})

	root := t.TempDir()
	importer := "TEST"
	if err := service.client.GamePaths.Insert(context.Background(), db.GamePathRow{
		Game:          "test",
		ModFolderPath: root,
		Importer:      &importer,
	}); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "NeedsFix")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "needs-fix"), []byte("pending"), 0o644); err != nil {
		t.Fatal(err)
	}

	service.QueueFixInspections([]string{target})
	service.fixInspectionWG.Wait()
	if snapshot := service.fixInspectionSnapshot(); len(snapshot.Inspections) != 0 {
		t.Fatalf("disabled inspection retained results: %+v", snapshot)
	}
}

func TestFixInspectionStateWatchesExternalChanges(t *testing.T) {
	service := newMarkerInspectionService()
	t.Cleanup(func() {
		if err := service.Shutdown(); err != nil {
			t.Errorf("shutdown tools service: %v", err)
		}
	})

	root := t.TempDir()
	target := filepath.Join(root, "NeedsFix")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(target, "needs-fix")
	if err := os.WriteFile(marker, []byte("pending"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := service.InspectModForFix(context.Background(), target, "TEST")
	if err != nil {
		t.Fatal(err)
	}
	if !result.NeedsFix || len(service.fixInspectionSnapshot().Inspections) != 1 {
		t.Fatalf(
			"expected one retained inspection, got result=%+v snapshot=%+v",
			result,
			service.fixInspectionSnapshot(),
		)
	}

	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}
	waitForFixInspectionCount(t, service, 0)

	second := filepath.Join(root, "MovedMod")
	if err := os.Mkdir(second, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(second, "needs-fix"), []byte("pending"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := service.InspectModForFix(context.Background(), second, "TEST"); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(second, filepath.Join(root, "MovedElsewhere")); err != nil {
		t.Fatal(err)
	}
	waitForFixInspectionCount(t, service, 0)

	removed := filepath.Join(root, "RemovedMod")
	if err := os.Mkdir(removed, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(removed, "needs-fix"), []byte("pending"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := service.InspectModForFix(context.Background(), removed, "TEST"); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(removed); err != nil {
		t.Fatal(err)
	}
	waitForFixInspectionCount(t, service, 0)
}

func TestInspectModForFixRunsInspectorOnce(t *testing.T) {
	inspector := &countingFixInspector{}
	service := New()
	service.inspectors = NewFixInspectorRegistry()
	service.inspectors.Register(inspector)
	t.Cleanup(func() {
		if err := service.Shutdown(); err != nil {
			t.Errorf("shutdown tools service: %v", err)
		}
	})

	target := t.TempDir()
	if _, err := service.InspectModForFix(context.Background(), target, "COUNT"); err != nil {
		t.Fatal(err)
	}
	if inspector.calls != 1 {
		t.Fatalf("inspector ran %d times, want 1", inspector.calls)
	}
}

func TestDiagnoseModForFixDoesNotTrack(t *testing.T) {
	service := newMarkerInspectionService()
	t.Cleanup(func() {
		if err := service.Shutdown(); err != nil {
			t.Errorf("shutdown tools service: %v", err)
		}
	})

	target := t.TempDir()
	if err := os.WriteFile(filepath.Join(target, "needs-fix"), []byte("pending"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := service.DiagnoseModForFix(context.Background(), target, "TEST")
	if err != nil {
		t.Fatal(err)
	}
	if !result.NeedsFix {
		t.Fatal("diagnosis did not report a needed fix")
	}
	if tracked := trackedFixInspectionCount(service); tracked != 0 {
		t.Fatalf("diagnosis tracked %d fix inspections", tracked)
	}
	if snapshot := service.fixInspectionSnapshot(); len(snapshot.Inspections) != 0 {
		t.Fatalf("diagnosis published a snapshot: %+v", snapshot)
	}
}

func TestShutdownFixInspectionsCancelsActiveRefresh(t *testing.T) {
	inspector := &blockingFixInspector{started: make(chan struct{}), stopped: make(chan struct{})}
	service := New()
	service.inspectors = NewFixInspectorRegistry()
	service.inspectors.Register(inspector)

	target := t.TempDir()
	key := fixInspectionKey(target)
	service.fixInspections[key] = &trackedFixInspection{record: FixInspectionRecord{
		ModPath: target,
		Result:  FixInspectionResult{Importer: "BLOCK"},
	}}
	service.queueFixInspectionRefresh(key)
	<-inspector.started

	shutdown := make(chan error, 1)
	go func() { shutdown <- service.Shutdown() }()
	select {
	case err := <-shutdown:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("service shutdown did not cancel the active inspector")
	}
	select {
	case <-inspector.stopped:
	default:
		t.Fatal("active inspector did not observe lifecycle cancellation")
	}
}

func TestFixInspectionStateFollowsDisabledFolderRenames(t *testing.T) {
	service := newMarkerInspectionService()
	t.Cleanup(func() {
		if err := service.Shutdown(); err != nil {
			t.Errorf("shutdown tools service: %v", err)
		}
	})

	root := t.TempDir()
	active := filepath.Join(root, "[LL] Cunning Soldier Anby - Skimpy Ver")
	if err := os.Mkdir(active, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(active, "needs-fix"), []byte("pending"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := service.InspectModForFix(context.Background(), active, "TEST"); err != nil {
		t.Fatal(err)
	}
	disabledSpace := filepath.Join(root, "DISABLED [LL] Cunning Soldier Anby - Skimpy Ver")
	if err := os.Rename(active, disabledSpace); err != nil {
		t.Fatal(err)
	}
	waitForFixInspectionPath(t, service, disabledSpace)

	if err := os.Rename(disabledSpace, active); err != nil {
		t.Fatal(err)
	}
	waitForFixInspectionPath(t, service, active)

	disabledUnderscore := filepath.Join(root, "DISABLED_[LL] Cunning Soldier Anby - Skimpy Ver")
	if err := os.Rename(active, disabledUnderscore); err != nil {
		t.Fatal(err)
	}
	waitForFixInspectionPath(t, service, disabledUnderscore)

	if err := os.Remove(filepath.Join(disabledUnderscore, "needs-fix")); err != nil {
		t.Fatal(err)
	}
	waitForFixInspectionCount(t, service, 0)
}

func TestDismissalSurvivesDisabledFolderRename(t *testing.T) {
	service := newMarkerInspectionService()
	t.Cleanup(func() {
		if err := service.Shutdown(); err != nil {
			t.Errorf("shutdown tools service: %v", err)
		}
	})

	root := t.TempDir()
	active := filepath.Join(root, "ModA")
	if err := os.Mkdir(active, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(active, "needs-fix"), []byte("pending"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := service.InspectModForFix(context.Background(), active, "TEST"); err != nil {
		t.Fatal(err)
	}
	waitForFixInspectionCount(t, service, 1)

	service.DismissFixInspection(active)
	waitForFixInspectionCount(t, service, 0)

	disabled := filepath.Join(root, "DISABLED ModA")
	if err := os.Rename(active, disabled); err != nil {
		t.Fatal(err)
	}
	waitForTrackedFixInspectionPath(t, service, disabled)

	// A rename re-keys the record without changing the problem, so the warning stays hidden.
	if visible := len(service.fixInspectionSnapshot().Inspections); visible != 0 {
		t.Fatalf("dismissed inspection became visible again: %d", visible)
	}
}

func TestRenameMigrationCarriesDismissalRecordedDuringRefresh(t *testing.T) {
	service := newMarkerInspectionService()
	t.Cleanup(func() {
		if err := service.Shutdown(); err != nil {
			t.Errorf("shutdown tools service: %v", err)
		}
	})

	root := t.TempDir()
	previous := filepath.Join(root, "ModA")
	renamed := filepath.Join(root, "DISABLED ModA")
	if err := os.Mkdir(renamed, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(renamed, "needs-fix"), []byte("pending"), 0o644); err != nil {
		t.Fatal(err)
	}
	record := FixInspectionRecord{
		ModPath:     previous,
		DisplayName: filepath.Base(previous),
		Result: FixInspectionResult{
			NeedsFix:   true,
			Importer:   "TEST",
			ToolName:   "Test Fixer",
			Summary:    "marker requires a fix",
			ActionTool: "marker",
		},
	}
	service.fixInspections[fixInspectionKey(previous)] = &trackedFixInspection{record: record}

	// The dismissal lands after the refresh snapshotted the record, so the migration has to move the
	// latest dismissal state rather than the value the refresh read earlier.
	service.DismissFixInspection(previous)
	migrated, stopped := service.migrateFixInspectionRename(record, renamed)
	if len(stopped) != 1 {
		t.Fatalf("migration returned %d watchers, want one", len(stopped))
	}
	if !watcher.SamePath(migrated.ModPath, renamed) {
		t.Fatalf("migration moved the record to %q, want %q", migrated.ModPath, renamed)
	}

	if snapshot := service.fixInspectionSnapshot(); len(snapshot.Inspections) != 0 {
		t.Fatalf("dismissed warning reappeared after the rename: %+v", snapshot.Inspections)
	}
	if path := trackedFixInspectionPath(service); !watcher.SamePath(path, renamed) {
		t.Fatalf("tracked rename target = %q, want %q", path, renamed)
	}
}

func TestDismissFixInspectionHidesWarning(t *testing.T) {
	service := newMarkerInspectionService()
	t.Cleanup(func() {
		if err := service.Shutdown(); err != nil {
			t.Errorf("shutdown tools service: %v", err)
		}
	})

	target := t.TempDir()
	marker := filepath.Join(target, "needs-fix")
	if err := os.WriteFile(marker, []byte("pending"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := service.InspectModForFix(context.Background(), target, "TEST"); err != nil {
		t.Fatal(err)
	}
	waitForFixInspectionCount(t, service, 1)

	service.DismissFixInspection(target)
	waitForFixInspectionCount(t, service, 0)

	dismissedRevision := service.fixInspectionSnapshot().Revision
	service.DismissFixInspection(target)
	service.DismissFixInspection(filepath.Join(target, "absent"))
	if revision := service.fixInspectionSnapshot().Revision; revision != dismissedRevision {
		t.Fatalf("repeat dismissal changed revision to %d, want %d", revision, dismissedRevision)
	}

	// Dismissal hides the warning without stopping the watch, so a fixed mod stops being tracked.
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}
	waitForTrackedFixInspectionCount(t, service, 0)
}

func TestDismissedFixInspectionRearmsOnChangedResult(t *testing.T) {
	service := newSummaryInspectionService()
	t.Cleanup(func() {
		if err := service.Shutdown(); err != nil {
			t.Errorf("shutdown tools service: %v", err)
		}
	})

	target := t.TempDir()
	summary := filepath.Join(target, "summary")
	if err := os.WriteFile(summary, []byte("first"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := service.InspectModForFix(context.Background(), target, "SUMMARY"); err != nil {
		t.Fatal(err)
	}
	waitForFixInspectionCount(t, service, 1)

	service.DismissFixInspection(target)
	waitForFixInspectionCount(t, service, 0)

	if err := os.WriteFile(summary, []byte("second"), 0o644); err != nil {
		t.Fatal(err)
	}
	waitForFixInspectionCount(t, service, 1)
	record := service.fixInspectionSnapshot().Inspections[0]
	if record.Result.Summary != "second" {
		t.Fatalf("rearmed inspection = %+v", record)
	}
}

func TestDismissedFixInspectionRearmsOnRestoredInspection(t *testing.T) {
	inspector := &mutableFixInspector{summary: "first"}
	service := New()
	service.inspectors = NewFixInspectorRegistry()
	service.inspectors.Register(inspector)
	t.Cleanup(func() {
		if err := service.Shutdown(); err != nil {
			t.Errorf("shutdown tools service: %v", err)
		}
	})

	target := t.TempDir()
	if _, err := service.InspectModForFix(context.Background(), target, "MUTABLE"); err != nil {
		t.Fatal(err)
	}
	waitForFixInspectionCount(t, service, 1)

	service.DismissFixInspection(target)
	waitForFixInspectionCount(t, service, 0)

	// The inspector reports a different problem without touching the watched folder.
	inspector.setSummary("second")
	if _, err := service.InspectModForFix(context.Background(), target, "MUTABLE"); err != nil {
		t.Fatal(err)
	}

	waitForFixInspectionCount(t, service, 1)
	record := service.fixInspectionSnapshot().Inspections[0]
	if record.Result.Summary != "second" {
		t.Fatalf("restored inspection = %+v", record)
	}
}

func TestDismissalAppliesAgainWhenTheSameResultReturns(t *testing.T) {
	inspector := &mutableFixInspector{summary: "first"}
	service := New()
	service.inspectors = NewFixInspectorRegistry()
	service.inspectors.Register(inspector)
	t.Cleanup(func() {
		if err := service.Shutdown(); err != nil {
			t.Errorf("shutdown tools service: %v", err)
		}
	})

	target := t.TempDir()
	if _, err := service.InspectModForFix(context.Background(), target, "MUTABLE"); err != nil {
		t.Fatal(err)
	}
	waitForFixInspectionCount(t, service, 1)

	service.DismissFixInspection(target)
	waitForFixInspectionCount(t, service, 0)

	inspector.setSummary("second")
	if _, err := service.InspectModForFix(context.Background(), target, "MUTABLE"); err != nil {
		t.Fatal(err)
	}
	waitForFixInspectionCount(t, service, 1)

	// The restored result is the same problem the user already closed, so it stays hidden.
	inspector.setSummary("first")
	if _, err := service.InspectModForFix(context.Background(), target, "MUTABLE"); err != nil {
		t.Fatal(err)
	}
	waitForFixInspectionCount(t, service, 0)
	if tracked := trackedFixInspectionCount(service); tracked != 1 {
		t.Fatalf("tracked inspections = %d, want 1", tracked)
	}
}

func TestDismissalDoesNotCarryOverToANewWarning(t *testing.T) {
	service := newMarkerInspectionService()
	t.Cleanup(func() {
		if err := service.Shutdown(); err != nil {
			t.Errorf("shutdown tools service: %v", err)
		}
	})

	target := t.TempDir()
	marker := filepath.Join(target, "needs-fix")
	if err := os.WriteFile(marker, []byte("pending"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := service.InspectModForFix(context.Background(), target, "TEST"); err != nil {
		t.Fatal(err)
	}
	waitForFixInspectionCount(t, service, 1)

	service.DismissFixInspection(target)
	waitForFixInspectionCount(t, service, 0)

	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}
	waitForTrackedFixInspectionCount(t, service, 0)

	if err := os.WriteFile(marker, []byte("pending"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := service.InspectModForFix(context.Background(), target, "TEST"); err != nil {
		t.Fatal(err)
	}
	waitForFixInspectionCount(t, service, 1)
}

func TestFixInspectionStateIsNotSharedWithNewService(t *testing.T) {
	first := newMarkerInspectionService()
	root := t.TempDir()
	target := filepath.Join(root, "NeedsFix")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "needs-fix"), []byte("pending"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := first.InspectModForFix(context.Background(), target, "TEST"); err != nil {
		t.Fatal(err)
	}
	if got := len(first.fixInspectionSnapshot().Inspections); got != 1 {
		t.Fatalf("expected first process state to contain one inspection, got %d", got)
	}

	second := newMarkerInspectionService()
	if got := len(second.fixInspectionSnapshot().Inspections); got != 0 {
		t.Fatalf("new service inherited %d in-memory inspections", got)
	}

	if err := first.Shutdown(); err != nil {
		t.Fatal(err)
	}
	if err := second.Shutdown(); err != nil {
		t.Fatal(err)
	}
}

type markerFixInspector struct{}

func (*markerFixInspector) CanInspect(importer string) bool { return importer == "TEST" }

func (*markerFixInspector) Inspect(_ context.Context, modPath string) (*FixInspectionResult, error) {
	_, err := os.Stat(filepath.Join(modPath, "needs-fix"))
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	return &FixInspectionResult{
		NeedsFix:   err == nil,
		Importer:   "TEST",
		ToolName:   "Test Fixer",
		Summary:    "marker requires a fix",
		ActionTool: "marker",
	}, nil
}

type summaryFixInspector struct{}

func (*summaryFixInspector) CanInspect(importer string) bool { return importer == "SUMMARY" }

func (*summaryFixInspector) Inspect(_ context.Context, modPath string) (*FixInspectionResult, error) {
	content, err := os.ReadFile(filepath.Join(modPath, "summary"))
	if err != nil {
		if os.IsNotExist(err) {
			return &FixInspectionResult{Importer: "SUMMARY", ToolName: "Summary Fixer"}, nil
		}
		return nil, err
	}
	return &FixInspectionResult{
		NeedsFix:   true,
		Importer:   "SUMMARY",
		ToolName:   "Summary Fixer",
		Summary:    string(content),
		ActionTool: "summary",
	}, nil
}

// mutableFixInspector reports a summary the test can change without touching the watched folder.
type mutableFixInspector struct {
	mu      sync.Mutex
	summary string
}

func (*mutableFixInspector) CanInspect(importer string) bool { return importer == "MUTABLE" }

func (i *mutableFixInspector) setSummary(summary string) {
	i.mu.Lock()
	i.summary = summary
	i.mu.Unlock()
}

func (i *mutableFixInspector) Inspect(_ context.Context, _ string) (*FixInspectionResult, error) {
	i.mu.Lock()
	summary := i.summary
	i.mu.Unlock()
	return &FixInspectionResult{
		NeedsFix:   true,
		Importer:   "MUTABLE",
		ToolName:   "Mutable Fixer",
		Summary:    summary,
		ActionTool: "mutable",
	}, nil
}

type countingFixInspector struct {
	calls int
}

func (*countingFixInspector) CanInspect(importer string) bool { return importer == "COUNT" }

func (i *countingFixInspector) Inspect(_ context.Context, _ string) (*FixInspectionResult, error) {
	i.calls++
	return &FixInspectionResult{
		NeedsFix: true,
		Importer: "COUNT",
		ToolName: "Counting Fixer",
	}, nil
}

type blockingFixInspector struct {
	started chan struct{}
	stopped chan struct{}
}

func (*blockingFixInspector) CanInspect(importer string) bool { return importer == "BLOCK" }

func (i *blockingFixInspector) Inspect(ctx context.Context, _ string) (*FixInspectionResult, error) {
	close(i.started)
	<-ctx.Done()
	close(i.stopped)
	return nil, ctx.Err()
}

func newMarkerInspectionService() *Service {
	service := New()
	service.inspectors = NewFixInspectorRegistry()
	service.inspectors.Register(&markerFixInspector{})
	return service
}

func newSummaryInspectionService() *Service {
	service := New()
	service.inspectors = NewFixInspectorRegistry()
	service.inspectors.Register(&summaryFixInspector{})
	return service
}

func waitForFixInspectionCount(t *testing.T, service *Service, expected int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if len(service.fixInspectionSnapshot().Inspections) == expected {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d fix inspections; snapshot=%+v", expected, service.fixInspectionSnapshot())
}

func waitForFixInspectionPath(t *testing.T, service *Service, expected string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		snapshot := service.fixInspectionSnapshot()
		if len(snapshot.Inspections) == 1 && watcher.SamePath(snapshot.Inspections[0].ModPath, expected) {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for fix inspection path %q; snapshot=%+v", expected, service.fixInspectionSnapshot())
}

func trackedFixInspectionCount(service *Service) int {
	service.fixInspectionMu.Lock()
	defer service.fixInspectionMu.Unlock()
	return len(service.fixInspections)
}

func trackedFixInspectionPath(service *Service) string {
	service.fixInspectionMu.Lock()
	defer service.fixInspectionMu.Unlock()
	for _, tracked := range service.fixInspections {
		return tracked.record.ModPath
	}
	return ""
}

func waitForTrackedFixInspectionPath(t *testing.T, service *Service, expected string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if watcher.SamePath(trackedFixInspectionPath(service), expected) {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for tracked fix inspection %q", expected)
}

func waitForTrackedFixInspectionCount(t *testing.T, service *Service, expected int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if trackedFixInspectionCount(service) == expected {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("timed out waiting for the watched fix inspection to be dropped")
}
