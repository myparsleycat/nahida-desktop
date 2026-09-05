package tools

import (
	"encoding/binary"
	"fmt"
	"math"
	"slices"
	"strings"

	"nahida.live/desktop/internal/infra"
)

func detectModelViewerPositionFrame(data []byte, stride int) bool {
	if stride < 40 || len(data) < stride {
		return false
	}
	vertexCount := len(data) / stride
	sampleCount := min(vertexCount, 2000)
	sampleStep := max(1, vertexCount/sampleCount)
	sampled, valid := 0, 0
	read := func(offset int) float64 {
		return float64(math.Float32frombits(binary.LittleEndian.Uint32(data[offset : offset+4])))
	}
	for vertex := 0; vertex < vertexCount && sampled < sampleCount; vertex += sampleStep {
		offset := vertex * stride
		nx, ny, nz := read(offset+12), read(offset+16), read(offset+20)
		tx, ty, tz, tw := read(offset+24), read(offset+28), read(offset+32), read(offset+36)
		normalLength := math.Sqrt(nx*nx + ny*ny + nz*nz)
		tangentLength := math.Sqrt(tx*tx + ty*ty + tz*tz)
		sampled++
		if !math.IsNaN(normalLength) && !math.IsInf(normalLength, 0) && !math.IsNaN(tangentLength) && !math.IsInf(tangentLength, 0) && !math.IsNaN(tw) && !math.IsInf(tw, 0) && normalLength >= 0.9 && normalLength <= 1.1 && tangentLength >= 0.9 && tangentLength <= 1.1 && math.Abs(tw) >= 0.9 && math.Abs(tw) <= 1.1 {
			valid++
		}
	}
	return sampled > 0 && float64(valid)/float64(sampled) >= 0.95
}

type modelViewerGeometry struct {
	Position      []float32
	Normal        []float32
	Tangent       []float32
	Texcoord0     []float32
	Color0        []float32
	Indices       []uint32
	VertexCount   int
	SourceIndices []uint32
}

func extractModelViewerGeometry(vb []byte, stride int, layout modelViewerFmtLayout, indices []uint32, includeTangents, includeColors, compact bool, warn func(string)) (*modelViewerGeometry, error) {
	if !strings.EqualFold(layout.Topology, "trianglelist") {
		return nil, fmt.Errorf("unsupported topology: %s", layout.Topology)
	}
	if stride <= 0 {
		return nil, fmt.Errorf("invalid vertex stride %d", stride)
	}
	position := findModelViewerElement(layout, "POSITION", -1)
	if position == nil {
		return nil, nil
	}
	vertexCount := len(vb) / stride
	sourceIndices := []uint32(nil)
	if compact && len(indices) > 0 {
		var ok bool
		indices, sourceIndices, ok = modelViewerCompactIndices(indices, vertexCount, warn)
		if !ok {
			return nil, nil
		}
		vertexCount = len(sourceIndices)
	}
	read := func(element *modelViewerFmtElement, width int, label string, required bool) ([]float32, bool) {
		if element == nil {
			return nil, !required
		}
		data, err := readModelViewerAttribute(vb, stride, len(vb)/stride, sourceIndices, *element, width)
		if err != nil {
			if warn != nil {
				warn(fmt.Sprintf("Skipping %s: failed to read %s @ %d: %v", label, element.Format, element.AlignedByteOffset, err))
			}
			return nil, false
		}
		return data, true
	}
	positionData, ok := read(position, 3, "POSITION", true)
	if !ok {
		return nil, nil
	}
	mesh := &modelViewerGeometry{Position: positionData, Indices: indices, VertexCount: vertexCount, SourceIndices: sourceIndices}
	if data, valid := read(findModelViewerElement(layout, "NORMAL", -1), 3, "NORMAL", false); valid && data != nil {
		mesh.Normal = data
	}
	if tangent := findModelViewerElement(layout, "TANGENT", -1); includeTangents && tangent != nil {
		width := min(4, modelViewerFormatComponentCount(tangent.Format))
		if data, valid := read(tangent, width, "TANGENT", false); valid && data != nil {
			mesh.Tangent = ensureModelViewerVec4(data, vertexCount, width, 1)
		}
	}
	if data, valid := read(findModelViewerElement(layout, "TEXCOORD", 0), 2, "TEXCOORD_0", false); valid && data != nil {
		mesh.Texcoord0 = data
	}
	if color := findModelViewerElement(layout, "COLOR", 0); includeColors && color != nil {
		width := min(4, modelViewerFormatComponentCount(color.Format))
		if data, valid := read(color, width, "COLOR_0", false); valid && data != nil {
			mesh.Color0 = ensureModelViewerVec4(data, vertexCount, width, 1)
		}
	}
	return mesh, nil
}

func findModelViewerElement(layout modelViewerFmtLayout, semantic string, index int) *modelViewerFmtElement {
	for i := range layout.Elements {
		element := &layout.Elements[i]
		if strings.EqualFold(element.SemanticName, semantic) && (index < 0 || element.SemanticIndex == index) {
			return element
		}
	}
	return nil
}

func readModelViewerAttribute(bytes []byte, stride, vertexCount int, sourceIndices []uint32, element modelViewerFmtElement, width int) ([]float32, error) {
	decoder, err := resolveModelViewerFormatDecoder(element.Format)
	if err != nil || decoder.byteSize <= 0 {
		return nil, infra.WithCause(fmt.Errorf("unsupported attribute format %s", element.Format), err)
	}
	endOffset := element.AlignedByteOffset + decoder.byteSize
	if element.AlignedByteOffset < 0 || endOffset > stride {
		return nil, fmt.Errorf("attribute exceeds vertex stride %d", stride)
	}
	if vertexCount > 0 && (vertexCount-1)*stride+endOffset > len(bytes) {
		return nil, fmt.Errorf("attribute exceeds vertex buffer length %d", len(bytes))
	}
	count := vertexCount
	if sourceIndices != nil {
		count = len(sourceIndices)
	}
	out := make([]float32, count*width)
	values := make([]float32, max(decoder.components, width))
	for i := range count {
		source := i
		if sourceIndices != nil {
			source = int(sourceIndices[i])
		}
		offset := source*stride + element.AlignedByteOffset
		if err := readModelViewerDecoded(bytes, offset, decoder, values); err != nil {
			return nil, err
		}
		copy(out[i*width:(i+1)*width], values[:min(width, decoder.components)])
	}
	return out, nil
}

func modelViewerCompactIndices(indices []uint32, vertexCount int, warn func(string)) ([]uint32, []uint32, bool) {
	seen := make(map[uint32]bool)
	sources := make([]uint32, 0, len(indices))
	for _, source := range indices {
		if int(source) >= vertexCount {
			if warn != nil {
				warn(fmt.Sprintf("Skipping compacted animation geometry: index %d exceeds vertex count %d", source, vertexCount))
			}
			return nil, nil, false
		}
		if !seen[source] {
			seen[source] = true
			sources = append(sources, source)
		}
	}
	slices.Sort(sources)
	lookup := make(map[uint32]uint32, len(sources))
	for index, source := range sources {
		lookup[source] = uint32(index)
	}
	remapped := make([]uint32, len(indices))
	for index, source := range indices {
		remapped[index] = lookup[source]
	}
	return remapped, sources, true
}

func reverseModelViewerTriangleWinding(indices []uint32) {
	for start := 0; start+2 < len(indices); start += 3 {
		indices[start+1], indices[start+2] = indices[start+2], indices[start+1]
	}
}

func ensureModelViewerVec4(data []float32, vertexCount, width int, fillW float32) []float32 {
	if width == 4 {
		return data
	}
	out := make([]float32, vertexCount*4)
	for i := range vertexCount {
		for component := range min(width, 3) {
			if i*width+component < len(data) {
				out[i*4+component] = data[i*width+component]
			}
		}
		out[i*4+3] = fillW
	}
	return out
}
