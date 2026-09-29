//go:build windows

package xxmi

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func TestRunLaunchHookPassesQuotedPathsToCmd(t *testing.T) {
	t.Parallel()
	systemDir, err := windows.GetSystemDirectory()
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "with space")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(dir, "marker file.txt")
	command := `"` + filepath.Join(systemDir, "where.exe") + `" cmd > "` + marker + `"`
	if err := runLaunchHook(t.Context(), CommandHook{Enabled: true, Command: command, Wait: true}, dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("quoted command did not run: %v", err)
	}
}

func TestRunLaunchHookSkipsEmptyCommand(t *testing.T) {
	t.Parallel()
	if err := runLaunchHook(t.Context(), CommandHook{Enabled: true, Command: "  ", Wait: true}, ""); err != nil {
		t.Fatalf("runLaunchHook() error = %v, want an enabled empty command skipped", err)
	}
}

func TestRunLaunchHookReportsExitCode(t *testing.T) {
	t.Parallel()
	err := runLaunchHook(t.Context(), CommandHook{Enabled: true, Command: "exit /b 3", Wait: true}, t.TempDir())
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 3 {
		t.Fatalf("error = %v, want exit code 3", err)
	}
}
