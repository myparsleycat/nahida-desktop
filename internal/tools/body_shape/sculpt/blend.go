package sculpt

import (
	"cmp"
	"encoding/binary"
	"math"
	"slices"
)

// Blend buffer layouts by stride:
//   - 4: EFMI rigid, one u32 bone index with an implied weight of 1.
//   - 8: WWMI compact, 4×u8 bone indices at 0 and 4×u8 weights at 4.
//   - 12: EFMI, 4×u16 UNORM weights at 0 and 4×u8 bone indices at 8.
//   - 32: MiHoYo, 4×float32 weights at 0 and 4×u32 bone indices at 16.
//   - 16 or more: WWMI padded, 4×u8 bone indices at 0 and 4×u8 weights at 8.
//
// modmesh.ListBlendBones and modmesh.ValidateBlendBuffer cover bone listing and
// validation; this file adds the per-vertex reads the editor needs, with
// float64 weights so bone ranking matches the original implementation.

const blendInfluenceCount = 4

// BoneInfluence is one bone's weight on a vertex.
type BoneInfluence struct {
	BoneID uint32
	Weight float64
}

// BoneScore is a bone's summed weight over a set of vertices.
type BoneScore struct {
	BoneID uint32
	Score  float64
}

func supportedBlendStride(stride int) bool {
	return stride == 4 || stride == 8 || stride == 12 || stride == 32 || stride >= 16
}

func forEachInfluence(data []byte, base, stride int, visit func(boneID uint32, weight float64)) {
	if !supportedBlendStride(stride) || base < 0 {
		return
	}

	switch stride {
	case 4:
		if base+4 > len(data) {
			return
		}
		visit(binary.LittleEndian.Uint32(data[base:]), 1)
	case 32:
		if base+stride > len(data) {
			return
		}
		for k := range blendInfluenceCount {
			weight := float64(math.Float32frombits(binary.LittleEndian.Uint32(data[base+k*4:])))
			if !isFinite(weight) || weight <= 0 {
				continue
			}
			visit(binary.LittleEndian.Uint32(data[base+16+k*4:]), weight)
		}
	case 12:
		if base+stride > len(data) {
			return
		}
		for k := range blendInfluenceCount {
			weight := float64(binary.LittleEndian.Uint16(data[base+k*2:])) / 65535
			if weight <= 0 {
				continue
			}
			visit(uint32(data[base+8+k]), weight)
		}
	default:
		weightsOffset := 8
		if stride == 8 {
			weightsOffset = 4
		}
		if base+weightsOffset+blendInfluenceCount > len(data) {
			return
		}
		for k := range blendInfluenceCount {
			weight := float64(data[base+weightsOffset+k]) / 255
			if weight <= 0 {
				continue
			}
			visit(uint32(data[base+k]), weight)
		}
	}
}

// ExtractBoneWeights returns each vertex's weight for one bone, zero where the
// bone has no influence or the stride is unsupported.
func ExtractBoneWeights(data []byte, boneID uint32, vertexCount, stride int) []float32 {
	weights := make([]float32, max(0, vertexCount))
	if stride <= 0 {
		return weights
	}

	limit := min(vertexCount, len(data)/stride)
	for i := range limit {
		w := 0.0
		forEachInfluence(data, i*stride, stride, func(id uint32, weight float64) {
			if id == boneID && weight > w {
				w = weight
			}
		})
		weights[i] = float32(w)
	}
	return weights
}

// InfluencesAtVertex lists a vertex's bone influences, heaviest first.
func InfluencesAtVertex(data []byte, vertex, stride int) []BoneInfluence {
	base := vertex * stride
	if stride <= 0 || base+stride > len(data) {
		return nil
	}

	var influences []BoneInfluence
	forEachInfluence(data, base, stride, func(boneID uint32, weight float64) {
		influences = append(influences, BoneInfluence{BoneID: boneID, Weight: weight})
	})
	slices.SortStableFunc(influences, func(a, b BoneInfluence) int { return cmp.Compare(b.Weight, a.Weight) })
	return influences
}

// RankBonesAtVertices sums each bone's weight over the vertices, highest first;
// it picks the bone under a mesh click.
func RankBonesAtVertices(data []byte, vertices []uint32, stride int) []BoneScore {
	var scores []BoneScore
	index := make(map[uint32]int)
	for _, vertex := range vertices {
		for _, influence := range InfluencesAtVertex(data, int(vertex), stride) {
			position, ok := index[influence.BoneID]
			if !ok {
				position = len(scores)
				index[influence.BoneID] = position
				scores = append(scores, BoneScore{BoneID: influence.BoneID})
			}
			scores[position].Score += influence.Weight
		}
	}
	slices.SortStableFunc(scores, func(a, b BoneScore) int { return cmp.Compare(b.Score, a.Score) })
	return scores
}
