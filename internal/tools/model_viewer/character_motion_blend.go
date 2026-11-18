package modelviewer

import (
	"encoding/binary"
	"io"
	"math"
	"path/filepath"
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
}

// resolveModelViewerBlendSource finds the blend buffer of a draw: the one bound as vb2, or the one named after
// the position buffer (FooPosition and FooBlend, or EFMI's _VB0 and _VB2). A blend buffer that also covers the
// other carriers of a merged buffer is read from the carrier's first row.
func resolveModelViewerBlendSource(
	modDir string,
	vb2 string,
	position modelViewerResource,
	carrierStart int,
	resourceMap map[string]modelViewerResource,
	resources []modelViewerResource,
	cache *modelViewerBufferCache,
) *modelViewerBlendSource {
	candidates := []modelViewerResource{}
	if blend, ok := resourceMap[modelViewerNormalizeKey(vb2)]; ok && vb2 != "" {
		candidates = append(candidates, blend)
	}
	if typed := parseModelViewerMihoyoResourceName(position.Name); typed != nil && typed.Kind == "position" {
		for _, resource := range resources {
			if other := parseModelViewerMihoyoResourceName(resource.Name); other != nil && other.Kind == "blend" &&
				modelViewerKeyMatches(other.Key, typed.Key, true) {
				candidates = append(candidates, resource)
			}
		}
	}
	if name := modelViewerNormalizeKey(position.Name); strings.HasSuffix(name, "vb0") {
		if blend, ok := resourceMap[strings.TrimSuffix(name, "vb0")+"vb2"]; ok {
			candidates = append(candidates, blend)
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
		if !modelViewerBlendStride(candidate.Stride) || candidate.Filename == "" {
			continue
		}
		path, ok := cache.modBufferPath(modDir, candidate.Filename)
		if !ok {
			continue
		}
		size, err := cache.fileSize(path)
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
			path:      path,
			palette:   strings.ToLower(filepath.ToSlash(candidate.Filename)) + "#" + strconv.Itoa(offset),
			stride:    candidate.Stride,
			rowOffset: offset,
		}
	}
	return nil
}

// modelViewerBlendStride reports the blend layouts read: four float32 weights and four int32 groups (GIMI,
// SRMI, ZZMI), or four UNORM16 weights followed by four uint16 (EFMI) or four uint8 groups (EFMI compact).
func modelViewerBlendStride(stride int) bool {
	return stride == 32 || stride == 16 || stride == 12
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
	if !modelViewerBlendStride(stride) || vertexCount <= 0 {
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

func writeModelViewerUint16s(w io.Writer, values []uint16) error {
	buffer := make([]byte, len(values)*2)
	for i, value := range values {
		binary.LittleEndian.PutUint16(buffer[i*2:], value)
	}
	_, err := w.Write(buffer)
	return err
}
