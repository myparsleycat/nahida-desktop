package sculpt

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"

	"nahida.live/desktop/internal/tools/modmesh"
)

// VectorLayoutSnorm8 is the 8-byte tangent and normal layout: tangent xyzw then
// normal xyzw as SNORM8. It is the vector layout name the backend reports.
const VectorLayoutSnorm8 = "snorm8-tangent-normal"

const snorm8Stride = 8

// WritePositionsIntoBuffer returns a copy of a position buffer with the xyz of
// each stride replaced, keeping padding and any other per-vertex fields.
func WritePositionsIntoBuffer(original []byte, stride int, positions []float32) ([]byte, error) {
	vertexCount, err := modmesh.ValidatePositionBuffer(len(original), stride, nil)
	if err != nil {
		return nil, err
	}
	if len(positions) != vertexCount*3 {
		return nil, fmt.Errorf(
			"position count %d does not match vertex count %d", len(positions)/3, vertexCount,
		)
	}
	if err := CheckFinitePositions(positions); err != nil {
		return nil, err
	}

	output := append([]byte(nil), original...)
	for i := range vertexCount {
		base := i * stride
		binary.LittleEndian.PutUint32(output[base:], math.Float32bits(positions[i*3]))
		binary.LittleEndian.PutUint32(output[base+4:], math.Float32bits(positions[i*3+1]))
		binary.LittleEndian.PutUint32(output[base+8:], math.Float32bits(positions[i*3+2]))
	}
	return output, nil
}

// CheckFinitePositions rejects NaN and infinite coordinates before a write.
func CheckFinitePositions(positions []float32) error {
	for i, value := range positions {
		if !isFinite(float64(value)) {
			return fmt.Errorf("non-finite position at index %d", i)
		}
	}
	return nil
}

// DetectSnorm8VectorLayout reports whether a vector buffer is the 8-byte SNORM8
// tangent and normal layout. Stride 0 means 8. Callers must leave any other
// layout untouched.
func DetectSnorm8VectorLayout(fileSize, vertexCount, stride int) bool {
	if stride == 0 {
		stride = snorm8Stride
	}
	return stride == snorm8Stride && fileSize == vertexCount*snorm8Stride
}

// ApplySnorm8VectorCorrection corrects the tangents and normals of weighted
// vertices for the scale deformation and returns a new buffer; the handedness
// bytes 3 and 7 are kept.
func ApplySnorm8VectorCorrection(original []int8, weights []float32, amount float64, axisScale Vec3) ([]int8, error) {
	if len(original)%snorm8Stride != 0 {
		return nil, errors.New("vector buffer length is not divisible by 8")
	}

	vertexCount := min(len(weights), len(original)/snorm8Stride)
	output := append([]int8(nil), original...)
	for i := range vertexCount {
		w := float64(weights[i])
		if w <= 0 || amount == 0 {
			continue
		}
		CorrectSnorm8TangentNormal(output, i, EffectiveScaleFactors(w, amount, axisScale))
	}
	return output, nil
}
