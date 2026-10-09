//go:build windows

package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/google/uuid"
	"golang.org/x/sys/windows"

	"nahida.live/desktop/internal/infra"
)

// runScript runs one prepared script to completion and returns what it printed. A non-zero exit code
// or a timeout is a result the model can act on, not an error.
func (e *toolExecutor) runScript(ctx context.Context, input scriptInput, directory string) (scriptResult, error) {
	lookup := e.lookupInterpreter
	if lookup == nil {
		lookup = lookupScriptInterpreter
	}
	interpreter, err := lookup(ctx, input.Language)
	if err != nil {
		return scriptResult{}, err
	}
	if e.appData == nil {
		return scriptResult{}, errors.New("agent app data is unavailable")
	}
	fail := func(stage string, cause error) (scriptResult, error) {
		return scriptResult{}, infra.ReportError(e.log, cause, "Agent", infra.Diagnostic{
			Operation: "agent-script", Stage: stage,
			Fields: map[string]any{
				"sessionId": e.sessionID, "language": input.Language, "executable": interpreter.path,
				"workingDirectory": directory,
			},
		})
	}

	extension, content := ".py", []byte(input.Script)
	if input.Language == scriptLanguagePowerShell {
		// Windows PowerShell reads a script without a BOM in the ANSI code page.
		extension, content = ".ps1", append([]byte{0xef, 0xbb, 0xbf}, content...)
	}
	relative := filepath.Join("agent", "scripts", e.sessionID, uuid.NewString()+extension)
	if err := e.appData.WriteFile(relative, content, 0o600); err != nil {
		return fail("write-script", err)
	}
	file, err := e.appData.Resolve(relative)
	if err != nil {
		return fail("write-script", err)
	}
	defer func() { _ = os.Remove(file) }()

	arguments := append(slices.Clone(interpreter.args), file)
	if input.Language == scriptLanguagePowerShell {
		arguments = []string{
			"-NoLogo", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-OutputFormat", "Text",
			"-Command", "[Console]::OutputEncoding=[Text.UTF8Encoding]::new($false); & '" +
				strings.ReplaceAll(file, "'", "''") + "'; exit $LASTEXITCODE",
		}
	}

	job, err := newProcessJob()
	if err != nil {
		return fail("create-job", err)
	}
	defer job.close()

	runCtx, cancel := context.WithTimeout(ctx, time.Duration(input.TimeoutSeconds)*time.Second)
	defer cancel()
	var stdout, stderr boundedOutput
	command := exec.CommandContext(runCtx, interpreter.path, arguments...)
	command.Dir = directory
	command.Env = append(os.Environ(), "PYTHONIOENCODING=utf-8", "PYTHONUTF8=1")
	command.Stdout, command.Stderr = &stdout, &stderr
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
	command.Cancel = job.terminate
	// A child that outlives the script keeps the output pipes open; do not wait for it.
	command.WaitDelay = 2 * time.Second

	started := time.Now()
	if err := command.Start(); err != nil {
		return fail("start", err)
	}
	// Children started before this call escape the job, which an interpreter does not do while it
	// is still loading.
	if err := job.assign(command.Process.Pid); err != nil {
		_ = command.Process.Kill()
		_ = command.Wait()
		return fail("assign-job", err)
	}
	waitErr := command.Wait()

	if err := ctx.Err(); err != nil {
		return scriptResult{}, err
	}
	if command.ProcessState == nil {
		return fail("wait", waitErr)
	}
	result := scriptResult{
		ExitCode:   command.ProcessState.ExitCode(),
		TimedOut:   errors.Is(runCtx.Err(), context.DeadlineExceeded),
		DurationMS: time.Since(started).Milliseconds(),
	}
	result.Stdout, result.StdoutTruncated = stdout.text()
	result.Stderr, result.StderrTruncated = stderr.text()
	return fitScriptResult(result), nil
}

func lookupScriptInterpreter(ctx context.Context, language string) (scriptInterpreter, error) {
	if language == scriptLanguagePowerShell {
		builtin := filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
		if _, err := os.Stat(builtin); err == nil {
			return scriptInterpreter{path: builtin}, nil
		}
		path, err := exec.LookPath("powershell.exe")
		if err != nil {
			return scriptInterpreter{}, fmt.Errorf("%w: Windows PowerShell was not found", errScriptInterpreterMissing)
		}
		return scriptInterpreter{path: path}, nil
	}

	for _, candidate := range []scriptInterpreter{
		{path: "python", args: []string{"-u"}},
		{path: "py", args: []string{"-3", "-u"}},
	} {
		path, err := exec.LookPath(candidate.path)
		if err != nil {
			continue
		}
		candidate.path = path
		if pythonStarts(ctx, candidate) {
			return candidate, nil
		}
	}
	if err := ctx.Err(); err != nil {
		return scriptInterpreter{}, err
	}
	return scriptInterpreter{}, fmt.Errorf(
		"%w: Python is not installed or the python command is not on PATH", errScriptInterpreterMissing,
	)
}

// pythonStarts tells a working interpreter from the Microsoft Store placeholder that Windows puts on
// PATH as python.exe: the placeholder is found by LookPath but exits with an error once it gets
// arguments.
func pythonStarts(ctx context.Context, interpreter scriptInterpreter) bool {
	probeCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	command := exec.CommandContext(probeCtx, interpreter.path, append(slices.Clone(interpreter.args), "-c", "")...)
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
	command.WaitDelay = time.Second
	return command.Run() == nil
}

// processJob ties a process tree to a handle: closing the handle, including when the application
// dies, ends every process in the job.
type processJob struct {
	handle windows.Handle
}

func newProcessJob() (*processJob, error) {
	handle, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, err
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
			LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
		},
	}
	if _, err := windows.SetInformationJobObject(
		handle,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	); err != nil {
		_ = windows.CloseHandle(handle)
		return nil, err
	}
	return &processJob{handle: handle}, nil
}

func (j *processJob) assign(pid int) error {
	process, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		return err
	}
	defer func() { _ = windows.CloseHandle(process) }()
	return windows.AssignProcessToJobObject(j.handle, process)
}

func (j *processJob) terminate() error {
	return windows.TerminateJobObject(j.handle, 1)
}

func (j *processJob) close() {
	_ = windows.CloseHandle(j.handle)
}
