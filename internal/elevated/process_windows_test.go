//go:build windows

package elevated

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestHelperStartupErrorPreservesCleanupFailureAfterCancellation(t *testing.T) {
	t.Parallel()

	err := helperStartupError(context.Canceled, windows.ERROR_ACCESS_DENIED)
	if !errors.Is(err, windows.ERROR_ACCESS_DENIED) || errors.Is(err, context.Canceled) {
		t.Fatalf("cleanup failure would be suppressed as cancellation: %v", err)
	}
	if err := helperStartupError(context.Canceled, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("successful cleanup lost cancellation: %v", err)
	}
}

func TestHelperInstanceRejectsDuplicateAndReleases(t *testing.T) {
	t.Parallel()

	identity := "test-" + strconv.Itoa(os.Getpid()) + "-" + t.Name()
	if err := checkHelperInstance(identity); err != nil {
		t.Fatal(err)
	}
	first, err := lockHelperInstance(identity)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if first != 0 {
			_ = windows.CloseHandle(first)
		}
	})
	if err := checkHelperInstance(identity); !errors.Is(err, errHelperAlreadyRunning) {
		t.Fatalf("checkHelperInstance = %v, want already running", err)
	}
	if second, err := lockHelperInstance(identity); second != 0 || !errors.Is(err, errHelperAlreadyRunning) {
		t.Fatalf("duplicate lock = %v, %v, want already running", second, err)
	}
	if err := windows.CloseHandle(first); err != nil {
		t.Fatal(err)
	}
	first = 0
	if err := checkHelperInstance(identity); err != nil {
		t.Fatalf("released instance is still reserved: %v", err)
	}
	replacement, err := lockHelperInstance(identity)
	if err != nil {
		t.Fatalf("replacement lock: %v", err)
	}
	_ = windows.CloseHandle(replacement)
}

func TestCloseLockedTerminatesProcessBeforeClearingState(t *testing.T) {
	t.Parallel()

	process, pid := startTestHelperProcess(t, windows.SYNCHRONIZE|windows.PROCESS_TERMINATE)
	observer, err := duplicateHandle(process)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = windows.CloseHandle(observer) }()
	client := &Client{conn: failingConn{}, process: process, pid: pid, secret: "secret"}
	if err := client.closeLocked(); err != nil {
		t.Fatal(err)
	}
	if client.conn != nil || client.process != 0 || client.pid != 0 || client.secret != "" {
		t.Fatalf("client retained state after successful teardown: %+v", client)
	}
	if event, err := windows.WaitForSingleObject(observer, 0); err != nil || event != windows.WAIT_OBJECT_0 {
		t.Fatalf("helper is still alive after teardown: %d, %v", event, err)
	}
}

func TestFailedTerminationRetainsProcessAndBlocksStart(t *testing.T) {
	t.Parallel()

	// A handle with no terminate access models an elevated helper the parent
	// cannot kill. A replacement must never be launched in this state.
	process, pid := startTestHelperProcess(t, windows.SYNCHRONIZE)
	client := NewClient()
	client.conn, client.process, client.pid, client.secret = failingConn{}, process, pid, "secret"
	defer func() {
		if client.process != 0 {
			_ = windows.CloseHandle(client.process)
		}
	}()
	if err := client.Close(); !errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		t.Fatalf("Close = %v, want access denied", err)
	}
	if client.conn != nil || client.process != process || client.pid != pid || client.secret != "" {
		t.Fatalf("failed teardown lost process ownership: %+v", client)
	}
	if err := client.Start(t.Context()); !errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		t.Fatalf("Start = %v, want previous process cleanup failure", err)
	}
	if client.process != process || client.pid != pid {
		t.Fatal("Start discarded the previous process")
	}

	killer, err := windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_TERMINATE, false, pid)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = windows.CloseHandle(killer) }()
	if err := stopHelperProcess(killer); err != nil {
		t.Fatal(err)
	}
	if err := client.Close(); err != nil {
		t.Fatalf("cleanup did not recover after process exit: %v", err)
	}
	if client.process != 0 || client.pid != 0 {
		t.Fatal("cleanup retained an exited process")
	}
}

func TestCloseCleansDisconnectedProcess(t *testing.T) {
	t.Parallel()

	process, pid := startTestHelperProcess(t, windows.SYNCHRONIZE|windows.PROCESS_TERMINATE)
	client := NewClient()
	client.process, client.pid = process, pid
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	if client.process != 0 || client.pid != 0 {
		t.Fatal("Close retained a disconnected process")
	}
}

func TestStopHelperProcessAllowsCleanupBeforeExit(t *testing.T) {
	t.Parallel()

	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(t.TempDir(), "cleanup-completed")
	command := exec.Command(executable, "-test.run=^TestElevatedProcessChild$")
	command.Env = append(os.Environ(), "NAHIDA_ELEVATED_PROCESS_TEST=cleanup", "NAHIDA_ELEVATED_CLEANUP_MARKER="+marker)
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = command.Process.Kill()
		_ = command.Wait()
	})
	process, err := windows.OpenProcess(
		windows.SYNCHRONIZE|windows.PROCESS_TERMINATE,
		false,
		uint32(command.Process.Pid),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = windows.CloseHandle(process) }()
	if err := stopHelperProcess(process); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("helper was terminated before cleanup completed: %v", err)
	}
}

func TestDialPipeStopsWhenHelperExits(t *testing.T) {
	t.Parallel()

	// A signaled event has the same wait result as an exited process.
	process, err := windows.CreateEvent(nil, 1, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = windows.CloseHandle(process) }()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if conn, err := dialPipeUntilReady(ctx, `\\.\pipe\nahida-exited-helper-test`, process); conn != nil || err == nil {
		t.Fatalf("dialPipeUntilReady = %v, %v, want exited helper error", conn, err)
	} else if errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("dialPipeUntilReady waited for the full connection timeout")
	}
}

func TestElevatedProcessChild(t *testing.T) {
	if os.Getenv("NAHIDA_ELEVATED_PROCESS_TEST") == "cleanup" {
		time.Sleep(250 * time.Millisecond)
		if err := os.WriteFile(os.Getenv("NAHIDA_ELEVATED_CLEANUP_MARKER"), []byte("done"), 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}
	if os.Getenv("NAHIDA_ELEVATED_PROCESS_TEST") != "1" {
		return
	}
	time.Sleep(5 * time.Minute)
}

func startTestHelperProcess(t *testing.T, access uint32) (windows.Handle, uint32) {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(executable, "-test.run=^TestElevatedProcessChild$")
	command.Env = append(os.Environ(), "NAHIDA_ELEVATED_PROCESS_TEST=1")
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = command.Process.Kill()
		_ = command.Wait()
	})
	pid := uint32(command.Process.Pid)
	process, err := windows.OpenProcess(access, false, pid)
	if err != nil {
		t.Fatal(err)
	}
	return process, pid
}
