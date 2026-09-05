//go:build windows

package tools

import (
	"errors"
	"os"
	"os/exec"

	"strconv"

	"nahida.live/desktop/internal/infra"
)

func killProcessTree(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	treeErr := exec.Command("taskkill", "/pid", strconv.Itoa(cmd.Process.Pid), "/f", "/t").Run()
	killErr := cmd.Process.Kill()
	if treeErr == nil || errors.Is(killErr, os.ErrProcessDone) {
		return nil
	}
	return infra.AnnotateError(infra.WithCause(treeErr, killErr), infra.Diagnostic{Operation: "execute-script", Stage: "terminate-tree", Fields: map[string]any{"pid": cmd.Process.Pid, "executable": cmd.Path, "parentTerminated": killErr == nil}})
}
