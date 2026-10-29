package platform

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

type LockedExecutable struct {
	File        *os.File
	Path        string
	directories []windows.Handle
}

// LockExecutable binds later path-based loads to an immutable file and directory chain.
// Directory handles also deny writes so an existing directory cannot become a reparse point.
func LockExecutable(path string) (_ *LockedExecutable, returnErr error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	handle, err := windows.CreateFile(name, windows.GENERIC_READ, windows.FILE_SHARE_READ,
		nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, fmt.Errorf("lock executable %q: %w", path, err)
	}
	locked := &LockedExecutable{File: os.NewFile(uintptr(handle), path)}
	defer func() {
		if returnErr != nil {
			returnErr = errors.Join(returnErr, locked.Close())
		}
	}()
	info, err := locked.File.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("executable %q is not a regular file", path)
	}
	locked.Path, err = FinalPathOfHandle(handle)
	if err != nil {
		return nil, err
	}
	if len(filepath.VolumeName(locked.Path)) != 2 || strings.HasPrefix(locked.Path, `\\`) {
		return nil, fmt.Errorf("executable %q resolves outside a local drive: %q", path, locked.Path)
	}

	var directories []string
	for dir := filepath.Dir(locked.Path); ; dir = filepath.Dir(dir) {
		directories = append(directories, dir)
		if filepath.Dir(dir) == dir {
			break
		}
	}
	for i := len(directories) - 1; i >= 0; i-- {
		dir := directories[i]
		pointer, err := windows.UTF16PtrFromString(dir)
		if err != nil {
			return nil, err
		}
		directory, err := windows.CreateFile(pointer, windows.FILE_READ_ATTRIBUTES, windows.FILE_SHARE_READ,
			nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
		if err != nil {
			return nil, fmt.Errorf("pin executable directory %q for %q: %w", dir, path, err)
		}
		locked.directories = append(locked.directories, directory)
		var attributes windows.ByHandleFileInformation
		if err := windows.GetFileInformationByHandle(directory, &attributes); err != nil {
			return nil, err
		}
		if attributes.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
			return nil, fmt.Errorf("executable directory %q changed to a link", dir)
		}
	}

	// A directory could have moved before its handle was pinned. Reopening the canonical
	// path after pinning must reach the same file object, not a replacement at its old name.
	canonical, err := os.Open(locked.Path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = canonical.Close() }()
	canonicalInfo, err := canonical.Stat()
	if err != nil {
		return nil, err
	}
	if !os.SameFile(info, canonicalInfo) {
		return nil, fmt.Errorf("executable %q changed while pinning %q", path, locked.Path)
	}
	return locked, nil
}

func (f *LockedExecutable) Close() error {
	var err error
	if f.File != nil {
		err = f.File.Close()
		f.File = nil
	}
	for i := len(f.directories) - 1; i >= 0; i-- {
		err = errors.Join(err, windows.CloseHandle(f.directories[i]))
	}
	f.directories = nil
	return err
}
