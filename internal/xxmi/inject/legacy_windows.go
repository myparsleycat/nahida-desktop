//go:build windows

package inject

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

func launchLegacy(ctx context.Context, spec LaunchSpec) (LaunchResult, error) {
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
	if err := startGameProcess(spec); err != nil {
		_ = loader.Process.Kill()
		return LaunchResult{}, err
	}
	pid, err := waitForProcess(ctx, spec.ProcessName, time.Duration(spec.TimeoutSeconds)*time.Second)
	if err != nil {
		_ = loader.Process.Kill()
		return LaunchResult{}, err
	}
	verified, err := processHasModule(pid, spec.ModuleDLL)
	result := LaunchResult{PID: pid, InjectionVerified: verified}
	if err != nil {
		result.Warnings = append(result.Warnings, "Could not verify legacy DLL in game process: "+err.Error())
	}
	return result, nil
}

func waitLegacyLoaderReady(ctx context.Context, pid int, modulePath string, timeout time.Duration) (bool, error) {
	deadline := time.Now().Add(timeout)
	for {
		ready, err := processHasModule(pid, modulePath)
		if err == nil && ready {
			return true, nil
		}
		process, openErr := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
		if openErr != nil {
			return false, errors.New("XXMI_LEGACY_LOADER_EXITED")
		}
		status, waitErr := windows.WaitForSingleObject(process, 0)
		_ = windows.CloseHandle(process)
		if waitErr == nil && status == windows.WAIT_OBJECT_0 {
			return false, errors.New("XXMI_LEGACY_LOADER_EXITED")
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

func processHasModule(pid int, modulePath string) (bool, error) {
	process, err := windows.OpenProcess(windows.PROCESS_QUERY_INFORMATION|windows.PROCESS_VM_READ, false, uint32(pid))
	if err != nil {
		return false, err
	}
	defer func() { _ = windows.CloseHandle(process) }()
	modules := make([]windows.Handle, 256)
	var needed uint32
	if err := windows.EnumProcessModules(
		process,
		&modules[0],
		uint32(len(modules))*uint32(unsafe.Sizeof(modules[0])),
		&needed,
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
