package platform

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

type shellExecuteFunc func(
	hwnd windows.Handle,
	verb *uint16,
	file *uint16,
	args *uint16,
	cwd *uint16,
	showCmd int32,
) error

func openShellPath(target string) error {
	return openShellPathWith(target, windows.ShellExecute)
}

func openShellPathWith(target string, execute shellExecuteFunc) error {
	absolute, err := filepath.Abs(target)
	if err != nil {
		return fmt.Errorf("resolve open path %q: %w", target, err)
	}
	if _, err := os.Stat(absolute); err != nil {
		return fmt.Errorf("stat open path %q: %w", absolute, err)
	}
	verb, err := windows.UTF16PtrFromString("open")
	if err != nil {
		return fmt.Errorf("encode open verb: %w", err)
	}
	file, err := windows.UTF16PtrFromString(absolute)
	if err != nil {
		return fmt.Errorf("encode open path %q: %w", absolute, err)
	}
	if err := execute(0, verb, file, nil, nil, windows.SW_SHOWNORMAL); err != nil {
		return fmt.Errorf("open path %q: %w", absolute, err)
	}
	return nil
}
