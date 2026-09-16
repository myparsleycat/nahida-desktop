package platform

import (
	"fmt"

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
