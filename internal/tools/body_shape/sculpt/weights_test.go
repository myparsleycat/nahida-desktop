package sculpt

import (
	"math"
	"testing"
)

func TestBrushFalloff(t *testing.T) {
	tests := []struct {
		distance float64
		want     float64
	}{
		{0, 1},
		{1, 0},
		{2, 0},
	}
	for _, test := range tests {
		if got := BrushFalloff(test.distance, 1, 1); got != test.want {
			t.Errorf("BrushFalloff(%v, 1) = %v, want %v", test.distance, got, test.want)
		}
	}
	if mid := BrushFalloff(0.5, 1, 1); mid <= 0 || mid >= 1 {
		t.Errorf("BrushFalloff(0.5, 1) = %v, want inside (0, 1)", mid)
	}
	if BrushFalloff(0.25, 1, 1) <= BrushFalloff(0.75, 1, 1) {
		t.Error("falloff does not decrease with distance")
	}
}

func TestPaintAndEraseClamp(t *testing.T) {
	tests := []struct {
		name string
		got  float64
		want float64
	}{
		{"paint clamps at 1", PaintVertex(0.9, 1, 0.5), 1},
		{"paint", PaintVertex(0.2, 0.5, 0.5), 0.45},
		{"erase clamps at 0", EraseVertex(0.1, 1, 0.5), 0},
		{"erase", EraseVertex(0.8, 0.5, 0.4), 0.6},
	}
	for _, test := range tests {
		if math.Abs(test.got-test.want) > 1e-9 {
			t.Errorf("%s = %v, want %v", test.name, test.got, test.want)
		}
	}
}

func TestApplyBrushStrokeStaysInsideRadius(t *testing.T) {
	positions := []float32{0, 0, 0, 0.5, 0, 0, 2, 0, 0}
	weights := make([]float32, 3)
	options := BrushStrokeOptions{Positions: positions, Weights: weights, Radius: 1, Strength: 1}

	changed := ApplyBrushStroke(options)
	assertEqual(t, "changed", changed, []uint32{0, 1})
	if weights[0] != 1 || weights[1] <= 0 || weights[1] >= 1 || weights[2] != 0 {
		t.Fatalf("painted weights = %v", weights)
	}

	options.Mode = BrushErase
	ApplyBrushStroke(options)
	assertEqual(t, "erased", weights[:2], []float32{0, 0})
}

func TestApplyBrushStrokeFacingFilter(t *testing.T) {
	// Two vertices at the same position with opposite normals.
	positions := []float32{0, 0, 0, 0, 0, 0}
	normals := []float32{0, 0, 1, 0, 0, -1}
	hitNormal := Vec3{0, 0, 1}

	front := make([]float32, 2)
	ApplyBrushStroke(BrushStrokeOptions{
		Positions: positions, Weights: front, Radius: 1, Strength: 1, Normals: normals, HitNormal: &hitNormal,
	})
	if front[0] <= 0 || front[1] != 0 {
		t.Fatalf("front-facing brush weights = %v", front)
	}

	through := make([]float32, 2)
	ApplyBrushStroke(BrushStrokeOptions{Positions: positions, Weights: through, Radius: 1, Strength: 1})
	if through[0] <= 0 || through[1] <= 0 {
		t.Fatalf("through brush weights = %v", through)
	}
}
