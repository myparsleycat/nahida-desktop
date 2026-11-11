package sculpt

import (
	"encoding/binary"
	"math"
	"testing"

	"nahida.live/desktop/internal/tools/modmesh"
)

func TestBlendLayouts(t *testing.T) {
	tests := []struct {
		name   string
		stride int
		// write fills three vertices: v0 is bone a only, v1 shares a and b with
		// a heavier, v2 is bone b only.
		write  func(data []byte)
		a, b   uint32
		v1     [2]float64
		ranked uint32
	}{
		{
			name:   "WWMI padded",
			stride: 16,
			write: func(data []byte) {
				data[0], data[8] = 5, 255
				data[16], data[17], data[24], data[25] = 5, 7, 128, 127
				data[32], data[40] = 7, 255
			},
			a: 5, b: 7, v1: [2]float64{128.0 / 255, 127.0 / 255}, ranked: 7,
		},
		{
			name:   "WWMI compact",
			stride: 8,
			write: func(data []byte) {
				data[0], data[4] = 3, 255
				data[8], data[9], data[12], data[13] = 3, 7, 128, 127
				data[16], data[20] = 7, 255
			},
			a: 3, b: 7, v1: [2]float64{128.0 / 255, 127.0 / 255}, ranked: 7,
		},
		{
			name:   "MiHoYo",
			stride: 32,
			write: func(data []byte) {
				putFloat := func(offset int, value float32) {
					binary.LittleEndian.PutUint32(data[offset:], math.Float32bits(value))
				}
				putFloat(0, 1)
				binary.LittleEndian.PutUint32(data[16:], 7)
				putFloat(32, 0.75)
				putFloat(36, 0.25)
				binary.LittleEndian.PutUint32(data[48:], 7)
				binary.LittleEndian.PutUint32(data[52:], 12)
				putFloat(64, 1)
				binary.LittleEndian.PutUint32(data[80:], 12)
			},
			a: 7, b: 12, v1: [2]float64{0.75, 0.25}, ranked: 12,
		},
		{
			name:   "EFMI",
			stride: 12,
			write: func(data []byte) {
				binary.LittleEndian.PutUint16(data[0:], 65535)
				data[8] = 3
				binary.LittleEndian.PutUint16(data[12:], 32768)
				binary.LittleEndian.PutUint16(data[14:], 32767)
				data[20], data[21] = 3, 7
				binary.LittleEndian.PutUint16(data[24:], 65535)
				data[32] = 7
			},
			a: 3, b: 7, v1: [2]float64{32768.0 / 65535, 32767.0 / 65535}, ranked: 7,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			data := make([]byte, 3*test.stride)
			test.write(data)

			bones := modmesh.ListBlendBones(data, 3, test.stride)
			want := []modmesh.BlendBoneInfo{{ID: test.a, VertexCount: 2}, {ID: test.b, VertexCount: 2}}
			assertEqual(t, "bones", bones, want)

			assertWeights(t, ExtractBoneWeights(data, test.a, 3, test.stride), []float64{1, test.v1[0], 0})
			assertWeights(t, ExtractBoneWeights(data, test.b, 3, test.stride), []float64{0, test.v1[1], 1})

			influences := InfluencesAtVertex(data, 1, test.stride)
			if len(influences) != 2 || influences[0].BoneID != test.a || influences[1].BoneID != test.b {
				t.Fatalf("influences = %+v", influences)
			}
			if ranked := RankBonesAtVertices(data, []uint32{1, 2}, test.stride); ranked[0].BoneID != test.ranked {
				t.Fatalf("ranked = %+v, want bone %d first", ranked, test.ranked)
			}
		})
	}
}

func TestBlendRigidLayout(t *testing.T) {
	data := make([]byte, 12)
	binary.LittleEndian.PutUint32(data[0:], 3)
	binary.LittleEndian.PutUint32(data[4:], 7)
	binary.LittleEndian.PutUint32(data[8:], 3)

	assertWeights(t, ExtractBoneWeights(data, 3, 3, 4), []float64{1, 0, 1})
	influences := InfluencesAtVertex(data, 1, 4)
	if len(influences) != 1 || influences[0] != (BoneInfluence{BoneID: 7, Weight: 1}) {
		t.Fatalf("influences = %+v", influences)
	}
}

func TestExtractBoneWeightsIgnoresUnsupportedStride(t *testing.T) {
	data := make([]byte, 24)
	data[8] = 255
	assertWeights(t, ExtractBoneWeights(data, 0, 4, 6), []float64{0, 0, 0, 0})
}

func assertWeights(t *testing.T, got []float32, want []float64) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("weights = %v, want %v", got, want)
	}
	for i := range got {
		if math.Abs(float64(got[i])-want[i]) > 1e-6 {
			t.Fatalf("weights = %v, want %v", got, want)
		}
	}
}
