//go:build windows

package app

import (
	"os"
	"os/exec"
	"syscall"
)

const (
	detachedProcess    = 0x00000008
	createNoWindow     = 0x08000000
	createNewProcGroup = 0x00000200
)

// relaunchSysProcAttr detaches the child so it survives Quit without inheriting
// a console. HideWindow and CREATE_NO_WINDOW must stay off: those flags belong
// on the updater helper, which has no UI. HideWindow sets STARTF_USESHOWWINDOW
// with SW_HIDE, and Windows then ignores the child's first ShowWindow call.
func relaunchSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{CreationFlags: detachedProcess | createNewProcGroup}
}

func spawnDetachedRelaunch(exe string, pid int) error {
	cmd := exec.Command(exe)
	cmd.Env = relaunchChildEnv(os.Environ(), pid)
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.SysProcAttr = relaunchSysProcAttr()
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

func relaunchProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	const processQueryLimitedInformation = 0x1000
	const stillActive = 259
	handle, err := syscall.OpenProcess(processQueryLimitedInformation, false, uint32(pid))
	if err != nil {
		return false
	}
	defer func() { _ = syscall.CloseHandle(handle) }()
	var code uint32
	if err := syscall.GetExitCodeProcess(handle, &code); err != nil {
		return false
	}
	return code == stillActive
}
