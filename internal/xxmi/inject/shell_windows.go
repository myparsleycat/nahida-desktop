//go:build windows

package inject

import (
	"context"
	"os/exec"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/windows"
)

// ShellCommand runs command through cmd.exe /C like Python's shell=True. cmd.exe does not understand the
// backslash quoting that exec.Command applies to arguments, so the command line is passed verbatim.
func ShellCommand(ctx context.Context, command string) (*exec.Cmd, error) {
	systemDir, err := windows.GetSystemDirectory()
	if err != nil {
		return nil, err
	}
	shell := filepath.Join(systemDir, "cmd.exe")
	cmd := exec.CommandContext(ctx, shell)
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: windows.EscapeArg(shell) + ` /C "` + command + `"`}
	return cmd, nil
}
