package sculpt

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"nahida.live/desktop/internal/tools/modmesh"
)

// symmetryTolerance is the distance within which a vertex counts as the mirror
// of another across x = 0.
const symmetryTolerance = 1e-3

// MeshInput is one selected mesh as the backend loaded it.
type MeshInput struct {
	ID          string
	Name        string
	VertexCount int
	// Positions holds packed float32 xyz, already extracted from the position
	// buffer's stride.
	Positions []float32
	// Indices is the triangle list; empty when the mesh has no index buffer.
	Indices    []uint32
	BlendBytes []byte
	// BlendStride 0 means modmesh.DefaultBlendStride.
	BlendStride int
	Bones       []modmesh.BlendBoneInfo
}

// MeshSegment maps one input mesh to its vertex range in the merged mesh.
type MeshSegment struct {
	MeshID       string
	Name         string
	VertexOffset int
	VertexCount  int
	// BlendBytes is the input's blend buffer, shared, not copied.
	BlendBytes  []byte
	BlendStride int
	// Bones are local to this segment's blend buffer, so they are never merged
	// across segments.
	Bones []modmesh.BlendBoneInfo
}

// LoadedMesh is the selected meshes merged into one vertex range, so selection,
// symmetry and deformation treat them as one surface, with everything derived
// from the original positions.
type LoadedMesh struct {
	// ID is MeshSelectionKey of MeshIDs.
	ID                string
	MeshIDs           []string
	VertexCount       int
	OriginalPositions []float32
	// PreviewPositions holds the deformed positions the viewport draws and the
	// export uploads.
	PreviewPositions []float32
	// Indices is nil unless every segment has indices.
	Indices  []uint32
	Segments []MeshSegment
	// SelectionWeights is the painted selection mask, one weight per vertex.
	SelectionWeights []float32
	// Adjacency, OriginalNormals and ComponentIDs are nil without indices.
	Adjacency       *VertexAdjacency
	OriginalNormals []float32
	ComponentIDs    []uint32
	// SymmetryMap maps each vertex to its mirror across x = 0, or -1.
	SymmetryMap []int32
	// WeldMap maps each vertex to the first vertex at the same position.
	WeldMap           []uint32
	BoundsCenter      Vec3
	GeodesicWorkspace *GeodesicBrushWorkspace

	boneWeights map[BoneSource][]float32
}

// MeshSelectionKey identifies a set of selected meshes.
func MeshSelectionKey(meshIDs []string) string {
	return strings.Join(meshIDs, "\n")
}

// LoadMesh merges the meshes in order and derives the weld map, adjacency,
// normals, components and symmetry map. It checks ctx between stages.
func LoadMesh(ctx context.Context, inputs []MeshInput) (*LoadedMesh, error) {
	if err := validateInputs(inputs); err != nil {
		return nil, err
	}

	vertexCount, indexCount := 0, 0
	indexed := true
	for _, input := range inputs {
		vertexCount += input.VertexCount
		indexCount += len(input.Indices)
		// A segment without indices cannot share an indexed draw, so the whole mesh falls back.
		indexed = indexed && len(input.Indices) > 0
	}
	positions := make([]float32, 0, vertexCount*3)
	var indices []uint32
	if indexed {
		indices = make([]uint32, 0, indexCount)
	}
	segments := make([]MeshSegment, 0, len(inputs))
	meshIDs := make([]string, 0, len(inputs))
	vertexOffset := 0
	for _, input := range inputs {
		positions = append(positions, input.Positions...)
		if indexed {
			for _, index := range input.Indices {
				indices = append(indices, index+uint32(vertexOffset))
			}
		}
		var blendBytes []byte
		if len(input.BlendBytes) > 0 {
			blendBytes = input.BlendBytes
		}
		segments = append(segments, MeshSegment{
			MeshID:       input.ID,
			Name:         input.Name,
			VertexOffset: vertexOffset,
			VertexCount:  input.VertexCount,
			BlendBytes:   blendBytes,
			BlendStride:  blendStride(input.BlendStride),
			Bones:        input.Bones,
		})
		meshIDs = append(meshIDs, input.ID)
		vertexOffset += input.VertexCount
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	mesh := &LoadedMesh{
		ID:                MeshSelectionKey(meshIDs),
		MeshIDs:           meshIDs,
		VertexCount:       vertexCount,
		OriginalPositions: positions,
		PreviewPositions:  append([]float32(nil), positions...),
		Indices:           indices,
		Segments:          segments,
		SelectionWeights:  make([]float32, vertexCount),
		WeldMap:           BuildPositionWeldMap(positions),
		BoundsCenter:      ComputeBoundingCenter(positions),
		GeodesicWorkspace: NewGeodesicBrushWorkspace(vertexCount),
		boneWeights:       make(map[BoneSource][]float32),
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if indices != nil {
		weldGroups := CollectWeldGroups(mesh.WeldMap)
		mesh.Adjacency = BuildVertexAdjacency(vertexCount, indices, weldGroups)
		if err := validateAdjacency(mesh.Adjacency, vertexCount); err != nil {
			return nil, err
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		mesh.OriginalNormals = ComputeVertexNormals(positions, indices, weldGroups)
		mesh.ComponentIDs = BuildConnectedComponents(vertexCount, mesh.Adjacency)
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
	mesh.SymmetryMap = BuildSymmetryMap(positions, 0, symmetryTolerance)
	return mesh, nil
}

func blendStride(stride int) int {
	if stride == 0 {
		return modmesh.DefaultBlendStride
	}
	return stride
}

func validateInputs(inputs []MeshInput) error {
	if len(inputs) == 0 {
		return errors.New("body shape loader received no meshes")
	}
	seen := make(map[string]bool, len(inputs))
	for _, input := range inputs {
		if seen[input.ID] {
			return fmt.Errorf("body shape loader received mesh %q twice", input.ID)
		}
		seen[input.ID] = true
		if err := validateInput(input); err != nil {
			return fmt.Errorf("body shape mesh %q: %w", input.ID, err)
		}
	}
	return nil
}

func validateInput(input MeshInput) error {
	if input.ID == "" {
		return errors.New("mesh identity is missing")
	}
	if input.VertexCount <= 0 || len(input.Positions) != input.VertexCount*3 {
		return fmt.Errorf(
			"position count %d does not match vertex count %d", len(input.Positions), input.VertexCount,
		)
	}
	if len(input.Indices)%3 != 0 {
		return fmt.Errorf("index count %d is not divisible by 3", len(input.Indices))
	}
	for _, index := range input.Indices {
		if int(index) >= input.VertexCount {
			return fmt.Errorf("index %d exceeds vertex count %d", index, input.VertexCount)
		}
	}
	if input.BlendStride < 0 {
		return fmt.Errorf("blend stride %d must be positive", input.BlendStride)
	}
	return nil
}

func validateAdjacency(adjacency *VertexAdjacency, vertexCount int) error {
	offsets := adjacency.Offsets
	if len(offsets) != vertexCount+1 || offsets[0] != 0 || int(offsets[vertexCount]) != len(adjacency.Neighbors) {
		return errors.New("body shape adjacency offsets are invalid")
	}
	for vertex := range vertexCount {
		if offsets[vertex] > offsets[vertex+1] {
			return errors.New("body shape adjacency offsets are not monotonic")
		}
	}
	for _, neighbor := range adjacency.Neighbors {
		if int(neighbor) >= vertexCount {
			return errors.New("body shape adjacency contains an invalid vertex")
		}
	}
	return nil
}

// SegmentPreview returns the preview positions of one segment, in the layout
// its position upload expects.
func (m *LoadedMesh) SegmentPreview(segment int) []float32 {
	s := m.Segments[segment]
	return m.PreviewPositions[s.VertexOffset*3 : (s.VertexOffset+s.VertexCount)*3]
}

// SegmentByMeshID finds the segment of a mesh.
func (m *LoadedMesh) SegmentByMeshID(meshID string) (int, bool) {
	for index, segment := range m.Segments {
		if segment.MeshID == meshID {
			return index, true
		}
	}
	return 0, false
}

// BoneCount is the number of selectable bones across all segments.
func (m *LoadedMesh) BoneCount() int {
	count := 0
	for _, segment := range m.Segments {
		count += len(segment.Bones)
	}
	return count
}
