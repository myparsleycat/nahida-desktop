//go:build windows

package mod

import (
	"sort"
	"testing"
)

func TestKoreanLocaleCompareMatchesElectronOrdering(t *testing.T) {
	t.Parallel()
	values := []string{"a2", "a10", "나", "가", "A", "a"}
	less := newLocaleLessFor("ko-KR")
	sort.SliceStable(values, func(i, j int) bool { return less(values[i], values[j]) })
	want := []string{"가", "나", "a", "A", "a10", "a2"}
	for i := range want {
		if values[i] != want[i] {
			t.Fatalf("locale order = %#v, want %#v", values, want)
		}
	}
}
