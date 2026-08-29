package mod

import (
	"testing"
)

func TestNaturalCompareMatchesElectronRustScanner(t *testing.T) {
	t.Parallel()
	tests := []struct {
		left  string
		right string
	}{
		{left: "shot-2", right: "shot-11"},
		{left: "Shot-2", right: "shot-2"},
		{left: "001", right: "0001"},
		{left: "1", right: "中"},
		{left: "第1章", right: "第1-2章"},
		{left: "가2", right: "가10"},
	}
	for _, test := range tests {
		if got := naturalCompare(test.left, test.right); got >= 0 {
			t.Errorf("naturalCompare(%q, %q) = %d, want less", test.left, test.right, got)
		}
		if got := naturalCompare(test.right, test.left); got <= 0 {
			t.Errorf("naturalCompare(%q, %q) = %d, want greater", test.right, test.left, got)
		}
	}
}
