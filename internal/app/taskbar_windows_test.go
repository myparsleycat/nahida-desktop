//go:build windows

package app

import (
	"testing"

	"github.com/rodrigocfd/windigo/co"
)

func TestTaskbarProgressFlag(t *testing.T) {
	t.Parallel()
	tests := []struct {
		mode string
		want co.TBPF
	}{
		{mode: "normal", want: co.TBPF_NORMAL},
		{mode: "indeterminate", want: co.TBPF_INDETERMINATE},
		{mode: "paused", want: co.TBPF_PAUSED},
		{mode: "error", want: co.TBPF_ERROR},
		{mode: "unknown", want: co.TBPF_NORMAL},
	}
	for _, tt := range tests {
		if got := taskbarProgressFlag(tt.mode); got != tt.want {
			t.Errorf("taskbarProgressFlag(%q) = %v, want %v", tt.mode, got, tt.want)
		}
	}
}
