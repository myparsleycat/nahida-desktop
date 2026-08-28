package tools

import (
	"encoding/binary"
	"image/color"
	"os"
	"path/filepath"
	"testing"
)

func TestPickWwmiDumpDiffusePrefersSRGBThenLarger(t *testing.T) {
	t.Parallel()
	got := pickWwmiDumpDiffuse([]wwmiDumpCandidate{
		{File: "Textures/Components-3 t=beef001a.dds", Area: 8192 * 8192, Bytes: 67_109_012, Order: 0},
		{File: "Textures/Components-3 t=beef001b.dds", SRGB: true, Area: 8192 * 8192, Bytes: 67_109_012, Order: 1},
		{File: "Textures/Components-3 t=beef0010.jpg", SRGB: true, Bytes: 1_615_266, Order: 2},
	})
	if got != "Textures/Components-3 t=beef001b.dds" {
		t.Fatalf("pickWwmiDumpDiffuse = %q", got)
	}
}

func TestPickWwmiDumpDiffusePrefersExclusiveOverShared(t *testing.T) {
	t.Parallel()
	got := pickWwmiDumpDiffuse([]wwmiDumpCandidate{
		{File: "Textures/Components-0-1-2-3-4-5 t=beef0012.dds", SRGB: true, Area: 2048 * 2048, Bytes: 4_194_452, Order: 0},
		{File: "Textures/Components-3 t=beef001c.dds", Area: 512 * 512, Bytes: 262_292, Order: 1},
	})
	if got != "Textures/Components-3 t=beef001c.dds" {
		t.Fatalf("pickWwmiDumpDiffuse = %q", got)
	}
}

func TestIsLikelyWwmiDiffuseRejectsLinearFlatPackedAndNormal(t *testing.T) {
	t.Parallel()
	ok := wwmiTextureHint{SRGB: true, ColorSpace: "srgb", Area: 1024, Bytes: 100}
	if !isLikelyWwmiDiffuse(ok) {
		t.Fatal("expected srgb hint to count as diffuse")
	}
	if !isLikelyWwmiDiffuse(wwmiTextureHint{ColorSpace: "unknown", Area: 1024, Bytes: 100}) {
		t.Fatal("unknown color space should still be accepted")
	}
	if isLikelyWwmiDiffuse(wwmiTextureHint{ColorSpace: "linear", Area: 1024, Bytes: 100}) {
		t.Fatal("linear should be rejected")
	}
	if isLikelyWwmiDiffuse(wwmiTextureHint{ColorSpace: "srgb", IsLikelyFlat: true, Area: 1024, Bytes: 100}) {
		t.Fatal("flat should be rejected")
	}
	if isLikelyWwmiDiffuse(wwmiTextureHint{ColorSpace: "srgb", IsLikelyNormal: true, Area: 1024, Bytes: 100}) {
		t.Fatal("normal should be rejected")
	}
	if isLikelyWwmiDiffuse(wwmiTextureHint{ColorSpace: "srgb", IsLikelyPacked: true, Area: 1024, Bytes: 100}) {
		t.Fatal("packed should be rejected")
	}
}

func TestInspectWwmiTextureHintDecodesInBudgetDDS(t *testing.T) {
	root := t.TempDir()
	flatPath := filepath.Join(root, "flat.dds")
	if err := os.WriteFile(flatPath, encodeWwmiUncompressedDDS(2, 2, []color.NRGBA{
		{R: 128, G: 64, B: 32, A: 255}, {R: 128, G: 64, B: 32, A: 255},
		{R: 128, G: 64, B: 32, A: 255}, {R: 128, G: 64, B: 32, A: 255},
	}), 0o600); err != nil {
		t.Fatal(err)
	}
	flat := inspectWwmiTextureHint(flatPath)
	if flat == nil || !flat.IsLikelyFlat || isLikelyWwmiDiffuse(*flat) {
		t.Fatalf("flat DDS hint = %#v", flat)
	}

	diffusePath := filepath.Join(root, "diffuse.dds")
	if err := os.WriteFile(diffusePath, encodeWwmiUncompressedDDS(2, 2, []color.NRGBA{
		{R: 255, A: 255}, {G: 255, A: 255}, {B: 255, A: 255}, {R: 255, G: 255, B: 255, A: 255},
	}), 0o600); err != nil {
		t.Fatal(err)
	}
	diffuse := inspectWwmiTextureHint(diffusePath)
	if diffuse == nil || diffuse.IsLikelyFlat || !isLikelyWwmiDiffuse(*diffuse) {
		t.Fatalf("diffuse DDS hint = %#v", diffuse)
	}
}

func TestKeepLikelyDiffuseAssignmentsPreservesRelativeOrder(t *testing.T) {
	assignments := []modelViewerDirectTextureAssignment{
		{role: "diffuse", file: "first"},
		{role: "normal_map", file: "normal"},
		{role: "diffuse", file: "flat"},
		{role: "light_map", file: "light"},
		{role: "diffuse", file: "last"},
	}
	inspect := func(file string) *wwmiTextureHint {
		return &wwmiTextureHint{ColorSpace: "srgb", IsLikelyFlat: file == "flat"}
	}
	kept := keepLikelyDiffuseAssignments(assignments, inspect)
	if len(kept) != 4 || kept[0].file != "first" || kept[1].file != "normal" || kept[2].file != "light" || kept[3].file != "last" {
		t.Fatalf("kept assignments = %#v", kept)
	}
}

func TestAttachWwmiDumpTextureDoesNotInventResourceName(t *testing.T) {
	root := t.TempDir()
	relative := filepath.Join("Textures", "Components-0 t=diffuse.dds")
	path := filepath.Join(root, relative)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, encodeWwmiUncompressedDDS(2, 2, []color.NRGBA{
		{R: 255, A: 255}, {G: 255, A: 255}, {B: 255, A: 255}, {R: 255, G: 255, B: 255, A: 255},
	}), 0o600); err != nil {
		t.Fatal(err)
	}
	meshes := []modelViewerDirectMesh{{component: "Component0"}}
	attachWwmiDumpTextures(meshes, []modelViewerResource{{Filename: relative}}, root)
	if len(meshes[0].textureAssignments) != 1 {
		t.Fatalf("texture assignments = %#v", meshes[0].textureAssignments)
	}
	assignment := meshes[0].textureAssignments[0]
	if assignment.file != relative || assignment.resource != "" || meshes[0].textureDefaultFile != relative {
		t.Fatalf("dump assignment = %#v, default = %q", assignment, meshes[0].textureDefaultFile)
	}
}

func encodeWwmiUncompressedDDS(width, height uint32, pixels []color.NRGBA) []byte {
	header := make([]byte, 128)
	copy(header[:4], "DDS ")
	binary.LittleEndian.PutUint32(header[4:8], 124)
	binary.LittleEndian.PutUint32(header[8:12], 0x100f)
	binary.LittleEndian.PutUint32(header[12:16], height)
	binary.LittleEndian.PutUint32(header[16:20], width)
	binary.LittleEndian.PutUint32(header[20:24], width*4)
	binary.LittleEndian.PutUint32(header[76:80], 32)
	binary.LittleEndian.PutUint32(header[80:84], 0x41)
	binary.LittleEndian.PutUint32(header[88:92], 32)
	binary.LittleEndian.PutUint32(header[92:96], 0x00ff0000)
	binary.LittleEndian.PutUint32(header[96:100], 0x0000ff00)
	binary.LittleEndian.PutUint32(header[100:104], 0x000000ff)
	binary.LittleEndian.PutUint32(header[104:108], 0xff000000)
	binary.LittleEndian.PutUint32(header[108:112], 0x1000)
	raw := make([]byte, 0, len(header)+len(pixels)*4)
	raw = append(raw, header...)
	for _, pixel := range pixels {
		raw = append(raw, pixel.B, pixel.G, pixel.R, pixel.A)
	}
	return raw
}
