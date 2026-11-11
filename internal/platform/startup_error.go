package platform

import (
	"errors"
	"strings"
)

const startupErrorTitle = "Nahida Desktop - Startup Error"

// ShowStartupError displays an error before the UI starts.
func ShowStartupError(err error) error {
	if err == nil {
		return nil
	}
	return showStartupError(startupErrorTitle, startupErrorMessage(err))
}

// ConfirmStartup asks a yes/no question before the UI starts and reports
// whether the user chose yes.
func ConfirmStartup(message string) (bool, error) {
	return confirmStartup("Nahida Desktop", strings.ReplaceAll(message, "\x00", "�"))
}

func startupErrorMessage(err error) string {
	if err == nil {
		err = errors.New("unknown startup error")
	}
	detail := strings.ReplaceAll(err.Error(), "\x00", "�")
	return "Nahida Desktop could not start.\n\n" + detail
}
