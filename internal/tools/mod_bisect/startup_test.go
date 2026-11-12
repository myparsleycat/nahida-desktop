package modbisect

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"nahida.live/desktop/internal/db"
)

func TestListBisectOrphansSkipsUnaddressableFolders(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	orphan := filepath.Join(root, "mod.ini")
	if err := os.WriteFile(bisectDisabledPath(orphan), []byte("pending"), 0o600); err != nil {
		t.Fatal(err)
	}
	extended := `\\?\` + root
	if err := os.Mkdir(extended+`\Loading...`, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(extended) })

	paths, err := listBisectOrphans(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 1 || paths[0] != orphan {
		t.Fatalf("orphans = %v", paths)
	}
}

func TestCancelledBisectRecoveryLeavesOrphansForNextLaunch(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	service, _, _ := newBisectTestService(t, root)
	original := filepath.Join(root, "mod.ini")
	disabled := bisectDisabledPath(original)
	if err := os.WriteFile(disabled, []byte("preserve"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := listBisectOrphans(ctx, root); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled directory walk = %v", err)
	}
	if restored := service.recoverINIs(ctx, []string{original}); restored != 0 {
		t.Fatal("cancelled recovery changed files")
	}
	if _, err := os.Stat(disabled); err != nil {
		t.Fatal("cancelled recovery lost the pending INI")
	}
	if err := service.RecoverBisects(context.Background()); err != nil {
		t.Fatal(err)
	}
	if content, err := os.ReadFile(original); err != nil || string(content) != "preserve" {
		t.Fatalf("next launch did not restore INI: %q, %v", content, err)
	}
}

func TestRecoverBisectsRestoresMarkedSessionAfterCrash(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	writeBisectINIs(t, root, 4)
	crashed, client, _ := newBisectTestService(t, root)
	if err := client.AppState.Upsert(ctx, legacyScanDoneKey, "1", appStateTimestamp()); err != nil {
		t.Fatal(err)
	}
	started, err := crashed.BisectStart(ctx, "test", nil)
	if err != nil {
		t.Fatal(err)
	}

	next := NewWithOptions(Options{})
	next.UseClient(client)
	if err := next.RecoverBisects(ctx); err != nil {
		t.Fatal(err)
	}
	for _, path := range started.CurrentBatch {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("crashed session INI not restored: %s: %v", path, err)
		}
	}
	assertNoSessionMarker(t, next)
}

func TestRecoverBisectsSurvivesMissingModFolder(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	writeBisectINIs(t, root, 4)
	crashed, client, _ := newBisectTestService(t, root)
	missing := filepath.Join(t.TempDir(), "unplugged")
	if err := client.GamePaths.Insert(ctx, db.GamePathRow{Game: "gone", ModFolderPath: missing}); err != nil {
		t.Fatal(err)
	}
	if err := crashed.writeSessionMarker(ctx, "gone", missing); err != nil {
		t.Fatal(err)
	}
	started, err := crashed.BisectStart(ctx, "test", nil)
	if err != nil {
		t.Fatal(err)
	}

	next := NewWithOptions(Options{})
	next.UseClient(client)
	if err := next.RecoverBisects(ctx); err != nil {
		t.Fatal(err)
	}
	for _, path := range started.CurrentBatch {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("INI not restored past a missing folder: %s: %v", path, err)
		}
	}
	if done, err := client.AppState.GetValue(ctx, legacyScanDoneKey); err != nil || done == nil {
		t.Fatalf("legacy walk not recorded as done: %v, %v", done, err)
	}
	markers, err := next.sessionMarkers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(markers) != 1 || markers[0].game != "gone" {
		t.Fatalf("markers = %#v, want only the unreachable root kept", markers)
	}
}

func assertNoSessionMarker(t *testing.T, service *Service) {
	t.Helper()
	markers, err := service.sessionMarkers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(markers) != 0 {
		t.Fatalf("session markers left: %#v", markers)
	}
}

func TestBisectStartRejectedDuringRecovery(t *testing.T) {
	t.Parallel()
	service, _, _ := newBisectTestService(t, t.TempDir())
	service.bisectRecovering = true
	if _, err := service.BisectStart(context.Background(), "test", nil); err == nil {
		t.Fatal("bisect started while recovery was marked in progress")
	}
}
