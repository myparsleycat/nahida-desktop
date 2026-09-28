//go:build windows

package xxmi

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"nahida.live/desktop/internal/infra"
)

const launcherImageName = "XXMI Launcher.exe"

func ensureLauncherClosed(ctx context.Context) error {
	return ensureLauncherClosedWith(
		ctx, launcherImageName, 5*time.Second, 100*time.Millisecond, findProcessPID, killProcess,
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
