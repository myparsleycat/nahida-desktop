package tools

import (
	"context"
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestModelViewerComputeSourcesCompactUnionAndDecodePacked(t *testing.T) {
	service := New()
	session := service.protocol.CreateMemorySession()
	path := filepath.Join(t.TempDir(), "packed.buf")
	raw := make([]byte, 4*20)
	for vertex := range 4 {
		binary.LittleEndian.PutUint16(raw[vertex*20:], uint16(0x3c00+vertex*0x400)) // 1,2,4,8
		binary.LittleEndian.PutUint16(raw[vertex*20+2:], 0x0001)                    // smallest half subnormal
		binary.LittleEndian.PutUint16(raw[vertex*20+4:], 0x8000)                    // negative zero
		binary.LittleEndian.PutUint16(raw[vertex*20+6:], 0x3c00)
		raw[vertex*20+8], raw[vertex*20+9], raw[vertex*20+10] = 128, 127, 255
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	base := ModelViewerComputeBinarySource{sourcePath: path, Stride: 20, ByteLength: 80}
	transport := ModelViewerTransport{Meshes: []ModelViewerMeshTransport{{ID: "a"}, {ID: "b"}}, ComputeDeformers: []ModelViewerComputeDeformerTransport{{Kind: "gimi_cyclic_packed_shape_v1", ID: "shape", MeshIDs: []string{"a", "b"}, VertexCount: 4, Base: base, ShapeStages: []ModelViewerComputeShapeStage{{Base: base, Target: base}}}}}
	meshes := []modelViewerMeshPayload{{Positions: make([]float32, 6), SourceIndices: []uint32{3, 1}}, {Positions: make([]float32, 3), SourceIndices: []uint32{1}}}
	cache := &modelViewerPositionCache{limit: 1024}
	if err := service.prepareModelViewerComputeSources(context.Background(), session, &transport, meshes, cache); err != nil {
		t.Fatal(err)
	}
	d := transport.ComputeDeformers[0]
	if d.VertexCount != 2 || d.Base.Stride != 28 || d.Base.ByteLength != 56 || d.Base.Encoding != modelViewerPackedFloatEncoding {
		t.Fatalf("deformer=%+v", d)
	}
	if cache.bytes != 0 {
		t.Fatal("compute records decoded eagerly")
	}
	if d.ShapeStages[0].Base.URL != d.Base.URL || d.ShapeStages[0].Target.URL != d.Base.URL {
		t.Fatal("shared sources were duplicated")
	}
	a := readModelViewerProtocolBytes(t, service.protocol, d.MeshSourceIndices["a"])
	if binary.LittleEndian.Uint32(a) != 1 || binary.LittleEndian.Uint32(a[4:]) != 0 {
		t.Fatalf("mesh mapping=%v", a)
	}
	data := readModelViewerProtocolBytes(t, service.protocol, d.Base.URL)
	values := make([]float32, len(data)/4)
	for i := range values {
		values[i] = math.Float32frombits(binary.LittleEndian.Uint32(data[i*4:]))
	}
	if !slices.Equal(values[:7], []float32{2, 1.0 / (1 << 24), 0, 1, -128, 127, -1}) || values[7] != 8 || !math.Signbit(float64(values[2])) {
		t.Fatalf("decoded=%v", values)
	}
	if meshes[0].SourceIndices[0] != 3 {
		t.Fatal("render mesh source indices were mutated")
	}
}

func TestModelViewerComputeSourcesPreserveUnpackedLayoutAndBlendMapping(t *testing.T) {
	service := New()
	session := service.protocol.CreateMemorySession()
	dir := t.TempDir()
	source := func(name string, stride int) ModelViewerComputeBinarySource {
		path := filepath.Join(dir, name)
		raw := make([]byte, 3*stride)
		for vertex := range 3 {
			raw[vertex*stride] = byte(vertex + 1)
			raw[(vertex+1)*stride-1] = byte(vertex + 11)
		}
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		return ModelViewerComputeBinarySource{sourcePath: path, Stride: stride, ByteLength: int64(len(raw))}
	}
	base, blend := source("base", 40), source("blend", 32)
	transport := ModelViewerTransport{Meshes: []ModelViewerMeshTransport{{ID: "a"}}, ComputeDeformers: []ModelViewerComputeDeformerTransport{{ID: "pose", Kind: "gimi_shape_pose_v1", MeshIDs: []string{"a"}, VertexCount: 3, Base: base, ShapePasses: []ModelViewerComputeShapePass{{Target: base}}, Pose: &ModelViewerComputePoseSource{Blend: blend, Frames: ModelViewerComputeBinarySource{sourcePath: "pose"}}}}}
	if err := service.prepareModelViewerComputeSources(context.Background(), session, &transport, []modelViewerMeshPayload{{Positions: make([]float32, 3), SourceIndices: []uint32{2}}}, &modelViewerPositionCache{limit: 1024}); err != nil {
		t.Fatal(err)
	}
	d := transport.ComputeDeformers[0]
	if d.VertexCount != 1 || d.Base.Stride != 40 || d.Base.Encoding != "" {
		t.Fatalf("deformer=%+v", d)
	}
	for _, entry := range []ModelViewerComputeBinarySource{d.Base, d.Pose.Blend} {
		data := readModelViewerProtocolBytes(t, service.protocol, entry.URL)
		if len(data) != entry.Stride || data[0] != 3 || data[len(data)-1] != 13 {
			t.Fatalf("records=%v", data)
		}
	}
}
