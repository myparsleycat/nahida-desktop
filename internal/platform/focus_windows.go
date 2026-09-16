//go:build windows

package platform

import (
	"path/filepath"
	"slices"
	"time"
	"unsafe"

	"github.com/samber/lo"
	"golang.org/x/sys/windows"
)

var (
	modUser32                      = windows.NewLazySystemDLL("user32.dll")
	modKernel32                    = windows.NewLazySystemDLL("kernel32.dll")
	procGetForegroundWindow        = modUser32.NewProc("GetForegroundWindow")
	procGetWindowThreadProcessId   = modUser32.NewProc("GetWindowThreadProcessId")
	procQueryFullProcessImageNameW = modKernel32.NewProc("QueryFullProcessImageNameW")
)

// PROCESS_NAME_WIN32 is zero in the Windows SDK. Keep the named constant here
// because the Electron Rust implementation passes that symbolic value too.
const processNameWin32 uintptr = 0

func (n *Native) StartFocusTracking() {
	if n == nil {
		return
	}
	n.focus.mu.Lock()
	if n.focus.started {
		n.focus.mu.Unlock()
		return
	}
	n.focus.started = true
	n.focus.mu.Unlock()
	go func() {
		for {
			hwnd, _, _ := procGetForegroundWindow.Call()
			if hwnd != 0 {
				var pid uint32
				_, _, _ = procGetWindowThreadProcessId.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
				if pid != 0 {
					n.focus.push(pid)
				}
			}
			time.Sleep(100 * time.Millisecond)
		}
	}()
}

func (t *focusTracker) push(pid uint32) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.history) > 0 && t.history[len(t.history)-1] == pid {
		return
	}
	t.history = append(t.history, pid)
	if len(t.history) > 30 {
		t.history = t.history[len(t.history)-30:]
	}
}

func (n *Native) PreviousPIDs(currentPID uint32) []uint32 {
	if n == nil {
		return nil
	}
	n.focus.mu.Lock()
	defer n.focus.mu.Unlock()
	reversed := slices.Clone(n.focus.history)
	slices.Reverse(reversed)

	return lo.Take(lo.Uniq(lo.Filter(reversed, func(pid uint32, _ int) bool {
		return pid != currentPID
	})), 5)
}

func (n *Native) ProcessName(pid uint32) string {
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return ""
	}
	defer func() { _ = windows.CloseHandle(handle) }()
	var buffer [1024]uint16
	size := uint32(len(buffer))
	r1, _, _ := procQueryFullProcessImageNameW.Call(
		uintptr(handle),
		processNameWin32,
		uintptr(unsafe.Pointer(&buffer[0])),
		uintptr(unsafe.Pointer(&size)),
	)
	if r1 == 0 {
		return ""
	}
	full := windows.UTF16ToString(buffer[:size])
	return filepath.Base(full)
}
