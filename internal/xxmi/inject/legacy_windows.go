//go:build windows

package inject

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

func launchLegacy(ctx context.Context, spec LaunchSpec) (LaunchResult, error) {
	if pid, err := findProcessPID(spec.ProcessName); err != nil {
		return LaunchResult{}, err
	} else if pid != 0 {
		return LaunchResult{}, errors.New("XXMI_GAME_RUNNING")
	}
	mutexName, _ := windows.UTF16PtrFromString(`Local\3DMigotoLoader`)
	mutex, err := windows.OpenMutex(windows.SYNCHRONIZE, false, mutexName)
	if err == nil {
		_ = windows.CloseHandle(mutex)
		return LaunchResult{}, errors.New("XXMI_LEGACY_LOADER_RUNNING")
	}
	if !errors.Is(err, windows.ERROR_FILE_NOT_FOUND) {
		return LaunchResult{}, err
	}
	loader := exec.Command(spec.LegacyLoader.Path)
	loader.Dir = filepath.Dir(spec.LegacyLoader.Path)
	loader.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_CONSOLE}
	if err := loader.Start(); err != nil {
		return LaunchResult{}, fmt.Errorf("start legacy loader: %w", err)
	}
	defer func() { _ = loader.Process.Release() }()
	ready, err := waitLegacyLoaderReady(ctx, loader.Process.Pid, spec.ModuleDLL, 10*time.Second)
	if err != nil {
		_ = loader.Process.Kill()
		return LaunchResult{}, err
	}
	if !ready {
		_ = loader.Process.Kill()
		return LaunchResult{}, errors.New("XXMI_LEGACY_LOADER_NOT_READY")
	}
	timer := time.NewTimer(300 * time.Millisecond)
	select {
	case <-ctx.Done():
		timer.Stop()
		_ = loader.Process.Kill()
		return LaunchResult{}, ctx.Err()
	case <-timer.C:
	}
	deadline := time.Now().Add(time.Duration(spec.TimeoutSeconds) * time.Second)
	if err := startGameProcess(spec); err != nil {
		_ = loader.Process.Kill()
		return LaunchResult{}, err
	}
	pid, err := waitForProcess(ctx, spec.ProcessName, time.Until(deadline))
	if err != nil {
		_ = loader.Process.Kill()
		return LaunchResult{}, err
	}
	var warnings []string
	if err := setProcessPriority(pid, spec.Priority); err != nil {
		warnings = append(warnings, "Could not set game process priority: "+err.Error())
	}
	if err := waitForVisibleWindow(ctx, pid, time.Until(deadline)); err != nil {
		_ = loader.Process.Kill()
		return LaunchResult{}, err
	}
	verified, err := processHasModule(pid, spec.ModuleDLL)
	result := LaunchResult{PID: pid, InjectionVerified: verified, Warnings: warnings}
	if err != nil {
		result.Warnings = append(result.Warnings, "Could not verify legacy DLL in game process: "+err.Error())
	}
	if err := waitLegacyLoaderExit(ctx, loader.Process.Pid, legacyLoaderExitTimeout(spec.ModuleDLL)); err != nil {
		_ = loader.Process.Kill()
		if ctx.Err() != nil {
			return LaunchResult{}, ctx.Err()
		}
		result.Warnings = append(result.Warnings, "Legacy loader did not exit after game launch: "+err.Error())
	}
	return result, nil
}

func legacyLoaderExitTimeout(modulePath string) time.Duration {
	file, err := os.Open(filepath.Join(filepath.Dir(modulePath), "d3dx.ini"))
	if err != nil {
		return 15 * time.Second
	}
	defer func() { _ = file.Close() }()
	raw, err := io.ReadAll(io.LimitReader(file, 1<<20))
	if err != nil {
		return 15 * time.Second
	}
	inLoader := false
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			inLoader = strings.EqualFold(line, "[Loader]")
			continue
		}
		if !inLoader || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found || !strings.EqualFold(strings.TrimSpace(key), "delay") {
			continue
		}
		seconds, err := strconv.Atoi(strings.TrimSpace(value))
		if err == nil && seconds > 10 && seconds <= 600 {
			return time.Duration(seconds+5) * time.Second
		}
		return 15 * time.Second
	}
	return 15 * time.Second
}

func waitLegacyLoaderReady(ctx context.Context, pid int, modulePath string, timeout time.Duration) (bool, error) {
	deadline := time.Now().Add(timeout)
	for {
		ready, err := processHasModule(pid, modulePath)
		if err == nil && ready {
			return true, nil
		}
		process, openErr := windows.OpenProcess(
			windows.SYNCHRONIZE|windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid),
		)
		if openErr != nil {
			return false, errors.New("XXMI_LEGACY_LOADER_EXITED")
		}
		status, waitErr := windows.WaitForSingleObject(process, 0)
		var exitCode uint32
		if waitErr == nil && status == windows.WAIT_OBJECT_0 {
			_ = windows.GetExitCodeProcess(process, &exitCode)
		}
		_ = windows.CloseHandle(process)
		if waitErr == nil && status == windows.WAIT_OBJECT_0 {
			return false, fmt.Errorf("XXMI_LEGACY_LOADER_EXITED: exit code %d", exitCode)
		}
		if time.Now().After(deadline) {
			return false, nil
		}
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return false, ctx.Err()
		case <-timer.C:
		}
	}
}

func waitLegacyLoaderExit(ctx context.Context, pid int, timeout time.Duration) error {
	process, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		return err
	}
	defer func() { _ = windows.CloseHandle(process) }()
	deadline := time.Now().Add(timeout)
	for {
		status, err := windows.WaitForSingleObject(process, 0)
		if err != nil {
			return err
		}
		if status == windows.WAIT_OBJECT_0 {
			return nil
		}
		if time.Now().After(deadline) {
			return errors.New("loader shutdown timed out")
		}
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func processHasModule(pid int, modulePath string) (bool, error) {
	process, err := windows.OpenProcess(windows.PROCESS_QUERY_INFORMATION|windows.PROCESS_VM_READ, false, uint32(pid))
	if err != nil {
		return false, err
	}
	defer func() { _ = windows.CloseHandle(process) }()
	modules := make([]windows.Handle, 256)
	var needed uint32
	if err := windows.EnumProcessModulesEx(
		process,
		&modules[0],
		uint32(len(modules))*uint32(unsafe.Sizeof(modules[0])),
		&needed,
		windows.LIST_MODULES_ALL,
	); err != nil {
		return false, err
	}
	count := int(needed / uint32(unsafe.Sizeof(modules[0])))
	if count > len(modules) {
		count = len(modules)
	}
	for _, module := range modules[:count] {
		buffer := make([]uint16, windows.MAX_LONG_PATH)
		if err := windows.GetModuleFileNameEx(process, module, &buffer[0], uint32(len(buffer))); err != nil {
			continue
		}
		if strings.EqualFold(filepath.Clean(windows.UTF16ToString(buffer)), filepath.Clean(modulePath)) {
			return true, nil
		}
	}
	return false, nil
}
