package modelviewer

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"os"
	"reflect"
	"strings"
	"testing"

	"nahida.live/desktop/internal/infra"
)

func TestModelViewerMeshBytesWritesMVG3(t *testing.T) {
	data := modelViewerMeshBytes(modelViewerMeshPayload{})
	if len(data) != 32 || binary.LittleEndian.Uint32(data[:4]) != 0x3347564d {
		t.Fatalf("mesh header = %x, want an empty MVG3 header", data)
	}
}

func TestModelViewerMeshBytesMatchesFrontendFixture(t *testing.T) {
	payload := modelViewerMeshPayload{
		Positions: []float32{1, -2, 3},
		Normals:   []float32{0, 0, 1},
		Tangents:  []float32{1, 0, 0, -1},
		UVs:       []float32{.25, .75},
		UVs1:      []float32{.875, .125},
		Colors:    []uint8{255, 128, 0, 64},
		Indices:   []uint32{0, 0, 0},
	}
	fixture, err := os.ReadFile("testdata/model_viewer_mesh_v3.hex")
	if err != nil {
		t.Fatal(err)
	}
	if got := hex.EncodeToString(modelViewerMeshBytes(payload)); got != strings.TrimSpace(string(fixture)) {
		t.Fatalf("mesh wire format changed: %s", got)
	}
}

// The spool receives meshes in fixed chunks; values split across chunk boundaries must arrive intact.
func TestModelViewerMeshBytesSurviveChunkBoundaries(t *testing.T) {
	vertices := modelViewerEncodeChunkBytes/12 + 7
	payload := modelViewerMeshPayload{
		Positions: make([]float32, vertices*3),
		Normals:   make([]float32, vertices*3),
		UVs:       make([]float32, vertices*2),
		Colors:    make([]uint8, vertices*4),
		Indices:   make([]uint32, vertices*3+1),
	}
	for index := range payload.Positions {
		payload.Positions[index] = float32(index) * .5
		payload.Normals[index] = -float32(index)
	}
	for index := range payload.UVs {
		payload.UVs[index] = float32(index) / 3
	}
	for index := range payload.Colors {
		payload.Colors[index] = uint8(index * 7)
	}
	for index := range payload.Indices {
		payload.Indices[index] = uint32(index * 3)
	}

	var want bytes.Buffer
	want.WriteString("MVG3")
	counts := []uint32{
		uint32(len(payload.Positions)), uint32(len(payload.Normals)), 0, uint32(len(payload.UVs)), 0,
		uint32(len(payload.Colors)), uint32(len(payload.Indices)),
	}
	for _, values := range []any{counts, payload.Positions, payload.Normals, payload.UVs, payload.Colors, payload.Indices} {
		if err := binary.Write(&want, binary.LittleEndian, values); err != nil {
			t.Fatal(err)
		}
	}
	var got bytes.Buffer
	if err := writeModelViewerMeshBytes(&got, payload); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.Bytes(), want.Bytes()) || got.Len() != modelViewerMeshByteLength(payload) {
		t.Fatalf("encoded %d bytes, want %d matching the reference", got.Len(), want.Len())
	}
}

func TestModelViewerPayloadStoresOneGeometryBufferPerMesh(t *testing.T) {
	protocol := infra.NewProtocol()
	service := NewWithOptions(Options{Protocol: protocol})
	sessionID := protocol.CreateMemorySession()
	transport := ModelViewerTransport{
		Meshes:   []ModelViewerMeshTransport{{ID: "mesh"}},
		Textures: map[string]ModelViewerTextureTransport{},
	}
	payload := modelViewerMeshPayload{
		Positions: []float32{0, 0, 0, 1, 0, 0, 0, 1, 0},
		Normals:   []float32{0, 0, 1, 0, 0, 1, 0, 0, 1},
		UVs:       []float32{0, 0, 1, 0, 0, 1},
		Indices:   []uint32{0, 1, 2},
	}
	if err := writeModelViewerPayload(
		context.Background(),
		service,
		sessionID,
		&transport,
		[]modelViewerMeshPayload{payload},
		nil,
		modelViewerPayloadOptions{},
	); err != nil {
		t.Fatal(err)
	}
	got := readViewerMesh(t, protocol, transport.Meshes[0].GeometryURL)
	if !reflect.DeepEqual(got, payload) {
		t.Fatalf("mesh = %#v, want %#v", got, payload)
	}
	sessions := reflect.ValueOf(protocol).Elem().FieldByName("sessions")
	if count := sessions.MapIndex(reflect.ValueOf(sessionID)).Elem().FieldByName("buffers").Len(); count != 1 {
		t.Fatalf("geometry buffers = %d, want 1", count)
	}
}
