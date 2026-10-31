package modelviewer

import (
	"encoding/binary"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const (
	efmiDXGIBGRA8     = 87
	efmiDXGIBGRA8SRGB = 91
)

func efmiDX10DDS(side, dxgi uint32, pixels []color.NRGBA) []byte {
	legacy := encodeWwmiUncompressedDDS(side, side, pixels)
	header := slices.Clone(legacy[:128])
	binary.LittleEndian.PutUint32(header[80:84], 0x4)
	copy(header[84:88], "DX10")
	clear(header[88:108])
	extension := make([]byte, 20)
	binary.LittleEndian.PutUint32(extension[0:4], dxgi)
	binary.LittleEndian.PutUint32(extension[4:8], 3)
	binary.LittleEndian.PutUint32(extension[12:16], 1)
	return slices.Concat(header, extension, legacy[128:])
}

func efmiSolidDDS(side uint32, pixel color.NRGBA) []byte {
	return efmiDX10DDS(side, efmiDXGIBGRA8, slices.Repeat([]color.NRGBA{pixel}, int(side*side)))
}

var (
	efmiTestLightMap  = color.NRGBA{R: 0, G: 255, B: 160, A: 128}
	efmiTestNormalMap = color.NRGBA{R: 128, G: 120, B: 77, A: 80}
	efmiTestGrayMask  = color.NRGBA{R: 90, G: 90, B: 90, A: 255}
)

const efmiComponent3Resources = `
[TextureOverride_Component3]
hash = 52ebe5f7
if DRAW_TYPE == 4
    handling = skip
    run = CommandList_Draw_Component3
endif
[Resource_Component3_IB]
type = Buffer
format = DXGI_FORMAT_R16_UINT
filename = Meshes/Component3_IB.buf
[Resource_Component3_VB0]
type = Buffer
stride = 16
filename = Meshes/Component3_VB0.buf
[Resource_Component3_VB1]
type = Buffer
stride = 12
filename = Meshes/Component3_VB1.buf
[Resource_Component3_VB2]
type = Buffer
stride = 12
filename = Meshes/Component3_VB2.buf
`

func TestLoadModViewerRecoversEFMISlotMaps(t *testing.T) {
	dir := t.TempDir()
	writeEFMIComponentBuffers(t, dir, "Component3")
	writeTextureFile(t, dir, "Textures/D.png", encodeColorPNG(32, 32))
	writeTextureFile(t, dir, "Textures/L.dds", efmiSolidDDS(32, efmiTestLightMap))
	writeTextureFile(t, dir, "Textures/N1.dds", efmiSolidDDS(32, efmiTestNormalMap))
	writeTextureFile(t, dir, "Textures/N2.dds", efmiSolidDDS(32, efmiTestNormalMap))
	writeTextureFile(t, dir, "Textures/HighLightMap.dds", efmiSolidDDS(32, efmiTestLightMap))
	if err := os.WriteFile(filepath.Join(dir, "mod.ini"), []byte(`[Constants]
global persist $T = 1
[KeySwapT]
key = 9
type = cycle
$T = 0,1
[CommandList_Draw_Component3]
run = CommandList\EFMIv1\OverrideTextures
ib = ref Resource_Component3_IB
vb0 = ref Resource_Component3_VB0
vb1 = ref Resource_Component3_VB1
vb2 = ref Resource_Component3_VB2
ps-t11 = Resource_HighLightMap
ps-t13 = Resource_D
ps-t14 = Resource_L
ps-t15 = Resource_N1
if $T == 1
ps-t15 = Resource_N2
endif
drawindexed = 3, 0, 0
[Resource_D]
filename = Textures/D.png
[Resource_L]
filename = Textures/L.dds
[Resource_N1]
filename = Textures/N1.dds
[Resource_N2]
filename = Textures/N2.dds
[Resource_HighLightMap]
filename = Textures/HighLightMap.dds
`+efmiComponent3Resources), 0o600); err != nil {
		t.Fatal(err)
	}

	result := loadViewerDir(t, dir).result
	if result.MaterialProfile != "efmi" || len(result.Meshes) != 1 {
		t.Fatalf("profile = %q, meshes = %#v", result.MaterialProfile, result.Meshes)
	}
	mesh := result.Meshes[0]
	if !strings.Contains(lightMapKey(mesh), "L.dds") {
		t.Fatalf("light map = %q", lightMapKey(mesh))
	}
	for value, want := range map[string]string{"0": "N1.dds", "1": "N2.dds"} {
		got := viewerEvalResolveTexture(mesh.NormalMapVariants, mesh.NormalMapKey, map[string]any{"T": value})
		if !strings.Contains(got, want) {
			t.Fatalf("T=%s normal = %q, want %s", value, got, want)
		}
	}
}

func TestLoadModViewerRecoversEFMIDumpMapsFromComponentList(t *testing.T) {
	dir := t.TempDir()
	writeEFMIComponentBuffers(t, dir, "Component3")
	files := map[string]color.NRGBA{
		"Components-3-4 t=11111111.dds":   efmiTestLightMap,
		"Components-3-4 t=22222222.dds":   efmiTestNormalMap,
		"Components-3-4 t=33333333.dds":   efmiTestGrayMask,
		"Components-3-4-5 t=44444444.dds": efmiTestLightMap,
		"Components-9 t=55555555.dds":     efmiTestNormalMap,
	}
	ini := `[Constants]
global $required_efmi_version = 1.41
[CommandList_Draw_Component3]
run = CommandList\EFMIv1\OverrideTextures
ib = ref Resource_Component3_IB
vb0 = ref Resource_Component3_VB0
vb1 = ref Resource_Component3_VB1
vb2 = ref Resource_Component3_VB2
drawindexed = 3, 0, 0
[Resource_Diffuse]
filename = Textures/Components-3-4 t=00000000 BC7-sRGB.dds
[TextureOverride_Diffuse]
hash = 00000000
this = Resource_Diffuse
`
	writeTextureFile(t, dir, "Textures/Components-3-4 t=00000000 BC7-sRGB.dds", encodeWwmiColorDDS(32))
	index := 0
	for name, pixel := range files {
		writeTextureFile(t, dir, "Textures/"+name, efmiSolidDDS(32, pixel))
		suffix := string(rune('A' + index))
		ini += "[Resource_T" + suffix + "]\nfilename = Textures/" + name + "\n"
		ini += "[TextureOverride_T" + suffix + "]\nhash = " + strings.TrimSuffix(
			name[strings.Index(name, "t=")+2:],
			".dds",
		) +
			"\nthis = Resource_T" + suffix + "\n"
		index++
	}
	if err := os.WriteFile(filepath.Join(dir, "mod.ini"), []byte(ini+efmiComponent3Resources), 0o600); err != nil {
		t.Fatal(err)
	}

	result := loadViewerDir(t, dir).result
	if len(result.Meshes) != 1 {
		t.Fatalf("meshes = %#v", result.Meshes)
	}
	mesh := result.Meshes[0]
	if !strings.Contains(texKey(mesh), "t=00000000") {
		t.Fatalf("diffuse = %q", texKey(mesh))
	}
	if !strings.Contains(lightMapKey(mesh), "t=11111111") {
		t.Fatalf("light map = %q", lightMapKey(mesh))
	}
	if mesh.NormalMapKey == nil || !strings.Contains(*mesh.NormalMapKey, "t=22222222") {
		t.Fatalf("normal map = %v", mesh.NormalMapKey)
	}
}

func TestLoadModViewerAssignsEFMIFaceMapsAndSkipsSharedNoise(t *testing.T) {
	dir := t.TempDir()
	writeEFMIComponentBuffers(t, dir, "Component3")

	const side = 64
	mask := make([]color.NRGBA, 0, side*side)
	highlight := slices.Repeat([]color.NRGBA{{A: 255}}, side*side)
	highlight[side*side/2] = color.NRGBA{R: 104, G: 104, B: 104, A: 255}
	for y := range side {
		for x := range side {
			green := uint8(0)
			if x < 8 || x >= side-8 || y < 8 {
				green = 255
			}
			mask = append(mask, color.NRGBA{R: 200, G: green, B: 60, A: 30})
		}
	}
	files := map[string][]byte{
		"Components-3-4 t=00000000 BC7-sRGB.dds":     encodeWwmiColorDDS(32),
		"Components-3-9 t=66666666 BC7-Linear.dds":   efmiDX10DDS(side, efmiDXGIBGRA8, mask),
		"Components-3-9 t=77777777 BC7-Linear.dds":   efmiDX10DDS(side, efmiDXGIBGRA8, highlight),
		"Components-1-2-3-4-5 t=88888888 Linear.dds": efmiSolidDDS(32, efmiTestNormalMap),
	}
	ini := `[Constants]
global $required_efmi_version = 1.41
[CommandList_Draw_Component3]
run = CommandList\EFMIv1\OverrideTextures
ib = ref Resource_Component3_IB
vb0 = ref Resource_Component3_VB0
vb1 = ref Resource_Component3_VB1
vb2 = ref Resource_Component3_VB2
drawindexed = 3, 0, 0
`
	index := 0
	for name, data := range files {
		writeTextureFile(t, dir, "Textures/"+name, data)
		suffix := string(rune('A' + index))
		hash := name[strings.Index(name, "t=")+2:][:8]
		ini += "[Resource_T" + suffix + "]\nfilename = Textures/" + name + "\n"
		ini += "[TextureOverride_T" + suffix + "]\nhash = " + hash + "\nthis = Resource_T" + suffix + "\n"
		index++
	}
	if err := os.WriteFile(filepath.Join(dir, "mod.ini"), []byte(ini+efmiComponent3Resources), 0o600); err != nil {
		t.Fatal(err)
	}

	result := loadViewerDir(t, dir).result
	if len(result.Meshes) != 1 {
		t.Fatalf("meshes = %#v", result.Meshes)
	}
	mesh := result.Meshes[0]
	if mesh.MaterialMapKey == nil || !strings.Contains(*mesh.MaterialMapKey, "t=66666666") {
		t.Fatalf("face mask = %v", mesh.MaterialMapKey)
	}
	if mesh.HighlightMapKey == nil || !strings.Contains(*mesh.HighlightMapKey, "t=77777777") {
		t.Fatalf("highlight map = %v", mesh.HighlightMapKey)
	}
	if mesh.NormalMapKey != nil {
		t.Fatalf("shared noise bound as normal map: %q", *mesh.NormalMapKey)
	}
}

func encodeWwmiColorDDS(side uint32) []byte {
	pixels := make([]color.NRGBA, 0, side*side)
	for y := range side {
		for x := range side {
			pixels = append(pixels, color.NRGBA{R: uint8(x * 8), G: uint8(y * 3), B: uint8((x + y) * 4), A: 255})
		}
	}
	return efmiDX10DDS(side, efmiDXGIBGRA8SRGB, pixels)
}

func TestDecodeModelViewerEFMINormalsFollowsCompactedVertices(t *testing.T) {
	const stride = 24
	combined := make([]byte, stride*3)
	binary.LittleEndian.PutUint32(combined[0*stride+12:], 511)
	binary.LittleEndian.PutUint32(combined[2*stride+12:], 511<<10)

	geometry := &modelViewerGeometry{VertexCount: 2, SourceIndices: []uint32{2, 0}}
	normals := decodeModelViewerEFMINormals(combined, stride, geometry)
	if !slices.Equal(normals, []float32{0, 1, 0, 1, 0, 0}) {
		t.Fatalf("normals = %v", normals)
	}

	geometry.SourceIndices = []uint32{2, 3}
	if normals := decodeModelViewerEFMINormals(combined, stride, geometry); normals != nil {
		t.Fatalf("normals past the buffer = %v", normals)
	}
}

func TestEFMIIsDiffRampRejectsSpecularRamps(t *testing.T) {
	ramp := func(width, height int, darkAlpha, litAlpha uint8) *image.NRGBA {
		img := image.NewNRGBA(image.Rect(0, 0, width, height))
		for y := range height {
			img.SetNRGBA(0, y, color.NRGBA{A: darkAlpha})
			img.SetNRGBA(width-1, y, color.NRGBA{A: litAlpha})
		}
		return img
	}
	tests := []struct {
		name string
		img  *image.NRGBA
		want bool
	}{
		{"coverage in alpha", ramp(256, 2, 5, 254), true},
		{"2D specular ramp", ramp(1024, 32, 5, 254), false},
		{"opaque specular ramp", ramp(256, 2, 255, 255), false},
	}
	for _, test := range tests {
		if got := efmiIsDiffRamp(test.img); got != test.want {
			t.Errorf("%s: got %v", test.name, got)
		}
	}
}
