//go:build windows

package tools

import (
	"os/exec"
	"syscall"
)

func configureTextureCommand(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
}
