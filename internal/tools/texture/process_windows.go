//go:build windows

package texture

import (
	"os/exec"
	"syscall"
)

func configureTextureCommand(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
}
