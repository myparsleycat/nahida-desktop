package platform

import (
	"errors"
	"os"
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
