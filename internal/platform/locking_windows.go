package platform

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const errorAccessDenied syscall.Errno = 5

const (
	cchRMSessionKey = 32
	cchRMMaxAppName = 255
	cchRMMaxSvcName = 63
)

type rmUniqueProcess struct {
	ProcessID        uint32
	ProcessStartTime windows.Filetime
}

type rmProcessInfo struct {
	Process             rmUniqueProcess
	StrAppName          [cchRMMaxAppName + 1]uint16
	StrServiceShortName [cchRMMaxSvcName + 1]uint16
	ApplicationType     int32
	AppStatus           uint32
	TSSessionID         uint32
	Restartable         int32
}

var (
	modRstrtmgr             = windows.NewLazySystemDLL("rstrtmgr.dll")
	procRmStartSession      = modRstrtmgr.NewProc("RmStartSession")
	procRmRegisterResources = modRstrtmgr.NewProc("RmRegisterResources")
	procRmGetList           = modRstrtmgr.NewProc("RmGetList")
	procRmEndSession        = modRstrtmgr.NewProc("RmEndSession")
)

func isPermissionError(err error) bool {
	var errno syscall.Errno
	return errors.As(err, &errno) && errno == errorAccessDenied
}

func isLockCandidate(err error) bool {
	return isBusy(err) || isPermissionError(err)
}

// getLockingProcesses is Electron native/fs getLockingProcesses via Restart
// Manager. There is no Electron test for this lookup. WalkDir does not follow
// directory links; the Rust WalkDir crate also defaults to not following them.
func getLockingProcesses(pathStr string) []ProcessInfo {
	normalized := filepath.FromSlash(pathStr)
	info, err := os.Stat(normalized)
	files := []string{normalized}
	if err == nil && info.IsDir() {
		files = lockingFilesInDir(normalized)
	}
	if len(files) == 0 {
		return []ProcessInfo{}
	}

	var session uint32
	var sessionKey [cchRMSessionKey + 1]uint16
	start, _, _ := procRmStartSession.Call(
		uintptr(unsafe.Pointer(&session)),
		0,
		uintptr(unsafe.Pointer(&sessionKey[0])),
	)
	if start != 0 {
		return []ProcessInfo{}
	}
	defer func() { _, _, _ = procRmEndSession.Call(uintptr(session)) }()

	wide := make([][]uint16, len(files))
	ptrs := make([]*uint16, len(files))
	for i, file := range files {
		encoded, encodeErr := windows.UTF16FromString(file)
		if encodeErr != nil {
			return []ProcessInfo{}
		}
		wide[i] = encoded
		ptrs[i] = &wide[i][0]
	}

	registered, _, _ := procRmRegisterResources.Call(
		uintptr(session),
		uintptr(len(ptrs)),
		uintptr(unsafe.Pointer(&ptrs[0])),
		0, 0, 0, 0,
	)
	if registered != 0 {
		return []ProcessInfo{}
	}

	var needed uint32
	var count uint32
	var reason uint32
	_, _, _ = procRmGetList.Call(
		uintptr(session),
		uintptr(unsafe.Pointer(&needed)),
		uintptr(unsafe.Pointer(&count)),
		0,
		uintptr(unsafe.Pointer(&reason)),
	)
	if needed == 0 {
		return []ProcessInfo{}
	}

	infos := make([]rmProcessInfo, needed)
	count = needed
	second, _, _ := procRmGetList.Call(
		uintptr(session),
		uintptr(unsafe.Pointer(&needed)),
		uintptr(unsafe.Pointer(&count)),
		uintptr(unsafe.Pointer(&infos[0])),
		uintptr(unsafe.Pointer(&reason)),
	)
	if second != 0 {
		return []ProcessInfo{}
	}

	result := make([]ProcessInfo, 0, count)
	for i := range count {
		result = append(result, ProcessInfo{
			Name: windows.UTF16ToString(infos[i].StrAppName[:]),
			PID:  infos[i].Process.ProcessID,
		})
	}
	return result
}

func lockingFilesInDir(root string) []string {
	files := []string{}
	_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry == nil {
			return nil //nolint:nilerr // Match jwalk: unreadable descendants are skipped.
		}
		// jwalk's default skip_hidden option only checks for a leading dot;
		// it does not inspect the Windows hidden file attribute. Preserve that
		// behavior for descendants while still allowing a dot-named root.
		if path != root && strings.HasPrefix(entry.Name(), ".") {
			if entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if entry.Type().IsRegular() {
			files = append(files, path)
		}
		return nil
	})
	return files
}
