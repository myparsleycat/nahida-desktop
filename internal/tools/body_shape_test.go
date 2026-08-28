package tools

import (
	"context"
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type bodyShapeTestDisabler struct {
	called string
}

func (d *bodyShapeTestDisabler) Disable(_ context.Context, path string) (string, error) {
	d.called = path
	target := filepath.Join(filepath.Dir(path), "DISABLED "+filepath.Base(path))
	return target, os.Rename(path, target)
}

func (d *bodyShapeTestDisabler) Enable(_ context.Context, path string) (string, error) {
	return path, nil
}

func writeTestPositions(t *testing.T, path string, positions []float32, stride int) {
	t.Helper()
	raw := make([]byte, len(positions)/3*stride)
	written, err := writeBodyPositions(raw, stride, positions)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, written, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestBodyShapedFolderBaseName(t *testing.T) {
	for input, expected := range map[string]string{
		"Astral Modulator":          "Astral Modulator (Body Shaped)",
		"DISABLED Astral Modulator": "Astral Modulator (Body Shaped)",
		"DISABLED_Foo":              "Foo (Body Shaped)",
	} {
		if actual := bodyShapedFolderBaseName(input); actual != expected {
			t.Fatalf("bodyShapedFolderBaseName(%q) = %q, want %q", input, actual, expected)
		}
	}
}

func TestBodyShapeResourceCollectionMatchesVectorAndBlendLODRules(t *testing.T) {
	t.Parallel()
	resources := []modBufferResource{
		{Name: "BodyVector_LOD", Filename: "vector.buf"},
		{Name: "BodyBlend_LOD", Filename: "blend.buf"},
	}
	if got := collectNamedResources(resources, "vector", false); len(got) != 1 {
		t.Fatalf("vector resources = %v, want LOD vector included", got)
	}
	if got := collectNamedResources(resources, "blend", true); len(got) != 0 {
		t.Fatalf("blend resources = %v, want LOD blend excluded", got)
	}
}

func TestScoreModINICapsOverrideAndResourceCounts(t *testing.T) {
	t.Parallel()
	var text strings.Builder
	for range 80 {
		text.WriteString("[TextureOverrideBody]\n[ResourceBody]\n")
	}
	if got := scoreModINI("mod.ini", text.String()); got != 100 {
		t.Fatalf("scoreModINI = %d, want capped score 100", got)
	}
}

func TestLoadBodyShapeModMatchesBuffersAndBones(t *testing.T) {
	root := t.TempDir()
	meshes := filepath.Join(root, "Meshes")
	if err := os.Mkdir(meshes, 0o700); err != nil {
		t.Fatal(err)
	}
	positions := []float32{0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0, 1}
	writeTestPositions(t, filepath.Join(meshes, "Position.buf"), positions, 12)
	indexBytes := make([]byte, 6*4)
	for index, value := range []uint32{0, 1, 2, 0, 2, 3} {
		binary.LittleEndian.PutUint32(indexBytes[index*4:], value)
	}
	if err := os.WriteFile(filepath.Join(meshes, "Index.buf"), indexBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	blend := make([]byte, 4*16)
	blend[16], blend[16+8] = 3, 255
	blend[32], blend[32+8] = 9, 128
	if err := os.WriteFile(filepath.Join(meshes, "Blend.buf"), blend, 0o600); err != nil {
		t.Fatal(err)
	}
	ini := "[ResourcePositionBuffer]\nstride = 12\nfilename = Meshes/Position.buf\n\n" +
		"[ResourceIndexBuffer]\nformat = DXGI_FORMAT_R32_UINT\nfilename = Meshes/Index.buf\n\n" +
		"[ResourceBlendBuffer]\nstride = 16\nfilename = Meshes/Blend.buf\n"
	if err := os.WriteFile(filepath.Join(root, "mod.ini"), []byte(ini), 0o600); err != nil {
		t.Fatal(err)
	}

	loaded, err := loadBodyShapeMod(root, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Meshes) != 1 {
		t.Fatalf("meshes = %#v", loaded.Meshes)
	}
	mesh := loaded.Meshes[0]
	if mesh.VertexCount != 4 || len(mesh.Positions) != 12 || len(mesh.Indices) != 6 || mesh.BlendStride == nil || *mesh.BlendStride != 16 {
		t.Fatalf("mesh = %#v", mesh)
	}
	if len(mesh.Bones) != 2 || mesh.Bones[0].ID != 3 || mesh.Bones[1].ID != 9 {
		t.Fatalf("bones = %#v", mesh.Bones)
	}
}

func TestLoadBodyShapeModMatchesNativeEFMIComponents(t *testing.T) {
	root := t.TempDir()
	meshes := filepath.Join(root, "Meshes")
	if err := os.Mkdir(meshes, 0o700); err != nil {
		t.Fatal(err)
	}
	writeTestPositions(t, filepath.Join(meshes, "Component0_VB0.buf"), []float32{0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0, 1}, 16)
	indices := make([]byte, 12)
	for index, value := range []uint16{0, 1, 2, 0, 2, 3} {
		binary.LittleEndian.PutUint16(indices[index*2:], value)
	}
	if err := os.WriteFile(filepath.Join(meshes, "Component0_IB.buf"), indices, 0o600); err != nil {
		t.Fatal(err)
	}
	blend := make([]byte, 4*12)
	binary.LittleEndian.PutUint16(blend[12:], 65535)
	blend[20] = 3
	if err := os.WriteFile(filepath.Join(meshes, "Component0_VB2.buf"), blend, 0o600); err != nil {
		t.Fatal(err)
	}
	ini := "[Resource_Component0_IB]\nformat = DXGI_FORMAT_R16_UINT\nfilename = Meshes/Component0_IB.buf\n\n" +
		"[Resource_Component0_VB0]\nstride = 16\nfilename = Meshes/Component0_VB0.buf\n\n" +
		"[Resource_Component0_VB2]\nstride = 12\nfilename = Meshes/Component0_VB2.buf\n"
	if err := os.WriteFile(filepath.Join(root, "mod.ini"), []byte(ini), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadBodyShapeMod(root, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	mesh := loaded.Meshes[0]
	if mesh.ID != "_Component0_VB0" || len(mesh.Indices) != 6 || mesh.BlendStride == nil || *mesh.BlendStride != 12 || len(mesh.Bones) != 1 || mesh.Bones[0].ID != 3 {
		t.Fatalf("mesh = %#v", mesh)
	}
}

func TestBodyShapeExportCopiesVariantAndDisablesUnmanagedSource(t *testing.T) {
	ctx := context.Background()
	sourceRoot := filepath.Join(t.TempDir(), "Character Mod")
	meshes := filepath.Join(sourceRoot, "Meshes")
	if err := os.MkdirAll(meshes, 0o700); err != nil {
		t.Fatal(err)
	}
	original := []float32{0, 0, 0, 1, 0, 0}
	positionPath := filepath.Join(meshes, "Position.buf")
	writeTestPositions(t, positionPath, original, 12)
	if err := os.WriteFile(filepath.Join(sourceRoot, shaderFixMarker), []byte("marker"), 0o600); err != nil {
		t.Fatal(err)
	}
	disabler := &bodyShapeTestDisabler{}
	service := NewWithOptions(Options{Mod: disabler})
	changed := []float32{0, 0, 0, 2, 0, 0}
	result, err := service.BodyShapeExport(ctx, BodyShapeExportInput{
		ModRoot: sourceRoot, PositionPath: positionPath, PositionStride: 12,
		Positions: changed, ChangeSummary: &BodyShapeChangeSummary{Amount: .5, AxisScale: []float64{1, 0, 0}, MovedVertices: 1, MaxDisplacement: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.ModRoot == nil || filepath.Base(*result.ModRoot) != "Character Mod (Body Shaped)" || result.SourceModPath == nil || disabler.called != sourceRoot {
		t.Fatalf("result = %#v, called = %q", result, disabler.called)
	}
	variantPosition := filepath.Join(*result.ModRoot, "Meshes", "Position.buf")
	variantRaw, err := os.ReadFile(variantPosition)
	if err != nil {
		t.Fatal(err)
	}
	variant, err := extractBodyPositions(variantRaw, 12)
	if err != nil || len(variant) != len(changed) {
		t.Fatalf("variant = %#v, %v", variant, err)
	}
	for index := range changed {
		if math.Float32bits(variant[index]) != math.Float32bits(changed[index]) {
			t.Fatalf("variant positions = %#v, want %#v", variant, changed)
		}
	}
	if _, err := os.Stat(filepath.Join(*result.ModRoot, shaderFixMarker)); !os.IsNotExist(err) {
		t.Fatalf("shader marker copied to variant: %v", err)
	}
	if result.ChangeLogPath == nil {
		t.Fatal("change log path missing")
	}
	disabledOriginal, err := os.ReadFile(filepath.Join(*result.SourceModPath, "Meshes", "Position.buf"))
	if err != nil {
		t.Fatal(err)
	}
	originalPositions, _ := extractBodyPositions(disabledOriginal, 12)
	if math.Float32bits(originalPositions[3]) != math.Float32bits(1) {
		t.Fatalf("source was modified: %#v", originalPositions)
	}
}

func TestBodyShapeResourceRejectsTraversal(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(filepath.Dir(root), "outside-body.buf")
	if err := os.WriteFile(outside, make([]byte, 12), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(outside) })
	if _, err := resolveBodyShapeResource(root, "../outside-body.buf"); err == nil {
		t.Fatal("resource traversal was accepted")
	}
}
