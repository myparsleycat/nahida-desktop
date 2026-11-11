//go:build windows

package platform

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

var setCurrentProcessExplicitAppUserModelID = windows.NewLazySystemDLL("shell32.dll").
	NewProc("SetCurrentProcessExplicitAppUserModelID")

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

// RegisterAppUserModelID records id under HKCU\Software\Classes\AppUserModelId. Windows drops toasts from an
// ID that has neither this key nor a Start menu shortcut carrying it, and the installer's shortcut does not.
// iconPath is the image file the toasts show; empty leaves the default icon.
func RegisterAppUserModelID(id, displayName, iconPath string) error {
	key, _, err := registry.CreateKey(registry.CURRENT_USER, `Software\Classes\AppUserModelId\`+id, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer func() { _ = key.Close() }()
	if err := key.SetStringValue("DisplayName", displayName); err != nil {
		return err
	}
	if iconPath == "" {
		return nil
	}
	return key.SetStringValue("IconUri", iconPath)
}
