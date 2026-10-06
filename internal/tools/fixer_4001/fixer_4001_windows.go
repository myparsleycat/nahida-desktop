//go:build windows

package fixer4001

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"nahida.live/desktop/internal/elevated"
)

func executeD3DBuild(ctx context.Context, vcvarsPath, projectPath, gitPath string) error {
	script, err := d3dBuildCommand(vcvarsPath, projectPath)
	if err != nil {
		return err
	}
	cmd := cmdScript(ctx, script)

	// The solution's version generator calls git from PATH, which the portable Git is not on.
	cmd.Env = append(os.Environ(), "PATH="+filepath.Dir(gitPath)+string(os.PathListSeparator)+os.Getenv("PATH"))
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("build failed: %w\n%s", err, tailBuildOutput(output.String(), 120))
	}
	return nil
}

func d3dBuildCommand(vcvarsPath, projectPath string) (string, error) {
	vcvars, err := cmdQuotedLocalPath(vcvarsPath)
	if err != nil {
		return "", fmt.Errorf("quote vcvars path %q: %w", vcvarsPath, err)
	}
	project, err := cmdQuotedLocalPath(projectPath)
	if err != nil {
		return "", fmt.Errorf("quote project path %q: %w", projectPath, err)
	}

	// vcvars64.bat can exit non-zero for non-fatal SDK detection errors while msbuild still resolves the SDK
	// through its own props, so its exit code must not gate the build.
	return vcvars + ` & cd /d ` + project + ` && msbuild StereovisionHacks.sln /nologo /verbosity:minimal /p:Configuration=Release /p:Platform=x64`, nil
}

func cmdQuotedLocalPath(path string) (string, error) {
	if !isLocalFilesystemPath(path) || strings.ContainsAny(path, "\"%&|<>^!\r\n") {
		return "", errors.New("invalid build path")
	}
	return `"` + path + `"`, nil
}

// cmdScript runs a cmd.exe script without Go's Windows argv quoting.
// exec.Command would turn inner quotes into \", which cmd.exe then treats as
// part of the command name (the original Electron exec() path does not).
func cmdScript(ctx context.Context, script string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "cmd.exe")
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow: true,
		CmdLine:    `cmd.exe /d /s /c "` + script + `"`,
	}
	return cmd
}

func elevatedCopyFiles(ctx context.Context, lease *elevated.FileLease, copies []fileCopy) error {
	ops := make([]elevated.FileOp, 0, len(copies))
	for _, item := range copies {
		op, err := elevated.NewCopyOp(item.Source, item.Target)
		if err != nil {
			return fmt.Errorf("XXMI_ERR_ELEVATED_COPY_FAILED: %w", err)
		}
		ops = append(ops, op)
	}
	if err := lease.Apply(ctx, ops); err != nil {
		return fmt.Errorf("XXMI_ERR_ELEVATED_COPY_FAILED: %w", err)
	}
	return nil
}

func elevatedRemoveFiles(ctx context.Context, lease *elevated.FileLease, paths []string) error {
	ops := make([]elevated.FileOp, 0, len(paths))
	for _, path := range paths {
		ops = append(ops, elevated.FileOp{Kind: elevated.FileOpRemove, Target: path})
	}
	if err := lease.Apply(ctx, ops); err != nil {
		return fmt.Errorf("XXMI_ERR_ELEVATED_REMOVE_FAILED: %w", err)
	}
	return nil
}
