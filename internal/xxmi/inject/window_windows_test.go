//go:build windows

package inject

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestWindowlessChild(t *testing.T) {
	if os.Getenv("NAHIDA_WINDOWLESS_CHILD") != "1" {
		return
	}
	time.Sleep(30 * time.Second)
}

func TestLateWindowPassesOnlyForARunningInjectedGame(t *testing.T) {
	t.Parallel()
	child := exec.Command(os.Args[0], "-test.run=^TestWindowlessChild$")
	child.Env = append(os.Environ(), "NAHIDA_WINDOWLESS_CHILD=1")
	child.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NO_WINDOW}
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = child.Process.Kill()
		_ = child.Wait()
	}()
	ctx := context.Background()
	pid := child.Process.Pid

	if late, err := waitForWindow(ctx, pid, 50*time.Millisecond, alwaysInjected); err != nil || !late {
		t.Fatalf("running injected game: late = %t, err = %v; want a late window", late, err)
	}
	late, err := waitForWindow(ctx, pid, 50*time.Millisecond, func() bool { return false })
	if !errors.Is(err, errWindowTimeout) || late {
		t.Fatalf("game without its DLL: late = %t, err = %v; want the window timeout", late, err)
	}

	if err := child.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = child.Wait()
	late, err = waitForWindow(ctx, pid, 50*time.Millisecond, alwaysInjected)
	if !errors.Is(err, errWindowTimeout) || late {
		t.Fatalf("exited game: late = %t, err = %v; want the window timeout", late, err)
	}
}
