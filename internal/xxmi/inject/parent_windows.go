//go:build windows

package inject

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"nahida.live/desktop/internal/platform"
)

// parentSettleDelay keeps the parent alive for a moment after the game window appears, as the reference
// launcher does, in case the game checks its parent again while it finishes starting.
const parentSettleDelay = 3 * time.Second

// parentProcess is a suspended, windowless instance of LaunchSpec.ParentExe. None of its code runs; it only
// has to exist while the game process is created as its child.
type parentProcess struct {
	path    string
	process windows.Handle
}

func (p *parentProcess) start() (windows.Handle, error) {
	if p.process != 0 {
		return p.process, nil
	}

	// Like the reference launcher, an instance that is already running is closed first.
	if err := terminateExecutable(p.path); err != nil {
		return 0, fmt.Errorf("close running %s: %w", filepath.Base(p.path), err)
	}

	application, err := windows.UTF16PtrFromString(p.path)
	if err != nil {
		return 0, err
	}
	command, err := windows.UTF16PtrFromString(windows.EscapeArg(p.path))
	if err != nil {
		return 0, err
	}
	directory, err := windows.UTF16PtrFromString(filepath.Dir(p.path))
	if err != nil {
		return 0, err
	}
	startup := windows.StartupInfo{Cb: uint32(unsafe.Sizeof(windows.StartupInfo{}))}
	var info windows.ProcessInformation
	err = windows.CreateProcess(application, command, nil, nil, false,
		windows.CREATE_SUSPENDED|windows.CREATE_NO_WINDOW, nil, directory, &startup, &info)
	if err != nil {
		return 0, fmt.Errorf("start %s: %w", filepath.Base(p.path), err)
	}
	_ = windows.CloseHandle(info.Thread)
	p.process = info.Process
	return p.process, nil
}

// stop terminates the parent. After a successful launch it first waits parentSettleDelay.
func (p *parentProcess) stop(ctx context.Context, settle bool) {
	if p.process == 0 {
		return
	}
	if settle {
		timer := time.NewTimer(parentSettleDelay)
		select {
		case <-ctx.Done():
			timer.Stop()
		case <-timer.C:
		}
	}
	_ = windows.TerminateProcess(p.process, 1)
	_ = windows.CloseHandle(p.process)
	p.process = 0
}

// terminateExecutable ends the processes running from path. Processes that only share its file name are left
// alone. Both sides are compared as final paths, since the launcher folder may be reached through a junction
// or a short name while a process reports its image by its real location.
func terminateExecutable(path string) error {
	target, err := platform.FinalPath(path)
	if err != nil {
		return err
	}

	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return err
	}
	defer func() { _ = windows.CloseHandle(snapshot) }()

	var pids []uint32
	entry := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	for err = windows.Process32First(snapshot, &entry); err == nil; err = windows.Process32Next(snapshot, &entry) {
		if strings.EqualFold(windows.UTF16ToString(entry.ExeFile[:]), filepath.Base(target)) {
			pids = append(pids, entry.ProcessID)
		}
	}

	for _, pid := range pids {
		process, err := windows.OpenProcess(
			windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.PROCESS_TERMINATE|windows.SYNCHRONIZE, false, pid,
		)
		if err != nil {
			// The process exited, or belongs to someone this helper cannot inspect.
			continue
		}
		buffer := make([]uint16, windows.MAX_LONG_PATH)
		size := uint32(len(buffer))
		if windows.QueryFullProcessImageName(process, 0, &buffer[0], &size) != nil {
			_ = windows.CloseHandle(process)
			continue
		}
		image, err := platform.FinalPath(windows.UTF16ToString(buffer[:size]))
		if err != nil || !platform.SamePathFold(image, target) {
			_ = windows.CloseHandle(process)
			continue
		}
		err = windows.TerminateProcess(process, 1)
		if err == nil {
			_, _ = windows.WaitForSingleObject(process, 5000)
		}
		_ = windows.CloseHandle(process)
		if err != nil {
			return err
		}
	}
	return nil
}
