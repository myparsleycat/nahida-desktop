package sculpt

import (
	"math"
	"slices"
	"testing"
)

func assertEqual[T comparable](t *testing.T, name string, got, want []T) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Fatalf("%s = %v, want %v", name, got, want)
	}
}

func chain3() *VertexAdjacency {
	return &VertexAdjacency{Offsets: []uint32{0, 1, 3, 4}, Neighbors: []uint32{1, 0, 2, 1}}
}

// Two triangles sharing an edge through split vertices 1/3 and 2/5, plus a flipped copy of the first.
var seamPositions = []float32{0, 0, 0, 1, 0, 0, 0, 1, 0, 1, 0, 0, 1, 1, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 1, 0}

var seamIndices = []uint32{0, 1, 2, 3, 4, 5, 6, 8, 7}

func TestBuildPositionWeldMap(t *testing.T) {
	tests := []struct {
		name      string
		positions []float32
		want      []uint32
	}{
		{"seam duplicates", seamPositions, []uint32{0, 1, 2, 1, 4, 2, 0, 1, 2}},
		{
			"float rounding but not nearby vertices",
			[]float32{0, 0, 0, 1e-6, 0, 0, 1e-2, 0, 0, 1, 0, 0},
			[]uint32{0, 0, 2, 3},
		},
		{"twins across a cell boundary", []float32{0, 0, 0, 2.9e-5, 0, 0, 3.1e-5, 0, 0, 1, 0, 0}, []uint32{0, 1, 1, 3}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertEqual(t, "weld map", BuildPositionWeldMap(test.positions), test.want)
		})
	}
}

func TestSeamWelding(t *testing.T) {
	weldMap := BuildPositionWeldMap(seamPositions)
	weldGroups := CollectWeldGroups(weldMap)

	t.Run("surface brush crosses a seam without tearing the mask", func(t *testing.T) {
		weights := make([]float32, 9)
		ApplyGeodesicBrush(GeodesicBrushOptions{
			Positions: seamPositions,
			Adjacency: BuildVertexAdjacency(9, seamIndices, weldGroups),
			Weights:   weights,
			Seeds:     []uint32{0},
			Radius:    1.2,
			Strength:  1,
		})
		SyncWeldedWeights(weights, weldMap, BrushPaint)
		if weights[1] <= 0 || weights[1] != weights[3] || weights[2] != weights[5] {
			t.Fatalf("weights = %v", weights)
		}
	})

	t.Run("erase sync keeps the lowest duplicate", func(t *testing.T) {
		weights := []float32{0, 1, 0, 0.25, 0, 0, 0, 1, 0}
		SyncWeldedWeights(weights, weldMap, BrushErase)
		assertEqual(t, "welded", []float32{weights[1], weights[3], weights[7]}, []float32{0.25, 0.25, 0.25})
	})

	t.Run("seam normals merge by facing so a double-sided shell does not cancel", func(t *testing.T) {
		folded := slices.Clone(seamPositions)
		folded[14] = 1
		normals := ComputeVertexNormals(folded, seamIndices, weldGroups)
		assertEqual(t, "normal 3", normals[9:12], normals[3:6])
		if normals[3] >= 0 {
			t.Fatalf("normal 1 = %v, want negative x", normals[3:6])
		}
		assertEqual(t, "normal 7", normals[21:24], []float32{0, 0, -1})
	})
}

func TestBuildVertexAdjacencyCSR(t *testing.T) {
	adjacency := BuildVertexAdjacency(4, []uint32{0, 1, 2, 2, 1, 3}, nil)
	assertEqual(t, "offsets", adjacency.Offsets, []uint32{0, 2, 5, 8, 10})
	assertEqual(t, "neighbors", adjacency.Neighbors, []uint32{1, 2, 0, 2, 3, 0, 1, 3, 1, 2})
}

func TestApplyGeodesicBrush(t *testing.T) {
	t.Run("does not cross to a nearby disconnected surface", func(t *testing.T) {
		positions := []float32{0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0, 0.01, 1, 0, 0.01, 0, 1, 0.01}
		weights := make([]float32, 6)
		changed := ApplyGeodesicBrush(GeodesicBrushOptions{
			Positions: positions,
			Adjacency: BuildVertexAdjacency(6, []uint32{0, 1, 2, 3, 4, 5}, nil),
			Weights:   weights,
			Seeds:     []uint32{0},
			Radius:    2,
			Strength:  1,
		})
		assertEqual(t, "changed", changed, []uint32{0, 1, 2})
		assertEqual(t, "far side", weights[3:], []float32{0, 0, 0})
	})

	t.Run("limits distance to the radius and applies falloff", func(t *testing.T) {
		weights := make([]float32, 3)
		changed := ApplyGeodesicBrush(GeodesicBrushOptions{
			Positions: []float32{0, 0, 0, 0.5, 0, 0, 1, 0, 0},
			Adjacency: chain3(),
			Weights:   weights,
			Seeds:     []uint32{0},
			Radius:    1,
			Strength:  1,
		})
		assertEqual(t, "changed", changed, []uint32{0, 1})
		assertEqual(t, "weights", weights, []float32{1, 0.5, 0})
	})

	t.Run("reused workspace does not leak distances between strokes", func(t *testing.T) {
		positions := []float32{0, 0, 0, 0.5, 0, 0, 1, 0, 0}
		workspace := NewGeodesicBrushWorkspace(3)
		stroke := func(weights []float32, seed uint32, workspace *GeodesicBrushWorkspace) []uint32 {
			return ApplyGeodesicBrush(GeodesicBrushOptions{
				Positions: positions,
				Adjacency: chain3(),
				Weights:   weights,
				Seeds:     []uint32{seed},
				Radius:    0.75,
				Strength:  1,
				Workspace: workspace,
			})
		}
		reusedWeights := make([]float32, 3)
		stroke(reusedWeights, 0, workspace)
		clear(reusedWeights)
		reused := stroke(reusedWeights, 2, workspace)
		freshWeights := make([]float32, 3)
		fresh := stroke(freshWeights, 2, nil)
		assertEqual(t, "changed", reused, fresh)
		assertEqual(t, "weights", reusedWeights, freshWeights)
	})

	t.Run("front-facing filter skips vertices facing away", func(t *testing.T) {
		positions := []float32{0, 0, 0, 1, 0, 0, 0, 1, 0}
		normals := []float32{0, 0, 1, 0, 0, 1, 0, 0, 1}
		weights := make([]float32, 3)
		stroke := func(hitNormal Vec3) {
			ApplyGeodesicBrush(GeodesicBrushOptions{
				Positions:       positions,
				Adjacency:       BuildVertexAdjacency(3, []uint32{0, 1, 2}, nil),
				Weights:         weights,
				Seeds:           []uint32{0},
				Radius:          2,
				Strength:        1,
				FrontFacingOnly: true,
				Normals:         normals,
				HitNormal:       &hitNormal,
			})
		}
		stroke(Vec3{0, 0, -1})
		assertEqual(t, "facing away", weights, []float32{0, 0, 0})
		stroke(Vec3{0, 0, 1})
		if weights[0] <= 0 {
			t.Fatalf("facing toward: weights = %v", weights)
		}
	})
}

func TestSmoothSelectionWeightsReadsIterationSnapshot(t *testing.T) {
	weights := []float32{1, 0, 0}
	SmoothSelectionWeights(weights, chain3(), 0.5, 1)
	assertEqual(t, "weights", weights, []float32{0.5, 0.25, 0})
}

func TestGrowSelectionWeights(t *testing.T) {
	// Triangles (0, 1, 4) and (1, 2, 5): 0 touches 1 and 1 touches 2, but 0 does
	// not touch 2. Vertex 3 belongs to the separate triangle (3, 6, 7).
	adjacency := BuildVertexAdjacency(8, []uint32{0, 1, 4, 1, 2, 5, 3, 6, 7}, nil)
	weights := make([]float32, 8)
	weights[0] = 1

	GrowSelectionWeights(weights, adjacency, 1, 1)
	assertEqual(t, "after full grow", weights[:4], []float32{1, 1, 0, 0})
	GrowSelectionWeights(weights, adjacency, 0.5, 1)
	assertEqual(t, "after half grow", weights[2:4], []float32{0.5, 0})
}

func TestShrinkSelectionWeights(t *testing.T) {
	adjacency := BuildVertexAdjacency(5, []uint32{0, 1, 4, 1, 2, 3}, nil)
	weights := []float32{0, 1, 1, 1, 1}
	ShrinkSelectionWeights(weights, adjacency, 1, 1)
	assertEqual(t, "weights", weights[:3], []float32{0, 0, 1})
}

func TestBuildConnectedComponents(t *testing.T) {
	ids := BuildConnectedComponents(6, BuildVertexAdjacency(6, []uint32{0, 1, 2, 3, 4, 5}, nil))
	assertEqual(t, "component ids", ids, []uint32{0, 0, 0, 1, 1, 1})
}

func TestSelectConnectedComponent(t *testing.T) {
	ids := []uint32{0, 0, 0, 1, 1, 1}
	weights := make([]float32, 6)

	SelectConnectedComponent(weights, ids, 0, ComponentAdd, 0.8)
	assertEqual(t, "add", weights, []float32{0.8, 0.8, 0.8, 0, 0, 0})
	SelectConnectedComponent(weights, ids, 1, ComponentReplace, 1)
	assertEqual(t, "replace", weights, []float32{0, 0, 0, 1, 1, 1})
	SelectConnectedComponent(weights, ids, 1, ComponentSubtract, 1)
	assertEqual(t, "subtract", weights, []float32{0, 0, 0, 0, 0, 0})
}

func TestSymmetryMapMirrorsWeights(t *testing.T) {
	positions := []float32{-1, 2, 3, 1, 2, 3, 0, 5, 0}
	symmetryMap := BuildSymmetryMap(positions, 0, 1e-3)
	assertEqual(t, "symmetry map", symmetryMap[:2], []int32{1, 0})

	weights := []float32{0.8, 0, 0}
	MirrorWeightsWithCache(weights, symmetryMap, []uint32{0}, BrushPaint)
	if math.Abs(float64(weights[1])-0.8) > 1e-6 {
		t.Fatalf("mirrored weight = %v, want 0.8", weights[1])
	}
}
