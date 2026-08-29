//go:build windows

package tools

import (
	"os/exec"
	"strconv"
)

func killProcessTree(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = exec.Command("taskkill", "/pid", strconv.Itoa(cmd.Process.Pid), "/f", "/t").Run()
	_ = cmd.Process.Kill()
}
