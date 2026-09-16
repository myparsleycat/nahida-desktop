package modmesh

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/platform"
)

// DefaultBlendStride is the blend layout used when a mod does not declare one.
const DefaultBlendStride = 16

type BlendBoneInfo struct {
	ID          uint32 `json:"id"`
	VertexCount int    `json:"vertexCount"`
}

func Float32Bytes(values []float32) []byte {
	data := make([]byte, len(values)*4)
	for index, value := range values {
		binary.LittleEndian.PutUint32(data[index*4:], math.Float32bits(value))
	}
	return data
}

func Uint32Bytes(values []uint32) []byte {
	data := make([]byte, len(values)*4)
	for index, value := range values {
		binary.LittleEndian.PutUint32(data[index*4:], value)
	}
	return data
}

func DecodeFloat32Bytes(data []byte) ([]float32, error) {
	if len(data)%4 != 0 {
		return nil, errors.New("float32 buffer is not 4-byte aligned")
	}
	values := make([]float32, len(data)/4)
	for index := range values {
		values[index] = math.Float32frombits(binary.LittleEndian.Uint32(data[index*4:]))
	}
	return values, nil
}

func ValidatePositionBuffer(size, stride int, expected *int) (int, error) {
	if stride < 12 {
		return 0, infra.ContractError(fmt.Sprintf("Unsupported position stride: %d", stride))
	}
	if size <= 0 || size%stride != 0 {
		return 0, infra.ContractError(fmt.Sprintf("Position file size %d is not divisible by stride %d", size, stride))
	}
	vertices := size / stride
	if expected != nil && vertices != *expected {
		return 0, infra.ContractError(
			fmt.Sprintf("Vertex count mismatch: file has %d, expected %d", vertices, *expected),
		)
	}
	return vertices, nil
}

func ExtractPositions(data []byte, stride int) ([]float32, error) {
	count, err := ValidatePositionBuffer(len(data), stride, nil)
	if err != nil {
		return nil, err
	}
	out := make([]float32, count*3)
	for vertex := range count {
		base, offset := vertex*stride, vertex*3
		out[offset] = math.Float32frombits(binary.LittleEndian.Uint32(data[base:]))
		out[offset+1] = math.Float32frombits(binary.LittleEndian.Uint32(data[base+4:]))
		out[offset+2] = math.Float32frombits(binary.LittleEndian.Uint32(data[base+8:]))
	}
	return out, nil
}

func ValidateBlendBuffer(size, vertexCount, stride int) error {
	if stride != 4 && stride != 8 && stride != 12 && stride != 32 && stride < 16 {
		return infra.ContractError(fmt.Sprintf("Unsupported blend stride: %d", stride))
	}
	if vertexCount <= 0 {
		return infra.ContractError("Blend vertex count must be positive")
	}
	if size < vertexCount*stride {
		return infra.ContractError(fmt.Sprintf("Blend buffer too small: %d < %d", size, vertexCount*stride))
	}
	return nil
}

func ListBlendBones(data []byte, vertexCount, stride int) []BlendBoneInfo {
	counts := make(map[uint32]int)
	limit := min(vertexCount, len(data)/stride)
	for vertex := range limit {
		seen := make(map[uint32]bool)
		VisitBlendInfluences(data, vertex*stride, stride, func(id uint32, weight float32) {
			if weight > 0 && !seen[id] {
				seen[id] = true
				counts[id]++
			}
		})
	}
	bones := make([]BlendBoneInfo, 0, len(counts))
	for id, count := range counts {
		bones = append(bones, BlendBoneInfo{ID: id, VertexCount: count})
	}
	sort.Slice(bones, func(i, j int) bool { return bones[i].ID < bones[j].ID })
	return bones
}

func VisitBlendInfluences(data []byte, base, stride int, visit func(uint32, float32)) {
	if base+stride > len(data) {
		return
	}
	switch stride {
	case 4:
		visit(binary.LittleEndian.Uint32(data[base:]), 1)
	case 12:
		for index := range 4 {
			weight := float32(binary.LittleEndian.Uint16(data[base+index*2:])) / 65535
			if weight > 0 {
				visit(uint32(data[base+8+index]), weight)
			}
		}
	case 32:
		for index := range 4 {
			weight := math.Float32frombits(binary.LittleEndian.Uint32(data[base+index*4:]))
			if weight > 0 && !math.IsNaN(float64(weight)) && !math.IsInf(float64(weight), 0) {
				visit(binary.LittleEndian.Uint32(data[base+16+index*4:]), weight)
			}
		}
	default:
		weightsOffset := 8
		if stride == 8 {
			weightsOffset = 4
		}
		for index := range 4 {
			if weight := float32(data[base+weightsOffset+index]) / 255; weight > 0 {
				visit(uint32(data[base+index]), weight)
			}
		}
	}
}

// ResolveResource resolves a buffer file declared by mod.ini against the mod
// root and rejects paths that escape it.
func ResolveResource(root, relative string) (string, error) {
	if strings.TrimSpace(relative) == "" {
		return "", errors.New("resource filename is empty")
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	path, err := filepath.Abs(filepath.Join(rootAbs, filepath.FromSlash(relative)))
	if err != nil || !platform.SameOrChildPath(rootAbs, path) || platform.SamePathFold(rootAbs, path) {
		return "", infra.WithCause(errors.New("resource path is outside mod root"), err)
	}
	realRoot, err := filepath.EvalSymlinks(rootAbs)
	if err != nil {
		return "", err
	}
	realPath, err := filepath.EvalSymlinks(path)
	if err != nil || !platform.SameOrChildPath(realRoot, realPath) || platform.SamePathFold(realRoot, realPath) {
		return "", infra.WithCause(errors.New("resource path is outside mod root"), err)
	}
	info, err := os.Stat(realPath)
	if err != nil || !info.Mode().IsRegular() {
		return "", infra.WithCause(errors.New("resource is not a regular file"), err)
	}
	return realPath, nil
}
