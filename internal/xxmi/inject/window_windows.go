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
)

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
			return errors.New("XXMI_GAME_START_TIMEOUT: game window did not appear")
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
