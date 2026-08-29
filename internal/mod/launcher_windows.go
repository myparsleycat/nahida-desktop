//go:build windows

package mod

import (
	"os/exec"
	"path/filepath"
	"syscall"
)

func startDetached(path string) error {
	command := exec.Command(path)
	command.Dir = filepath.Dir(path)
	command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x00000008, HideWindow: true}
	return command.Start()
}
