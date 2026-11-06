package platform

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

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

// WriteFileAtomic writes content to a temporary file next to path and replaces path with it, so a reader
// sees either the old or the new content. reports receives cleanup failures that do not fail the write.
func WriteFileAtomic(path string, content []byte, mode os.FileMode, reports ...func(error)) error {
	report := func(err error) {
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			for _, callback := range reports {
				if callback != nil {
					callback(err)
				}
			}
		}
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".nhd-write-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer func() { report(os.Remove(temporaryPath)) }()
	if _, err := temporary.Write(content); err != nil {
		report(temporary.Close())
		return err
	}
	if err := temporary.Sync(); err != nil {
		report(temporary.Close())
		return err
	}
	if err := temporary.Chmod(mode); err != nil {
		report(temporary.Close())
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return ReplaceAtomic(temporaryPath, path)
}

// MoveNoReplace renames source to destination and fails with an error wrapping fs.ErrExist when
// destination already exists, instead of replacing it as os.Rename does on Windows.
func MoveNoReplace(source, destination string) error {
	sourcePtr, err := windows.UTF16PtrFromString(source)
	if err != nil {
		return err
	}
	destinationPtr, err := windows.UTF16PtrFromString(destination)
	if err != nil {
		return err
	}
	err = windows.MoveFileEx(sourcePtr, destinationPtr, windows.MOVEFILE_WRITE_THROUGH)
	if errors.Is(err, windows.ERROR_ALREADY_EXISTS) || errors.Is(err, windows.ERROR_FILE_EXISTS) {
		return fmt.Errorf("move file: %w: %w", fs.ErrExist, err)
	}
	if err != nil {
		return fmt.Errorf("move file: %w", err)
	}
	return nil
}
