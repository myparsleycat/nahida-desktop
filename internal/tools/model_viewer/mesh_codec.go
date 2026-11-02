package modelviewer

import (
	"encoding/binary"
	"math"
)

// MVG2 stores six element counts followed by position, normal, tangent, and
// UV float32 arrays, RGBA8 vertex colors, and uint32 indices. All fields are
// little-endian; colors hold four bytes per vertex, so every array stays
// four-byte aligned.
func modelViewerMeshBytes(payload modelViewerMeshPayload) []byte {
	attributes := [][]float32{payload.Positions, payload.Normals, payload.Tangents, payload.UVs}
	count := len(payload.Indices)
	for _, values := range attributes {
		count += len(values)
	}
	data := make([]byte, 0, 28+count*4+len(payload.Colors))
	data = append(data, 'M', 'V', 'G', '2')
	for _, values := range attributes {
		data = binary.LittleEndian.AppendUint32(data, uint32(len(values)))
	}
	data = binary.LittleEndian.AppendUint32(data, uint32(len(payload.Colors)))
	data = binary.LittleEndian.AppendUint32(data, uint32(len(payload.Indices)))

	for _, values := range attributes {
		for _, value := range values {
			data = binary.LittleEndian.AppendUint32(data, math.Float32bits(value))
		}
	}
	data = append(data, payload.Colors...)
	for _, index := range payload.Indices {
		data = binary.LittleEndian.AppendUint32(data, index)
	}
	return data
}

func modelViewerColorBytes(colors []float32) []uint8 {
	if len(colors) == 0 {
		return nil
	}
	data := make([]uint8, len(colors))
	for index, value := range colors {
		data[index] = uint8(math.Round(float64(min(max(value, 0), 1)) * 255))
	}
	return data
}
