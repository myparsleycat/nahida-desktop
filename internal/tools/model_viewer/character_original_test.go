package modelviewer

import (
	"slices"
	"testing"
)

func TestCompactOriginalSharedBuffer(t *testing.T) {
	positions := []float32{0, 0, 0, 1, 1, 1, 2, 2, 2, 3, 3, 3, 4, 4, 4, 5, 5, 5}

	used, rebased, err := compactOriginalIndices([]float32{5, 3, 4}, 6)
	if err != nil {
		t.Fatal(err)
	}
	if want := []int{3, 4, 5}; !slices.Equal(used, want) {
		t.Errorf("used = %v, want %v", used, want)
	}
	if want := []uint32{2, 0, 1}; !slices.Equal(rebased, want) {
		t.Errorf("indices = %v, want %v", rebased, want)
	}
	if got, want := compactOriginalVertices(positions, 3, used), positions[9:]; !slices.Equal(got, want) {
		t.Errorf("positions = %v, want %v", got, want)
	}

	if _, _, err := compactOriginalIndices([]float32{0, 1, 6}, 6); err == nil {
		t.Error("index past the buffer was accepted")
	}
}
