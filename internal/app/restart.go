package app

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	"nahida.live/desktop/internal/infra"
)

const (
	relaunchWaitPIDEnv  = "NAHIDA_RELAUNCH_WAIT_PID"
	relaunchWaitTimeout = 30 * time.Second

	detachedProcess    = 0x00000008
	createNoWindow     = 0x08000000
	createNewProcGroup = 0x00000200
)

var (
	executablePath       = os.Executable
	currentPID           = os.Getpid
	spawnRelaunchProcess = spawnDetachedRelaunch
	relaunchParentAlive  = relaunchProcessAlive
)

// restartApp starts a new instance and quits this one. The new instance waits for this process to exit before
// it takes the single-instance lock, so it does not hand its launch back here and exit.
func restartApp(log *infra.Log, quit func()) error {
	exe, err := executablePath()
	if err != nil {
		return reportRestartError(log, fmt.Errorf("restart.failed: %w", err), "resolve-executable", 0, "")
	}
	pid := currentPID()
	if err := spawnRelaunchProcess(exe, pid); err != nil {
		return reportRestartError(log, fmt.Errorf("restart.failed: %w", err), "spawn", pid, exe)
	}
	quit()
	return nil
}

func reportRestartError(log *infra.Log, err error, stage string, pid int, exe string) error {
	fields := map[string]any{}
	if pid > 0 {
		fields["pid"] = pid
	}
	if exe != "" {
		fields["executable"] = exe
	}
	return infra.ReportError(log, err, "App.Restart", infra.Diagnostic{
		Severity: infra.DiagnosticError, Operation: "App.Restart", Stage: stage, Fields: fields,
	})
}

// waitPendingRelaunch holds a relaunched instance until the process that started it has exited.
func waitPendingRelaunch() {
	waitPendingRelaunchFor(relaunchWaitTimeout)
}

func waitPendingRelaunchFor(timeout time.Duration) {
	value, ok := os.LookupEnv(relaunchWaitPIDEnv)
	if !ok || value == "" {
		return
	}
	if pid, err := strconv.Atoi(value); err == nil && pid > 0 {
		deadline := time.Now().Add(timeout)
		for time.Now().Before(deadline) && relaunchParentAlive(pid) {
			time.Sleep(100 * time.Millisecond)
		}
	}
	_ = os.Unsetenv(relaunchWaitPIDEnv)
}

func relaunchChildEnv(environ []string, pid int) []string {
	prefix := relaunchWaitPIDEnv + "="
	env := make([]string, 0, len(environ)+1)
	for _, item := range environ {
		if !strings.HasPrefix(item, prefix) {
			env = append(env, item)
		}
	}
	return append(env, prefix+strconv.Itoa(pid))
}

// relaunchSysProcAttr detaches the child so it survives Quit without inheriting a console. HideWindow and
// CREATE_NO_WINDOW must stay off: those flags belong on the updater helper, which has no UI. HideWindow sets
// STARTF_USESHOWWINDOW with SW_HIDE, and Windows then ignores the child's first ShowWindow call.
func relaunchSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{CreationFlags: detachedProcess | createNewProcGroup}
}

func spawnDetachedRelaunch(exe string, pid int) error {
	cmd := exec.Command(exe)
	cmd.Env = relaunchChildEnv(os.Environ(), pid)
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
