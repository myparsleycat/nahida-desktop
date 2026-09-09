package tools

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
)

// Match Three.js's area-weighted indexed normals, including Float32 accumulation.
func modelViewerVertexNormals(ctx context.Context, positions []float32, indices []uint32) ([]float32, error) {
	normals := make([]float32, len(positions))
	for triangle := 0; triangle+2 < len(indices); triangle += 3 {
		if triangle%12288 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		a, b, c := int(indices[triangle])*3, int(indices[triangle+1])*3, int(indices[triangle+2])*3
		if a+2 >= len(positions) || b+2 >= len(positions) || c+2 >= len(positions) {
			return nil, fmt.Errorf("triangle index exceeds vertex count")
		}
		ab := [3]float64{float64(positions[a]) - float64(positions[b]), float64(positions[a+1]) - float64(positions[b+1]), float64(positions[a+2]) - float64(positions[b+2])}
		cb := [3]float64{float64(positions[c]) - float64(positions[b]), float64(positions[c+1]) - float64(positions[b+1]), float64(positions[c+2]) - float64(positions[b+2])}
		n := [3]float64{cb[1]*ab[2] - cb[2]*ab[1], cb[2]*ab[0] - cb[0]*ab[2], cb[0]*ab[1] - cb[1]*ab[0]}
		for _, offset := range []int{a, b, c} {
			for axis := range 3 {
				normals[offset+axis] = float32(float64(normals[offset+axis]) + n[axis])
			}
		}
	}
	for offset := 0; offset < len(normals); offset += 3 {
		x, y, z := float64(normals[offset]), float64(normals[offset+1]), float64(normals[offset+2])
		length := math.Sqrt(x*x + y*y + z*z)
		if length > 0 {
			for axis := range 3 {
				normals[offset+axis] = float32(float64(normals[offset+axis]) / length)
			}
		}
	}
	return normals, ctx.Err()
}

func modelViewerGeometryBounds(ctx context.Context, positions []float32) (*ModelViewerBounds, error) {
	bounds := &ModelViewerBounds{}
	if len(positions) == 0 {
		return bounds, nil
	}
	if len(positions)%3 != 0 {
		return nil, fmt.Errorf("invalid position attribute length")
	}
	for axis := range 3 {
		bounds.Min[axis], bounds.Max[axis] = math.Inf(1), math.Inf(-1)
	}
	for offset := 0; offset < len(positions); offset += 3 {
		if offset%12288 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		for axis := range 3 {
			value := float64(positions[offset+axis])
			if math.IsNaN(value) || math.IsInf(value, 0) {
				return nil, fmt.Errorf("non-finite position at vertex %d", offset/3)
			}
			bounds.Min[axis] = min(bounds.Min[axis], value)
			bounds.Max[axis] = max(bounds.Max[axis], value)
		}
	}
	for axis := range 3 {
		bounds.Center[axis] = (bounds.Min[axis] + bounds.Max[axis]) / 2
	}
	var radiusSquared float64
	for offset := 0; offset < len(positions); offset += 3 {
		x, y, z := float64(positions[offset])-bounds.Center[0], float64(positions[offset+1])-bounds.Center[1], float64(positions[offset+2])-bounds.Center[2]
		radiusSquared = max(radiusSquared, x*x+y*y+z*z)
	}
	bounds.Radius = math.Sqrt(radiusSquared)
	return bounds, ctx.Err()
}

// Geometry variants: ten float64 bounds values, then compact float32 XYZ and normals.
const modelViewerVariantHeaderBytes = 10 * 8

func modelViewerVariantBytes(bounds *ModelViewerBounds, positions, normals []float32) []byte {
	data := make([]byte, modelViewerVariantHeaderBytes+4*(len(positions)+len(normals)))
	values := []float64{bounds.Min[0], bounds.Min[1], bounds.Min[2], bounds.Max[0], bounds.Max[1], bounds.Max[2], bounds.Center[0], bounds.Center[1], bounds.Center[2], bounds.Radius}
	for i, value := range values {
		binary.LittleEndian.PutUint64(data[i*8:], math.Float64bits(value))
	}
	offset := modelViewerVariantHeaderBytes
	for _, attribute := range [][]float32{positions, normals} {
		for _, value := range attribute {
			binary.LittleEndian.PutUint32(data[offset:], math.Float32bits(value))
			offset += 4
		}
	}
	return data
}
