//go:build native_kernels && amd64

package modelviewer

import (
	"bytes"
	"encoding/binary"
	"math"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"syscall"
	"testing"

	"golang.org/x/sys/cpu"

	"nahida.live/desktop/internal/appdata"
	"nahida.live/desktop/internal/nativecompute"
)

func TestPackedSkinningBundledKernelMatchesGo(t *testing.T) {
	t.Parallel()
	const cacheEnv = "NAHIDA_TEST_PACKED_SKINNING_CACHE"
	cache := os.Getenv(cacheEnv)
	if cache == "" {
		// Windows keeps the DLL file locked until the subprocess exits.
		exe, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.CommandContext(t.Context(), exe, "-test.run=^TestPackedSkinningBundledKernelMatchesGo$")
		cmd.Env = append(cmd.Environ(), cacheEnv+"="+t.TempDir())
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("packed skinning subprocess: %v\n%s", err, output)
		}
		return
	}
	store, err := appdata.Open(cache)
	if err != nil {
		t.Fatal(err)
	}

	service := New()
	service.UseAppData(store)
	service.UseNativeCompute(nativecompute.New(store, nil))
	available := cpu.X86.HasAVX && cpu.X86.HasF16C
	procs := service.packedSkinning()
	if (procs.legacy != 0) != available || (procs.variants != 0) != available {
		t.Fatalf(
			"loaded legacy = %t, loaded variants = %t, want both %t",
			procs.legacy != 0,
			procs.variants != 0,
			available,
		)
	}
	session := service.protocol.CreateMemorySession()
	t.Cleanup(func() { service.protocol.CleanupMemorySession(session) })
	source := func(t *testing.T, id string, stride int, data []byte) ModelViewerComputeBinarySource {
		t.Helper()
		url, storeErr := service.protocol.StoreMemoryBuffer(session, id, data, "application/octet-stream")
		if storeErr != nil {
			t.Fatal(storeErr)
		}
		return ModelViewerComputeBinarySource{URL: url, Stride: stride, ByteLength: int64(len(data))}
	}
	blend := func(weights [4]float32, bones [4]uint32) []byte {
		record := make([]byte, computeBlendStride)
		for i := range 4 {
			binary.LittleEndian.PutUint32(record[i*4:], math.Float32bits(weights[i]))
			binary.LittleEndian.PutUint32(record[16+i*4:], bones[i])
		}
		return record
	}
	errorText := func(err error) string {
		if err == nil {
			return ""
		}
		return err.Error()
	}

	half := float32(math.Sqrt(0.5))
	identity := []float32{1, 1, 1, 0, 0, 0, 0, 0, 0, 1, 0, 0, 0, 0}
	turned := []float32{2, 1, 1, 1, -1, 0, 0, -half, 0, -half, 0, 0, 0, 0}
	orthogonal := []float32{1, 2, 1, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0}
	single := blend([4]float32{1}, [4]uint32{})
	tests := []struct {
		name      string
		vertices  int
		bones     int
		blend     []byte
		poses     [][]float32
		wantError string
		directOK  bool
	}{
		{
			name: "one bone", vertices: 1, bones: 1, blend: single,
			poses: [][]float32{identity, turned},
		},
		{
			name: "two bones with translation", vertices: 2, bones: 2,
			blend: slices.Concat(single, blend([4]float32{0.25, 0.75}, [4]uint32{1, 0, 1, 1})),
			poses: [][]float32{
				identity, {1, 1, 1, 0.5, 0, 0, half, 0, 0, half, 0.1, 0.2, 0, 0},
				turned, {1, 2, 1, 0, 0, 0.5, 0, 0, half, half, 0, 0.1, 0.3, 0},
			},
		},
		{
			name: "orthogonal second influence", vertices: 1, bones: 2,
			blend: blend([4]float32{0.5, 0.5}, [4]uint32{0, 1, 0, 0}),
			poses: [][]float32{identity, orthogonal, identity, orthogonal},
		},
		{
			name: "largest finite float16", vertices: 1, bones: 1, blend: single,
			poses: [][]float32{
				{65504, 1, 1, 0, 0, 0, 0, 0, 0, 1, 0, 0, 0, 0},
				{65504, 1, 1, 0, 0, 0, 0, 0, 0, 1, 0, 0, 0, 0},
			},
		},
		{
			name: "float16 overflow", vertices: 1, bones: 1, blend: single,
			poses: [][]float32{
				{65520, 1, 1, 0, 0, 0, 0, 0, 0, 1, 0, 0, 0, 0},
				{65520, 1, 1, 0, 0, 0, 0, 0, 0, 1, 0, 0, 0, 0},
			},
			wantError: "packed dual-quaternion position exceeds finite float16 range",
		},
		{
			name: "degenerate pose at vertex 1", vertices: 2, bones: 2,
			blend:     slices.Concat(single, blend([4]float32{1}, [4]uint32{1, 1, 1, 1})),
			poses:     [][]float32{identity, make([]float32, 14), identity, make([]float32, 14)},
			wantError: "invalid packed dual-quaternion pose at vertex 1",
			directOK:  true,
		},
		{
			name: "non-finite pose at zero weight", vertices: 1, bones: 1, blend: single,
			poses: [][]float32{
				identity, {1, 1, 1, 0, 0, 0, float32(math.Inf(1)), 0, 0, 1, 0, 0, 0, 0},
			},
			wantError: "invalid packed dual-quaternion pose at vertex 0",
		},
	}
	variants := []struct {
		pose   ModelViewerDualQuaternionVariant
		native packedSkinningVariant
	}{
		{"", packedSkinningLegacy},
		{modelViewerObjectPoseVariant, packedSkinningObject},
		{modelViewerObjectDirectPoseVariant, packedSkinningObjectDirect},
	}
	for index, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			id := strconv.Itoa(index)
			descriptor := ModelViewerComputeDeformerTransport{
				Kind: modelViewerPackedDualQuaternionKind, ID: "packed-" + id, VertexCount: tt.vertices,
				Base: source(t,
					"base-"+id, computePackedVertexStride, bytes.Repeat(packedHalfVertex(), tt.vertices),
				),
				Pose: &ModelViewerComputePoseSource{
					Blend: source(t, "blend-"+id, computeBlendStride, tt.blend),
					Frames: source(t,
						"pose-"+id, computeDualQuaternionSize, modelViewerFloat32Bytes(slices.Concat(tt.poses...)),
					),
					BoneCount: tt.bones, FrameCount: 2,
				},
			}
			for _, variant := range variants {
				name := string(variant.pose)
				if name == "" {
					name = "legacy"
				}
				t.Run(name, func(t *testing.T) {
					descriptor.Pose.DualQuaternionVariant = variant.pose
					wantError := tt.wantError
					if variant.pose == modelViewerObjectDirectPoseVariant && tt.directOK {
						wantError = ""
					}
					var calls []packedSkinningVariant
					observed := packedSkinningProcs{}
					if available {
						observed.legacy = syscall.NewCallback(func(args uintptr) uintptr {
							calls = append(calls, packedSkinningLegacy)
							result, _, _ := syscall.SyscallN(procs.legacy, args)
							return result
						})
						observed.variants = syscall.NewCallback(func(args, variant uintptr) uintptr {
							calls = append(calls, packedSkinningVariant(variant))
							result, _, _ := syscall.SyscallN(procs.variants, args, variant)
							return result
						})
					}
					modelService := NewWithOptions(Options{Protocol: service.protocol})
					modelService.packedSkinning = func() packedSkinningProcs { return observed }
					got, err := modelService.NewComputeDeformer(t.Context(), descriptor)
					if err != nil {
						t.Fatal(err)
					}
					want, err := NewComputeDeformer(t.Context(), descriptor, service.ReadResource)
					if err != nil {
						t.Fatal(err)
					}

					for _, frame := range []float64{0, 0.25, 0.5, 1, 2} {
						calls = calls[:0]
						gotFrame, gotErr := got.compute(frame, 0, nil)
						if available && (len(calls) != 1 || calls[0] != variant.native) {
							t.Fatalf(
								"frame %g: native calls = %v, want one call for variant %d",
								frame,
								calls,
								variant.native,
							)
						}
						wantFrame, wantErr := want.compute(frame, 0, nil)
						if errorText(wantErr) != wantError {
							t.Fatalf("frame %g: Go error = %v, want %q", frame, wantErr, wantError)
						}
						if errorText(gotErr) != errorText(wantErr) {
							t.Fatalf("frame %g: Rust error = %v, want %v", frame, gotErr, wantErr)
						}
						if wantErr != nil {
							continue
						}
						for _, stream := range []struct {
							name      string
							got, want []float32
						}{
							{"positions", gotFrame.Positions, wantFrame.Positions},
							{"normals", gotFrame.Normals, wantFrame.Normals},
							{"tangents", gotFrame.Tangents, wantFrame.Tangents},
						} {
							if len(stream.got) != len(stream.want) {
								t.Fatalf("frame %g %s length = %d, want %d",
									frame, stream.name, len(stream.got), len(stream.want))
							}
							for i, value := range stream.want {
								if math.Float32bits(stream.got[i]) != math.Float32bits(value) {
									t.Fatalf("frame %g %s[%d] = %08x, want %08x",
										frame, stream.name, i, math.Float32bits(stream.got[i]), math.Float32bits(value))
								}
							}
						}
					}
				})
			}
		})
	}
}
