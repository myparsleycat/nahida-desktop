//go:build windows

package platform

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	shcneUpdateItem = 0x00002000
	shcnfPathW      = 0x0005
)

var shellChangeNotify = windows.NewLazySystemDLL("shell32.dll").NewProc("SHChangeNotify")

// NotifyShellItemUpdated tells Explorer that the file at path changed, so a rewritten shortcut shows its new icon.
func NotifyShellItemUpdated(path string) {
	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return
	}
	_, _, _ = shellChangeNotify.Call(shcneUpdateItem, shcnfPathW, uintptr(unsafe.Pointer(pathPtr)), 0)
}
