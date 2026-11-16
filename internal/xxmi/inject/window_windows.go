//go:build windows

package inject

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var errWindowTimeout = errors.New("XXMI_GAME_START_TIMEOUT: game window did not appear")

var (
	windowUser32        = syscall.NewLazyDLL("user32.dll")
	windowEnum          = windowUser32.NewProc("EnumWindows")
	windowProcessID     = windowUser32.NewProc("GetWindowThreadProcessId")
	windowVisible       = windowUser32.NewProc("IsWindowVisible")
	windowIconic        = windowUser32.NewProc("IsIconic")
	windowCallback      = syscall.NewCallback(findVisibleWindow)
	windowSearches      sync.Map
	windowSearchCounter atomic.Uint64
)

type visibleWindowSearch struct {
	pid   uint32
	found bool
}

func waitForVisibleWindow(ctx context.Context, pid int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if hasVisibleWindow(pid) {
			return nil
		}
		if !time.Now().Before(deadline) {
			return errWindowTimeout
		}
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

// waitForWindow waits for the game window. A window that is late does not fail a launch whose game still runs
// once injected reports the DLLs are in it; late is then true instead.
func waitForWindow(ctx context.Context, pid int, timeout time.Duration, injected func() bool) (late bool, err error) {
	err = waitForVisibleWindow(ctx, pid, timeout)
	if errors.Is(err, errWindowTimeout) && injected() && processRunning(pid) {
		return true, nil
	}
	return false, err
}

func alwaysInjected() bool { return true }

func processRunning(pid int) bool {
	process, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		// Windows reports ERROR_INVALID_PARAMETER for a PID that no longer exists; access denied, as an
		// anti-cheat driver may cause, still means the process is running.
		return !errors.Is(err, windows.ERROR_INVALID_PARAMETER)
	}
	defer func() { _ = windows.CloseHandle(process) }()
	event, err := windows.WaitForSingleObject(process, 0)
	return err != nil || event == uint32(windows.WAIT_TIMEOUT)
}

func hasVisibleWindow(pid int) bool {
	search := &visibleWindowSearch{pid: uint32(pid)}
	id := windowSearchCounter.Add(1)
	windowSearches.Store(id, search)
	defer windowSearches.Delete(id)
	_, _, _ = windowEnum.Call(windowCallback, uintptr(id))
	return search.found
}

func findVisibleWindow(hwnd, lparam uintptr) uintptr {
	value, ok := windowSearches.Load(uint64(lparam))
	if !ok {
		return 0
	}
	search := value.(*visibleWindowSearch)
	var pid uint32
	_, _, _ = windowProcessID.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	if pid != search.pid {
		return 1
	}
	visible, _, _ := windowVisible.Call(hwnd)
	iconic, _, _ := windowIconic.Call(hwnd)
	if visible != 0 && iconic == 0 {
		search.found = true
		return 0
	}
	return 1
}
