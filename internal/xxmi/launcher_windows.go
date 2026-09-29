//go:build windows

package xxmi

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"nahida.live/desktop/internal/infra"
)

const launcherImageName = "XXMI Launcher.exe"

var (
	shell32XXMI             = syscall.NewLazyDLL("shell32.dll")
	user32XXMI              = syscall.NewLazyDLL("user32.dll")
	procShellExecuteExW     = shell32XXMI.NewProc("ShellExecuteExW")
	procEnumWindows         = user32XXMI.NewProc("EnumWindows")
	procGetWindowProcessID  = user32XXMI.NewProc("GetWindowThreadProcessId")
	procIsWindowVisibleXXMI = user32XXMI.NewProc("IsWindowVisible")
	procIsIconicXXMI        = user32XXMI.NewProc("IsIconic")
	enumVisibleWindowProc   = syscall.NewCallback(enumVisibleProcessWindow)
	visibleWindowSearches   sync.Map
	visibleWindowSearchID   atomic.Uint64
)

const (
	seeMaskNoCloseProcess = 0x00000040
	swHide                = 0
	waitObject0           = 0
	waitTimeout           = 258
)

type shellExecuteInfoW struct {
	cbSize       uint32
	fMask        uint32
	hwnd         uintptr
	lpVerb       *uint16
	lpFile       *uint16
	lpParameters *uint16
	lpDirectory  *uint16
	nShow        int32
	hInstApp     uintptr
	lpIDList     uintptr
	lpClass      *uint16
	hkeyClass    uintptr
	dwHotKey     uint32
	hIcon        uintptr
	hProcess     windows.Handle
}

type visibleWindowSearch struct {
	pid   uint32
	found bool
}

func ensureLauncherClosed(ctx context.Context) error {
	return ensureLauncherClosedWith(
		ctx, launcherImageName, 5*time.Second, 100*time.Millisecond, findProcessPID, killProcess,
	)
}

func ensureLauncherClosedAt(ctx context.Context, executable string) error {
	return ensureLauncherClosedWith(
		ctx,
		executable,
		5*time.Second,
		100*time.Millisecond,
		findProcessPID,
		killProcessForExecutable(executable),
	)
}

func ensureLauncherClosedWith(
	ctx context.Context,
	executable string,
	timeout time.Duration,
	pollInterval time.Duration,
	find func(context.Context, string) (int, error),
	kill func(int) error,
) error {
	deadline := time.Now().Add(timeout)
	for {
		pid, err := find(ctx, executable)
		if err != nil {
			return err
		}
		if pid == 0 {
			return nil
		}
		if err := kill(pid); err != nil {
			return infra.AnnotateError(
				infra.WithCause(errors.New("failed to close XXMI Launcher"), err),
				infra.Diagnostic{
					Operation: "close-launcher",
					Stage:     "terminate",
					Fields:    map[string]any{"pid": pid, "executable": executable},
				},
			)
		}
		if time.Now().After(deadline) {
			return infra.AnnotateError(
				errors.New("XXMI Launcher is still running"),
				infra.Diagnostic{
					Operation: "close-launcher",
					Stage:     "wait",
					Fields:    map[string]any{"pid": pid, "executable": executable},
				},
			)
		}
		timer := time.NewTimer(pollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func killProcess(pid int) error {
	if pid <= 0 {
		return errors.New("invalid pid")
	}
	handle, err := windows.OpenProcess(windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		return err
	}
	defer func() { _ = windows.CloseHandle(handle) }()
	return windows.TerminateProcess(handle, 1)
}

func killProcessForExecutable(executable string) func(int) error {
	return func(pid int) error {
		matches, err := processMatchesExecutable(pid, executable)
		if err != nil {
			return err
		}
		if !matches {
			return errors.New("XXMI Launcher process identity changed before termination")
		}
		return killProcess(pid)
	}
}

func startLauncher(ctx context.Context, executable, importer string) error {
	verb, err := syscall.UTF16PtrFromString("runas")
	if err != nil {
		return err
	}
	file, err := syscall.UTF16PtrFromString(executable)
	if err != nil {
		return err
	}
	parameters, err := syscall.UTF16PtrFromString("--nogui --xxmi " + syscall.EscapeArg(importer))
	if err != nil {
		return err
	}
	directory, err := syscall.UTF16PtrFromString(filepath.Dir(executable))
	if err != nil {
		return err
	}
	info := shellExecuteInfoW{
		fMask: seeMaskNoCloseProcess, lpVerb: verb, lpFile: file,
		lpParameters: parameters, lpDirectory: directory, nShow: swHide,
	}
	info.cbSize = uint32(unsafe.Sizeof(info))
	result, _, callErr := procShellExecuteExW.Call(uintptr(unsafe.Pointer(&info)))
	if result == 0 {
		return fmt.Errorf("start XXMI Launcher: %w", callErr)
	}
	if info.hProcess == 0 {
		return errors.New("start XXMI Launcher: missing process handle")
	}
	defer func() { _ = windows.CloseHandle(info.hProcess) }()
	for {
		result, waitErr := windows.WaitForSingleObject(info.hProcess, 100)
		switch result {
		case waitObject0:
			var exitCode uint32
			if err := windows.GetExitCodeProcess(info.hProcess, &exitCode); err != nil {
				return fmt.Errorf("read XXMI Launcher exit code: %w", err)
			}
			return nil
		case waitTimeout:
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
		default:
			return fmt.Errorf("wait for XXMI Launcher: result %d: %w", result, waitErr)
		}
	}
}

func processHasVisibleWindow(pid int) bool {
	if pid <= 0 {
		return false
	}
	search := visibleWindowSearch{pid: uint32(pid)}
	id := visibleWindowSearchID.Add(1)
	visibleWindowSearches.Store(id, &search)
	defer visibleWindowSearches.Delete(id)
	_, _, _ = procEnumWindows.Call(enumVisibleWindowProc, uintptr(id))
	return search.found
}

func enumVisibleProcessWindow(hwnd, lparam uintptr) uintptr {
	value, ok := visibleWindowSearches.Load(uint64(lparam))
	if !ok {
		return 0
	}
	search := value.(*visibleWindowSearch)
	var pid uint32
	_, _, _ = procGetWindowProcessID.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	if pid != search.pid {
		return 1
	}
	visible, _, _ := procIsWindowVisibleXXMI.Call(hwnd)
	iconic, _, _ := procIsIconicXXMI.Call(hwnd)
	if visible != 0 && iconic == 0 {
		search.found = true
		return 0
	}
	return 1
}

func findProcessPID(ctx context.Context, executable string) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	imageName := filepath.Base(executable)
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return 0, err
	}
	defer func() { _ = windows.CloseHandle(snapshot) }()

	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	for err = windows.Process32First(snapshot, &entry); err == nil; err = windows.Process32Next(snapshot, &entry) {
		if !strings.EqualFold(windows.UTF16ToString(entry.ExeFile[:]), imageName) {
			continue
		}
		if !filepath.IsAbs(executable) {
			return int(entry.ProcessID), nil
		}
		matches, matchErr := processMatchesExecutable(int(entry.ProcessID), executable)
		if matchErr != nil {
			return 0, matchErr
		}
		if matches {
			return int(entry.ProcessID), nil
		}
	}
	if errors.Is(err, windows.ERROR_NO_MORE_FILES) {
		return 0, nil
	}
	return 0, err
}

func processMatchesExecutable(pid int, executable string) (bool, error) {
	if pid <= 0 {
		return false, errors.New("invalid pid")
	}
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if errors.Is(err, windows.ERROR_ACCESS_DENIED) || errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer func() { _ = windows.CloseHandle(handle) }()

	buffer := make([]uint16, windows.MAX_LONG_PATH)
	size := uint32(len(buffer))
	if err := windows.QueryFullProcessImageName(
		handle,
		0,
		&buffer[0],
		&size,
	); errors.Is(
		err,
		windows.ERROR_ACCESS_DENIED,
	) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	processPath := filepath.Clean(windows.UTF16ToString(buffer[:size]))
	expectedInfo, err := os.Stat(filepath.Clean(executable))
	if err != nil {
		return false, err
	}
	processInfo, err := os.Stat(processPath)
	if errors.Is(err, fs.ErrPermission) || errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return os.SameFile(expectedInfo, processInfo), nil
}
