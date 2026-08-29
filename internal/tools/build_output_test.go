package tools

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestExtractBuildErrorMessagePrefersFirstTwelveCompilerErrors(t *testing.T) {
	lines := []string{"Build failed: exit status 1", "stdout:"}
	for index := range 14 {
		lines = append(lines, fmt.Sprintf("file.cpp(%d): error C%04d: failure %d", index+1, 1000+index, index+1))
	}
	got := extractBuildErrorMessage(errors.New(strings.Join(lines, "\r\n")))
	gotLines := strings.Split(got, "\n")
	if len(gotLines) != 12 || !strings.Contains(gotLines[0], "C1000") || !strings.Contains(gotLines[11], "C1011") {
		t.Fatalf("digest = %q", got)
	}
}

func TestExtractBuildErrorMessageFallsBackToLastTwelveNonemptyLines(t *testing.T) {
	lines := make([]string, 15)
	for index := range lines {
		lines[index] = fmt.Sprintf("line %d", index+1)
	}
	got := extractBuildErrorMessage(errors.New(strings.Join(lines, "\n")))
	gotLines := strings.Split(got, "\n")
	if len(gotLines) != 12 || gotLines[0] != "line 4" || gotLines[11] != "line 15" {
		t.Fatalf("fallback = %q", got)
	}
}
