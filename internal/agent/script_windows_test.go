//go:build windows

package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"

	"nahida.live/desktop/internal/appdata"
	"nahida.live/desktop/internal/db"
)

func newScriptExecutor(t *testing.T) (*toolExecutor, string) {
	t.Helper()
	root := t.TempDir()
	sandbox, err := NewSandbox([]SandboxRoot{{ID: "root", Name: "Root", Path: root}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sandbox.Close() })
	store, err := appdata.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return &toolExecutor{sandbox: sandbox, appData: store, sessionID: "session"}, root
}

func scriptCall(t *testing.T, input scriptInput) ToolCall {
	t.Helper()
	arguments, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	return ToolCall{ID: "call-1", Name: "run_script", Arguments: arguments}
}

// A script must not start until the user approved it: without a grant the call only produces an
// approval request, and a read-only folder is refused before any request is made.
func TestRunScriptWaitsForApproval(t *testing.T) {
	t.Parallel()
	executor, root := newScriptExecutor(t)
	executor.lookupInterpreter = func(context.Context, string) (scriptInterpreter, error) {
		t.Error("interpreter resolved before approval")
		return scriptInterpreter{}, errScriptInterpreterMissing
	}

	execution, err := executor.Execute(context.Background(), scriptCall(t, scriptInput{
		Language: "PowerShell", Script: "Set-Content marker.txt ran", RootID: "root",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if execution.Approval == nil || execution.Approval.ActionID != scriptActionID ||
		execution.Approval.Kind != "script" || execution.Output != nil {
		t.Fatalf("execution = %#v", execution)
	}
	var sealed scriptInput
	if err := json.Unmarshal(execution.Approval.Arguments, &sealed); err != nil {
		t.Fatal(err)
	}
	if sealed.Language != scriptLanguagePowerShell || sealed.TimeoutSeconds != defaultScriptTimeoutSeconds ||
		sealed.Script != "Set-Content marker.txt ran" {
		t.Fatalf("approval arguments = %#v", sealed)
	}
	if _, err := os.Stat(filepath.Join(root, "marker.txt")); !os.IsNotExist(err) {
		t.Fatalf("script ran before approval: %v", err)
	}

	reference := t.TempDir()
	sandbox, err := NewSandbox([]SandboxRoot{{ID: "core", Name: "Core", Path: reference, ReadOnly: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sandbox.Close() }()
	executor.sandbox, executor.scriptsPreapproved = sandbox, true
	execution, err = executor.Execute(context.Background(), scriptCall(t, scriptInput{
		Language: scriptLanguagePowerShell, Script: "Set-Content marker.txt ran", RootID: "core",
	}))
	if !errors.Is(err, errSandboxReadOnly) || execution.Approval != nil {
		t.Fatalf("read-only working directory: execution = %#v, err = %v", execution, err)
	}
}

// The conversation-wide grant is an event, so the events that survive a revert decide it.
func TestScriptsAllowedFollowsEvents(t *testing.T) {
	t.Parallel()
	events := []db.AgentEventRow{
		{EventType: "turn/start"}, {EventType: "approval/requested"}, {EventType: scriptSessionAllowedEvent},
	}
	if !scriptsAllowedFromEvents(events) {
		t.Fatal("grant event did not allow scripts")
	}
	if scriptsAllowedFromEvents(events[:2]) {
		t.Fatal("scripts stayed allowed after the grant was reverted")
	}
}

// Output that JSON escaping inflates must not push the result past the size where it is replaced by
// a cut-off preview, which would drop stderr and the exit code.
func TestScriptResultFitsToolOutput(t *testing.T) {
	t.Parallel()
	result := fitScriptResult(scriptResult{
		ExitCode: 2, Stdout: strings.Repeat("\x00", 22<<10), Stderr: "boom" + strings.Repeat("<", 22<<10),
	})
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) > maxToolOutput {
		t.Fatalf("encoded result is %d bytes, limit %d", len(encoded), maxToolOutput)
	}
	if result.ExitCode != 2 || !result.StdoutTruncated || !result.StderrTruncated ||
		!strings.HasPrefix(result.Stderr, "boom") {
		t.Fatalf("result = exit %d, truncated %v/%v", result.ExitCode, result.StdoutTruncated, result.StderrTruncated)
	}
}

func TestRunScriptReturnsPowerShellResult(t *testing.T) {
	t.Parallel()
	executor, root := newScriptExecutor(t)
	executor.scriptsPreapproved = true

	execution, err := executor.Execute(context.Background(), scriptCall(t, scriptInput{
		Language: scriptLanguagePowerShell, RootID: "root",
		Script: "Set-Content -Path marker.txt -Value ran\n" +
			"Write-Output '한글 출력'\n[Console]::Error.WriteLine('warning')\nexit 3\n",
	}))
	if err != nil {
		t.Fatal(err)
	}
	result, ok := execution.Output.(scriptResult)
	if !ok || result.ExitCode != 3 || result.TimedOut || strings.TrimSpace(result.Stdout) != "한글 출력" ||
		!strings.Contains(result.Stderr, "warning") {
		t.Fatalf("result = %#v", execution.Output)
	}
	if _, err := os.Stat(filepath.Join(root, "marker.txt")); err != nil {
		t.Fatalf("script did not run in the working directory: %v", err)
	}
	if entries, _ := os.ReadDir(
		filepath.Join(executor.appData.Root(), "agent", "scripts", "session"),
	); len(
		entries,
	) != 0 {
		t.Fatalf("script file left behind: %v", entries)
	}
}

// Stopping a run has to end the processes the script started, not only the interpreter.
func TestRunScriptCancellationEndsChildProcesses(t *testing.T) {
	t.Parallel()
	executor, root := newScriptExecutor(t)
	executor.scriptsPreapproved = true
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		_, err := executor.Execute(ctx, scriptCall(t, scriptInput{
			Language: scriptLanguagePowerShell, RootID: "root",
			Script: "$child = Start-Process ping -ArgumentList '-n','120','127.0.0.1' -WindowStyle Hidden -PassThru\n" +
				"Set-Content -Path child.pid -Value $child.Id\nStart-Sleep -Seconds 120\n",
		}))
		done <- err
	}()

	var pid int
	deadline := time.Now().Add(60 * time.Second)
	for pid == 0 {
		if data, err := os.ReadFile(filepath.Join(root, "child.pid")); err == nil {
			pid, _ = strconv.Atoi(strings.TrimSpace(string(data)))
		}
		if pid != 0 {
			break
		}
		select {
		case err := <-done:
			t.Fatalf("script ended before it reported its child: %v", err)
		case <-time.After(50 * time.Millisecond):
		}
		if time.Now().After(deadline) {
			t.Fatal("script never reported its child process")
		}
	}
	child, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		t.Fatalf("open child process: %v", err)
	}
	defer func() { _ = windows.CloseHandle(child) }()

	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled run err = %v", err)
	}
	if event, err := windows.WaitForSingleObject(child, 30_000); err != nil || event != windows.WAIT_OBJECT_0 {
		t.Fatalf("child process survived the cancelled run: event = %d, err = %v", event, err)
	}
}
