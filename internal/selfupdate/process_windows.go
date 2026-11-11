package selfupdate

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"nahida.live/desktop/internal/platform"
)

// currentExecutable resolves junctions and links so the staging directory lands on the executable's volume.
func currentExecutable() (string, error) {
	path, err := os.Executable()
	if err != nil {
		return "", err
	}
	return platform.FinalPath(path)
}

type execHelper struct{ cmd *exec.Cmd }

func (h execHelper) Abort() {
	_ = h.cmd.Process.Kill()
	_ = h.cmd.Wait()
}

func (h execHelper) Detach() { _ = h.cmd.Process.Release() }

// spawnHelper starts the helper detached from this process. Launchers and file managers often run the
// application in a kill-on-close job, which would end the helper together with the application unless it
// breaks away from that job.
func spawnHelper(executable string, env []string) (helperProcess, error) {
	cmd := exec.Command(executable)
	cmd.Env = env
	cmd.Dir = filepath.Dir(executable)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow: true,
		CreationFlags: windows.DETACHED_PROCESS | windows.CREATE_NO_WINDOW | windows.CREATE_NEW_PROCESS_GROUP |
			windows.CREATE_BREAKAWAY_FROM_JOB,
	}
	// A job that neither kills its processes on close nor allows breakaway would reject the flag, and keeping
	// the helper in it is harmless. A failed query, including no job at all, keeps the flag.
	var info windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	err := windows.QueryInformationJobObject(0, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)), nil)
	const needsBreakaway = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE | windows.JOB_OBJECT_LIMIT_BREAKAWAY_OK |
		windows.JOB_OBJECT_LIMIT_SILENT_BREAKAWAY_OK
	if err == nil && info.BasicLimitInformation.LimitFlags&needsBreakaway == 0 {
		cmd.SysProcAttr.CreationFlags &^= windows.CREATE_BREAKAWAY_FROM_JOB
	}

	if err := cmd.Start(); err != nil {
		if errors.Is(err, windows.ERROR_ACCESS_DENIED) {
			return nil, fmt.Errorf("the application's job does not let the update helper break away: %w", err)
		}
		return nil, err
	}
	return execHelper{cmd: cmd}, nil
}

func launchDetached(path string) error {
	cmd := exec.Command(path)
	cmd.Dir = filepath.Dir(path)
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

type parentProcess struct{ handle windows.Handle }

// openParent returns an empty handle when pid is unset or the process has already exited.
func openParent(pid int) (parentProcess, error) {
	if pid <= 0 {
		return parentProcess{}, nil
	}
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
		return parentProcess{}, nil
	}
	if err != nil {
		return parentProcess{}, err
	}
	return parentProcess{handle: handle}, nil
}

func (p parentProcess) wait(timeout time.Duration) error {
	if p.handle == 0 {
		return nil
	}
	event, err := windows.WaitForSingleObject(p.handle, uint32(timeout.Milliseconds()))
	if err != nil {
		return err
	}
	if event != windows.WAIT_OBJECT_0 {
		return fmt.Errorf("still running after %s", timeout)
	}
	return nil
}

func (p parentProcess) close() {
	if p.handle != 0 {
		_ = windows.CloseHandle(p.handle)
	}
}
