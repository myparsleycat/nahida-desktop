package tools

import (
	"context"
	"encoding/hex"
	"os"
	"reflect"
	"strings"
	"testing"

	"nahida.live/desktop/internal/infra"
)

func TestModelViewerMeshBytesMatchesFrontendFixture(t *testing.T) {
	payload := modelViewerMeshPayload{
		Positions: []float32{1, -2, 3},
		Normals:   []float32{0, 0, 1},
		Tangents:  []float32{1, 0, 0, -1},
		UVs:       []float32{.25, .75},
		Indices:   []uint32{0, 0, 0},
	}
	fixture, err := os.ReadFile("testdata/model_viewer_mesh.hex")
	if err != nil {
		t.Fatal(err)
	}
	if got := hex.EncodeToString(modelViewerMeshBytes(payload)); got != strings.TrimSpace(string(fixture)) {
		t.Fatalf("mesh wire format changed: %s", got)
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
