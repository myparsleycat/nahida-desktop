package sculpt

import (
	"slices"
	"testing"
)

func TestComputeVertexNormalsPlanarTriangle(t *testing.T) {
	normals := ComputeVertexNormals([]float32{0, 0, 0, 1, 0, 0, 0, 1, 0}, []uint32{0, 1, 2}, nil)
	assertEqual(t, "normals", normals, []float32{0, 0, 1, 0, 0, 1, 0, 0, 1})
}

func TestAnisotropicScaleFromOriginal(t *testing.T) {
	original := []float32{0, 0, 0, 2, 0, 0, 0, 4, 0}
	weights := []float32{0, 1, 0.5}
	scale := func(amount float64, axisScale Vec3) []float32 {
		preview := make([]float32, len(original))
		AnisotropicScaleFromOriginal(original, preview, weights, Vec3{}, amount, axisScale)
		return preview
	}

	assertEqual(t, "x scale", scale(0.5, Vec3{1, 0, 0}), []float32{0, 0, 0, 3, 0, 0, 0, 4, 0})
	assertEqual(t, "y scale", scale(0.5, Vec3{0, 1, 0})[6:], []float32{0, 5, 0})
	assertEqual(t, "zero amount", scale(0, Vec3{1, 0, 0}), original)
}

func TestDisplacementMetrics(t *testing.T) {
	metrics := DisplacementMetrics(
		[]float32{0, 0, 0, 1, 0, 0, 0, 1, 0},
		[]float32{0, 0, 0, 2, 0, 0, 0, 1, 0},
		DefaultDisplacementEpsilon,
	)
	want := DeformMetrics{VertexCount: 3, MovedVertices: 1, MaxDisplacement: 1, MeanDisplacement: 1}
	if metrics != want {
		t.Fatalf("metrics = %+v, want %+v", metrics, want)
	}
}

func TestApplyMultiRegionDeform(t *testing.T) {
	tests := []struct {
		name     string
		original []float32
		regions  []ActiveRegionDeform
		want     []float32
	}{
		{
			name:     "scale regions compose from originals",
			original: []float32{0, 0, 0, 2, 0, 0, 0, 2, 0},
			regions: []ActiveRegionDeform{
				{ID: "waist", Weights: []float32{1, 0, 0}, Amount: 0.5, AxisScale: Vec3{1, 0, 0}},
				{ID: "hips", Weights: []float32{0, 1, 0}, Amount: 1, AxisScale: Vec3{1, 0, 0}},
			},
			want: []float32{0, 0, 0, 4, 0, 0, 0, 2, 0},
		},
		{
			name:     "inflate moves along normals by weight",
			original: []float32{0, 0, 0, 1, 0, 0, 0, 1, 0},
			regions: []ActiveRegionDeform{{
				Weights: []float32{0, 0.5, 1}, Amount: 2, Operation: OpInflate,
				Normals: []float32{0, 0, 1, 0, 0, 1, 0, 0, 1},
			}},
			want: []float32{0, 0, 0, 1, 0, 1, 0, 1, 2},
		},
		{
			name:     "translate moves by weight",
			original: []float32{0, 0, 0, 1, 0, 0},
			regions: []ActiveRegionDeform{{
				Weights: []float32{1, 0.5}, Amount: 1, Operation: OpTranslate, Translation: Vec3{0, 2, 0},
			}},
			want: []float32{0, 2, 0, 1, 1, 0},
		},
		{
			// Height 0 keeps the vertex; height 2 scales x by 1 * 0.5 * 1 * 2.
			name:     "taper grows with height above the pivot",
			original: []float32{1, 0, 0, 1, 2, 0},
			regions: []ActiveRegionDeform{{
				Weights: []float32{1, 1}, Amount: 1, Operation: OpTaper, TaperFactor: 0.5,
			}},
			want: []float32{1, 0, 0, 2, 2, 0},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			preview := make([]float32, len(test.original))
			metrics := ApplyMultiRegionDeform(test.original, preview, test.regions, DefaultDisplacementEpsilon)
			assertEqual(t, "preview", preview, test.want)
			if want := DisplacementMetrics(test.original, preview, DefaultDisplacementEpsilon); metrics != want {
				t.Fatalf("metrics = %+v, want %+v", metrics, want)
			}

			again := make([]float32, len(test.original))
			ApplyMultiRegionDeform(test.original, again, test.regions, DefaultDisplacementEpsilon)
			assertEqual(t, "second preview", again, preview)
		})
	}
}

type maskMesh struct {
	positions []float32
	adjacency *VertexAdjacency
	weights   []float32
}

func triangles(points ...Vec3) maskMesh {
	var positions []float32
	var indices []uint32
	for index, point := range points {
		positions = append(positions, float32(point[0]), float32(point[1]), float32(point[2]))
		if index%3 == 0 {
			indices = append(indices, uint32(index), uint32(index+1), uint32(index+2))
		}
	}
	weights := make([]float32, len(points))
	for i := range weights {
		weights[i] = 1
	}
	return maskMesh{positions, BuildVertexAdjacency(len(points), indices, nil), weights}
}

func TestComputeMaskPivots(t *testing.T) {
	left := []Vec3{{-2, 0, 0}, {-1, 0, 0}, {-1.5, 1, 0}}
	right := []Vec3{{2, 0, 0}, {1, 0, 0}, {1.5, 1, 0}}
	pocket := []Vec3{{-1.6, 0.2, 0}, {-1.5, 0.2, 0}, {-1.55, 0.3, 0}}

	t.Run("separate parts scale around their own centers", func(t *testing.T) {
		mesh := triangles(slices.Concat(left, right)...)
		pivots := ComputeMaskPivots(mesh.positions, mesh.weights, mesh.adjacency)
		if pivots == nil {
			t.Fatal("pivots = nil, want one per part")
		}
		preview := make([]float32, len(mesh.positions))
		ApplyMultiRegionDeform(mesh.positions, preview, []ActiveRegionDeform{{
			ID: "legs", Weights: mesh.weights, Amount: 0.5, AxisScale: Vec3{1, 0, 0}, VertexPivots: pivots,
		}}, DefaultDisplacementEpsilon)
		assertEqual(t, "left x", []float32{preview[0], preview[3]}, []float32{-2.25, -0.75})
		assertEqual(t, "right x", []float32{preview[9], preview[12]}, []float32{2.25, 0.75})
	})

	single := []struct {
		name string
		mesh maskMesh
	}{
		{"nested parts", triangles(slices.Concat(left, pocket)...)},
		{"single part", triangles(left...)},
		{"unmasked second part", func() maskMesh {
			mesh := triangles(slices.Concat(left, right)...)
			clear(mesh.weights[3:])
			return mesh
		}()},
	}
	for _, test := range single {
		t.Run(test.name+" keep one pivot", func(t *testing.T) {
			if pivots := ComputeMaskPivots(test.mesh.positions, test.mesh.weights, test.mesh.adjacency); pivots != nil {
				t.Fatalf("pivots = %v, want nil", pivots)
			}
		})
	}
}

func TestGenerateRegionWeights(t *testing.T) {
	// A tall figure along y: feet, waist, head and a rear hip vertex.
	positions := []float32{0, 0, 0, 0, 0.5, 0, 0, 1, 0, 0.2, 0.38, -0.4}
	waist := GenerateRegionWeights(positions, RegionWaist, nil)
	head := GenerateRegionWeights(positions, RegionHead, nil)
	calves := GenerateRegionWeights(positions, RegionCalves, nil)
	if head[2] <= head[0] || calves[0] <= calves[2] || waist[1] < waist[2] {
		t.Fatalf("waist %v, head %v, calves %v", waist, head, calves)
	}
	for _, w := range slices.Concat(waist, head, calves) {
		if w < 0 || w > 1 {
			t.Fatalf("weight %v outside [0, 1]", w)
		}
	}

	shifted := slices.Clone(positions)
	for i := 1; i < len(shifted); i += 3 {
		shifted[i] += 10
	}
	if !slices.ContainsFunc(GenerateRegionWeights(shifted, RegionWaist, nil), func(w float32) bool { return w > 0 }) {
		t.Fatal("shifted mesh has no waist weights")
	}
}

func TestRecalculateNormalsAndTangents(t *testing.T) {
	updated := RecalculateNormalsAndTangents([]float32{0, 0, 0, 1, 0, 0, 0, 1, 0}, []uint32{0, 1, 2}, make([]int8, 24))
	assertEqual(t, "normal z bytes", []int8{updated[6], updated[14], updated[22]}, []int8{127, 127, 127})
}
