package modelviewer

import (
	"cmp"
	"encoding/binary"
	"io"
	"math"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// modelViewerBlendSource is the blend buffer whose rows pair with a mesh's position rows.
type modelViewerBlendSource struct {
	path string
	// palette names the bone palette the buffer's groups index: meshes sharing the buffer share it.
	palette   string
	stride    int
	rowOffset int
	// wwmi marks WWMI's layout: eight uint8 (stride 16) or uint16 (stride 32) groups then their weights, or four
	// uint8 groups then four UNORM8 weights (stride 8).
	wwmi bool
}

// resolveModelViewerBlendSource finds the blend buffer of a draw: one of the resources bound as its blend slot
// (bound: vb2, or vb4 for a WWMI draw), or the one named after the position buffer (FooPosition and FooBlend, EFMI's
// _VB0 and _VB2, or WWMI's FooPositionBuffer and FooBlendBuffer). A blend buffer that also covers the other carriers
// of a merged buffer is read from the carrier's first row.
func resolveModelViewerBlendSource(
	modDir string,
	bound []string,
	position modelViewerResource,
	carrierStart int,
	wwmi bool,
	resourceMap map[string]modelViewerResource,
	resources []modelViewerResource,
	cache *modelViewerBufferCache,
) *modelViewerBlendSource {
	candidates := []modelViewerResource{}
	for _, name := range bound {
		if blend, ok := resourceMap[modelViewerNormalizeKey(name)]; ok && name != "" {
			candidates = append(candidates, blend)
		}
	}
	parse := parseModelViewerMihoyoResourceName
	if wwmi {
		parse = parseModelViewerWwmiResourceName
	}
	if typed := parse(position.Name); typed != nil && typed.Kind == "position" {
		for _, resource := range resources {
			if other := parse(resource.Name); other != nil && other.Kind == "blend" &&
				modelViewerKeyMatches(other.Key, typed.Key, true) {
				candidates = append(candidates, resource)
			}
		}
	}
	if !wwmi {
		if name := modelViewerNormalizeKey(position.Name); strings.HasSuffix(name, "vb0") {
			if blend, ok := resourceMap[strings.TrimSuffix(name, "vb0")+"vb2"]; ok {
				candidates = append(candidates, blend)
			}
		}
	}

	positionPath, ok := cache.modBufferPath(modDir, position.Filename)
	if !ok || position.Stride <= 0 {
		return nil
	}
	positionSize, err := cache.fileSize(positionPath)
	if err != nil {
		return nil
	}
	rows := int(positionSize / int64(position.Stride))
	for _, candidate := range candidates {
		if !modelViewerBlendStride(candidate.Stride, wwmi) || candidate.Filename == "" {
			continue
		}
		blendPath, ok := cache.modBufferPath(modDir, candidate.Filename)
		if !ok {
			continue
		}
		size, err := cache.fileSize(blendPath)
		if err != nil {
			continue
		}
		blendRows := int(size / int64(candidate.Stride))
		offset := 0
		if blendRows != rows && carrierStart > 0 && blendRows >= carrierStart+rows {
			offset = carrierStart
		} else if blendRows < rows {
			continue
		}
		return &modelViewerBlendSource{
			path:      blendPath,
			palette:   strings.ToLower(filepath.ToSlash(candidate.Filename)) + "#" + strconv.Itoa(offset),
			stride:    candidate.Stride,
			rowOffset: offset,
			wwmi:      wwmi,
		}
	}
	return nil
}

// wwmiComponentPalette names the palette of one WWMI component's draws in a blend buffer. Mods without a merged
// skeleton index each component's own bones from zero, so the components of one buffer do not share a palette;
// with a merged skeleton each component's groups are still one consistent subset of it.
func wwmiComponentPalette(palette, component string) string {
	return palette + "|" + strings.ToLower(component)
}

// modelViewerBlendStride reports the blend layouts read: four float32 weights and four int32 groups (GIMI,
// SRMI, ZZMI), four UNORM16 weights followed by four uint16 (EFMI) or four uint8 groups (EFMI compact), or one
// uint32 group without weights that binds the vertex rigidly (EFMI rigid parts); for WWMI, the strides of the
// layouts its blend source names.
func modelViewerBlendStride(stride int, wwmi bool) bool {
	if wwmi {
		return stride == 8 || stride == 16 || stride == 32
	}
	return stride == 32 || stride == 16 || stride == 12 || stride == 4
}

// readModelViewerMeshBlend returns four groups, then four UNORM16 weights, per mesh vertex; nil when the buffer
// cannot be read or does not cover the mesh.
func readModelViewerMeshBlend(
	cache *modelViewerBufferCache,
	source *modelViewerBlendSource,
	sourceIndices []uint32,
	vertexCount int,
) []uint16 {
	if source == nil || vertexCount <= 0 {
		return nil
	}
	data, err := cache.read(source.path)
	if err != nil {
		return nil
	}
	if source.wwmi {
		return decodeModelViewerWwmiMeshBlend(data, source.stride, source.rowOffset, sourceIndices, vertexCount)
	}
	return decodeModelViewerMeshBlend(data, source.stride, source.rowOffset, sourceIndices, vertexCount)
}

// decodeModelViewerMeshBlend reads the blend rows of data, in one of the layouts modelViewerBlendStride accepts,
// into four groups and four UNORM16 weights per mesh vertex; nil when the rows do not cover the mesh.
func decodeModelViewerMeshBlend(
	data []byte,
	stride, rowOffset int,
	sourceIndices []uint32,
	vertexCount int,
) []uint16 {
	if !modelViewerBlendStride(stride, false) || vertexCount <= 0 {
		return nil
	}
	rows := len(data) / stride
	out := make([]uint16, vertexCount*8)
	for vertex := range vertexCount {
		row := vertex
		if sourceIndices != nil {
			if vertex >= len(sourceIndices) {
				return nil
			}
			row = int(sourceIndices[vertex])
		}
		row += rowOffset
		if row < 0 || row >= rows {
			return nil
		}
		record := data[row*stride:]
		var groups [4]int64
		var weights [4]float64
		for k := range 4 {
			switch stride {
			case 32:
				weights[k] = float64(math.Float32frombits(binary.LittleEndian.Uint32(record[k*4:])))
				groups[k] = int64(int32(binary.LittleEndian.Uint32(record[16+k*4:])))
			case 16:
				weights[k] = float64(binary.LittleEndian.Uint16(record[k*2:])) / 65535
				groups[k] = int64(binary.LittleEndian.Uint16(record[8+k*2:]))
			case 12:
				weights[k] = float64(binary.LittleEndian.Uint16(record[k*2:])) / 65535
				groups[k] = int64(record[8+k])
			case 4:
				if k == 0 {
					weights[k] = 1
					groups[k] = int64(binary.LittleEndian.Uint32(record))
				}
			}
			if math.IsNaN(weights[k]) || weights[k] < 0 || groups[k] < 0 || groups[k] > math.MaxUint16 {
				weights[k] = 0
			}
		}
		sum := weights[0] + weights[1] + weights[2] + weights[3]
		if sum <= 0 || math.IsInf(sum, 0) {
			continue
		}
		for k := range 4 {
			if weights[k] > 0 {
				out[vertex*8+k] = uint16(groups[k])
				out[vertex*8+4+k] = uint16(math.Round(weights[k] / sum * 65535))
			}
		}
	}
	return out
}

// decodeModelViewerWwmiMeshBlend reads WWMI blend rows into the four strongest groups and their UNORM16 weights per
// mesh vertex; nil when the rows do not cover the mesh.
func decodeModelViewerWwmiMeshBlend(
	data []byte,
	stride, rowOffset int,
	sourceIndices []uint32,
	vertexCount int,
) []uint16 {
	if !modelViewerBlendStride(stride, true) || vertexCount <= 0 {
		return nil
	}
	slots, size := stride/2, 1
	if stride == 32 {
		slots, size = 8, 2
	}
	read := func(record []byte, k int) int {
		if size == 2 {
			return int(binary.LittleEndian.Uint16(record[k*2:]))
		}
		return int(record[k])
	}
	scale := float64(int(1)<<(8*size) - 1)

	rows := len(data) / stride
	out := make([]uint16, vertexCount*8)
	type influence struct {
		group  int
		weight float64
	}
	influences := make([]influence, 0, slots)
	for vertex := range vertexCount {
		row := vertex
		if sourceIndices != nil {
			if vertex >= len(sourceIndices) {
				return nil
			}
			row = int(sourceIndices[vertex])
		}
		row += rowOffset
		if row < 0 || row >= rows {
			return nil
		}
		record := data[row*stride : (row+1)*stride]
		influences = influences[:0]
		for k := range slots {
			if weight := float64(read(record, slots+k)) / scale; weight > 0 {
				influences = append(influences, influence{group: read(record, k), weight: weight})
			}
		}
		slices.SortStableFunc(influences, func(a, b influence) int { return cmp.Compare(b.weight, a.weight) })
		influences = influences[:min(len(influences), 4)]
		sum := 0.0
		for _, in := range influences {
			sum += in.weight
		}
		if sum <= 0 {
			continue
		}
		for k, in := range influences {
			out[vertex*8+k] = uint16(in.group)
			out[vertex*8+4+k] = uint16(math.Round(in.weight / sum * 65535))
		}
	}
	return out
}

func writeModelViewerUint16s(w io.Writer, values []uint16) error {
	buffer := make([]byte, len(values)*2)
	for i, value := range values {
		binary.LittleEndian.PutUint16(buffer[i*2:], value)
	}
	_, err := w.Write(buffer)
	return err
}
