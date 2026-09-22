package xxmi

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestConfiguredGameExecutableUsesAbsoluteGamePath(t *testing.T) {
	t.Parallel()
	folder := t.TempDir()
	second := filepath.Join(folder, "GameBeta.exe")
	if err := os.WriteFile(second, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	got := configuredGameExecutable(folder, []string{"Game.exe", "GameBeta.exe"})
	if got != second {
		t.Fatalf("configuredGameExecutable = %q, want %q", got, second)
	}
}

func TestConfiguredGameExecutableFallsBackToBasenameWithoutAbsoluteFolder(t *testing.T) {
	t.Parallel()
	if got := configuredGameExecutable("", []string{"Game.exe"}); got != "" {
		t.Fatalf("configuredGameExecutable = %q, want empty", got)
	}
}

func TestWaitForVisibleProcessRejectsHeadlessProcessUntilWindowAppears(t *testing.T) {
	t.Parallel()
	windowChecks := 0
	pid, err := waitForVisibleProcessWith(
		context.Background(),
		"Game.exe",
		time.Second,
		time.Millisecond,
		func(context.Context, string) (int, error) { return 4242, nil },
		func(gotPID int) bool {
			if gotPID != 4242 {
				t.Fatalf("window check pid = %d", gotPID)
			}
			windowChecks++
			return windowChecks >= 2
		},
	)
	if err != nil || pid != 4242 {
		t.Fatalf("waitForVisibleProcessWith = %d, %v", pid, err)
	}
	if windowChecks != 2 {
		t.Fatalf("window checks = %d, want 2", windowChecks)
	}
}

func TestWaitForVisibleProcessHonorsContextCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := waitForVisibleProcessWith(
		ctx,
		"Game.exe",
		time.Second,
		time.Millisecond,
		func(context.Context, string) (int, error) { return 0, nil },
		func(int) bool { return false },
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("waitForVisibleProcessWith error = %v, want context.Canceled", err)
	}
}
