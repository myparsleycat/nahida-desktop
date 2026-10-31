package modelviewer

import (
	"encoding/binary"
	"image"
	"image/color"
	"image/draw"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/myparsleycat/ddsutil"
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
		{
			File:  "Textures/Components-0-1-2-3-4-5 t=beef0012.dds",
			SRGB:  true,
			Area:  2048 * 2048,
			Bytes: 4_194_452,
			Order: 0,
		},
		{File: "Textures/Components-3 t=beef001c.dds", Area: 512 * 512, Bytes: 262_292, Order: 1},
	})
	if got != "Textures/Components-3 t=beef001c.dds" {
		t.Fatalf("pickWwmiDumpDiffuse = %q", got)
	}
}

func TestPickWwmiDumpDiffusePrefersSharedAtlasOverSmallerSharedEffect(t *testing.T) {
	t.Parallel()
	got := pickWwmiDumpDiffuse([]wwmiDumpCandidate{
		{File: "Textures/Components-3-4 t=beef0001.dds", SRGB: true, Area: 512 * 512},
		{File: "Textures/Components-0-1-3-4 t=beef0002.dds", SRGB: true, Area: 4096 * 4096},
		{File: "Textures/Components-0-1-3 t=beef0003.dds", SRGB: true, Area: 4096 * 4096},
		{File: "Textures/Components-3-4 t=beef0004.dds", Area: 8192 * 8192},
	})
	if got != "Textures/Components-0-1-3 t=beef0003.dds" {
		t.Fatalf("pickWwmiDumpDiffuse = %q", got)
	}
}

func TestPickWwmiDumpDiffuseSkipsSolidPaletteOnlyForPaintedRival(t *testing.T) {
	t.Parallel()
	const (
		palette = "Textures/Components-1 t=9750205d.dds"
		atlas   = "Textures/Components-1-10-11 t=b78ceea6.dds"
	)
	area := 1024 * 1024
	tests := []struct {
		name       string
		dedicated  float64
		shared     float64
		sharedArea int
		analyzed   bool
		want       string
	}{
		{"painted shared atlas wins", 0.83, 0.1, area, true, atlas},
		{"smaller painted atlas loses", 0.83, 0.1, area / 4, true, palette},
		{"flat-shaded rival loses", 0.83, 0.8, area, true, palette},
		{"soft skin map stays", 0.46, 0.1, area, true, palette},
		{"unanalyzed rival loses", 0.83, 0, area, false, palette},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := pickWwmiDumpDiffuse([]wwmiDumpCandidate{
				{File: palette, SRGB: true, Area: area, Analyzed: true, ColorBlocks: test.dedicated},
				{File: atlas, SRGB: true, Area: test.sharedArea, Analyzed: test.analyzed, ColorBlocks: test.shared},
			})
			if got != test.want {
				t.Fatalf("pickWwmiDumpDiffuse = %q, want %q", got, test.want)
			}
		})
	}
}

func TestAnalyzeRGBAColorBlockFractionSeparatesSolidFromGradient(t *testing.T) {
	t.Parallel()
	const size = 64
	solid, gradient := make([]byte, size*size*4), make([]byte, size*size*4)
	for index := range size * size {
		copy(solid[index*4:], []byte{byte(index % 3 * 80), 40, 40, 255})
		copy(gradient[index*4:], []byte{byte(200 + index%50), byte(150 + index%37), byte(140 + index%29), 255})
	}
	if got := analyzeRGBA(solid, size, size).colorBlockFraction; got < wwmiPaletteColorBlocks {
		t.Fatalf("solid fraction = %v", got)
	}
	if got := analyzeRGBA(gradient, size, size).colorBlockFraction; got >= wwmiPaintedColorBlocks {
		t.Fatalf("gradient fraction = %v", got)
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

func TestWwmiHintMipmap(t *testing.T) {
	t.Parallel()
	cases := []struct {
		width, height uint32
		mipmaps       uint32
		wantMip       uint32
		wantW, wantH  uint32
	}{
		{4096, 4096, 14, 4, 256, 256},
		{4096, 4096, 1, 0, 256, 256},
		{4096, 4096, 2, 1, 256, 256},
		{128, 128, 8, 0, 128, 128},
		{math.MaxUint32, math.MaxUint32, 1, 0, 255, 255},
		{math.MaxUint32, math.MaxUint32, 32, 24, 255, 255},
	}
	for _, test := range cases {
		mip, width, height := wwmiHintMipmap(test.width, test.height, test.mipmaps)
		if mip != test.wantMip || width != test.wantW || height != test.wantH {
			t.Fatalf("wwmiHintMipmap(%d, %d, %d) = (%d, %d, %d), want (%d, %d, %d)",
				test.width, test.height, test.mipmaps, mip, width, height, test.wantMip, test.wantW, test.wantH)
		}
	}
}

func TestTextureAreaClampsUint32Overflow(t *testing.T) {
	t.Parallel()
	if got := textureArea(math.MaxUint32, math.MaxUint32); got != math.MaxInt {
		t.Fatalf("textureArea(MaxUint32, MaxUint32) = %d, want MaxInt", got)
	}
	header := make([]byte, 24)
	copy(header, pngSignature)
	copy(header[12:16], "IHDR")
	binary.BigEndian.PutUint32(header[16:20], math.MaxUint32)
	binary.BigEndian.PutUint32(header[20:24], math.MaxUint32)
	if got := pngIhdrArea(header); got != math.MaxInt {
		t.Fatalf("pngIhdrArea = %d, want MaxInt", got)
	}
}

func TestInspectWwmiTextureHintKeepsOversizedHeaderWithoutDecode(t *testing.T) {
	ddsPath := filepath.Join(t.TempDir(), "huge.dds")
	if err := os.WriteFile(ddsPath, encodeUncompressedDDSHeader(math.MaxUint32, math.MaxUint32), 0o600); err != nil {
		t.Fatal(err)
	}
	ddsHint := inspectWwmiTextureHint(ddsPath)
	if ddsHint == nil || ddsHint.Area != math.MaxInt || ddsHint.ColorSpace != "unknown" {
		t.Fatalf("dds hint = %#v", ddsHint)
	}

	pngHeader := make([]byte, 24)
	copy(pngHeader, pngSignature)
	copy(pngHeader[12:16], "IHDR")
	binary.BigEndian.PutUint32(pngHeader[16:20], math.MaxUint32)
	binary.BigEndian.PutUint32(pngHeader[20:24], math.MaxUint32)
	pngPath := filepath.Join(t.TempDir(), "huge.png")
	if err := os.WriteFile(pngPath, pngHeader, 0o600); err != nil {
		t.Fatal(err)
	}
	pngHint := inspectWwmiTextureHint(pngPath)
	if pngHint == nil || pngHint.Area != math.MaxInt || !pngHint.SRGB {
		t.Fatalf("png hint = %#v", pngHint)
	}
}

func TestInspectWwmiTextureHintKeepsHeaderAreaAfterDownsample(t *testing.T) {
	root := t.TempDir()
	const dim = 512
	flatPixels := make([]color.NRGBA, dim*dim)
	for index := range flatPixels {
		flatPixels[index] = color.NRGBA{R: 128, G: 64, B: 32, A: 255}
	}
	flatPath := filepath.Join(root, "flat.dds")
	if err := os.WriteFile(flatPath, encodeWwmiUncompressedDDS(dim, dim, flatPixels), 0o600); err != nil {
		t.Fatal(err)
	}
	flatInfo, err := os.Stat(flatPath)
	if err != nil {
		t.Fatal(err)
	}
	flatDecoded, err := decodeModelViewerDDSHint(flatPath, flatInfo.Size())
	if err != nil {
		t.Fatal(err)
	}
	if flatDecoded.Bounds().Dx() != 256 || flatDecoded.Bounds().Dy() != 256 {
		t.Fatalf("flat decode size = %dx%d, want 256x256", flatDecoded.Bounds().Dx(), flatDecoded.Bounds().Dy())
	}
	flat := inspectWwmiTextureHint(flatPath)
	if flat == nil || flat.Area != dim*dim || !flat.IsLikelyFlat || isLikelyWwmiDiffuse(*flat) {
		t.Fatalf("flat DDS hint = %#v", flat)
	}

	diffusePixels := make([]color.NRGBA, dim*dim)
	for y := range dim {
		for x := range dim {
			diffusePixels[y*dim+x] = color.NRGBA{
				R: uint8(x * 255 / (dim - 1)),
				G: uint8(y * 255 / (dim - 1)),
				B: 90,
				A: 255,
			}
		}
	}
	diffusePath := filepath.Join(root, "diffuse.dds")
	if err := os.WriteFile(diffusePath, encodeWwmiUncompressedDDS(dim, dim, diffusePixels), 0o600); err != nil {
		t.Fatal(err)
	}
	diffuse := inspectWwmiTextureHint(diffusePath)
	if diffuse == nil || diffuse.Area != dim*dim || diffuse.IsLikelyFlat || !isLikelyWwmiDiffuse(*diffuse) {
		t.Fatalf("diffuse DDS hint = %#v", diffuse)
	}
}

func TestAnalyzeRGBAImagePremultipliesNRGBA(t *testing.T) {
	t.Parallel()
	src := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	src.SetNRGBA(0, 0, color.NRGBA{R: 100, G: 0, B: 0, A: 128})
	premul := image.NewRGBA(image.Rect(0, 0, 1, 1))
	draw.Draw(premul, premul.Bounds(), src, image.Point{}, draw.Src)
	got := analyzeRGBAImage(src)
	want := analyzeRGBA(premul.Pix, 1, 1)
	if got != want {
		t.Fatalf("analyzeRGBAImage = %#v, want premultiplied %#v", got, want)
	}
}

func TestInspectWwmiTextureHintDecodesCompressedMipChain(t *testing.T) {
	root := t.TempDir()
	const dim = 512
	flatPixels := make([]color.NRGBA, dim*dim)
	for index := range flatPixels {
		flatPixels[index] = color.NRGBA{R: 128, G: 64, B: 32, A: 255}
	}
	flatPath := filepath.Join(root, "flat.dds")
	if err := os.WriteFile(flatPath, encodeWwmiBC1MipDDS(t, dim, dim, flatPixels), 0o600); err != nil {
		t.Fatal(err)
	}
	flatInfo, err := os.Stat(flatPath)
	if err != nil {
		t.Fatal(err)
	}
	flatDecoded, err := decodeModelViewerDDSHint(flatPath, flatInfo.Size())
	if err != nil {
		t.Fatal(err)
	}
	if flatDecoded.Bounds().Dx() != 256 || flatDecoded.Bounds().Dy() != 256 {
		t.Fatalf(
			"flat compressed decode size = %dx%d, want 256x256",
			flatDecoded.Bounds().Dx(),
			flatDecoded.Bounds().Dy(),
		)
	}
	flat := inspectWwmiTextureHint(flatPath)
	if flat == nil || flat.Area != dim*dim || !flat.IsLikelyFlat || isLikelyWwmiDiffuse(*flat) {
		t.Fatalf("flat compressed DDS hint = %#v", flat)
	}

	diffusePixels := make([]color.NRGBA, dim*dim)
	for y := range dim {
		for x := range dim {
			diffusePixels[y*dim+x] = color.NRGBA{
				R: uint8(x * 255 / (dim - 1)),
				G: uint8(y * 255 / (dim - 1)),
				B: 90,
				A: 255,
			}
		}
	}
	diffusePath := filepath.Join(root, "diffuse.dds")
	if err := os.WriteFile(diffusePath, encodeWwmiBC1MipDDS(t, dim, dim, diffusePixels), 0o600); err != nil {
		t.Fatal(err)
	}
	diffuse := inspectWwmiTextureHint(diffusePath)
	if diffuse == nil || diffuse.Area != dim*dim || diffuse.IsLikelyFlat || !isLikelyWwmiDiffuse(*diffuse) {
		t.Fatalf("diffuse compressed DDS hint = %#v", diffuse)
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
	if len(kept) != 4 || kept[0].file != "first" || kept[1].file != "normal" || kept[2].file != "light" ||
		kept[3].file != "last" {
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

func TestLoadModViewerLeavesGameTextureDrawWithoutOtherStateDump(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "Textures"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(dir, "Textures", "Components-0 t=0000abcd.dds"),
		encodeWwmiUncompressedDDS(2, 2, []color.NRGBA{
			{R: 255, A: 255}, {G: 255, A: 255}, {B: 255, A: 255}, {R: 255, G: 255, B: 255, A: 255},
		}),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	result := loadViewerMod(t, dir, `[Constants]
global $char = 0
[KeyChar]
type = cycle
$char = 0, 1
[CommandListSharedBuffers]
ib = ResourceBodyIB
vb0 = ResourcePos
vb1 = ResourceTc
[TextureOverrideComponent0]
if $char == 0
ps-t1 = ResourceSwapped
endif
run = CommandListSharedBuffers
if $char == 1
drawindexed = 3, 0, 0
else
drawindexed = 3, 0, 1
endif
[ResourceSwapped]
filename = Textures/Components-0 t=0000abcd.dds
`+viewerBodyResources)

	swapped := evaluateViewerTransport(result, map[string]any{"char": "0"})
	game := evaluateViewerTransport(result, map[string]any{"char": "1"})
	textures := func(state viewerEvalState) []string {
		var keys []string
		for _, mesh := range state.Meshes {
			if mesh.Visible {
				keys = append(keys, mesh.TexKey)
			}
		}
		return keys
	}
	if keys := textures(swapped); len(keys) != 1 || !strings.Contains(keys[0], "0000abcd") {
		t.Fatalf("swapped textures = %q", keys)
	}
	if keys := textures(game); len(keys) != 1 || keys[0] != "" {
		t.Fatalf("game textures = %q", keys)
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

func encodeWwmiBC1MipDDS(t *testing.T, width, height int, pixels []color.NRGBA) []byte {
	t.Helper()
	if len(pixels) != width*height {
		t.Fatalf("pixel count = %d, want %d", len(pixels), width*height)
	}
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			img.Set(x, y, pixels[y*width+x])
		}
	}
	encoded, err := ddsutil.DdsFromImage(
		img,
		ddsutil.BC1RgbaUnormSrgb,
		ddsutil.QualityFast,
		ddsutil.MipmapsGeneratedAutomatic,
	)
	if err != nil {
		t.Fatal(err)
	}
	if encoded.GetNumMipmapLevels() < 2 {
		t.Fatalf("mipmaps = %d, want at least 2", encoded.GetNumMipmapLevels())
	}
	raw, err := encoded.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
