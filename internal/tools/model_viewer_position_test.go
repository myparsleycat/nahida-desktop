package tools

import (
	"context"
	"encoding/binary"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestModelViewerVariantPositionsCompactAndValidate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "positions.buf")
	raw := make([]byte, 3*40)
	for vertex := range 3 {
		for axis := range 3 {
			binary.LittleEndian.PutUint32(raw[vertex*40+axis*4:], math.Float32bits(float32(vertex*3+axis+1)))
		}
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	source := modelViewerDirectPositionAssignment{sourcePath: path, stride: 40, sourceBytes: int64(len(raw))}
	positions, err := readModelViewerVariantPositions(context.Background(), source, []uint32{2, 0}, 2)
	if err != nil || !slices.Equal(positions, []float32{7, 8, 9, 1, 2, 3}) {
		t.Fatalf("positions=%v err=%v", positions, err)
	}
	if _, err := readModelViewerVariantPositions(context.Background(), source, []uint32{3}, 1); err == nil {
		t.Fatal("out-of-range source accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := readModelViewerVariantPositions(ctx, source, nil, 3); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled read: %v", err)
	}
	if err := os.Truncate(path, 40); err != nil {
		t.Fatal(err)
	}
	if _, err := readModelViewerVariantPositions(context.Background(), source, nil, 1); err == nil {
		t.Fatal("changed source size accepted")
	}
}

func TestModelViewerDerivedGeometryMatchesAreaWeightedNormals(t *testing.T) {
	positions := []float32{0, 0, 0, 2, 0, 0, 0, 2, 0, 0, 0, 1}
	normals, err := modelViewerVertexNormals(context.Background(), positions, []uint32{0, 1, 2, 0, 3, 1})
	if err != nil {
		t.Fatal(err)
	}
	if normals[0] != 0 || math.Abs(float64(normals[1])-1/math.Sqrt(5)) > 1e-6 || math.Abs(float64(normals[2])-2/math.Sqrt(5)) > 1e-6 {
		t.Fatalf("normals=%v", normals)
	}
	bounds, err := modelViewerGeometryBounds(context.Background(), positions)
	if err != nil || bounds.Min != [3]float64{0, 0, 0} || bounds.Max != [3]float64{2, 2, 1} || bounds.Center != [3]float64{1, 1, 0.5} || bounds.Radius != 1.5 {
		t.Fatalf("bounds=%+v err=%v", bounds, err)
	}
	if _, err := modelViewerGeometryBounds(context.Background(), []float32{float32(math.NaN()), 0, 0}); err == nil {
		t.Fatal("non-finite position accepted")
	}
}

func TestModelViewerPositionCacheBoundsAndCancellation(t *testing.T) {
	cache := &modelViewerPositionCache{limit: 10}
	calls := 0
	read := func(context.Context) ([]byte, error) { calls++; return make([]byte, 4), nil }
	for _, key := range []string{"a", "b", "a", "c", "a"} {
		if _, err := cache.load(context.Background(), key, read); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 3 || cache.bytes != 8 || cache.entries["b"] != nil {
		t.Fatalf("calls=%d bytes=%d", calls, cache.bytes)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := cache.load(ctx, "a", read); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled cache hit: %v", err)
	}
	if _, err := cache.load(context.Background(), "large", func(context.Context) ([]byte, error) { return make([]byte, 11), nil }); err != nil {
		t.Fatal(err)
	}
	if cache.bytes != 8 || cache.entries["large"] != nil {
		t.Fatal("oversized entry retained")
	}
}

func TestModelViewerPositionLoaderIsLazyAndSessionBound(t *testing.T) {
	service := New()
	session := service.protocol.CreateMemorySession()
	path := filepath.Join(t.TempDir(), "late.buf")
	cache := &modelViewerPositionCache{limit: 1024}
	url, err := service.registerModelViewerPosition(session, "mesh", 0, modelViewerDirectPositionAssignment{sourcePath: path, stride: 12, sourceBytes: 36}, []uint32{0, 1, 2}, nil, 3, cache)
	if err != nil {
		t.Fatal(err)
	}
	// Registration must not read files or prepare unused frames.
	if err := os.WriteFile(path, modelViewerFloat32Bytes([]float32{0, 0, 0, 1, 0, 0, 0, 1, 0}), 0o600); err != nil {
		t.Fatal(err)
	}
	data := readModelViewerProtocolBytes(t, service.protocol, url)
	if len(data) != 152 || math.Float32frombits(binary.LittleEndian.Uint32(data[80+36+8:])) != 1 {
		t.Fatal("prepared normals missing")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if next := readModelViewerProtocolBytes(t, service.protocol, url); !slices.Equal(data, next) {
		t.Fatal("cached geometry changed")
	}
	service.protocol.CleanupMemorySession(session)
	response := httptest.NewRecorder()
	service.protocol.ServeHTTP(response, httptest.NewRequest(http.MethodGet, url, nil))
	if response.Code != http.StatusNotFound {
		t.Fatalf("cleaned session status=%d", response.Code)
	}
}
