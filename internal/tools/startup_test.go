package tools

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

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

func TestCancelledTempCleanupPreservesPendingDirectory(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	stale := filepath.Join(root, legacyModelViewerTempPrefix+"old")
	if err := os.Mkdir(stale, 0o700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := cleanupStaleModelViewerDirs(ctx, root); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled cleanup = %v", err)
	}
	if _, err := os.Stat(stale); err != nil {
		t.Fatal("cancelled cleanup removed a directory")
	}
}
