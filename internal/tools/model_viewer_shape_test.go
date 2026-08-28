package tools

import (
	"context"
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"

	"nahida.live/desktop/internal/infra"
)

func TestCollectModelViewerShapeKeys(t *testing.T) {
	sections := parseModINI(`[CustomShaderShape]
cs = ShapeKey.hlsl
ResourceBodyPosition = copy ref cs-u5
cs-u5 = copy ResourceBodyPositionBase
[CommandListApply]
x88 = $sliderbody
cs-t52 = copy ResourceBodySmall
cs-t51 = copy ResourceBodyBig
run = CustomShaderShape
[ResourceBodyPosition]
filename = out.buf
stride = 40
[ResourceBodyPositionBase]
filename = base.buf
stride = 40
[ResourceBodySmall]
filename = small.buf
[ResourceBodyBig]
filename = big.buf`)
	shapes := collectModelViewerShapeKeys(sections, collectModelViewerResources(sections), t.TempDir())
	if len(shapes) != 1 || len(shapes[0].Dimensions) != 1 || shapes[0].Dimensions[0].VariableID != "sliderbody" {
		t.Fatalf("shapes = %#v", shapes)
	}
}

func TestCollectModelViewerSimpleShapeKeys(t *testing.T) {
	modDir := t.TempDir()
	sections := parseModINI(`[CustomShaderFace]
x0 = $smile
cs-t0 = copy ResourceFace.base
cs-t1 = copy ResourceFaceTarget
[ResourceFace.base]
filename = face.buf
stride = 40
[ResourceFaceTarget]
filename = face-smile.buf
stride = 40`)

	shapes := collectModelViewerShapeKeys(sections, collectModelViewerResources(sections), modDir)
	if len(shapes) != 1 || len(shapes[0].Dimensions) != 1 {
		t.Fatalf("shapes = %#v", shapes)
	}
	dimension := shapes[0].Dimensions[0]
	if shapes[0].BasePath != filepath.Join(modDir, "face.buf") || shapes[0].VertexStride != 40 || dimension.VariableID != "smile" || dimension.Mode != "" || dimension.SmallerPath != "" || dimension.BiggerPath != filepath.Join(modDir, "face-smile.buf") {
		t.Fatalf("shape = %#v", shapes[0])
	}
}

func TestBuildModelViewerDirectMeshPayloadMatchesSimpleShapeByPositionFile(t *testing.T) {
	modDir := t.TempDir()
	sections := parseModINI(`[CustomShaderFace]
x0 = $smile
cs-t0 = copy ResourceFace.base
cs-t1 = copy ResourceFaceTarget
[ResourceFace.base]
filename = face.buf
stride = 12
[ResourceFaceTarget]
filename = face-smile.buf
stride = 12`)
	shapes := collectModelViewerShapeKeys(sections, collectModelViewerResources(sections), modDir)
	target := make([]byte, 36)
	for index, value := range []float32{1, 0, 0, 2, 0, 0, 1, 1, 0} {
		binary.LittleEndian.PutUint32(target[index*4:], math.Float32bits(value))
	}
	if err := os.WriteFile(filepath.Join(modDir, "face-smile.buf"), target, 0o600); err != nil {
		t.Fatal(err)
	}
	mesh := modelViewerDirectMesh{
		id:           "face",
		positionFile: "face.buf",
		geometry:     &modelViewerGeometry{Position: []float32{0, 0, 0, 1, 0, 0, 0, 1, 0}, VertexCount: 3},
	}

	transport, payload := buildModelViewerDirectMeshPayload(mesh, nil, nil, shapes, newModelViewerBufferCache())
	if len(transport.ShapeTargets) != 1 || transport.ShapeTargets[0].Var != "smile" || len(payload.ShapePositions) != 1 || len(payload.ShapeLowPositions) != 1 {
		t.Fatalf("transport = %#v payload = %#v", transport, payload)
	}
	if payload.ShapePositions[0][0] != 1 || payload.ShapePositions[0][3] != 2 || payload.ShapeLowPositions[0][3] != 1 {
		t.Fatalf("shape positions = %#v low = %#v", payload.ShapePositions[0], payload.ShapeLowPositions[0])
	}
}

func TestCollectModelViewerX88ShapeKeys(t *testing.T) {
	modDir := t.TempDir()
	sections := parseModINI(`[CommandListDrawSliderSmile]
x87 = $smile * x87
[CommandListDrawSliderFrown]
x87 = $frown * x87
[CustomShaderFaces]
$mappedSmile = ($smile * 2 - 1)
$mappedFrown = $frown * 2 - 1
$mappedSneer = $sneer * 2 - 1
x88 = $mappedSmile * -1
cs-t50 = copy ResourceFace.b
cs-t51 = copy ResourceSmileLow
x88 = $mappedSmile
cs-t50 = copy ResourceFace.b
cs-t51 = copy ResourceSmileHigh
x88 = $mappedFrown * -1
cs-t50 = copy ResourceFace.b
cs-t51 = copy ResourceFrownLow
x88 = $mappedFrown
cs-t50 = copy ResourceFace.b
cs-t51 = copy ResourceFrownHigh
x88 = $mappedSneer * -1
cs-t50 = copy ResourceFace.b
cs-t51 = copy ResourceSneerLow
x88 = $mappedSneer
cs-t50 = copy ResourceFace.b
cs-t51 = copy ResourceSneerHigh
[ResourceFace.b]
filename = generated-face.buf
stride = 40
[ResourceFace]
filename = runtime-face.buf
stride = 40
[ResourceSmileLow]
filename = smile-low.buf
stride = 40
[ResourceSmileHigh]
filename = smile-high.buf
stride = 40
[ResourceFrownLow]
filename = frown-low.buf
stride = 40
[ResourceFrownHigh]
filename = frown-high.buf
stride = 40
[ResourceSneerLow]
filename = sneer-low.buf
stride = 40
[ResourceSneerHigh]
filename = sneer-high.buf
stride = 40`)

	shapes := collectModelViewerShapeKeys(sections, collectModelViewerResources(sections), modDir)
	byVariable := make(map[string]modelViewerShapeKey)
	for _, shape := range shapes {
		if len(shape.Dimensions) == 1 {
			byVariable[shape.Dimensions[0].VariableID] = shape
		}
	}
	if len(byVariable) != 2 {
		t.Fatalf("shapes = %#v", shapes)
	}
	for variable, files := range map[string][2]string{
		"smile": {"smile-low.buf", "smile-high.buf"},
		"frown": {"frown-low.buf", "frown-high.buf"},
	} {
		shape := byVariable[variable]
		dimension := shape.Dimensions[0]
		if shape.BasePath != filepath.Join(modDir, "runtime-face.buf") || dimension.Mode != "midpoint_pair" || dimension.SmallerPath != filepath.Join(modDir, files[0]) || dimension.BiggerPath != filepath.Join(modDir, files[1]) {
			t.Fatalf("%s shape = %#v", variable, shape)
		}
	}
}

func TestCollectModelViewerX88ShapeKeysRejectsMixedBases(t *testing.T) {
	sections := parseModINI(`[CustomShaderFaces]
x88 = $smile
cs-t50 = copy ResourceFaceA
cs-t51 = copy ResourceSmile
x88 = $frown
cs-t50 = copy ResourceFaceB
cs-t51 = copy ResourceFrown
[ResourceFaceA]
filename = face-a.buf
[ResourceFaceB]
filename = face-b.buf
[ResourceSmile]
filename = smile.buf
[ResourceFrown]
filename = frown.buf`)

	if shapes := collectModelViewerShapeKeys(sections, collectModelViewerResources(sections), t.TempDir()); len(shapes) != 0 {
		t.Fatalf("shapes = %#v", shapes)
	}
}

func TestCollectModelViewerSparseShapeKeys(t *testing.T) {
	modDir := t.TempDir()
	sections := parseModINI(`[CommandListDrawSliderJaw]
x87 = $jaw * x87
[CommandListShapeJaw]
$\WWMIv1\shapekey_id = 127
$\WWMIv1\shapekey_value = $jaw
cs-t6 = ResourcePosition
cs-t33 = ResourceShapeOffsets
cs-t0 = ResourceShapeVertexIDs
cs-t1 = ResourceShapeDeltas
[Constants]
global $shapekey_vertex_offset_batch1 = 9
[ResourcePosition]
filename = position.buf
[ResourceShapeOffsets]
filename = offsets.buf
[ResourceShapeVertexIDs]
filename = vertex-ids.buf
[ResourceShapeDeltas]
filename = deltas.buf`)

	shapes := collectModelViewerShapeKeys(sections, collectModelViewerResources(sections), modDir)
	if len(shapes) != 1 || len(shapes[0].Dimensions) != 1 {
		t.Fatalf("shapes = %#v", shapes)
	}
	dimension := shapes[0].Dimensions[0]
	if shapes[0].BasePath != filepath.Join(modDir, "position.buf") || shapes[0].VertexStride != 12 || dimension.VariableID != "jaw" || !dimension.Sparse || dimension.BufferShapeID != 128 || dimension.SparseOffset != 9 || dimension.OffsetPath != filepath.Join(modDir, "offsets.buf") || dimension.VertexIDPath != filepath.Join(modDir, "vertex-ids.buf") || dimension.VertexDeltaPath != filepath.Join(modDir, "deltas.buf") {
		t.Fatalf("shape = %#v", shapes[0])
	}
}

func TestReadModelViewerSparseShapePositionsUsesUncompactedVertexIDs(t *testing.T) {
	modDir := t.TempDir()
	offsets := make([]byte, 8)
	binary.LittleEndian.PutUint32(offsets[4:], 1)
	vertexIDs := make([]byte, 4)
	binary.LittleEndian.PutUint32(vertexIDs, 1)
	deltas := make([]byte, 12)
	binary.LittleEndian.PutUint16(deltas, 0x3c00)
	for name, data := range map[string][]byte{"offsets.buf": offsets, "vertex-ids.buf": vertexIDs, "deltas.buf": deltas} {
		if err := os.WriteFile(filepath.Join(modDir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	geometry := &modelViewerGeometry{Position: []float32{0, 0, 0, 2, 0, 0, 4, 0, 0}, VertexCount: 3}
	dimension := modelViewerShapeKeyDimension{Sparse: true, OffsetPath: filepath.Join(modDir, "offsets.buf"), VertexIDPath: filepath.Join(modDir, "vertex-ids.buf"), VertexDeltaPath: filepath.Join(modDir, "deltas.buf")}

	positions, err := readModelViewerSparseShapePositions(newModelViewerBufferCache(), dimension, geometry)
	if err != nil {
		t.Fatal(err)
	}
	want := []float32{0, 0, 0, 3, 0, 0, 4, 0, 0}
	if len(positions) != len(want) {
		t.Fatalf("positions = %#v", positions)
	}
	for index := range want {
		if positions[index] != want[index] {
			t.Fatalf("positions = %#v", positions)
		}
	}
}

func TestCollectModelViewerMultiShapeKeys(t *testing.T) {
	modDir := t.TempDir()
	sections := parseModINI(`[CommandListMenu]
x87 = 1 - $smile
x87 = 1 - $frown
[CommandListApplyFaces]
cs-t50 = copy ResourceFace
cs-t51 = copy ResourceSmileHigh
cs-t52 = copy ResourceSmileLow
cs-t53 = copy ResourceFrownHigh
cs-t54 = copy ResourceFrownLow
x88 = $smile
x89 = $frown
[ResourceFace]
filename = face.buf
stride = 40
[ResourceSmileHigh]
filename = smile-high.buf
stride = 40
[ResourceSmileLow]
filename = smile-low.buf
stride = 40
[ResourceFrownHigh]
filename = frown-high.buf
stride = 40
[ResourceFrownLow]
filename = frown-low.buf
stride = 40`)

	shapes := collectModelViewerShapeKeys(sections, collectModelViewerResources(sections), modDir)
	byVariable := make(map[string]modelViewerShapeKeyDimension)
	for _, shape := range shapes {
		if shape.BasePath != filepath.Join(modDir, "face.buf") || len(shape.Dimensions) != 1 {
			t.Fatalf("shape = %#v", shape)
		}
		byVariable[shape.Dimensions[0].VariableID] = shape.Dimensions[0]
	}
	if len(byVariable) != 2 || byVariable["smile"].Mode != "midpoint_pair" || byVariable["smile"].SmallerPath != filepath.Join(modDir, "smile-low.buf") || byVariable["smile"].BiggerPath != filepath.Join(modDir, "smile-high.buf") || byVariable["frown"].SmallerPath != filepath.Join(modDir, "frown-low.buf") || byVariable["frown"].BiggerPath != filepath.Join(modDir, "frown-high.buf") {
		t.Fatalf("dimensions = %#v", byVariable)
	}
}

func TestLoadModViewerExposesMidpointShapeSliderWithoutKeyBinding(t *testing.T) {
	modDir := t.TempDir()
	ini := `[Constants]
global persist $sliderbody = 0.5
[TextureOverrideBody]
ib = ResourceBodyIB
drawindexed = 3, 0, 0
[CustomShaderShape]
cs = ShapeKey.hlsl
ResourceBodyPosition = copy ref cs-u5
cs-u5 = copy ResourceBodyPositionBase
[CommandListApplyShape]
x88 = $sliderbody
cs-t52 = copy ResourceBodySmall
cs-t51 = copy ResourceBodyBig
run = CustomShaderShape
[ResourceBody]
filename = Body.buf
stride = 12
[ResourceBodyIB]
filename = Body.ib
format = DXGI_FORMAT_R16_UINT
[ResourceBodyPosition]
filename = out.buf
stride = 40
[ResourceBodyPositionBase]
filename = base.buf
stride = 40
[ResourceBodySmall]
filename = small.buf
stride = 40
[ResourceBodyBig]
filename = big.buf
stride = 40`
	if err := os.WriteFile(filepath.Join(modDir, "mod.ini"), []byte(ini), 0o600); err != nil {
		t.Fatal(err)
	}
	positions := make([]byte, 36)
	for index, value := range []float32{0, 0, 0, 1, 0, 0, 0, 1, 0} {
		binary.LittleEndian.PutUint32(positions[index*4:], math.Float32bits(value))
	}
	if err := os.WriteFile(filepath.Join(modDir, "Body.buf"), positions, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(modDir, "Body.ib"), []byte{0, 0, 1, 0, 2, 0}, 0o600); err != nil {
		t.Fatal(err)
	}
	shape := make([]byte, 3*40)
	for _, name := range []string{"out.buf", "base.buf", "small.buf", "big.buf"} {
		if err := os.WriteFile(filepath.Join(modDir, name), shape, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	protocol := infra.NewProtocol()
	service := NewWithOptions(Options{Protocol: protocol})
	service.UseClient(openToolsTestDB(t))
	result, err := service.LoadModViewer(context.Background(), modDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Meshes) != 1 || len(result.Meshes[0].ShapeTargets) != 1 || result.Meshes[0].ShapeTargets[0].Var != "sliderbody" {
		t.Fatalf("shape targets = %#v", result.Meshes)
	}
	if len(result.Variables) != 1 {
		t.Fatalf("variables = %#v", result.Variables)
	}
	variable := result.Variables[0]
	if variable.ID != "sliderbody" || variable.ControlType != "slider" || variable.Slider == nil || variable.Slider.Min != 0 || variable.Slider.Max != 1 || variable.Slider.Step != 0.01 || variable.DefaultValue != "0.5" {
		t.Fatalf("shape variable = %#v", variable)
	}
}
