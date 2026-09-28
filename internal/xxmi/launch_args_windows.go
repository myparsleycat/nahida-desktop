//go:build windows

package xxmi

import (
	"errors"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

func splitLaunchOptions(raw string) ([]string, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	value, err := windows.UTF16PtrFromString("launch " + raw)
	if err != nil {
		return nil, err
	}
	var count int32
	args, err := windows.CommandLineToArgv(value, &count)
	if err != nil {
		return nil, err
	}
	defer func() { _, _ = windows.LocalFree(windows.Handle(unsafe.Pointer(args))) }()
	if count < 1 || count > 8192 {
		return nil, errors.New("invalid launch argument count")
	}
	result := make([]string, 0, count-1)
	for i := int32(1); i < count; i++ {
		result = append(result, windows.UTF16PtrToString(&args[i][0]))
	}
	return result, nil
}
