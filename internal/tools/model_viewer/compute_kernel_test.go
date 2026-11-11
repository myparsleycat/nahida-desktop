package modelviewer

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"strings"
	"testing"
)

// computeFixture serves the sources of a deformer from memory.
type computeFixture map[string][]byte

func (f computeFixture) source(url string, stride int, data []byte) ModelViewerComputeBinarySource {
	f[url] = data
	return ModelViewerComputeBinarySource{URL: url, ByteLength: int64(len(data)), Stride: stride}
}

func (f computeFixture) frame(
	t *testing.T,
	deformer ModelViewerComputeDeformerTransport,
	poseFrame int,
	phaseSeconds float64,
	groupWeights []float64,
) ComputeFrame {
	t.Helper()
	kernel, err := f.bind(deformer)
	if err != nil {
		t.Fatalf("bind: %v", err)
	}
	frame, err := kernel.Frame(poseFrame, phaseSeconds, groupWeights)
	if err != nil {
		t.Fatalf("frame: %v", err)
	}
	return frame
}

func (f computeFixture) bind(deformer ModelViewerComputeDeformerTransport) (*ComputeDeformer, error) {
	return NewComputeDeformer(context.Background(), deformer, func(_ context.Context, url string) ([]byte, error) {
		data, ok := f[url]
		if !ok {
			return nil, fmt.Errorf("no source %s", url)
		}
		return data, nil
	})
}

// oneBoneBlend is the blend record of a vertex bound fully to bone 0.
func oneBoneBlend() []byte {
	blend := make([]byte, computeBlendStride)
	binary.LittleEndian.PutUint32(blend, math.Float32bits(1))
	return blend
}

// packedHalfVertex is a 20-byte packed vertex with a w of 4, which the kernels must ignore, and a normal
// along +Z.
func packedHalfVertex() []byte {
	vertex := make([]byte, computePackedVertexStride)
	for axis, half := range []uint16{0x3c00, 0x4000, 0x4200, 0x4400} { // 1, 2, 3, 4
		binary.LittleEndian.PutUint16(vertex[axis*2:], half)
	}
	vertex[10] = 127
	return vertex
}

func expectComputeVector(t *testing.T, name string, got []float32, want ...float64) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s = %v, want %v", name, got, want)
	}
	for i := range want {
		if math.Abs(float64(got[i])-want[i]) > 1e-3 {
			t.Fatalf("%s = %v, want %v", name, got, want)
		}
	}
}

func TestComputeShapePoseKernel(t *testing.T) {
	t.Parallel()
	vertex := func(position [3]float32, normal [3]float32) []byte {
		return modelViewerFloat32Bytes(append(append(position[:], normal[:]...), 1, 0, 0, 1))
	}
	posed := func(fixture computeFixture, base []byte, pose ...float32) ModelViewerComputeDeformerTransport {
		return ModelViewerComputeDeformerTransport{
			Kind: modelViewerGIMIShapePoseKind, ID: "cloth", VertexCount: 1,
			Base: fixture.source("/base", 40, base),
			Pose: &ModelViewerComputePoseSource{
				Blend:     fixture.source("/blend", 32, oneBoneBlend()),
				Frames:    fixture.source("/pose", 56, modelViewerFloat32Bytes(pose)),
				BoneCount: 1, FrameCount: 1,
			},
		}
	}
	up := [3]float32{0, 1, 0}

	t.Run("shape deltas add up in pass order", func(t *testing.T) {
		t.Parallel()
		fixture := computeFixture{}
		pass := func(url string, x float32) ModelViewerComputeShapePass {
			return ModelViewerComputeShapePass{
				Target:    fixture.source(url, 40, vertex([3]float32{x, 0, 0}, up)),
				PhaseRate: 1, AngularScale: 1, Bias: 0.5,
			}
		}
		frame := fixture.frame(t, ModelViewerComputeDeformerTransport{
			Kind: modelViewerGIMIShapePoseKind, ID: "cloth", VertexCount: 1,
			Base:        fixture.source("/base", 40, vertex([3]float32{1, 0, 0}, up)),
			ShapePasses: []ModelViewerComputeShapePass{pass("/a", 3), pass("/b", 5)},
		}, 0, 0, nil)
		expectComputeVector(t, "positions", frame.Positions, 4, 0, 0)
	})

	t.Run("a pose scales, offsets and translates", func(t *testing.T) {
		t.Parallel()
		fixture := computeFixture{}
		deformer := posed(fixture, vertex([3]float32{1, 1, 1}, up), 2, 3, 4, 1, 1, 1, 0, 0, 0, 1, 1, 0, 0, 0)
		frame := fixture.frame(t, deformer, 0, 0, nil)
		expectComputeVector(t, "positions", frame.Positions, 5, 4, 5)
		expectComputeVector(t, "normals", frame.Normals, 0, 1, 0)
	})

	t.Run("a pose turns positions, normals and tangents", func(t *testing.T) {
		t.Parallel()
		fixture := computeFixture{}
		half := float32(math.Sqrt2 / 2)
		deformer := posed(
			fixture, vertex([3]float32{1, 0, 0}, [3]float32{1, 0, 0}), 1, 1, 1, 0, 0, 0, 0, 0, half, half, 0, 0, 0, 0,
		)
		frame := fixture.frame(t, deformer, 0, 0, nil)
		expectComputeVector(t, "positions", frame.Positions, 0, 1, 0)
		expectComputeVector(t, "normals", frame.Normals, 0, 1, 0)
		expectComputeVector(t, "tangents", frame.Tangents, 0, 1, 0, 1)
	})
}

func TestComputePackedDualQuaternionKernel(t *testing.T) {
	t.Parallel()
	identity := []float32{1, 1, 1, 0, 0, 0, 0, 0, 0, 1, 0, 0, 0, 0}
	moved := []float32{2, 3, 4, 5, 6, 7, 0, 0, 0, 1, 1, 2, 3, 0}
	deformer := func(
		fixture computeFixture,
		variant ModelViewerDualQuaternionVariant,
		pose []float32,
	) ModelViewerComputeDeformerTransport {
		return ModelViewerComputeDeformerTransport{
			Kind: modelViewerPackedDualQuaternionKind, ID: "dq", VertexCount: 1,
			Base: fixture.source("/base", 20, packedHalfVertex()),
			Pose: &ModelViewerComputePoseSource{
				DualQuaternionVariant: variant,
				Blend:                 fixture.source("/blend", 32, oneBoneBlend()),
				Frames:                fixture.source("/pose", 56, modelViewerFloat32Bytes(append(pose, pose...))),
				BoneCount:             1, FrameCount: 2,
			},
		}
	}

	t.Run("the legacy shader keeps game coordinates and ignores the packed w", func(t *testing.T) {
		t.Parallel()
		fixture := computeFixture{}
		frame := fixture.frame(t, deformer(fixture, "", identity), 0, 0, nil)
		expectComputeVector(t, "positions", frame.Positions, 1, 2, 3)
		expectComputeVector(t, "normals", frame.Normals, 0, 0, 1)
		if frame.Tangents != nil {
			t.Errorf("tangents = %v, want none", frame.Tangents)
		}
	})

	t.Run("the legacy shader poses in Blender axes", func(t *testing.T) {
		t.Parallel()
		fixture := computeFixture{}
		// Blender input (1,-3,2) -> scale and bias (7,-3,15) -> translation (9,1,21).
		frame := fixture.frame(t, deformer(fixture, "", moved), 0, 0, nil)
		expectComputeVector(t, "positions", frame.Positions, 9, 21, -1)
	})

	t.Run("the direct object shader keeps position axes and converts normals", func(t *testing.T) {
		t.Parallel()
		fixture := computeFixture{}
		frame := fixture.frame(t, deformer(fixture, modelViewerObjectDirectPoseVariant, moved), 0, 0, nil)
		expectComputeVector(t, "positions", frame.Positions, 9, 16, 25)
		expectComputeVector(t, "normals", frame.Normals, 0, -1, 0)
	})

	t.Run("the EFMI shader poses its own axes after the shape passes", func(t *testing.T) {
		t.Parallel()
		fixture := computeFixture{}
		efmi := deformer(fixture, modelViewerEFMIArmatureVariant, moved)
		efmi.Base = fixture.source("/base", 28, modelViewerFloat32Bytes([]float32{1, 2, 3, 1, 0, 0, 1}))
		efmi.Base.Encoding = modelViewerPackedFloatEncoding
		// (1,3,-2) -> scale and bias (7,15,-1) -> translation (9,19,5).
		frame := fixture.frame(t, efmi, 0, 0, nil)
		expectComputeVector(t, "positions", frame.Positions, 9, -5, 19)
		expectComputeVector(t, "normals", frame.Normals, 0, 0, 1)

		target := fixture.source("/target", 28, modelViewerFloat32Bytes([]float32{2, 2, 3, 1, 0, 0, 1}))
		target.Encoding = modelViewerPackedFloatEncoding
		efmi.ShapePasses = []ModelViewerComputeShapePass{{
			Target: target, PhaseRate: 1, PhaseOffset: math.Pi / 2, AngularScale: 1, Amplitude: 0.5, Bias: 0.5,
		}}
		frame = fixture.frame(t, efmi, 0, 0, nil)
		expectComputeVector(t, "morphed positions", frame.Positions, 11, -5, 19)
	})

	t.Run("a legacy deformer with shape passes is rejected", func(t *testing.T) {
		t.Parallel()
		fixture := computeFixture{}
		legacy := deformer(fixture, "", identity)
		legacy.ShapePasses = []ModelViewerComputeShapePass{{Target: legacy.Base}}
		if _, err := fixture.bind(legacy); err == nil {
			t.Fatal("bind succeeded")
		}
	})
}

func TestComputeCyclicPackedKernel(t *testing.T) {
	t.Parallel()
	fixture := computeFixture{}
	pose := []float32{1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0}
	for range 12 {
		pose = append(pose, float32(math.NaN()))
	}
	// The second pose frame has no weight at frame 0, so its NaN must not reach the vertex.
	frame := fixture.frame(t, ModelViewerComputeDeformerTransport{
		Kind: modelViewerPackedObjectKind, ID: "closet", VertexCount: 1,
		Base: fixture.source("/base", 20, packedHalfVertex()),
		Pose: &ModelViewerComputePoseSource{
			Blend:     fixture.source("/blend", 32, oneBoneBlend()),
			Frames:    fixture.source("/pose", 48, modelViewerFloat32Bytes(pose)),
			BoneCount: 1, FrameCount: 2,
		},
	}, 0, 0, nil)
	expectComputeVector(t, "positions", frame.Positions, 1, 2, 3)
	expectComputeVector(t, "normals", frame.Normals, 0, 0, 1)
}

func TestComputePackedShapeKernel(t *testing.T) {
	t.Parallel()
	fixture := computeFixture{}
	vertex := func(url string, values ...float32) ModelViewerComputeBinarySource {
		source := fixture.source(url, 28, modelViewerFloat32Bytes(values))
		source.Encoding = modelViewerPackedFloatEncoding
		return source
	}
	stage := func(name string, target ...float32) ModelViewerComputeShapeStage {
		return ModelViewerComputeShapeStage{
			Base:      vertex("/base"+name, 0, 0, 0, 1, 127, 0, 0),
			Target:    vertex("/target"+name, target...),
			PhaseRate: 1, AngularScale: 1, Amplitude: 0.5, Bias: 0.5, Duration: 1,
		}
	}
	deformer := ModelViewerComputeDeformerTransport{
		Kind: modelViewerPackedShapeKind, ID: "kimono", VertexCount: 1,
		ShapeStages: []ModelViewerComputeShapeStage{
			stage("1", 10, 0, 0, 1, 0, 127, 0), stage("2", 0, 10, 0, 1, 0, 127, 0),
		},
	}

	// Each stage rests at half its weight; the second takes over after the first one's duration.
	frame := fixture.frame(t, deformer, 0, 0, nil)
	expectComputeVector(t, "first stage positions", frame.Positions, 5, 0, 0)
	expectComputeVector(t, "first stage normals", frame.Normals, math.Sqrt2/2, math.Sqrt2/2, 0)
	frame = fixture.frame(t, deformer, 0, 1, nil)
	expectComputeVector(t, "second stage positions", frame.Positions, 0, 5, 0)
}

func TestComputeMergedDeltaKernel(t *testing.T) {
	t.Parallel()
	base := modelViewerFloat32Bytes([]float32{
		1, 1, 1, 0, 1, 0, 1, 0, 0, 1,
		2, 2, 2, 0, 1, 0, 1, 0, 0, 1,
	})
	// Three keys of one record each: vertex 0 +X, vertex 1 +Y, vertex 1 +4Z.
	deltas := modelViewerUint32Bytes([]uint32{0, 1, 1, 1, 2, 1})
	for _, record := range [][4]float32{{0, 1, 0, 0}, {1, 0, 1, 0}, {1, 0, 0, 4}} {
		deltas = append(deltas, modelViewerUint32Bytes([]uint32{uint32(record[0])})...)
		deltas = append(deltas, modelViewerFloat32Bytes(record[1:])...)
	}
	deformer := func(fixture computeFixture, deltas []byte) ModelViewerComputeDeformerTransport {
		fixture["/deltas"] = deltas
		return ModelViewerComputeDeformerTransport{
			Kind: modelViewerMergedDeltaKind, ID: "deformer", VertexCount: 2,
			Base: fixture.source("/base", 40, base),
			MergedDeltas: &ModelViewerComputeMergedDeltas{
				URL: "/deltas",
				Keys: []ModelViewerComputeDeltaKey{
					{Group: 0, Scale: 2}, {Group: 0, Scale: 2, Offset: 1}, {Group: -1, Weight: 0.5},
				},
			},
		}
	}

	t.Run("each key adds its deltas at the weight its group gives it", func(t *testing.T) {
		t.Parallel()
		fixture := computeFixture{}
		kernel, err := fixture.bind(deformer(fixture, deltas))
		if err != nil {
			t.Fatal(err)
		}
		frame, _ := kernel.Frame(0, 0, []float64{0.25})
		expectComputeVector(t, "quarter positions", frame.Positions, 1.5, 1, 1, 2, 2, 4)
		frame, _ = kernel.Frame(0, 0, []float64{1})
		expectComputeVector(t, "full positions", frame.Positions, 2, 1, 1, 2, 3, 4)
		expectComputeVector(t, "normals", frame.Normals, 0, 1, 0, 0, 1, 0)
		if len(frame.Tangents) != 8 {
			t.Errorf("tangents = %v, want 8 values", frame.Tangents)
		}
	})

	t.Run("a delta buffer that does not match its key table is rejected", func(t *testing.T) {
		t.Parallel()
		fixture := computeFixture{}
		_, err := fixture.bind(deformer(fixture, make([]byte, 24+8)))
		if err == nil || !strings.Contains(err.Error(), "key table") {
			t.Fatalf("bind error = %v, want a key table mismatch", err)
		}
	})
}

func TestComputePlayback(t *testing.T) {
	t.Parallel()
	const tick = 1.0 / 60
	group := ModelViewerComputeShapeGroup{
		AutoPlayVar: "play", SpeedVar: "speed", WeightVar: "weight",
		Steps:     []ModelViewerComputeShapeStep{{Until: 0.5, Step: 1}, {Until: 1, Step: 2}},
		StepLimit: 100, FrameEnd: 10,
	}
	merged := func(group ModelViewerComputeShapeGroup) *ComputePlayback {
		return NewComputePlayback(ModelViewerComputeDeformerTransport{
			MergedDeltas: &ModelViewerComputeMergedDeltas{Groups: []ModelViewerComputeShapeGroup{group}},
		})
	}
	playing := map[string]any{"play": 1.0, "speed": 100.0, "weight": 0.8}
	paused := func(weight float64) map[string]any {
		return map[string]any{"play": 0.0, "speed": 100.0, "weight": weight}
	}
	tenths := func(playback *ComputePlayback, frames int) []int {
		var weights []int
		for frame := range frames {
			_, groups := playback.Advance("clip", float64(frame)*tick, 10, playing)
			weights = append(weights, int(math.Round(groups[0]*10)))
		}
		return weights
	}

	t.Run("a group steps by the range its weight is in and runs back from the end", func(t *testing.T) {
		t.Parallel()
		got, want := tenths(merged(group), 10), []int{0, 1, 2, 3, 4, 5, 7, 9, 10, 9}
		if fmt.Sprint(got) != fmt.Sprint(want) {
			t.Errorf("weights = %v, want %v", got, want)
		}
	})

	t.Run("a wrapping group restarts from zero past the end", func(t *testing.T) {
		t.Parallel()
		wrapping := group
		wrapping.Wrap, wrapping.Steps = true, nil
		got, want := tenths(merged(wrapping), 13), []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 0, 1}
		if fmt.Sprint(got) != fmt.Sprint(want) {
			t.Errorf("weights = %v, want %v", got, want)
		}
	})

	t.Run("a group keeps running when the clip wraps", func(t *testing.T) {
		t.Parallel()
		playback := merged(group)
		playback.Advance("clip", 1, 1, playing)
		_, wrapped := playback.Advance("clip", 0, 1, playing)
		_, next := playback.Advance("clip", tick, 1, playing)
		if math.Abs(wrapped[0]) > 1e-9 || math.Abs(next[0]-0.1) > 1e-9 {
			t.Errorf("weights = %v then %v, want 0 then 0.1", wrapped[0], next[0])
		}
	})

	t.Run("a paused group holds its pose until the slider sets another weight", func(t *testing.T) {
		t.Parallel()
		playback := merged(group)
		playback.Advance("clip", 0, 10, playing)
		playback.Advance("clip", tick, 10, playing)
		_, held := playback.Advance("clip", 2*tick, 10, paused(0.8))
		_, set := playback.Advance("clip", 3*tick, 10, paused(0.3))
		if math.Abs(held[0]-0.1) > 1e-9 || math.Abs(set[0]-0.3) > 1e-9 {
			t.Errorf("weights = %v then %v, want 0.1 then 0.3", held[0], set[0])
		}
	})

	t.Run("EFMI shape passes keep their phase when the pose clip restarts", func(t *testing.T) {
		t.Parallel()
		playback := NewComputePlayback(ModelViewerComputeDeformerTransport{
			Pose: &ModelViewerComputePoseSource{DualQuaternionVariant: modelViewerEFMIArmatureVariant},
		})
		playback.Advance("clip", 1.9, 2, nil)
		restarted, _ := playback.Advance("clip", 0.1, 2, nil)
		// A step back of under half the clip is a seek, which moves the phase with it.
		sought, _ := playback.Advance("clip", 0, 2, nil)
		if math.Abs(restarted-2.1) > 1e-9 || math.Abs(sought-2) > 1e-9 {
			t.Errorf("phase = %v then %v, want 2.1 then 2", restarted, sought)
		}
	})
}
