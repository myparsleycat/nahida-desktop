package xxmi

import (
	"context"
	"errors"
	"testing"
	"time"
)

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
