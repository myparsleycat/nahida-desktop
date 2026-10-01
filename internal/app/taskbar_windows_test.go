//go:build windows

package app

import (
	"testing"

	"github.com/rodrigocfd/windigo/x/cosh"
)

func TestTaskbarProgressFlag(t *testing.T) {
	t.Parallel()
	tests := []struct {
		mode string
		want cosh.TBPF
	}{
		{mode: "normal", want: cosh.TBPF_NORMAL},
		{mode: "indeterminate", want: cosh.TBPF_INDETERMINATE},
		{mode: "paused", want: cosh.TBPF_PAUSED},
		{mode: "error", want: cosh.TBPF_ERROR},
		{mode: "unknown", want: cosh.TBPF_NORMAL},
	}
	for _, tt := range tests {
		if got := taskbarProgressFlag(tt.mode); got != tt.want {
			t.Errorf("taskbarProgressFlag(%q) = %v, want %v", tt.mode, got, tt.want)
		}
	}
}
