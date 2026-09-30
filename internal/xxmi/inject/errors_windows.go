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

func hookFailureReason(code uintptr) string {
	switch code {
	case 100:
		return "another 3DMigoto loader instance is running"
	case 200:
		return "failed to load the module DLL"
	case 300:
		return "module DLL is missing the expected entry point"
	case 400:
		return "failed to set up the Windows hook"
	default:
		return "unknown hook failure"
	}
}

func injectFailureReason(code uintptr) string {
	switch code {
	case 100:
		return "target process was not found"
	case 110:
		return "invalid DLL path"
	case 120:
		return "failed to resolve kernel32.dll"
	case 130:
		return "failed to resolve LoadLibraryW"
	case 200:
		return "failed to allocate remote memory"
	case 300:
		return "failed to write the DLL path to process memory"
	case 400:
		return "failed to create a remote thread"
	case 500:
		return "injection thread timed out"
	case 510:
		return "waiting for the injection thread failed"
	case 600:
		return "DLL injection failed"
	case 700:
		return "unknown low-level error"
	default:
		return "unknown injection failure"
	}
}
