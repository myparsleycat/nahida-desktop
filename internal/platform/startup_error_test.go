package platform

import (
	"errors"
	"strings"
	"testing"
)

func TestStartupErrorMessageIsUserFacingAndNULTerminationSafe(t *testing.T) {
	message := startupErrorMessage(errors.New("open data.db\x00failed"))
	if !strings.HasPrefix(message, "Nahida Desktop could not start.\n\n") {
		t.Fatalf("message = %q", message)
	}
	if strings.ContainsRune(message, '\x00') || !strings.Contains(message, "open data.db�failed") {
		t.Fatalf("message = %q", message)
	}
}
