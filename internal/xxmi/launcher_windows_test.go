//go:build windows

package xxmi

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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

func TestFindProcessPIDMatchesExactExecutable(t *testing.T) {
	t.Parallel()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	pid, err := findProcessPID(context.Background(), executable)
	if err != nil {
		t.Fatal(err)
	}
	if pid != os.Getpid() {
		t.Fatalf("pid = %d, want %d", pid, os.Getpid())
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
