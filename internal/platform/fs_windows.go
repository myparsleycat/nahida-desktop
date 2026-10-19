package platform

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

const (
	errorSharingViolation syscall.Errno = 32
	errorLockViolation    syscall.Errno = 33
)

func fileTimesFromSys(info os.FileInfo) (ctime, birth time.Time) {
	stat, ok := info.Sys().(*syscall.Win32FileAttributeData)
	if !ok {
		return
	}
	ctime = time.Unix(0, stat.CreationTime.Nanoseconds())
	birth = ctime
	return
}

func isBusy(err error) bool {
	var errno syscall.Errno
	if !errors.As(err, &errno) {
		return false
	}
	return errno == errorSharingViolation || errno == errorLockViolation
}

// HideFile preserves the current Windows file attributes and adds the hidden
// attribute, matching `attrib +h` without starting a child process.
func HideFile(path string) error {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	attrs, err := windows.GetFileAttributes(name)
	if err != nil {
		return err
	}
	if attrs&windows.FILE_ATTRIBUTE_HIDDEN != 0 {
		return nil
	}
	return windows.SetFileAttributes(name, attrs|windows.FILE_ATTRIBUTE_HIDDEN)
}

// FinalPath returns the path Windows resolves an existing file or directory to, following every
// junction and symbolic link on the way. filepath.EvalSymlinks leaves junctions in place, so it
// cannot tell where a write below one really lands.
func FinalPath(path string) (string, error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return "", err
	}
	handle, err := windows.CreateFile(
		name, 0, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil,
		windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0,
	)
	if err != nil {
		return "", &os.PathError{Op: "open", Path: path, Err: err}
	}
	defer func() { _ = windows.CloseHandle(handle) }()

	final, err := FinalPathOfHandle(handle)
	if err != nil {
		return "", &os.PathError{Op: "resolve", Path: path, Err: err}
	}
	return final, nil
}

// FinalPathOfHandle returns the path of an open file or directory in the form FinalPath does.
func FinalPathOfHandle(handle windows.Handle) (string, error) {
	buffer := make([]uint16, windows.MAX_PATH)
	for {
		length, err := windows.GetFinalPathNameByHandle(handle, &buffer[0], uint32(len(buffer)), 0)
		if err != nil {
			return "", err
		}
		if int(length) < len(buffer) {
			break
		}
		buffer = make([]uint16, length+1)
	}

	final := windows.UTF16ToString(buffer)
	if rest, ok := strings.CutPrefix(final, `\\?\UNC\`); ok {
		return `\\` + rest, nil
	}
	return strings.TrimPrefix(final, `\\?\`), nil
}

// PhysicalPath is FinalPath for a path that may not exist yet: the deepest existing ancestor is resolved and the
// missing names are appended to it, so links above an entry not created yet are still followed.
func PhysicalPath(path string) (string, error) {
	missing := []string{}
	for current := filepath.Clean(path); ; current = filepath.Dir(current) {
		final, err := FinalPath(current)
		if err == nil {
			slices.Reverse(missing)
			return filepath.Join(append([]string{final}, missing...)...), nil
		}
		if !errors.Is(err, os.ErrNotExist) || filepath.Dir(current) == current {
			return "", err
		}
		missing = append(missing, filepath.Base(current))
	}
}
