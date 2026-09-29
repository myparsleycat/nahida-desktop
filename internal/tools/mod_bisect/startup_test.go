package modbisect

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
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

func TestBisectStartRejectedDuringRecovery(t *testing.T) {
	t.Parallel()
	service, _, _ := newBisectTestService(t, t.TempDir())
	service.bisectRecovering = true
	if _, err := service.BisectStart(context.Background(), "test", nil); err == nil {
		t.Fatal("bisect started while recovery was marked in progress")
	}
}
