package app

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"nahida.live/desktop/internal/infra"
)

const (
	relaunchWaitPIDEnv  = "NAHIDA_RELAUNCH_WAIT_PID"
	relaunchWaitTimeout = 30 * time.Second
)

var (
	executablePath         = os.Executable
	currentPID             = os.Getpid
	spawnRelaunchProcess   = spawnDetachedRelaunch
	relaunchParentAlive    = relaunchProcessAlive
	currentApplication     = application.Get
	waitForPendingRelaunch = waitPendingRelaunch
	newApplication         = application.New
	quitApplication        = func(app *application.App) {
		if app != nil {
			app.Quit()
		}
	}
)

func waitPendingRelaunch() {
	waitPendingRelaunchFor(relaunchWaitTimeout)
}

func newLockedApplication(opts application.Options) *application.App {
	waitForPendingRelaunch()
	return newApplication(opts)
}

func waitPendingRelaunchFor(timeout time.Duration) {
	value, ok := os.LookupEnv(relaunchWaitPIDEnv)
	if !ok || value == "" {
		return
	}
	if pid, err := strconv.Atoi(value); err == nil && pid > 0 {
		deadline := time.Now().Add(timeout)
		for time.Now().Before(deadline) {
			if !relaunchParentAlive(pid) {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
	_ = os.Unsetenv(relaunchWaitPIDEnv)
}

func relaunchChildEnv(environ []string, pid int) []string {
	prefix := relaunchWaitPIDEnv + "="
	env := make([]string, 0, len(environ)+1)
	for _, item := range environ {
		if strings.HasPrefix(item, prefix) {
			continue
		}
		env = append(env, item)
	}
	return append(env, prefix+strconv.Itoa(pid))
}

func (w *Window) Restart() error {
	var app *application.App
	var log *infra.Log
	if w != nil {
		w.mu.Lock()
		app = w.app
		log = w.log
		w.mu.Unlock()
	}
	if app == nil {
		app = currentApplication()
	}
	if app == nil {
		return reportRestartError(log, errors.New("restart.failed"), "quit", 0, "")
	}
	exe, err := executablePath()
	if err != nil {
		return reportRestartError(log, fmt.Errorf("restart.failed: %w", err), "resolve-executable", 0, "")
	}
	pid := currentPID()
	if err := spawnRelaunchProcess(exe, pid); err != nil {
		return reportRestartError(log, fmt.Errorf("restart.failed: %w", err), "spawn", pid, exe)
	}
	quitApplication(app)
	return nil
}

func reportRestartError(log *infra.Log, err error, stage string, pid int, exe string) error {
	fields := map[string]any{"stage": stage}
	if pid > 0 {
		fields["pid"] = pid
	}
	if exe != "" {
		fields["executable"] = exe
	}
	return infra.ReportError(log, err, "Window.Restart", infra.Diagnostic{
		Severity:  infra.DiagnosticError,
		Operation: "Window.Restart",
		Stage:     stage,
		Fields:    fields,
	})
}
