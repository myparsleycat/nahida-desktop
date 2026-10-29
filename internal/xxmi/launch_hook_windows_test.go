//go:build windows

package xxmi

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

type customCommandProbe struct {
	SID       string
	Elevated  bool
	Directory string
	ParentPID uint32
}

func TestCustomCommandTokenChild(t *testing.T) {
	path := os.Getenv("NAHIDA_CUSTOM_COMMAND_PROBE")
	if path == "" {
		return
	}
	token := windows.GetCurrentProcessToken()
	user, err := token.GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(customCommandProbe{SID: user.User.Sid.String(), Elevated: token.IsElevated(),
		Directory: dir, ParentPID: uint32(os.Getppid())})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	name, err := windows.UTF16PtrFromString(os.Getenv("NAHIDA_CUSTOM_COMMAND_EVENT"))
	if err != nil {
		t.Fatal(err)
	}
	event, err := windows.OpenEvent(windows.EVENT_MODIFY_STATE, false, name)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = windows.CloseHandle(event) }()
	if err := windows.SetEvent(event); err != nil {
		t.Fatal(err)
	}
}

func TestCustomCommandInheritsAppTokenAndDirectory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "probe.json")
	eventName := `Local\nahida-custom-command-test-` + rand.Text()
	name, err := windows.UTF16PtrFromString(eventName)
	if err != nil {
		t.Fatal(err)
	}
	event, err := windows.CreateEvent(nil, 1, 0, name)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = windows.CloseHandle(event) }()
	t.Setenv("NAHIDA_CUSTOM_COMMAND_PROBE", path)
	t.Setenv("NAHIDA_CUSTOM_COMMAND_EVENT", eventName)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := startCustomCommand(
		t.Context(),
		windows.EscapeArg(executable)+" -test.run=TestCustomCommandTokenChild",
		dir,
	); err != nil {
		t.Fatal(err)
	}
	state, err := windows.WaitForSingleObject(event, 10000)
	if err != nil || state != windows.WAIT_OBJECT_0 {
		t.Fatalf("child did not report token: state %d, error %v", state, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var probe customCommandProbe
	if err := json.Unmarshal(data, &probe); err != nil {
		t.Fatal(err)
	}
	process, err := windows.OpenProcess(windows.SYNCHRONIZE, false, probe.ParentPID)
	if err == nil {
		state, waitErr := windows.WaitForSingleObject(process, 10000)
		_ = windows.CloseHandle(process)
		if waitErr != nil || state != windows.WAIT_OBJECT_0 {
			t.Fatalf("command did not exit: %d, %v", state, waitErr)
		}
	} else if !errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
		t.Fatal(err)
	}
	token := windows.GetCurrentProcessToken()
	user, err := token.GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	if probe.SID != user.User.Sid.String() || probe.Elevated != token.IsElevated() {
		t.Fatalf("child token differs from app token: %+v", probe)
	}
	actual, err := os.Stat(probe.Directory)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(actual, expected) {
		t.Fatalf("child directory = %q, want %q", probe.Directory, dir)
	}
}

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
