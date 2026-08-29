package tools

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"

	"nahida.live/desktop/internal/db"
)

var ansiEscapeRE = regexp.MustCompile(`[\x1B\x9B][[()#;?]*(?:[0-9]{1,4}(?:;[0-9]{0,4})*)?[0-9A-ORZcf-nqry=><]`)

type scriptExecutor struct {
	mu    sync.Mutex
	cmd   *exec.Cmd
	stdin io.WriteCloser
	onLog func(string, bool)
}

func newScriptExecutor(onLog func(string, bool)) *scriptExecutor {
	return &scriptExecutor{onLog: onLog}
}

func (e *scriptExecutor) execute(ctx context.Context, filePath string, scriptType db.ScriptType, cwd string, args []string) error {
	var cmd *exec.Cmd
	switch scriptType {
	case db.ScriptTypePython:
		pythonArgs := append([]string{"-u", filePath}, args...)
		cmd = exec.CommandContext(ctx, "python", pythonArgs...)
	case db.ScriptTypeExec:
		cmd = exec.CommandContext(ctx, filePath, args...)
	default:
		return fmt.Errorf("unsupported script type: %s", scriptType)
	}
	cmd.Dir = cwd
	cmd.Env = append(os.Environ(), "PYTHONIOENCODING=utf-8", "PYTHONUTF8=1", "PYTHONLEGACYWINDOWSSTDIO=1")

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		return err
	}

	e.mu.Lock()
	e.cmd = cmd
	e.stdin = stdin
	e.mu.Unlock()

	var streams sync.WaitGroup
	streams.Add(2)
	go func() { defer streams.Done(); e.consume(stdout) }()
	go func() { defer streams.Done(); e.consume(stderr) }()

	killDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			killProcessTree(cmd)
		case <-killDone:
		}
	}()
	waitErr := cmd.Wait()
	close(killDone)
	streams.Wait()

	e.mu.Lock()
	e.cmd = nil
	e.stdin = nil
	e.mu.Unlock()
	_ = stdin.Close()

	if ctx.Err() != nil {
		return context.Canceled
	}
	if waitErr != nil {
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) {
			return fmt.Errorf("process exited with code %d", exitErr.ExitCode())
		}
		return waitErr
	}
	return nil
}

func (e *scriptExecutor) consume(reader io.Reader) {
	chunk := make([]byte, 32*1024)
	buffer := ""
	hasPartialLog := false
	var readErr error
	for {
		count, err := reader.Read(chunk)
		if count > 0 {
			buffer, hasPartialLog = e.consumeOutputChunk(buffer+string(chunk[:count]), hasPartialLog)
		}
		if err != nil {
			if !errors.Is(err, io.EOF) {
				readErr = err
			}
			break
		}
	}

	final := strings.TrimSpace(ansiEscapeRE.ReplaceAllString(buffer, ""))
	if final != "" {
		for _, line := range strings.Split(strings.ReplaceAll(final, "\r\n", "\n"), "\n") {
			if strings.TrimSpace(line) != "" {
				e.emit(line, hasPartialLog)
				hasPartialLog = false
			}
		}
	}
	if readErr != nil {
		e.emit("Output read error: "+readErr.Error(), false)
	}
}

func (e *scriptExecutor) consumeOutputChunk(buffer string, hasPartialLog bool) (string, bool) {
	lastEscape := strings.LastIndex(buffer, "\x1b")
	toProcess := buffer
	remaining := ""
	if lastEscape >= 0 && len(buffer)-lastEscape < 10 {
		toProcess = buffer[:lastEscape]
		remaining = buffer[lastEscape:]
	}
	if toProcess == "" {
		return remaining, hasPartialLog
	}

	clean := ansiEscapeRE.ReplaceAllString(toProcess, "")
	clean = strings.ReplaceAll(clean, "\r\n", "\n")
	lines := strings.Split(clean, "\n")
	endsWithNewline := strings.HasSuffix(clean, "\n") || strings.HasSuffix(clean, "\r")
	processCount := len(lines)
	if !endsWithNewline {
		processCount--
	}
	for index := range processCount {
		if strings.TrimSpace(lines[index]) != "" {
			e.emit(lines[index], hasPartialLog)
		}
		hasPartialLog = false
	}

	if !endsWithNewline {
		lastLine := lines[len(lines)-1]
		remaining = lastLine + remaining
		partial := strings.TrimSpace(ansiEscapeRE.ReplaceAllString(remaining, ""))
		if partial != "" {
			e.emit(partial, hasPartialLog)
			if isScriptAutoReplyPrompt(partial) {
				remaining = ""
				hasPartialLog = false
			} else {
				hasPartialLog = true
			}
		}
	}
	return remaining, hasPartialLog
}

func (e *scriptExecutor) emit(message string, replaceLast bool) {
	if message == "" {
		return
	}
	if e.onLog != nil {
		e.onLog(message, replaceLast)
	}
	if isScriptAutoReplyPrompt(message) {
		e.sendInput("\n")
	}
}

func isScriptAutoReplyPrompt(message string) bool {
	lower := strings.ToLower(message)
	for _, prompt := range []string{"press any key", `press "enter" to quit`, "done"} {
		if strings.Contains(lower, prompt) {
			return true
		}
	}
	return false
}

func (e *scriptExecutor) sendInput(input string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.stdin == nil {
		return false
	}
	_, err := io.WriteString(e.stdin, input)
	return err == nil
}
