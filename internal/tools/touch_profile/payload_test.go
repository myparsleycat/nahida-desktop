package touchprofile

import (
	"context"
	"reflect"
	"testing"

	"nahida.live/desktop/internal/tools/modmesh"
)

func fakeBuffers(buffers map[string][]byte) bufferReader {
	return func(_ context.Context, url string) ([]byte, error) { return buffers[url], nil }
}

func TestReadTouchMeshDecodesTopologyAndBlend(t *testing.T) {
	indices, blend, stride := "/indices", "/blend", 16
	mesh, err := readTouchMesh(context.Background(), TouchMeshDescriptor{
		SessionID: "session", ComponentID: "mesh", VertexCount: 3,
		PositionsURL: "/positions", PositionsCount: 9,
		IndicesURL: &indices, IndexCount: 3,
		Bones:       []modmesh.BlendBoneInfo{{ID: 3, VertexCount: 1}},
		BlendStride: &stride, BlendURL: &blend, BlendBytes: 4,
	}, fakeBuffers(map[string][]byte{
		"/positions": modmesh.Float32Bytes([]float32{0, 0, 0, 1, 0, 0, 0, 1, 0}),
		"/indices":   modmesh.Uint32Bytes([]uint32{0, 1, 2}),
		"/blend":     {3, 0, 0, 0},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(mesh.Positions, []float32{0, 0, 0, 1, 0, 0, 0, 1, 0}) ||
		!reflect.DeepEqual(mesh.Indices, []uint32{0, 1, 2}) ||
		!reflect.DeepEqual(mesh.BlendBytes, []byte{3, 0, 0, 0}) || mesh.BlendStride != 16 {
		t.Fatalf("mesh = %+v", mesh)
	}

	_, err = readTouchMesh(context.Background(), TouchMeshDescriptor{PositionsURL: "/positions", PositionsCount: 6},
		fakeBuffers(map[string][]byte{"/positions": modmesh.Float32Bytes(make([]float32, 9))}))
	if err == nil {
		t.Fatal("positions shorter than the descriptor count were accepted")
	}
}

func TestReadTouchPreviewSlicesPackedZoneWeights(t *testing.T) {
	mesh := TouchMesh{SessionID: "session", ComponentID: "mesh", VertexCount: 3}
	descriptor := TouchProfilePreviewDescriptor{
		SessionID: "session", ComponentID: "mesh", VertexCount: 3, WeightsURL: "/weights", WeightsCount: 6,
		Zones: []TouchPreviewZoneDescriptor{
			{TouchZoneSpec: TouchZoneSpec{ID: "left"}, WeightOffset: 0},
			{TouchZoneSpec: TouchZoneSpec{ID: "right"}, WeightOffset: 3},
		},
	}
	read := fakeBuffers(map[string][]byte{"/weights": modmesh.Float32Bytes([]float32{0, .5, 1, 1, .5, 0})})

	preview, err := readTouchPreview(context.Background(), descriptor, mesh, read)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(preview.Zones[0].Weights, []float32{0, .5, 1}) ||
		!reflect.DeepEqual(preview.Zones[1].Weights, []float32{1, .5, 0}) {
		t.Fatalf("zones = %+v", preview.Zones)
	}

	descriptor.Zones[1].WeightOffset = 4
	if _, err = readTouchPreview(context.Background(), descriptor, mesh, read); err == nil {
		t.Fatal("a zone reading past the weights was accepted")
	}
	mesh.ComponentID = "other"
	if _, err = readTouchPreview(context.Background(), descriptor, mesh, read); err == nil {
		t.Fatal("a preview for another component's topology was accepted")
	}
}
