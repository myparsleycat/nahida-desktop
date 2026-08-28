//go:build windows

package xxmi

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestEnsureLauncherClosedTerminatesUntilAbsent(t *testing.T) {
	t.Parallel()
	pids := []int{41, 42, 0}
	var killed []int
	err := ensureLauncherClosedWith(
		context.Background(), time.Second, time.Millisecond,
		func(context.Context, string) (int, error) {
			pid := pids[0]
			pids = pids[1:]
			return pid, nil
		},
		func(pid int) error {
			killed = append(killed, pid)
			return nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(killed) != 2 || killed[0] != 41 || killed[1] != 42 {
		t.Fatalf("killed PIDs = %v", killed)
	}
}

func TestEnsureLauncherClosedPreservesContractError(t *testing.T) {
	t.Parallel()
	err := ensureLauncherClosedWith(
		context.Background(), time.Second, time.Millisecond,
		func(context.Context, string) (int, error) { return 41, nil },
		func(int) error { return errors.New("access denied") },
	)
	if err == nil || err.Error() != "failed to close XXMI Launcher" {
		t.Fatalf("error = %v", err)
	}
}
