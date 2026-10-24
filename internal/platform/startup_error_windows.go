//go:build windows

package platform

import (
	"errors"
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	messageBoxYesNo         = 0x00000004
	messageBoxIconError     = 0x00000010
	messageBoxIconWarning   = 0x00000030
	messageBoxSetForeground = 0x00010000
	messageBoxResultYes     = 6
)

var messageBoxW = windows.NewLazySystemDLL("user32.dll").NewProc("MessageBoxW")

func showStartupError(title, message string) error {
	_, err := startupMessageBox(title, message, messageBoxIconError|messageBoxSetForeground)
	return err
}

func confirmStartup(title, message string) (bool, error) {
	result, err := startupMessageBox(title, message, messageBoxYesNo|messageBoxIconWarning|messageBoxSetForeground)
	return result == messageBoxResultYes, err
}

func startupMessageBox(title, message string, style uintptr) (uintptr, error) {
	titlePtr, err := windows.UTF16PtrFromString(title)
	if err != nil {
		return 0, fmt.Errorf("encode startup dialog title: %w", err)
	}
	messagePtr, err := windows.UTF16PtrFromString(message)
	if err != nil {
		return 0, fmt.Errorf("encode startup dialog message: %w", err)
	}
	result, _, callErr := messageBoxW.Call(
		0,
		uintptr(unsafe.Pointer(messagePtr)),
		uintptr(unsafe.Pointer(titlePtr)),
		style,
	)
	if result != 0 {
		return result, nil
	}
	if !errors.Is(callErr, windows.ERROR_SUCCESS) {
		return 0, fmt.Errorf("show startup dialog: %w", callErr)
	}
	return 0, errors.New("show startup dialog failed")
}
