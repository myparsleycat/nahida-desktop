package ui

import (
	"slices"
	"testing"
)

func TestMotionBounds(t *testing.T) {
	// Node 0 moves its box 10 along x; the last box holds the vertices weighted to no node and stays.
	boxes := []viewerMotionBox{
		{minimum: vec3{1, 0, 0}, maximum: vec3{2, 1, 1}, ok: true},
		{},
		{minimum: vec3{-1, -1, -1}, maximum: vec3{0, 0, 0}, ok: true},
	}
	bones := [][12]float32{{1, 0, 0, 10, 0, 1, 0, 0, 0, 0, 1, 0}, viewerMotionIdentity}

	minimum, maximum, ok := motionBounds(boxes, bones)
	if !ok || minimum != (vec3{-1, -1, -1}) || maximum != (vec3{12, 1, 1}) {
		t.Fatalf("bounds %v..%v (ok %v), want [-1 -1 -1]..[12 1 1]", minimum, maximum, ok)
	}
}

func TestAnchorNode(t *testing.T) {
	// neck -> head -> {jaw, eye}; the jaw carries the most weight of any one node.
	parents := []int32{-1, 0, 1, 1}

	if got := anchorNode([]float64{5, 30, 40, 25}, parents); got != 1 {
		t.Fatalf("face anchors on node %d, want the head (1)", got)
	}
	if got := anchorNode([]float64{0, 0, 25, 0}, parents); got != 2 {
		t.Fatalf("jaw-only mesh anchors on node %d, want the jaw (2)", got)
	}
	if got := anchorNode(make([]float64, 4), parents); got != -1 {
		t.Fatalf("unweighted mesh anchors on node %d, want -1", got)
	}
}

func TestPackMotionBlend(t *testing.T) {
	// Groups 0 and 1 map to nodes 7 and 300; group 2 has no node and group 3 is past the palette.
	palette := []int32{7, 300, -1}
	blend := []uint16{0, 1, 2, 3, 40000, 20000, 5535, 0}

	got := packMotionBlend(blend, palette)
	want := []uint32{
		7 | 300<<16,
		viewerMotionNoNode | viewerMotionNoNode<<16,
		40000 | 20000<<16,
		5535,
	}
	if !slices.Equal(got, want) {
		t.Fatalf("packed %#x, want %#x", got, want)
	}
}
