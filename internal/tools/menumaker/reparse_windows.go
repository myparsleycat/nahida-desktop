//go:build windows

package menumaker

import (
	"os"
	"syscall"

	"golang.org/x/sys/windows"
)

func isReparsePoint(path string) bool {
	info, err := os.Lstat(path)
	if err != nil {
		return false
	}
	data, ok := info.Sys().(*syscall.Win32FileAttributeData)
	return ok && data.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0
}
