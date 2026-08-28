package tools

import (
	"regexp"
	"strings"
)

var buildErrorLineRE = regexp.MustCompile(`(?i)\berror\s+[A-Z]+\d+:`)

func tailBuildOutput(output string, maxLines int) string {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) <= maxLines {
		return strings.Join(lines, "\n")
	}
	omitted := len(lines) - maxLines
	return "[showing last " + itoa(maxLines) + " lines, omitted " + itoa(omitted) + " earlier lines]\n" + strings.Join(lines[len(lines)-maxLines:], "\n")
}

func extractBuildErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	raw := strings.Split(strings.ReplaceAll(err.Error(), "\r\n", "\n"), "\n")
	lines := make([]string, 0, len(raw))
	for _, line := range raw {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			lines = append(lines, trimmed)
		}
	}
	errorLines := make([]string, 0, 12)
	for _, line := range lines {
		if buildErrorLineRE.MatchString(line) {
			errorLines = append(errorLines, line)
			if len(errorLines) == 12 {
				break
			}
		}
	}
	if len(errorLines) > 0 {
		return strings.Join(errorLines, "\n")
	}
	if len(lines) > 12 {
		lines = lines[len(lines)-12:]
	}
	return strings.Join(lines, "\n")
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	var buffer [20]byte
	index := len(buffer)
	for value > 0 {
		index--
		buffer[index] = byte('0' + value%10)
		value /= 10
	}
	return string(buffer[index:])
}
