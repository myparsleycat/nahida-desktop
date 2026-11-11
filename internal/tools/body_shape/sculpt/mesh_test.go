package sculpt

import (
	"context"
	"testing"
)

func triangleInput(id string, x float32, blend []byte) MeshInput {
	return MeshInput{
		ID:          id,
		Name:        id,
		VertexCount: 3,
		Positions:   []float32{x, 0, 0, x + 1, 0, 0, x, 1, 0},
		Indices:     []uint32{0, 1, 2},
		BlendBytes:  blend,
		BlendStride: 8,
	}
}

func TestLoadMeshOffsetsLaterSegments(t *testing.T) {
	mesh, err := LoadMesh(context.Background(), []MeshInput{
		triangleInput("body", 2, nil),
		triangleInput("dress", 5, make([]byte, 24)),
	})
	if err != nil {
		t.Fatal(err)
	}

	assertEqual(t, "indices", mesh.Indices, []uint32{0, 1, 2, 3, 4, 5})
	body, dress := mesh.Segments[0], mesh.Segments[1]
	if body.MeshID != "body" || body.VertexOffset != 0 || body.VertexCount != 3 || body.BlendBytes != nil {
		t.Fatalf("body segment = %+v", body)
	}
	if dress.MeshID != "dress" || dress.VertexOffset != 3 || dress.BlendStride != 8 || len(dress.BlendBytes) != 24 {
		t.Fatalf("dress segment = %+v", dress)
	}
	assertEqual(t, "neighbors of 3", mesh.Adjacency.NeighborsOf(3), []uint32{4, 5})
	assertEqual(t, "components", mesh.ComponentIDs, []uint32{0, 0, 0, 1, 1, 1})
}

func TestLoadMeshRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name   string
		inputs []MeshInput
	}{
		{"no meshes", nil},
		{"duplicate mesh", []MeshInput{triangleInput("body", 0, nil), triangleInput("body", 1, nil)}},
		{"index out of range", []MeshInput{func() MeshInput {
			input := triangleInput("body", 0, nil)
			input.Indices = []uint32{0, 1, 3}
			return input
		}()}},
		{"position count mismatch", []MeshInput{func() MeshInput {
			input := triangleInput("body", 0, nil)
			input.VertexCount = 4
			return input
		}()}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := LoadMesh(context.Background(), test.inputs); err == nil {
				t.Fatal("LoadMesh accepted invalid input")
			}
		})
	}
}
