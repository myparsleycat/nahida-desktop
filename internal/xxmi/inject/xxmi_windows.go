//go:build windows

package inject

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

func Launch(ctx context.Context, spec LaunchSpec) (LaunchResult, error) {
	if err := ValidateLaunchSpec(spec); err != nil {
		return LaunchResult{}, err
	}
	if spec.Mode == ModeLegacy {
		return launchLegacy(ctx, spec)
	}
	result := make(chan struct {
		value LaunchResult
		err   error
	}, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		value, err := launchXXMI(ctx, spec)
		result <- struct {
			value LaunchResult
			err   error
		}{value, err}
	}()
	select {
	case <-ctx.Done():
		return LaunchResult{}, ctx.Err()
	case outcome := <-result:
		return outcome.value, outcome.err
	}
}

func launchXXMI(ctx context.Context, spec LaunchSpec) (LaunchResult, error) {
	if pid, err := findProcessPID(spec.ProcessName); err != nil {
		return LaunchResult{}, err
	} else if pid != 0 {
		return LaunchResult{}, errors.New("XXMI_GAME_RUNNING")
	}
	library, err := windows.LoadDLL(spec.LoaderDLL.Path)
	if err != nil {
		return LaunchResult{}, fmt.Errorf("load XXMI injector DLL: %w", err)
	}
	defer func() { _ = library.Release() }()
	hookProc, hookErr := library.FindProc("HookLibrary")
	waitProc, waitErr := library.FindProc("WaitForInjection")
	unhookProc, unhookErr := library.FindProc("UnhookLibrary")
	injectProc, injectErr := library.FindProc("Inject")
	if injectErr != nil {
		return LaunchResult{}, errors.New("XXMI_LOADER_TOO_OLD")
	}
	useHook := spec.UseHook && spec.InjectMode == "Hook"
	if useHook && (hookErr != nil || waitErr != nil || unhookErr != nil) {
		return LaunchResult{}, errors.New("XXMI_LOADER_TOO_OLD")
	}
	module, err := windows.UTF16PtrFromString(spec.ModuleDLL)
	if err != nil {
		return LaunchResult{}, err
	}
	process, err := windows.UTF16PtrFromString(spec.ProcessName)
	if err != nil {
		return LaunchResult{}, err
	}
	var hook, mutex windows.Handle
	if useHook {
		code, _, _ := hookProc.Call(
			uintptr(unsafe.Pointer(module)),
			uintptr(unsafe.Pointer(&hook)),
			uintptr(unsafe.Pointer(&mutex)),
		)
		if code != 0 || hook == 0 {
			if code == 0 {
				return LaunchResult{}, errors.New("XXMI_INJECT_FAILED: HookLibrary returned a null hook")
			}
			return LaunchResult{}, fmt.Errorf("XXMI_INJECT_FAILED: HookLibrary returned %d (%s)",
				code, hookFailureReason(code))
		}
		defer func() { _, _, _ = unhookProc.Call(uintptr(unsafe.Pointer(&hook)), uintptr(unsafe.Pointer(&mutex))) }()
	}
	deadline := time.Now().Add(time.Duration(spec.TimeoutSeconds) * time.Second)
	if err := startGameProcess(spec); err != nil {
		return LaunchResult{}, err
	}
	pid, err := waitForProcess(ctx, spec.ProcessName, time.Until(deadline))
	if err != nil {
		return LaunchResult{}, err
	}
	var warnings []string
	if err := setProcessPriority(pid, spec.Priority); err != nil {
		warnings = append(warnings, "Could not set game process priority: "+err.Error())
	}
	if err := injectExtraDLLs(injectProc, pid, spec.ExtraDLLs, spec.TimeoutSeconds); err != nil {
		return LaunchResult{}, err
	}
	if spec.InjectMode == "Bypass" {
		if err := waitForVisibleWindow(ctx, pid, time.Until(deadline)); err != nil {
			return LaunchResult{}, err
		}
		return LaunchResult{PID: pid, InjectionVerified: false, Warnings: warnings}, nil
	}
	if !useHook {
		code, _, _ := injectProc.Call(uintptr(pid), uintptr(unsafe.Pointer(module)), uintptr(spec.TimeoutSeconds))
		if code != 0 {
			return LaunchResult{}, fmt.Errorf("XXMI_INJECT_FAILED: Inject returned %d (%s)",
				code, injectFailureReason(code))
		}
		if err := waitForVisibleWindow(ctx, pid, time.Until(deadline)); err != nil {
			return LaunchResult{}, err
		}
		return LaunchResult{PID: pid, InjectionVerified: true, Warnings: warnings}, nil
	}
	earlyCode, _, _ := waitProc.Call(uintptr(unsafe.Pointer(module)), uintptr(unsafe.Pointer(process)), 5)
	if err := waitForVisibleWindow(ctx, pid, time.Until(deadline)); err != nil {
		return LaunchResult{}, err
	}
	lateCode, _, _ := waitProc.Call(uintptr(unsafe.Pointer(module)), uintptr(unsafe.Pointer(process)), 5)
	verified, warning := summarizeHookVerification(earlyCode, lateCode)
	if warning != "" {
		warnings = append(warnings, warning)
	}
	return LaunchResult{PID: pid, InjectionVerified: verified, Warnings: warnings}, nil
}

func summarizeHookVerification(earlyCode, lateCode uintptr) (bool, string) {
	if earlyCode == 0 || lateCode == 0 {
		return true, ""
	}
	return false, fmt.Sprintf("Could not verify XXMI hook injection (early code %d, late code %d)", earlyCode, lateCode)
}

func injectExtraDLLs(proc *windows.Proc, pid int, dlls []string, timeoutSeconds int) error {
	for _, dll := range dlls {
		pointer, err := windows.UTF16PtrFromString(dll)
		if err != nil {
			return err
		}
		code, _, _ := proc.Call(uintptr(pid), uintptr(unsafe.Pointer(pointer)), uintptr(timeoutSeconds))
		if code != 0 {
			return fmt.Errorf("XXMI_INJECT_FAILED: extra DLL Inject returned %d (%s): %s",
				code, injectFailureReason(code), dll)
		}
	}
	return nil
}

func startGameProcess(spec LaunchSpec) error {
	if spec.StartMethod == "Manual" {
		return nil
	}
	if spec.CustomLaunchCmd != "" {
		systemDir, err := windows.GetSystemDirectory()
		if err != nil {
			return err
		}
		command := exec.Command(filepath.Join(systemDir, "cmd.exe"), "/C", spec.CustomLaunchCmd)
		command.Dir = spec.WorkDir
		command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_CONSOLE}
		if err := command.Start(); err != nil {
			return err
		}
		return command.Process.Release()
	}
	if spec.StartMethod == "Shell" {
		verb, _ := windows.UTF16PtrFromString("open")
		file, _ := windows.UTF16PtrFromString(spec.StartExe)
		escaped := make([]string, len(spec.StartArgs))
		for i, arg := range spec.StartArgs {
			escaped[i] = windows.EscapeArg(arg)
		}
		args, _ := windows.UTF16PtrFromString(strings.Join(escaped, " "))
		workDir, _ := windows.UTF16PtrFromString(spec.WorkDir)
		return windows.ShellExecute(0, verb, file, args, workDir, 1)
	}
	command := exec.Command(spec.StartExe, spec.StartArgs...)
	command.Dir = spec.WorkDir
	priority, err := priorityClass(spec.Priority)
	if err != nil {
		return err
	}
	command.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: windows.CREATE_NEW_CONSOLE | windows.CREATE_DEFAULT_ERROR_MODE | priority,
	}
	if err := command.Start(); err != nil {
		return err
	}
	return command.Process.Release()
}

func setProcessPriority(pid int, priority string) error {
	class, err := priorityClass(priority)
	if err != nil {
		return err
	}
	process, err := windows.OpenProcess(windows.PROCESS_SET_INFORMATION, false, uint32(pid))
	if err != nil {
		return err
	}
	defer func() { _ = windows.CloseHandle(process) }()
	return windows.SetPriorityClass(process, class)
}

func waitForProcess(ctx context.Context, name string, timeout time.Duration) (int, error) {
	deadline := time.Now().Add(timeout)
	for {
		pid, err := findProcessPID(name)
		if err != nil {
			return 0, err
		}
		if pid != 0 {
			return pid, nil
		}
		if time.Now().After(deadline) {
			return 0, errors.New("XXMI_GAME_START_TIMEOUT")
		}
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return 0, ctx.Err()
		case <-timer.C:
		}
	}
}

func findProcessPID(name string) (int, error) {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return 0, err
	}
	defer func() { _ = windows.CloseHandle(snapshot) }()
	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	for err = windows.Process32First(snapshot, &entry); err == nil; err = windows.Process32Next(snapshot, &entry) {
		if strings.EqualFold(windows.UTF16ToString(entry.ExeFile[:]), filepath.Base(name)) {
			return int(entry.ProcessID), nil
		}
	}
	if errors.Is(err, windows.ERROR_NO_MORE_FILES) {
		return 0, nil
	}
	return 0, err
}
