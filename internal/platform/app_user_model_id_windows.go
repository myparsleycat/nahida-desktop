//go:build windows

package platform

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

var setCurrentProcessExplicitAppUserModelID = windows.NewLazySystemDLL("shell32.dll").NewProc("SetCurrentProcessExplicitAppUserModelID")

// SetAppUserModelID assigns the explicit Windows taskbar and notification
// identity for this process. It must be called before the first window is
// created.
func SetAppUserModelID(id string) error {
	value, err := windows.UTF16PtrFromString(id)
	if err != nil {
		return err
	}

	hresult, _, _ := setCurrentProcessExplicitAppUserModelID.Call(uintptr(unsafe.Pointer(value)))
	if int32(hresult) < 0 {
		return fmt.Errorf("SetCurrentProcessExplicitAppUserModelID failed: HRESULT 0x%08X", uint32(hresult))
	}
	return nil
}
