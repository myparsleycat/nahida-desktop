//go:build windows

package inject

import (
	"strings"
	"testing"
)

func TestSummarizeHookVerificationTriesBothChecks(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name              string
		early, late       uintptr
		verified, warning bool
	}{
		{name: "both pass", verified: true},
		{name: "early passes", late: 100, verified: true},
		{name: "late passes", early: 100, verified: true},
		{name: "both fail", early: 100, late: 200, warning: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			verified, warning := summarizeHookVerification(tt.early, tt.late)
			if verified != tt.verified || (warning != "") != tt.warning {
				t.Fatalf("summarizeHookVerification(%d, %d) = (%t, %q)",
					tt.early, tt.late, verified, warning)
			}
			if tt.warning && (!strings.Contains(warning, "100") || !strings.Contains(warning, "200")) {
				t.Fatalf("warning omits native codes: %q", warning)
			}
		})
	}
}
