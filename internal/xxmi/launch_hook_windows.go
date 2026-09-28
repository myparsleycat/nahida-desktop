//go:build windows

package xxmi

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

func runLaunchHook(ctx context.Context, hook CommandHook, workDir string) error {
	if !hook.Enabled {
		return nil
	}
	if strings.TrimSpace(hook.Command) == "" {
		return errors.New("launch hook command is empty")
	}
	systemDir, err := windows.GetSystemDirectory()
	if err != nil {
		return err
	}
	command := exec.CommandContext(ctx, filepath.Join(systemDir, "cmd.exe"), "/C", hook.Command)
	command.Dir = workDir
	if hook.Wait {
		return command.Run()
	}
	if err := command.Start(); err != nil {
		return err
	}
	return command.Process.Release()
}
