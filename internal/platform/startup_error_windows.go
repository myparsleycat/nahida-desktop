//go:build windows

package platform

import (
	"errors"
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	messageBoxIconError     = 0x00000010
	messageBoxSetForeground = 0x00010000
)

var messageBoxW = windows.NewLazySystemDLL("user32.dll").NewProc("MessageBoxW")

func showStartupError(title, message string) error {
	titlePtr, err := windows.UTF16PtrFromString(title)
	if err != nil {
		return fmt.Errorf("encode startup error title: %w", err)
	}
	messagePtr, err := windows.UTF16PtrFromString(message)
	if err != nil {
		return fmt.Errorf("encode startup error message: %w", err)
	}
	result, _, callErr := messageBoxW.Call(
		0,
		uintptr(unsafe.Pointer(messagePtr)),
		uintptr(unsafe.Pointer(titlePtr)),
		messageBoxIconError|messageBoxSetForeground,
	)
	if result != 0 {
		return nil
	}
	if !errors.Is(callErr, windows.ERROR_SUCCESS) {
		return fmt.Errorf("show startup error dialog: %w", callErr)
	}
	return errors.New("show startup error dialog failed")
}
