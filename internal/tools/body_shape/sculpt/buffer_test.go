package sculpt

import (
	"encoding/binary"
	"math"
	"slices"
	"testing"

	"nahida.live/desktop/internal/tools/modmesh"
)

func TestWritePositionsIntoBufferKeepsPadding(t *testing.T) {
	const stride, vertexCount = 16, 4
	original := make([]byte, vertexCount*stride)
	for i := range vertexCount {
		for axis := range 3 {
			binary.LittleEndian.PutUint32(original[i*stride+axis*4:], math.Float32bits(float32(i*(axis+1))))
		}
		binary.LittleEndian.PutUint32(original[i*stride+12:], 0xdeadbeef)
	}
	positions, err := modmesh.ExtractPositions(original, stride)
	if err != nil {
		t.Fatal(err)
	}
	preview := make([]float32, len(positions))
	AnisotropicScaleFromOriginal(positions, preview, []float32{1, 0, 1, 0}, Vec3{}, 1, Vec3{1, 0, 0})

	written, err := WritePositionsIntoBuffer(original, stride, preview)
	if err != nil {
		t.Fatal(err)
	}
	if len(written) != len(original) {
		t.Fatalf("written %d bytes, want %d", len(written), len(original))
	}
	for i := range vertexCount {
		if pad := binary.LittleEndian.Uint32(written[i*stride+12:]); pad != 0xdeadbeef {
			t.Fatalf("vertex %d padding = %#x", i, pad)
		}
	}
	roundTrip, err := modmesh.ExtractPositions(written, stride)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "x", []float32{roundTrip[0], roundTrip[3], roundTrip[6], roundTrip[9]}, []float32{0, 1, 4, 3})

	preview[0] = float32(math.NaN())
	if _, err := WritePositionsIntoBuffer(original, stride, preview); err == nil {
		t.Fatal("NaN position was written")
	}
}

func TestSnorm8VectorCorrection(t *testing.T) {
	tests := []struct {
		fileSize, vertexCount, stride int
		want                          bool
	}{
		{16, 2, 0, true},
		{16, 3, 0, false},
		{12, 2, 12, false},
	}
	for _, test := range tests {
		if got := DetectSnorm8VectorLayout(test.fileSize, test.vertexCount, test.stride); got != test.want {
			t.Errorf("DetectSnorm8VectorLayout(%d, %d, %d) = %v", test.fileSize, test.vertexCount, test.stride, got)
		}
	}

	// A diagonal tangent and normal show a non-uniform scale after normalization.
	vectors := []int8{90, 90, 0, 1, 0, 90, 90, -1, 90, 90, 0, 1, 0, 90, 90, -1}
	corrected, err := ApplySnorm8VectorCorrection(vectors, []float32{1, 0}, 1, Vec3{2, 1, 0.5})
	if err != nil {
		t.Fatal(err)
	}
	if corrected[3] != 1 || corrected[7] != -1 {
		t.Fatalf("handedness bytes changed: %v", corrected[:8])
	}
	assertEqual(t, "unweighted vertex", corrected[8:], vectors[8:])
	if slices.Equal(corrected[0:3], vectors[0:3]) || slices.Equal(corrected[4:7], vectors[4:7]) {
		t.Fatalf("weighted vertex unchanged: %v", corrected[:8])
	}

	if _, err := ApplySnorm8VectorCorrection(vectors[:7], nil, 1, Vec3{2, 1, 1}); err == nil {
		t.Fatal("misaligned vector buffer was accepted")
	}
}
