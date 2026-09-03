package tools

import (
	"context"
	"os"
	"path/filepath"
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
		if err := service.ServiceShutdown(); err != nil {
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
		if err := service.ServiceShutdown(); err != nil {
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
		if err := service.ServiceShutdown(); err != nil {
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
		t.Fatalf("expected one retained inspection, got result=%+v snapshot=%+v", result, service.fixInspectionSnapshot())
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
	service.fixInspectors = NewFixInspectorRegistry()
	service.fixInspectors.Register(inspector)
	t.Cleanup(func() {
		if err := service.ServiceShutdown(); err != nil {
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

func TestShutdownFixInspectionsCancelsActiveRefresh(t *testing.T) {
	inspector := &blockingFixInspector{started: make(chan struct{}), stopped: make(chan struct{})}
	service := New()
	service.fixInspectors = NewFixInspectorRegistry()
	service.fixInspectors.Register(inspector)

	target := t.TempDir()
	key := fixInspectionKey(target)
	service.fixInspections[key] = &trackedFixInspection{record: FixInspectionRecord{
		ModPath: target,
		Result:  FixInspectionResult{Importer: "BLOCK"},
	}}
	service.queueFixInspectionRefresh(key)
	<-inspector.started

	shutdown := make(chan error, 1)
	go func() { shutdown <- service.ServiceShutdown() }()
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
		if err := service.ServiceShutdown(); err != nil {
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

	if err := first.ServiceShutdown(); err != nil {
		t.Fatal(err)
	}
	if err := second.ServiceShutdown(); err != nil {
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

func newMarkerInspectionService() *Tools {
	service := New()
	service.fixInspectors = NewFixInspectorRegistry()
	service.fixInspectors.Register(&markerFixInspector{})
	return service
}

func waitForFixInspectionCount(t *testing.T, service *Tools, expected int) {
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

func waitForFixInspectionPath(t *testing.T, service *Tools, expected string) {
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
