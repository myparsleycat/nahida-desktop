//go:build windows

package xxmi

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
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

func TestFindProcessPIDFindsCurrentProcessCaseInsensitive(t *testing.T) {
	t.Parallel()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	imageName := strings.ToUpper(filepath.Base(executable))
	pid, err := findProcessPID(context.Background(), imageName)
	if err != nil {
		t.Fatal(err)
	}
	if pid <= 0 {
		t.Fatalf("pid = %d, want a running process", pid)
	}
}

func TestFindProcessPIDReturnsZeroForUnknownImage(t *testing.T) {
	t.Parallel()
	pid, err := findProcessPID(context.Background(), "nahida-desktop-missing-process.exe")
	if err != nil {
		t.Fatal(err)
	}
	if pid != 0 {
		t.Fatalf("pid = %d, want 0", pid)
	}
}

func TestFindProcessPIDHonorsContextCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	pid, err := findProcessPID(ctx, "nahida-desktop-missing-process.exe")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if pid != 0 {
		t.Fatalf("pid = %d, want 0", pid)
	}
}
