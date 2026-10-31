package modelviewer

import (
	"bytes"
	"context"
	"errors"
	"math"
	"slices"
	"testing"
)

func TestRecoverModelViewerGeometryPreservesAttributesAndSources(t *testing.T) {
	for _, test := range []struct {
		name  string
		value float32
	}{
		{name: "nan", value: float32(math.NaN())},
		{name: "positive infinity", value: float32(math.Inf(1))},
		{name: "negative infinity", value: float32(math.Inf(-1))},
	} {
		t.Run(test.name, func(t *testing.T) {
			geometry := &modelViewerGeometry{
				Position: []float32{test.value, 0, 0, 0, 0, 0, 2, 0, 0, 1, 0, 0, 0, 1, 0, test.value, 0, 0},
				Indices:  []uint32{0, 1, 2, 4, 3, 1, 3, 4, 1}, VertexCount: 6,
				SourceIndices: []uint32{10, 11, 12, 13, 14, 15},
			}
			for vertex := range 6 {
				geometry.Normal = append(geometry.Normal, float32(vertex), 1, 0)
				geometry.Tangent = append(geometry.Tangent, 1, float32(vertex), 0, -1)
				geometry.Texcoord0 = append(geometry.Texcoord0, float32(vertex), .5)
				geometry.Color0 = append(geometry.Color0, float32(vertex), 0, 0, 1)
			}
			original := modelViewerFloat32Bytes(geometry.Position)
			recovered, stats, err := recoverModelViewerGeometry(context.Background(), geometry)
			if err != nil {
				t.Fatal(err)
			}
			if recovered == nil || recovered.VertexCount != 3 ||
				!slices.Equal(recovered.Indices, []uint32{2, 1, 0, 1, 2, 0}) ||
				!slices.Equal(recovered.SourceIndices, []uint32{11, 13, 14}) ||
				!slices.Equal(recovered.sourceFaces, []uint32{1, 2}) ||
				!slices.Equal(recovered.Position, []float32{0, 0, 0, 1, 0, 0, 0, 1, 0}) {
				t.Fatalf("recovered = %+v", recovered)
			}
			if !slices.Equal(recovered.Normal, []float32{1, 1, 0, 3, 1, 0, 4, 1, 0}) ||
				!slices.Equal(recovered.Tangent, []float32{1, 1, 0, -1, 1, 3, 0, -1, 1, 4, 0, -1}) ||
				!slices.Equal(recovered.Texcoord0, []float32{1, .5, 3, .5, 4, .5}) ||
				!slices.Equal(recovered.Color0, []float32{1, 0, 0, 1, 3, 0, 0, 1, 4, 0, 0, 1}) {
				t.Fatalf("recovered attributes = %+v", recovered)
			}
			if stats.invalidVertices != 2 || stats.firstInvalidSourceVertex != 10 || stats.removedTriangles != 1 {
				t.Fatalf("stats = %+v", stats)
			}
			recovered.Position[0], recovered.Normal[0], recovered.Tangent[0] = 99, 99, 99
			recovered.Texcoord0[0], recovered.Color0[0], recovered.SourceIndices[0] = 99, 99, 99
			recovered.Indices[0] = 99
			if !bytes.Equal(original, modelViewerFloat32Bytes(geometry.Position)) || geometry.Normal[1*3] != 1 ||
				geometry.Tangent[1*4+1] != 1 || geometry.Texcoord0[1*2] != 1 || geometry.Color0[1*4] != 1 ||
				geometry.SourceIndices[0] != 10 || geometry.Indices[0] != 0 {
				t.Fatalf("source geometry was mutated: %+v", geometry)
			}
		})
	}
}

func TestRecoverModelViewerGeometryEmptyUnusedAndClean(t *testing.T) {
	geometry := &modelViewerGeometry{
		Position: []float32{0, 0, 0, 1, 0, 0, 0, 1, 0, float32(math.NaN()), 0, 0},
		Indices:  []uint32{0, 1, 2}, VertexCount: 4,
	}
	recovered, stats, err := recoverModelViewerGeometry(context.Background(), geometry)
	if err != nil || recovered == nil || recovered.VertexCount != 3 || stats.removedTriangles != 0 ||
		recovered.sourceFaces != nil || !slices.Equal(recovered.SourceIndices, []uint32{0, 1, 2}) {
		t.Fatalf("unused invalid vertex: geometry=%+v stats=%+v err=%v", recovered, stats, err)
	}
	clean, _, err := recoverModelViewerGeometry(context.Background(), recovered)
	if err != nil || clean != recovered {
		t.Fatalf("clean geometry was copied: %v", err)
	}
	geometry.Indices = []uint32{0, 1, 3}
	empty, stats, err := recoverModelViewerGeometry(context.Background(), geometry)
	if err != nil || empty != nil || stats.removedTriangles != 1 {
		t.Fatalf("empty geometry=%+v stats=%+v err=%v", empty, stats, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := recoverModelViewerGeometry(ctx, geometry); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled recovery: %v", err)
	}
}

func TestRecoverModelViewerGeometryIgnoresIncompleteTail(t *testing.T) {
	for _, test := range []struct {
		name    string
		indices []uint32
		faces   []uint32
	}{
		{name: "one trailing index", indices: []uint32{0, 1, 2, 2, 3, 4, 0}},
		{name: "two trailing indices", indices: []uint32{0, 1, 2, 2, 3, 4, 0, 99}},
		{name: "no complete triangle", indices: []uint32{99, 99}},
		{name: "existing face mapping", indices: []uint32{0, 1, 2, 2, 3, 4, 0}, faces: []uint32{5, 8}},
	} {
		t.Run(test.name, func(t *testing.T) {
			geometry := &modelViewerGeometry{
				Position: []float32{float32(math.NaN()), 0, 0, 0, 0, 0, 0, 0, 0, 1, 0, 0, 0, 1, 0},
				Indices:  test.indices, VertexCount: 5, sourceFaces: test.faces,
			}
			originalIndices := slices.Clone(geometry.Indices)
			recovered, stats, err := recoverModelViewerGeometry(context.Background(), geometry)
			if err != nil || stats.discardedIndices != len(test.indices)%3 {
				t.Fatalf("stats=%+v err=%v", stats, err)
			}
			if len(test.indices) < 3 {
				if recovered != nil {
					t.Fatalf("incomplete triangle was retained: %+v", recovered)
				}
				return
			}
			face := uint32(1)
			if test.faces != nil {
				face = test.faces[1]
			}
			if recovered == nil || stats.removedTriangles != 1 ||
				!slices.Equal(recovered.Indices, []uint32{0, 1, 2}) ||
				!slices.Equal(recovered.SourceIndices, []uint32{2, 3, 4}) ||
				!slices.Equal(recovered.sourceFaces, []uint32{face}) {
				t.Fatalf("recovered=%+v stats=%+v", recovered, stats)
			}
			if !slices.Equal(geometry.Indices, originalIndices) {
				t.Fatal("source index buffer was modified")
			}
		})
	}
}

func TestRecoverModelViewerGeometryOmitsOnlyInvalidFrameAttributes(t *testing.T) {
	for _, normal := range []bool{false, true} {
		name := "tangent"
		if normal {
			name = "normal"
		}
		t.Run(name, func(t *testing.T) {
			geometry := &modelViewerGeometry{
				Position: []float32{0, 0, 0, 1, 0, 0, 0, 1, 0},
				Normal:   []float32{0, 0, 1, 0, 0, 1, 0, 0, 1},
				Tangent:  []float32{1, 0, 0, 1, 1, 0, 0, 1, 1, 0, 0, 1},
				Indices:  []uint32{0, 1, 2}, VertexCount: 3,
			}
			if normal {
				geometry.Normal[0] = float32(math.NaN())
			} else {
				geometry.Tangent[0] = float32(math.Inf(-1))
			}
			recovered, stats, err := recoverModelViewerGeometry(context.Background(), geometry)
			if err != nil || recovered == nil || recovered.Tangent != nil || stats.removedTriangles != 0 ||
				stats.omittedNormals != normal || !stats.omittedTangents || (recovered.Normal == nil) != normal {
				t.Fatalf("geometry=%+v stats=%+v err=%v", recovered, stats, err)
			}
			if !slices.Equal(recovered.Indices, geometry.Indices) || recovered.sourceFaces != nil ||
				!slices.Equal(
					recovered.Position,
					geometry.Position,
				) || len(geometry.Tangent) != 12 || len(geometry.Normal) != 9 {
				t.Fatal("frame recovery changed topology or original attributes")
			}
		})
	}
}
