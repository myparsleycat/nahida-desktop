package tools

import (
	"encoding/binary"
	"image"
	"image/color"
	"image/draw"
	"math"
	"os"
	"path/filepath"
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
			diffusePixels[y*dim+x] = color.NRGBA{R: uint8(x * 255 / (dim - 1)), G: uint8(y * 255 / (dim - 1)), B: 90, A: 255}
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
		t.Fatalf("flat compressed decode size = %dx%d, want 256x256", flatDecoded.Bounds().Dx(), flatDecoded.Bounds().Dy())
	}
	flat := inspectWwmiTextureHint(flatPath)
	if flat == nil || flat.Area != dim*dim || !flat.IsLikelyFlat || isLikelyWwmiDiffuse(*flat) {
		t.Fatalf("flat compressed DDS hint = %#v", flat)
	}

	diffusePixels := make([]color.NRGBA, dim*dim)
	for y := range dim {
		for x := range dim {
			diffusePixels[y*dim+x] = color.NRGBA{R: uint8(x * 255 / (dim - 1)), G: uint8(y * 255 / (dim - 1)), B: 90, A: 255}
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
	encoded, err := ddsutil.DdsFromImage(img, ddsutil.BC1RgbaUnormSrgb, ddsutil.QualityFast, ddsutil.MipmapsGeneratedAutomatic)
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
