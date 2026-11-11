package sculpt

import (
	"math"
	"testing"
)

func TestRaycastPicksNearestFaceFromEitherSide(t *testing.T) {
	// Two stacked triangles at z = 0 and z = 1, the far one listed first; the
	// near one is wound clockwise as seen from the ray, so it is a back face.
	positions := []float32{
		0, 0, 0, 1, 0, 0, 0, 1, 0,
		0, 0, 1, 0, 1, 1, 1, 0, 1,
	}
	indices := []uint32{0, 1, 2, 3, 4, 5}
	tests := []struct {
		name      string
		indices   []uint32
		origin    Vec3
		direction Vec3
		ok        bool
		face      int
		normal    Vec3
		distance  float64
	}{
		{"indexed back face", indices, Vec3{0.2, 0.2, 5}, Vec3{0, 0, -1}, true, 1, Vec3{0, 0, -1}, 4},
		{"unindexed back face", nil, Vec3{0.2, 0.2, 5}, Vec3{0, 0, -1}, true, 1, Vec3{0, 0, -1}, 4},
		{"from below", indices, Vec3{0.2, 0.2, -2}, Vec3{0, 0, 2}, true, 0, Vec3{0, 0, 1}, 2},
		{"outside the triangles", indices, Vec3{2, 2, 5}, Vec3{0, 0, -1}, false, 0, Vec3{}, 0},
		{"pointing away", indices, Vec3{0.2, 0.2, 5}, Vec3{0, 0, 1}, false, 0, Vec3{}, 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			hit, ok := Raycast(positions, test.indices, test.origin, test.direction)
			if ok != test.ok {
				t.Fatalf("hit = %v, want %v", ok, test.ok)
			}
			if !ok {
				return
			}
			if hit.FaceIndex != test.face || hit.FaceNormal != test.normal ||
				math.Abs(hit.Distance-test.distance) > 1e-12 {
				t.Fatalf("hit = %+v", hit)
			}
			if hit.Point[0] != 0.2 || hit.Point[1] != 0.2 {
				t.Fatalf("point = %v", hit.Point)
			}
		})
	}
}

func TestModelSpaceUp(t *testing.T) {
	tests := []struct {
		orientation Vec3
		want        Vec3
	}{
		{Vec3{0, 0, 0}, Vec3{0, 1, 0}},
		{Vec3{0, 0, 90}, Vec3{0, 1, 0}},
		{Vec3{90, 0, 0}, Vec3{1, 0, 0}},
		{Vec3{0, 90, 0}, Vec3{0, 0, -1}},
	}
	for _, test := range tests {
		got := ModelSpaceUp(test.orientation)
		for axis := range 3 {
			if math.Abs(got[axis]-test.want[axis]) > 1e-12 {
				t.Errorf("ModelSpaceUp(%v) = %v, want %v", test.orientation, got, test.want)
				break
			}
		}
	}
}
