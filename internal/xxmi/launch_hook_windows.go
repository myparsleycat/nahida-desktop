//go:build windows

package xxmi

import (
	"context"
	"strings"

	"nahida.live/desktop/internal/xxmi/inject"
)

// runLaunchHook runs a user command through cmd.exe. The reference launcher ignores the command's exit code,
// so a non-zero exit is returned as *exec.ExitError for the caller to downgrade to a warning. An enabled but
// empty command is skipped, as the reference launcher does. An empty workDir inherits the current directory.
func runLaunchHook(ctx context.Context, hook CommandHook, workDir string) error {
	if !hook.Enabled || strings.TrimSpace(hook.Command) == "" {
		return nil
	}

	// A detached command must outlive the launch request, so only a waited command is bound to ctx.
	commandCtx := context.Background()
	if hook.Wait {
		commandCtx = ctx
	}
	command, err := inject.ShellCommand(commandCtx, hook.Command)
	if err != nil {
		return err
	}
	command.Dir = workDir
	if hook.Wait {
		return command.Run()
	}
	if err := command.Start(); err != nil {
		return err
	}
	return command.Process.Release()
}
