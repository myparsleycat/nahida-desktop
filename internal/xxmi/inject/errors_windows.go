//go:build windows

package inject

import (
	"context"
	"errors"
	"strings"
)

func ClassifyError(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.Canceled) {
		return "XXMI_LAUNCH_CANCELED"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "XXMI_GAME_START_TIMEOUT"
	}
	for _, code := range []string{
		"XXMI_LOADER_TOO_OLD",
		"XXMI_INJECT_FAILED",
		"XXMI_GAME_START_TIMEOUT",
		"XXMI_GAME_RUNNING",
		"XXMI_LEGACY_LOADER_RUNNING",
		"XXMI_LEGACY_LOADER_NOT_READY",
		"XXMI_LEGACY_LOADER_EXITED",
	} {
		if strings.Contains(err.Error(), code) {
			return code
		}
	}
	return "XXMI_LAUNCH_FAILED"
}
