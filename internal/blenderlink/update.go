package blenderlink

import (
	"cmp"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"slices"
)

// ObjectUpdate is one committed revision of a Blender object with every attribute decoded. The
// slices are owned by the receiver.
type ObjectUpdate struct {
	SceneRev     uint32
	ObjectKey    string
	Revision     uint32
	VertexCount  int
	LoopCount    int
	VertexGroups []string
	// Positions holds x, y, z per vertex.
	Positions []float32
	// LoopVertices holds the vertex of each triangle corner, three corners per triangle.
	LoopVertices []uint32
	// Normals and Tangents hold x, y, z per loop and BitangentSigns one value per loop; each is
	// nil when the client did not send it.
	Normals        []float32
	Tangents       []float32
	BitangentSigns []float32
	// UVs hold u, v per loop and Colors r, g, b, a per loop, ordered by layer index.
	UVs     []Layer
	Colors  []Layer
	Weights Weights
	// Changed lists the attributes whose content differs from the previous revision of the object
	// on this connection, in ascending order; every attribute on the first revision.
	Changed []AttrID
}

type Layer struct {
	Index int
	Data  []float32
}

// Weights is the sparse vertex group assignment: vertex v owns the next Counts[v] entries of
// Groups and Values. Counts is nil when the object has no weights.
type Weights struct {
	Counts []uint8
	Groups []uint16
	Values []float32
}

type attrDomain uint8

const (
	domainVertex attrDomain = iota
	domainLoop
	// domainWeight attributes have one element per vertex group assignment, the sum of VG_COUNT.
	domainWeight
)

type attrLayout struct {
	domain   attrDomain
	elemSize uint64
}

func layoutOf(id AttrID) (attrLayout, bool) {
	switch {
	case id == AttrVertCo:
		return attrLayout{domainVertex, 12}, true
	case id == AttrLoopVert, id == AttrLoopBTSign:
		return attrLayout{domainLoop, 4}, true
	case id == AttrLoopNormal, id == AttrLoopTangent:
		return attrLayout{domainLoop, 12}, true
	case id >= attrUVBase && id < attrUVBase+maxLayers:
		return attrLayout{domainLoop, 8}, true
	case id >= attrColorBase && id < attrColorBase+maxLayers:
		return attrLayout{domainLoop, 16}, true
	case id == AttrVGCount:
		return attrLayout{domainVertex, 1}, true
	case id == AttrVGIndex:
		return attrLayout{domainWeight, 2}, true
	case id == AttrVGWeight:
		return attrLayout{domainWeight, 4}, true
	default:
		return attrLayout{}, false
	}
}

// checkLen validates a declared byte length against the counts in MeshBegin. Weight attributes are
// only bounded here; buildUpdate checks them against VG_COUNT.
func (l attrLayout) checkLen(byteLen, vertices, loops uint32) error {
	n := uint64(byteLen)
	switch l.domain {
	case domainVertex:
		if want := uint64(vertices) * l.elemSize; n != want {
			return fmt.Errorf("%d bytes, want %d for %d vertices", n, want, vertices)
		}
	case domainLoop:
		if want := uint64(loops) * l.elemSize; n != want {
			return fmt.Errorf("%d bytes, want %d for %d loops", n, want, loops)
		}
	case domainWeight:
		if limit := uint64(vertices) * math.MaxUint8 * l.elemSize; n%l.elemSize != 0 || n > limit {
			return fmt.Errorf("%d bytes for %d vertices", n, vertices)
		}
	}
	return nil
}

// buildUpdate decodes and cross-checks the attribute bytes of a committed transfer.
func buildUpdate(begin MeshBegin, data map[AttrID][]byte) (ObjectUpdate, error) {
	update := ObjectUpdate{
		SceneRev: begin.SceneRev, ObjectKey: begin.ObjectKey, Revision: begin.Revision,
		VertexCount: int(begin.VertexCount), LoopCount: int(begin.LoopCount),
		VertexGroups: slices.Clone(begin.VertexGroups),
	}
	positions, ok := data[AttrVertCo]
	if !ok {
		return ObjectUpdate{}, errors.New("missing VERT_CO")
	}
	loops, ok := data[AttrLoopVert]
	if !ok {
		return ObjectUpdate{}, errors.New("missing LOOP_VERT")
	}
	update.Positions = float32s(positions)
	update.LoopVertices = uint32s(loops)
	for corner, vertex := range update.LoopVertices {
		if vertex >= begin.VertexCount {
			return ObjectUpdate{}, fmt.Errorf("loop %d refers to vertex %d of %d", corner, vertex, begin.VertexCount)
		}
	}

	weights, err := buildWeights(data, len(begin.VertexGroups))
	if err != nil {
		return ObjectUpdate{}, err
	}
	update.Weights = weights

	for id, bytes := range data {
		switch {
		case id == AttrLoopNormal:
			update.Normals = float32s(bytes)
		case id == AttrLoopTangent:
			update.Tangents = float32s(bytes)
		case id == AttrLoopBTSign:
			update.BitangentSigns = float32s(bytes)
		case id >= attrUVBase && id < attrUVBase+maxLayers:
			update.UVs = append(update.UVs, Layer{Index: int(id - attrUVBase), Data: float32s(bytes)})
		case id >= attrColorBase && id < attrColorBase+maxLayers:
			update.Colors = append(update.Colors, Layer{Index: int(id - attrColorBase), Data: float32s(bytes)})
		}
	}
	byIndex := func(a, b Layer) int { return cmp.Compare(a.Index, b.Index) }
	slices.SortFunc(update.UVs, byIndex)
	slices.SortFunc(update.Colors, byIndex)
	return update, nil
}

func buildWeights(data map[AttrID][]byte, groups int) (Weights, error) {
	counts, hasCounts := data[AttrVGCount]
	indices, hasIndices := data[AttrVGIndex]
	values, hasValues := data[AttrVGWeight]
	if !hasCounts && !hasIndices && !hasValues {
		return Weights{}, nil
	}
	if !hasCounts || !hasIndices || !hasValues {
		return Weights{}, errors.New("VG_COUNT, VG_INDEX and VG_WEIGHT must be sent together")
	}

	total := 0
	for _, n := range counts {
		total += int(n)
	}
	if len(indices) != 2*total || len(values) != 4*total {
		return Weights{}, fmt.Errorf(
			"%d VG_INDEX and %d VG_WEIGHT bytes for %d assignments", len(indices), len(values), total,
		)
	}
	weights := Weights{Counts: slices.Clone(counts), Groups: uint16s(indices), Values: float32s(values)}
	for i, group := range weights.Groups {
		if int(group) >= groups {
			return Weights{}, fmt.Errorf("assignment %d refers to vertex group %d of %d", i, group, groups)
		}
	}
	return weights, nil
}

func float32s(b []byte) []float32 {
	out := make([]float32, len(b)/4)
	for i := range out {
		out[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[4*i:]))
	}
	return out
}

func uint32s(b []byte) []uint32 {
	out := make([]uint32, len(b)/4)
	for i := range out {
		out[i] = binary.LittleEndian.Uint32(b[4*i:])
	}
	return out
}

func uint16s(b []byte) []uint16 {
	out := make([]uint16, len(b)/2)
	for i := range out {
		out[i] = binary.LittleEndian.Uint16(b[2*i:])
	}
	return out
}
