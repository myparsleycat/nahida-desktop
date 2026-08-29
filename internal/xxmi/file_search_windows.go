//go:build windows

package xxmi

import (
	"fmt"
	"unicode/utf16"

	"golang.org/x/sys/windows"
)

func xxmiSearchRoots() ([]string, error) {
	required, err := windows.GetLogicalDriveStrings(0, nil)
	if err != nil {
		return nil, fmt.Errorf("list logical drives: %w", err)
	}
	if required == 0 {
		return nil, nil
	}
	buffer := make([]uint16, required)
	written, err := windows.GetLogicalDriveStrings(uint32(len(buffer)), &buffer[0])
	if err != nil {
		return nil, fmt.Errorf("list logical drives: %w", err)
	}
	buffer = buffer[:min(int(written), len(buffer))]
	roots := make([]string, 0)
	for start := 0; start < len(buffer); {
		end := start
		for end < len(buffer) && buffer[end] != 0 {
			end++
		}
		if end == start {
			break
		}
		roots = append(roots, string(utf16.Decode(buffer[start:end])))
		start = end + 1
	}
	return roots, nil
}
