package tools

import (
	"encoding/binary"
	"math"
	"slices"
	"testing"
)

func TestModelViewerFmtAndGeometry(t *testing.T) {
	fmtText := `stride: 24
topology: trianglelist
format: DXGI_FORMAT_R16_UINT
element[0]:
  SemanticName: POSITION
  SemanticIndex: 0
  Format: DXGI_FORMAT_R32G32B32_FLOAT
  InputSlot: 0
  AlignedByteOffset: 0
  InputSlotClass: per-vertex
  InstanceDataStepRate: 0
element[1]:
  SemanticName: NORMAL
  SemanticIndex: 0
  Format: DXGI_FORMAT_R16G16B16A16_SNORM
  InputSlot: 0
  AlignedByteOffset: append
  InputSlotClass: per-vertex
  InstanceDataStepRate: 0`
	layout, err := parseModelViewerFmt(fmtText, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(layout.Elements) != 2 || layout.Elements[1].AlignedByteOffset != 12 || layout.Stride != 24 {
		t.Fatalf("layout = %#v", layout)
	}
	vb := make([]byte, 3*24)
	positions := [][3]float32{{0, 0, 0}, {1, 0, 0}, {0, 1, 0}}
	for vertex, position := range positions {
		for component, value := range position {
			binary.LittleEndian.PutUint32(vb[vertex*24+component*4:], math.Float32bits(value))
		}
		binary.LittleEndian.PutUint16(vb[vertex*24+12:], uint16(0))
		binary.LittleEndian.PutUint16(vb[vertex*24+14:], uint16(0))
		binary.LittleEndian.PutUint16(vb[vertex*24+16:], uint16(32767))
	}
	mesh, err := extractModelViewerGeometry(vb, 24, layout, []uint32{0, 1, 2, 2, 2, 0}, true, true, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if mesh == nil || len(mesh.Indices) != 6 || len(mesh.Position) != 9 || len(mesh.Normal) != 9 || mesh.Normal[2] != 1 {
		t.Fatalf("mesh = %#v", mesh)
	}
}

func TestModelViewerGeometryMatchesElectronPayloadPostProcessing(t *testing.T) {
	layout := modelViewerFmtLayout{Topology: "trianglelist", Stride: 40, Elements: []modelViewerFmtElement{
		{SemanticName: "POSITION", Format: "DXGI_FORMAT_R32G32B32_FLOAT", AlignedByteOffset: 0},
		{SemanticName: "NORMAL", Format: "DXGI_FORMAT_R32G32B32_FLOAT", AlignedByteOffset: 12},
		{SemanticName: "TANGENT", Format: "DXGI_FORMAT_R32G32B32A32_FLOAT", AlignedByteOffset: 24},
	}}
	vb := make([]byte, 4*40)
	for vertex := range 4 {
		offset := vertex * 40
		binary.LittleEndian.PutUint32(vb[offset:], math.Float32bits(float32(vertex)))
		binary.LittleEndian.PutUint32(vb[offset+20:], math.Float32bits(2))
		binary.LittleEndian.PutUint32(vb[offset+24:], math.Float32bits(2))
		binary.LittleEndian.PutUint32(vb[offset+36:], math.Float32bits(-0.5))
	}

	mesh, err := extractModelViewerGeometry(vb, 40, layout, []uint32{3, 1, 3, 2, 2, 1}, true, false, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantIndices := []uint32{2, 0, 2, 1, 1, 0}
	wantSources := []uint32{1, 2, 3}
	if mesh == nil || !slices.Equal(mesh.Indices, wantIndices) || !slices.Equal(mesh.SourceIndices, wantSources) {
		t.Fatalf("mesh = %#v", mesh)
	}
	if mesh.Normal[2] != 2 || mesh.Tangent[0] != 2 || mesh.Tangent[3] != -0.5 {
		t.Fatalf("normal = %#v tangent = %#v", mesh.Normal, mesh.Tangent)
	}
}

func TestModelViewerDXGIFormatsAndIndices(t *testing.T) {
	packed := uint32(1023 | (512 << 10) | (3 << 30))
	data := make([]byte, 4)
	binary.LittleEndian.PutUint32(data, packed)
	values, err := modelViewerReadDXGI(data, 0, "DXGI_FORMAT_R10G10B10A2_UNORM")
	if err != nil || values[0] != 1 || math.Abs(float64(values[1]-float32(512.0/1023.0))) > 1e-6 || values[3] != 1 {
		t.Fatalf("values=%v err=%v", values, err)
	}
	half := []byte{0x00, 0x3c, 0x00, 0xc0}
	values, err = modelViewerReadDXGI(half, 0, "DXGI_FORMAT_R16G16_FLOAT")
	if err != nil || values[0] != 1 || values[1] != -2 {
		t.Fatalf("values=%v err=%v", values, err)
	}
	indices, err := modelViewerDecodeIndices([]byte{0, 0, 2, 0}, "DXGI_FORMAT_R16_UINT")
	if err != nil || len(indices) != 2 || indices[1] != 2 {
		t.Fatalf("indices=%v err=%v", indices, err)
	}
}

func TestDetectModelViewerPositionFrameRejectsPaddingAndKeepsAuthoredTBN(t *testing.T) {
	data := make([]byte, 3*40)
	if detectModelViewerPositionFrame(data, 40) {
		t.Fatal("zero padding must not be treated as authored TBN data")
	}
	for vertex := range 3 {
		offset := vertex * 40
		binary.LittleEndian.PutUint32(data[offset+16:], math.Float32bits(1))
		binary.LittleEndian.PutUint32(data[offset+24:], math.Float32bits(1))
		binary.LittleEndian.PutUint32(data[offset+36:], math.Float32bits(-1))
	}
	if !detectModelViewerPositionFrame(data, 40) {
		t.Fatal("unit authored normals and tangents must be preserved")
	}
}
