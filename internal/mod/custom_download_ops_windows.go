//go:build windows

package mod

import (
	"errors"

	"golang.org/x/sys/windows"
)

func isCrossDeviceMoveError(err error) bool {
	return errors.Is(err, windows.ERROR_NOT_SAME_DEVICE)
}
