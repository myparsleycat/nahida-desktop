package platform

import (
	"errors"
	"strings"
)

const startupErrorTitle = "Nahida Desktop - Startup Error"

// ShowStartupError displays an error before the Wails runtime is available.
func ShowStartupError(err error) error {
	if err == nil {
		return nil
	}
	return showStartupError(startupErrorTitle, startupErrorMessage(err))
}

func startupErrorMessage(err error) string {
	if err == nil {
		err = errors.New("unknown startup error")
	}
	detail := strings.ReplaceAll(err.Error(), "\x00", "�")
	return "Nahida Desktop could not start.\n\n" + detail
}
