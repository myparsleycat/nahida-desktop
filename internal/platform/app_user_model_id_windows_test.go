//go:build windows

package platform

import (
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	getCurrentProcessExplicitAppUserModelID = windows.NewLazySystemDLL("shell32.dll").NewProc("GetCurrentProcessExplicitAppUserModelID")
	coTaskMemFree                           = windows.NewLazySystemDLL("ole32.dll").NewProc("CoTaskMemFree")
)

func TestSetAppUserModelID(t *testing.T) {
	const want = "com.nahida.test"
	if err := SetAppUserModelID(want); err != nil {
		t.Fatalf("SetAppUserModelID: %v", err)
	}

	var value *uint16
	hresult, _, _ := getCurrentProcessExplicitAppUserModelID.Call(uintptr(unsafe.Pointer(&value)))
	if int32(hresult) < 0 {
		t.Fatalf("GetCurrentProcessExplicitAppUserModelID: HRESULT 0x%08X", uint32(hresult))
	}
	if value == nil {
		t.Fatal("GetCurrentProcessExplicitAppUserModelID returned nil")
	}
	defer func() {
		_, _, _ = coTaskMemFree.Call(uintptr(unsafe.Pointer(value)))
	}()

	if got := windows.UTF16PtrToString(value); got != want {
		t.Fatalf("AppUserModelID = %q, want %q", got, want)
	}
}
