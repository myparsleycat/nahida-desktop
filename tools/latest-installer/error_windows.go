//go:build windows

package main

import (
	"golang.org/x/sys/windows"
)

func showError(title string, message string) {
	titlePtr, err := windows.UTF16PtrFromString(title)
	if err != nil {
		return
	}
	messagePtr, err := windows.UTF16PtrFromString(message)
	if err != nil {
		return
	}
	_, _ = windows.MessageBox(0, messagePtr, titlePtr, windows.MB_OK|windows.MB_ICONERROR)
}
