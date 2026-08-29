//go:build windows

package tools

import (
	"fmt"
	"syscall"
	"unsafe"
)

const (
	moveFileReplaceExisting = 0x1
	moveFileWriteThrough    = 0x8
)

var (
	kernel32Tools    = syscall.NewLazyDLL("kernel32.dll")
	moveFileExWTools = kernel32Tools.NewProc("MoveFileExW")
)

func replaceAtomic(source, destination string) error {
	sourcePtr, err := syscall.UTF16PtrFromString(source)
	if err != nil {
		return err
	}
	destinationPtr, err := syscall.UTF16PtrFromString(destination)
	if err != nil {
		return err
	}
	result, _, callErr := moveFileExWTools.Call(
		uintptr(unsafe.Pointer(sourcePtr)), uintptr(unsafe.Pointer(destinationPtr)),
		moveFileReplaceExisting|moveFileWriteThrough,
	)
	if result == 0 {
		return fmt.Errorf("replace file: %w", callErr)
	}
	return nil
}
