package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"nahida.live/desktop/internal/db"
)

const (
	scriptActionID            = "script.run"
	scriptSessionAllowedEvent = "script/session-allowed"
	scriptLanguagePython      = "python"
	scriptLanguagePowerShell  = "powershell"

	defaultScriptTimeoutSeconds = 120
	maxScriptTimeoutSeconds     = 600
	maxScriptBytes              = 256 << 10

	// Both streams together must stay below maxToolOutput so the model receives the result as
	// structured JSON instead of a cut-off preview.
	scriptOutputHeadBytes = 8 << 10
	scriptOutputTailBytes = 16 << 10
)

var errScriptInterpreterMissing = errors.New("script interpreter is not installed")

type scriptInput struct {
	Language         string `json:"language"`
	Script           string `json:"script"`
	RootID           string `json:"rootId"`
	WorkingDirectory string `json:"workingDirectory,omitempty"`
	TimeoutSeconds   int    `json:"timeoutSeconds,omitempty"`
}

type scriptResult struct {
	ExitCode        int    `json:"exitCode"`
	Stdout          string `json:"stdout"`
	Stderr          string `json:"stderr"`
	TimedOut        bool   `json:"timedOut,omitempty"`
	DurationMS      int64  `json:"durationMs"`
	StdoutTruncated bool   `json:"stdoutTruncated,omitempty"`
	StderrTruncated bool   `json:"stderrTruncated,omitempty"`
}

type scriptInterpreter struct {
	path string
	args []string
}

// prepareScript normalizes one run_script call and resolves its working directory, which must be a
// folder the sandbox lets the agent change.
func (e *toolExecutor) prepareScript(input scriptInput) (scriptInput, string, error) {
	input.Language = strings.ToLower(strings.TrimSpace(input.Language))
	if input.Language != scriptLanguagePython && input.Language != scriptLanguagePowerShell {
		return scriptInput{}, "", fmt.Errorf("unsupported script language %q: use python or powershell", input.Language)
	}
	if strings.TrimSpace(input.Script) == "" {
		return scriptInput{}, "", errors.New("script is empty")
	}
	if len(input.Script) > maxScriptBytes {
		return scriptInput{}, "", fmt.Errorf("script is larger than %d bytes", maxScriptBytes)
	}
	if input.TimeoutSeconds == 0 {
		input.TimeoutSeconds = defaultScriptTimeoutSeconds
	}
	if input.TimeoutSeconds < 1 || input.TimeoutSeconds > maxScriptTimeoutSeconds {
		return scriptInput{}, "", fmt.Errorf("timeoutSeconds must be between 1 and %d", maxScriptTimeoutSeconds)
	}
	if input.WorkingDirectory == "" {
		input.WorkingDirectory = "."
	}

	directory, err := e.sandbox.Writable().ResolveExisting(input.RootID, input.WorkingDirectory)
	if err != nil {
		return scriptInput{}, "", err
	}
	info, err := os.Stat(directory)
	if err != nil {
		return scriptInput{}, "", err
	}
	if !info.IsDir() {
		return scriptInput{}, "", fmt.Errorf("working directory %q is not a folder", input.WorkingDirectory)
	}
	return input, directory, nil
}

func scriptSummary(language string) string {
	if language == scriptLanguagePowerShell {
		return "Run a PowerShell script."
	}
	return "Run a Python script."
}

// scriptsAllowedFromEvents reports whether the user allowed scripts for the rest of the
// conversation. The grant lives in the event log, so reverting past it withdraws it.
func scriptsAllowedFromEvents(events []db.AgentEventRow) bool {
	for _, event := range events {
		if event.EventType == scriptSessionAllowedEvent {
			return true
		}
	}
	return false
}

// fitScriptResult shrinks the captured output until the encoded result fits one tool result. The
// stream caps count raw bytes, and JSON escaping can expand control characters and markup sixfold;
// an oversized result would reach the model as a cut-off preview without its later fields.
func fitScriptResult(result scriptResult) scriptResult {
	for {
		encoded, _ := json.Marshal(result)
		if len(encoded) <= maxToolOutput {
			return result
		}
		if len(result.Stdout) >= len(result.Stderr) {
			result.Stdout, result.StdoutTruncated = dropOutputMiddle(result.Stdout), true
		} else {
			result.Stderr, result.StderrTruncated = dropOutputMiddle(result.Stderr), true
		}
	}
}

func dropOutputMiddle(text string) string {
	keep := len(text) / 4
	return strings.ToValidUTF8(text[:keep], "") + "\n... [output omitted] ...\n" +
		strings.ToValidUTF8(text[len(text)-keep:], "")
}

// boundedOutput keeps the beginning and the end of a stream and drops the middle.
type boundedOutput struct {
	head  []byte
	tail  []byte
	total int
}

func (b *boundedOutput) Write(data []byte) (int, error) {
	written := len(data)
	b.total += written
	if room := scriptOutputHeadBytes - len(b.head); room > 0 {
		take := min(room, len(data))
		b.head = append(b.head, data[:take]...)
		data = data[take:]
	}
	b.tail = append(b.tail, data...)
	if len(b.tail) > 2*scriptOutputTailBytes {
		b.tail = append(b.tail[:0], b.tail[len(b.tail)-scriptOutputTailBytes:]...)
	}
	return written, nil
}

func (b *boundedOutput) text() (string, bool) {
	tail := b.tail
	if len(tail) > scriptOutputTailBytes {
		tail = tail[len(tail)-scriptOutputTailBytes:]
	}
	omitted := b.total - len(b.head) - len(tail)
	if omitted <= 0 {
		return strings.ToValidUTF8(string(b.head)+string(tail), "�"), false
	}
	return strings.ToValidUTF8(string(b.head), "") +
		fmt.Sprintf("\n... [%d bytes omitted] ...\n", omitted) +
		strings.ToValidUTF8(string(tail), ""), true
}
