//go:build windows

package platform

const (
	swRestore     = 9
	hwndTopmost   = ^uintptr(0)
	hwndNoTopmost = ^uintptr(1)
	swpNoSize     = 0x0001
	swpNoMove     = 0x0002
)

var (
	procIsIconic            = modUser32.NewProc("IsIconic")
	procShowWindow          = modUser32.NewProc("ShowWindow")
	procAttachThreadInput   = modUser32.NewProc("AttachThreadInput")
	procBringWindowToTop    = modUser32.NewProc("BringWindowToTop")
	procSetForegroundWindow = modUser32.NewProc("SetForegroundWindow")
	procSetFocus            = modUser32.NewProc("SetFocus")
	procSetWindowPos        = modUser32.NewProc("SetWindowPos")
	procGetCurrentThreadId  = modKernel32.NewProc("GetCurrentThreadId")
)

// ForceForegroundWindow restores hwnd if it is minimized and tries to make it
// the foreground window. Call it on the window's UI thread. SetForegroundWindow
// alone is ignored when a second instance (for example a nahida:// protocol
// launch) notifies this process after the helper has already exited.
func ForceForegroundWindow(hwnd uintptr) {
	if hwnd == 0 {
		return
	}
	iconic, _, _ := procIsIconic.Call(hwnd)
	if iconic != 0 {
		_, _, _ = procShowWindow.Call(hwnd, swRestore)
	}

	fg, _, _ := procGetForegroundWindow.Call()
	if fg != 0 && fg != hwnd {
		fgThread, _, _ := procGetWindowThreadProcessId.Call(fg, 0)
		currentThread, _, _ := procGetCurrentThreadId.Call()
		if fgThread != 0 && currentThread != 0 && fgThread != currentThread {
			attached, _, _ := procAttachThreadInput.Call(fgThread, currentThread, 1)
			if attached != 0 {
				defer func() { _, _, _ = procAttachThreadInput.Call(fgThread, currentThread, 0) }()
			}
		}
	}

	_, _, _ = procBringWindowToTop.Call(hwnd)
	_, _, _ = procSetForegroundWindow.Call(hwnd)
	_, _, _ = procSetFocus.Call(hwnd)
	current, _, _ := procGetForegroundWindow.Call()
	if current == hwnd {
		return
	}

	flags := uintptr(swpNoMove | swpNoSize)
	_, _, _ = procSetWindowPos.Call(hwnd, hwndTopmost, 0, 0, 0, 0, flags)
	_, _, _ = procSetWindowPos.Call(hwnd, hwndNoTopmost, 0, 0, 0, 0, flags)
	_, _, _ = procSetForegroundWindow.Call(hwnd)
}
