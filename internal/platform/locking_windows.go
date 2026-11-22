package platform

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
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
	return lockingProcessesForFiles(files)
}

// FindDirectoryLockingProcesses lists the other processes holding files below path, or path itself when it is a
// file. A ctx deadline stops collecting files, and the files collected by then are still checked; cancelling
// ctx returns nil.
func (f *FS) FindDirectoryLockingProcesses(ctx context.Context, path string) []ProcessInfo {
	const maxFiles = 4096

	info, err := os.Stat(path)
	if err != nil {
		return nil
	}
	files := []string{path}
	if info.IsDir() {
		files, err = directoryLockCandidates(ctx, path, maxFiles)
		if err != nil {
			return nil
		}
	}
	if errors.Is(ctx.Err(), context.Canceled) {
		return nil
	}

	self := windows.GetCurrentProcessId()
	return slices.DeleteFunc(lockingProcessesForFiles(files), func(process ProcessInfo) bool {
		return process.PID == self
	})
}

// directoryLockCandidates registers the directory's own files before any subfolder: a process usually holds DLLs
// mapped from the root, and byte order walks upper-case folders such as Mods first, which can fill the cap alone.
func directoryLockCandidates(ctx context.Context, directory string, maxFiles int) ([]string, error) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, err
	}
	var files, folders []string
	for _, entry := range entries {
		path := filepath.Join(directory, entry.Name())
		if entry.Type().IsRegular() {
			files = append(files, path)
		} else if entry.IsDir() {
			folders = append(folders, path)
		}
	}
	files = files[:min(len(files), maxFiles)]

	for _, folder := range folders {
		if len(files) == maxFiles {
			break
		}
		err := filepath.WalkDir(folder, func(path string, entry fs.DirEntry, err error) error {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if err != nil {
				return nil //nolint:nilerr // An unreadable entry does not prevent checking its siblings.
			}
			if entry.Type().IsRegular() {
				files = append(files, path)
				if len(files) == maxFiles {
					return fs.SkipAll
				}
			}
			return nil
		})
		if errors.Is(err, context.DeadlineExceeded) {
			break
		}
		if err != nil {
			return nil, err
		}
	}
	return files, nil
}

func lockingProcessesForFiles(files []string) []ProcessInfo {
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

	// The list can grow between the size query and the fill when another process opens a registered file.
	var infos []rmProcessInfo
	for attempt := 0; ; attempt++ {
		var needed, reason uint32
		count := uint32(len(infos))
		var buffer *rmProcessInfo
		if count > 0 {
			buffer = &infos[0]
		}
		status, _, _ := procRmGetList.Call(
			uintptr(session),
			uintptr(unsafe.Pointer(&needed)),
			uintptr(unsafe.Pointer(&count)),
			uintptr(unsafe.Pointer(buffer)),
			uintptr(unsafe.Pointer(&reason)),
		)
		if status == 0 {
			infos = infos[:count]
			break
		}
		if syscall.Errno(status) != windows.ERROR_MORE_DATA || attempt == 3 {
			return []ProcessInfo{}
		}
		infos = make([]rmProcessInfo, needed)
	}

	result := make([]ProcessInfo, 0, len(infos))
	for i := range infos {
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
