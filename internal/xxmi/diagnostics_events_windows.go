//go:build windows

package xxmi

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unicode/utf16"

	"golang.org/x/sys/windows"
)

const (
	crashEventsTimeout = 15 * time.Second
	crashEventsLimit   = 50
	// crashEventsScan bounds the records read before filtering, since other applications' crashes count toward it.
	crashEventsScan = 1000
)

// queryCrashEvents returns the Application log records of crashes and hangs since the session started that
// name one of the executables, and how many there are.
func queryCrashEvents(ctx context.Context, since time.Time, executables []string) (string, int, error) {
	systemRoot := os.Getenv("SystemRoot")
	if systemRoot == "" {
		return "", 0, errors.New("SystemRoot is not set")
	}
	query := fmt.Sprintf(
		"*[System[(Provider[@Name='Application Error'] or Provider[@Name='Application Hang'] or "+
			"Provider[@Name='Windows Error Reporting']) and TimeCreated[@SystemTime>='%s']]]",
		since.UTC().Format("2006-01-02T15:04:05.000Z"),
	)
	ctx, cancel := context.WithTimeout(ctx, crashEventsTimeout)
	defer cancel()
	// Unicode output keeps the localized event text readable regardless of the console code page.
	command := exec.CommandContext(
		ctx, filepath.Join(systemRoot, "System32", "wevtutil.exe"),
		"qe", "Application", "/q:"+query, "/f:text", "/rd:true", fmt.Sprintf("/c:%d", crashEventsScan), "/uni:true",
	)
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
	output, err := command.Output()
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", 0, ctxErr
		}
		return "", 0, fmt.Errorf("query crash events: %w", err)
	}
	text, count := filterCrashEvents(decodeUTF16Output(output), executables)
	return text, count, nil
}

func decodeUTF16Output(data []byte) string {
	if len(data) >= 2 && data[0] == 0xFF && data[1] == 0xFE {
		data = data[2:]
	}
	units := make([]uint16, len(data)/2)
	for index := range units {
		units[index] = uint16(data[2*index]) | uint16(data[2*index+1])<<8
	}
	return string(utf16.Decode(units))
}

// filterCrashEvents keeps the first crashEventsLimit records of wevtutil text output that name one of the
// executables; the query reads newest first, so those are the latest.
func filterCrashEvents(text string, executables []string) (string, int) {
	var kept []string
	var record strings.Builder
	flush := func() {
		defer record.Reset()
		if len(kept) >= crashEventsLimit {
			return
		}
		lower := strings.ToLower(record.String())
		for _, executable := range executables {
			if executable != "" && strings.Contains(lower, strings.ToLower(executable)) {
				kept = append(kept, strings.TrimRight(record.String(), "\r\n"))
				break
			}
		}
	}
	for line := range strings.Lines(text) {
		if strings.HasPrefix(line, "Event[") && record.Len() > 0 {
			flush()
		}
		record.WriteString(line)
	}
	flush()
	if len(kept) == 0 {
		return "", 0
	}
	return strings.Join(kept, "\r\n\r\n") + "\r\n", len(kept)
}
