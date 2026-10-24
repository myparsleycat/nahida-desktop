package platform

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	errorSharingViolation syscall.Errno = 32
	errorLockViolation    syscall.Errno = 33
)

// traverseCheckBypassed reports whether the process token has SeChangeNotifyPrivilege enabled. Windows
// then skips the FILE_TRAVERSE check on folders along a path, so a folder that refuses it stays usable.
var traverseCheckBypassed = sync.OnceValue(func() bool {
	name, err := windows.UTF16PtrFromString("SeChangeNotifyPrivilege")
	if err != nil {
		return false
	}
	var luid windows.LUID
	if err := windows.LookupPrivilegeValue(nil, name, &luid); err != nil {
		return false
	}

	token := windows.GetCurrentProcessToken()
	var size uint32
	_ = windows.GetTokenInformation(token, windows.TokenPrivileges, nil, 0, &size)
	if size == 0 {
		return false
	}
	buffer := make([]byte, size)
	if err := windows.GetTokenInformation(token, windows.TokenPrivileges, &buffer[0], size, &size); err != nil {
		return false
	}
	for _, privilege := range (*windows.Tokenprivileges)(unsafe.Pointer(&buffer[0])).AllPrivileges() {
		if privilege.Luid == luid {
			return privilege.Attributes&windows.SE_PRIVILEGE_ENABLED != 0
		}
	}
	return false
})

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

// AccessDenied reports whether the permissions of an existing entry refuse the current account the
// access the app uses: reading and writing a file, or listing a directory and creating entries in it.
// A junction or symbolic link is checked at the entry it points at, which is where reads and writes
// through it land. A missing entry, a read-only file, and an entry another process holds open are
// not reported, because restoring permissions does not change them.
func AccessDenied(path string) bool {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return false
	}

	// GENERIC_WRITE on a directory also covers changing its own attributes, which a folder the app
	// only creates entries in can refuse without breaking anything. A file keeps it because SQLite
	// opens its database files with GENERIC_READ|GENERIC_WRITE.
	access := uint32(windows.GENERIC_READ | windows.GENERIC_WRITE)
	if info, err := os.Stat(path); err == nil {
		if info.Mode().IsRegular() && info.Mode().Perm()&0o200 == 0 {
			return false
		}
		if info.IsDir() {
			// FILE_WRITE_DATA and FILE_APPEND_DATA are FILE_ADD_FILE and FILE_ADD_SUBDIRECTORY on a directory.
			access = windows.FILE_LIST_DIRECTORY | windows.FILE_WRITE_DATA | windows.FILE_APPEND_DATA
			if !traverseCheckBypassed() {
				access |= windows.FILE_TRAVERSE
			}
		}
	}
	handle, err := windows.CreateFile(
		name, access,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS, 0,
	)
	if err != nil {
		return errors.Is(err, windows.ERROR_ACCESS_DENIED)
	}
	_ = windows.CloseHandle(handle)
	return false
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
