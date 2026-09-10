package tools

import (
	"encoding/binary"
	"math"
)

// MVG1 stores five element counts followed by position, normal, tangent, UV,
// and uint32 index arrays. All fields are little-endian and four-byte aligned.
func modelViewerMeshBytes(payload modelViewerMeshPayload) []byte {
	attributes := [][]float32{payload.Positions, payload.Normals, payload.Tangents, payload.UVs}
	count := len(payload.Indices)
	for _, values := range attributes {
		count += len(values)
	}
	data := make([]byte, 0, 24+count*4)
	data = append(data, 'M', 'V', 'G', '1')
	for _, values := range attributes {
		data = binary.LittleEndian.AppendUint32(data, uint32(len(values)))
	}
	data = binary.LittleEndian.AppendUint32(data, uint32(len(payload.Indices)))

	for _, values := range attributes {
		for _, value := range values {
			data = binary.LittleEndian.AppendUint32(data, math.Float32bits(value))
		}
	}
	for _, index := range payload.Indices {
		data = binary.LittleEndian.AppendUint32(data, index)
	}
	return data
}
