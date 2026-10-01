//go:build windows

package inject

import (
	"context"
	"debug/pe"
	"encoding/binary"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	nativeKernel32              = windows.NewLazySystemDLL("kernel32.dll")
	nativeVirtualAllocEx        = nativeKernel32.NewProc("VirtualAllocEx")
	nativeVirtualFreeEx         = nativeKernel32.NewProc("VirtualFreeEx")
	nativeCreateRemoteThread    = nativeKernel32.NewProc("CreateRemoteThread")
	nativeFlushInstructionCache = nativeKernel32.NewProc("FlushInstructionCache")
	nativeGetExitCodeThread     = nativeKernel32.NewProc("GetExitCodeThread")
)

// The x64 thunk calls LoadLibraryW, saves the full 64-bit HMODULE and the target's
// GetLastError, then returns zero. Its stack includes the Windows ABI shadow space.
// This ports desktop-old/native/dll-injector's remote call to a focused DLL load.
var nativeLoadCode = []byte{
	0x53,                   // push rbx
	0x48, 0x83, 0xec, 0x20, // sub rsp, 20h
	0x48, 0x89, 0xcb, // mov rbx, rcx
	0x48, 0x8b, 0x4b, 0x10, // mov rcx, [rbx+10h] (path)
	0xff, 0x13, // call [rbx] (LoadLibraryW)
	0x48, 0x89, 0x43, 0x18, // mov [rbx+18h], rax (HMODULE)
	0xff, 0x53, 0x08, // call [rbx+8] (GetLastError)
	0x89, 0x43, 0x20, // mov [rbx+20h], eax (error)
	0x31, 0xc0, // xor eax, eax
	0x48, 0x83, 0xc4, 0x20, // add rsp, 20h
	0x5b, // pop rbx
	0xc3, // ret
}

type nativeCall struct {
	process windows.Handle
	thread  windows.Handle
	code    uintptr
	data    uintptr
}

func launchNative(ctx context.Context, spec LaunchSpec) (LaunchResult, error) {
	if err := ctx.Err(); err != nil {
		return LaunchResult{}, err
	}
	if runtime.GOARCH != "amd64" {
		return LaunchResult{}, errors.New("XXMI_INJECT_FAILED: native injection requires an x64 launcher")
	}
	if pid, err := findProcessPID(spec.ProcessName); err != nil {
		return LaunchResult{}, err
	} else if pid != 0 {
		return LaunchResult{}, errors.New("XXMI_GAME_RUNNING")
	}
	if err := startGameProcess(spec); err != nil {
		return LaunchResult{}, err
	}
	pid, err := waitForProcess(ctx, spec.ProcessName, spec.timeout())
	if err != nil {
		return LaunchResult{}, err
	}
	warnings := []string{}
	if err := setProcessPriority(pid, spec.Priority); err != nil {
		warnings = append(warnings, "Could not set game process priority: "+err.Error())
	}
	dlls := slices.Clone(spec.ExtraDLLs)
	if spec.InjectMode != "Bypass" {
		dlls = slices.DeleteFunc(dlls, func(dll string) bool {
			return strings.EqualFold(filepath.Clean(dll), filepath.Clean(spec.ModuleDLL))
		})
		dlls = append([]string{spec.ModuleDLL}, dlls...)
	}
	for _, dll := range dlls {
		if _, err := injectNativeDLL(ctx, pid, dll, spec.timeout()); err != nil {
			return LaunchResult{}, fmt.Errorf("XXMI_INJECT_FAILED: native injection PID %d DLL %q: %w", pid, dll, err)
		}
	}
	if err := waitForVisibleWindow(ctx, pid, spec.timeout()); err != nil {
		return LaunchResult{}, err
	}
	return LaunchResult{PID: pid, InjectionVerified: spec.InjectMode != "Bypass", Warnings: warnings}, nil
}

func injectNativeDLL(ctx context.Context, pid int, path string, timeout time.Duration) (uint64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if runtime.GOARCH != "amd64" {
		return 0, errors.New("native injection requires an x64 launcher")
	}
	if err := validateRegularLocalFile(path); err != nil {
		return 0, fmt.Errorf("validate DLL: %w", err)
	}
	widePath, err := windows.UTF16FromString(path)
	if err != nil {
		return 0, fmt.Errorf("encode DLL path: %w", err)
	}
	process, err := windows.OpenProcess(
		windows.PROCESS_QUERY_INFORMATION|windows.PROCESS_CREATE_THREAD|windows.PROCESS_VM_OPERATION|
			windows.PROCESS_VM_READ|windows.PROCESS_VM_WRITE|windows.SYNCHRONIZE,
		false, uint32(pid),
	)
	if err != nil {
		return 0, fmt.Errorf("open target process: %w", err)
	}
	call := &nativeCall{process: process}
	defer func() { call.close() }()
	var processMachine, nativeMachine uint16
	if err := windows.IsWow64Process2(process, &processMachine, &nativeMachine); err != nil {
		return 0, fmt.Errorf("query target architecture: %w", err)
	}
	if processMachine == pe.IMAGE_FILE_MACHINE_UNKNOWN {
		processMachine = nativeMachine
	}
	if processMachine != pe.IMAGE_FILE_MACHINE_AMD64 {
		return 0, fmt.Errorf("native injection requires an x64 target (machine %#x)", processMachine)
	}
	loadLibrary, err := remoteSystemProc(ctx, pid, "LoadLibraryW")
	if err != nil {
		return 0, err
	}
	getLastError, err := remoteSystemProc(ctx, pid, "GetLastError")
	if err != nil {
		return 0, err
	}

	// Allocate for the actual UTF-16 path, including its terminator, rather than a fixed page.
	const headerSize = 40
	data := make([]byte, headerSize+len(widePath)*2)
	call.data, _, err = nativeVirtualAllocEx.Call(uintptr(process), 0, uintptr(len(data)),
		windows.MEM_COMMIT|windows.MEM_RESERVE, windows.PAGE_READWRITE)
	if call.data == 0 {
		return 0, fmt.Errorf("allocate remote arguments: %w", err)
	}
	binary.LittleEndian.PutUint64(data[0:8], uint64(loadLibrary))
	binary.LittleEndian.PutUint64(data[8:16], uint64(getLastError))
	binary.LittleEndian.PutUint64(data[16:24], uint64(call.data)+headerSize)
	for i, unit := range widePath {
		binary.LittleEndian.PutUint16(data[headerSize+i*2:], unit)
	}
	if err := writeNativeMemory(process, call.data, data); err != nil {
		return 0, fmt.Errorf("write remote arguments: %w", err)
	}
	call.code, _, err = nativeVirtualAllocEx.Call(uintptr(process), 0, uintptr(len(nativeLoadCode)),
		windows.MEM_COMMIT|windows.MEM_RESERVE, windows.PAGE_READWRITE)
	if call.code == 0 {
		return 0, fmt.Errorf("allocate remote code: %w", err)
	}
	if err := writeNativeMemory(process, call.code, nativeLoadCode); err != nil {
		return 0, fmt.Errorf("write remote code: %w", err)
	}
	var oldProtection uint32
	if err := windows.VirtualProtectEx(process, call.code, uintptr(len(nativeLoadCode)),
		windows.PAGE_EXECUTE_READ, &oldProtection); err != nil {
		return 0, fmt.Errorf("protect remote code: %w", err)
	}
	if ok, _, err := nativeFlushInstructionCache.Call(
		uintptr(process),
		call.code,
		uintptr(len(nativeLoadCode)),
	); ok == 0 {
		return 0, fmt.Errorf("flush remote instruction cache: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	thread, _, err := nativeCreateRemoteThread.Call(uintptr(process), 0, 0, call.code, call.data, 0, 0)
	if thread == 0 {
		return 0, fmt.Errorf("create remote thread: %w", err)
	}
	call.thread = windows.Handle(thread)
	if err := waitNativeThread(ctx, call.thread, timeout); err != nil {
		// A running LoadLibraryW cannot be safely terminated or have its memory freed.
		// Transfer ownership to cleanup, which waits for the thread or target to exit.
		go call.closeAfterExit()
		call = &nativeCall{}
		return 0, fmt.Errorf("wait for remote DLL load (cleanup pending until thread or process exit): %w", err)
	}
	var exitCode uint32
	if ok, _, err := nativeGetExitCodeThread.Call(uintptr(call.thread), uintptr(unsafe.Pointer(&exitCode))); ok == 0 {
		return 0, fmt.Errorf("read remote thread exit code: %w", err)
	}
	if exitCode != 0 {
		return 0, fmt.Errorf("remote thread exited abnormally: %#x", exitCode)
	}
	result := make([]byte, headerSize)
	var read uintptr
	if err := windows.ReadProcessMemory(process, call.data, &result[0], uintptr(len(result)), &read); err != nil {
		return 0, fmt.Errorf("read remote DLL load result: %w", err)
	}
	if read != uintptr(len(result)) {
		return 0, errors.New("incomplete remote DLL load result")
	}
	module := binary.LittleEndian.Uint64(result[24:32])
	if module == 0 {
		code := binary.LittleEndian.Uint32(result[32:36])
		if code == 0 {
			return 0, errors.New("remote LoadLibraryW returned a null module without an error code")
		}
		return 0, fmt.Errorf("remote LoadLibraryW (Windows error %d): %w", code, windows.Errno(code))
	}
	return module, nil
}

func remoteSystemProc(ctx context.Context, pid int, name string) (uintptr, error) {
	proc := nativeKernel32.NewProc(name)
	if err := proc.Find(); err != nil {
		return 0, fmt.Errorf("resolve %s locally: %w", name, err)
	}
	// Forwarded kernel32 exports may live in KernelBase. Find the actual owning DLL
	// and apply its RVA to the target module, rather than assuming identical ASLR bases.
	var info windows.MemoryBasicInformation
	if err := windows.VirtualQuery(proc.Addr(), &info, unsafe.Sizeof(info)); err != nil {
		return 0, fmt.Errorf("locate %s module: %w", name, err)
	}
	buffer := make([]uint16, windows.MAX_LONG_PATH)
	if _, err := windows.GetModuleFileName(
		windows.Handle(info.AllocationBase),
		&buffer[0],
		uint32(len(buffer)),
	); err != nil {
		return 0, fmt.Errorf("read %s module name: %w", name, err)
	}
	moduleName := filepath.Base(windows.UTF16ToString(buffer))
	snapshot, err := snapshotNativeModules(ctx, uint32(pid), windows.CreateToolhelp32Snapshot)
	if err != nil {
		return 0, fmt.Errorf("snapshot target modules for %s: %w", name, err)
	}
	defer func() { _ = windows.CloseHandle(snapshot) }()
	entry := windows.ModuleEntry32{Size: uint32(windows.SizeofModuleEntry32)}
	for err = windows.Module32First(snapshot, &entry); err == nil; err = windows.Module32Next(snapshot, &entry) {
		if strings.EqualFold(windows.UTF16ToString(entry.Module[:]), moduleName) {
			offset := proc.Addr() - info.AllocationBase
			if offset >= uintptr(entry.ModBaseSize) {
				return 0, fmt.Errorf("%s address is outside target module %q", name, moduleName)
			}
			return entry.ModBaseAddr + offset, nil
		}
	}
	if !errors.Is(err, windows.ERROR_NO_MORE_FILES) {
		return 0, fmt.Errorf("enumerate target modules for %s: %w", name, err)
	}
	return 0, fmt.Errorf("target module %q for %s is not loaded", moduleName, name)
}

func snapshotNativeModules(
	ctx context.Context,
	pid uint32,
	createSnapshot func(uint32, uint32) (windows.Handle, error),
) (windows.Handle, error) {
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		snapshot, err := createSnapshot(windows.TH32CS_SNAPMODULE, pid)
		if !errors.Is(err, windows.ERROR_BAD_LENGTH) {
			return snapshot, err
		}

		// The target loader can change the module list during startup. Retry only
		// this transient snapshot failure within the DLL load's shared deadline.
		select {
		case <-ctx.Done():
			return 0, errors.Join(err, ctx.Err())
		case <-ticker.C:
		}
	}
}

func writeNativeMemory(process windows.Handle, address uintptr, data []byte) error {
	var written uintptr
	if err := windows.WriteProcessMemory(process, address, &data[0], uintptr(len(data)), &written); err != nil {
		return err
	}
	if written != uintptr(len(data)) {
		return errors.New("incomplete remote memory write")
	}
	return nil
}

func waitNativeThread(ctx context.Context, thread windows.Handle, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		status, err := windows.WaitForSingleObject(thread, 50)
		if err != nil {
			return err
		}
		if status == windows.WAIT_OBJECT_0 {
			return nil
		}
		if status != uint32(windows.WAIT_TIMEOUT) {
			return fmt.Errorf("unexpected remote thread wait status %#x", status)
		}
	}
}

func (call *nativeCall) closeAfterExit() {
	status, err := windows.WaitForMultipleObjects([]windows.Handle{call.thread, call.process}, false, windows.INFINITE)
	if err != nil || status != windows.WAIT_OBJECT_0 {
		// On process exit allocations are already gone. On wait failure leave them
		// for process exit, since the remote thread may still be using both buffers.
		call.code, call.data = 0, 0
	}
	call.close()
}

func (call *nativeCall) close() {
	if call.code != 0 {
		_, _, _ = nativeVirtualFreeEx.Call(uintptr(call.process), call.code, 0, windows.MEM_RELEASE)
	}
	if call.data != 0 {
		_, _, _ = nativeVirtualFreeEx.Call(uintptr(call.process), call.data, 0, windows.MEM_RELEASE)
	}
	if call.thread != 0 {
		_ = windows.CloseHandle(call.thread)
	}
	if call.process != 0 {
		_ = windows.CloseHandle(call.process)
	}
}
