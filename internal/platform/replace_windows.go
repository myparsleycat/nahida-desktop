package platform

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

// ReplaceAtomic renames source over destination, replacing any existing file
// and flushing the change through to disk.
func ReplaceAtomic(source, destination string) error {
	sourcePtr, err := windows.UTF16PtrFromString(source)
	if err != nil {
		return err
	}
	destinationPtr, err := windows.UTF16PtrFromString(destination)
	if err != nil {
		return err
	}
	err = windows.MoveFileEx(
		sourcePtr, destinationPtr,
		windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH,
	)
	if err != nil {
		return fmt.Errorf("replace file: %w", err)
	}
	return nil
}

// ReplaceAtomicAt renames temporary over name, both entries of dir, replacing any existing file. The rename
// is relative to the directory dir holds open, so a link swapped into the path after dir was opened cannot
// redirect it.
func ReplaceAtomicAt(dir *os.Root, temporary, name string) error {
	if err := dir.Rename(temporary, name); err != nil {
		return fmt.Errorf("replace file: %w", err)
	}
	return nil
}
