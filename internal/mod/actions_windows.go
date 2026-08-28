//go:build windows

package mod

import (
	"errors"
	"os"
	"syscall"

	"golang.org/x/sys/windows"
)

func isRetryableTogglePlatformError(err error) bool {
	if errors.Is(err, os.ErrPermission) {
		return true
	}
	var errno syscall.Errno
	if !errors.As(err, &errno) {
		return false
	}
	return errno == windows.ERROR_BUSY ||
		errno == windows.ERROR_SHARING_VIOLATION ||
		errno == windows.ERROR_LOCK_VIOLATION ||
		errno == windows.ERROR_ACCESS_DENIED
}
