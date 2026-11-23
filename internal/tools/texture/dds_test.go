package texture

import (
	"context"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/myparsleycat/ddsutil"
)

// Mirrors the Rust sidecar's downscale_candidates_match_source_backend and
// target_selection_matches_source_backend tests.
func TestCalculateTextureResizeTargetMatchesSourceBackend(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		width      uint32
		height     uint32
		mode       textureResizeMode
		wantWidth  int
		wantHeight int
		wantFound  bool
	}{
		{"percent-60", 4096, 4096, textureResizeMode{percent: true, percentValue: 60}, 2048, 2048, true},
		{"custom-bounds", 4096, 4096, textureResizeMode{maxWidth: 3072, maxHeight: 2048}, 2048, 2048, true},
		{"no-candidate", 2048, 1024, textureResizeMode{percent: true, percentValue: 50}, 0, 0, false},
		{"below-minimum", 512, 512, textureResizeMode{percent: true, percentValue: 50}, 0, 0, false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			width, height, ok := calculateTextureResizeTarget(testCase.width, testCase.height, &testCase.mode)
			if ok != testCase.wantFound {
				t.Fatalf("found = %v, want %v", ok, testCase.wantFound)
			}
			if ok && (width != testCase.wantWidth || height != testCase.wantHeight) {
				t.Fatalf("target = (%d, %d), want (%d, %d)", width, height, testCase.wantWidth, testCase.wantHeight)
			}
		})
	}
}

func TestTextureCandidatesMatchSourceBackend(t *testing.T) {
	t.Parallel()
	if candidates := textureCandidates(4096, 4096); len(candidates) != 3 ||
		candidates[0] != [2]int{
			1024,
			1024,
		} || candidates[1] != [2]int{2048, 2048} || candidates[2] != [2]int{3072, 3072} {
		t.Fatalf("candidates = %v", candidates)
	}
	if candidates := textureCandidates(4096, 2048); len(candidates) != 1 || candidates[0] != [2]int{2048, 1024} {
		t.Fatalf("candidates = %v", candidates)
	}
	if candidates := textureCandidates(2048, 1024); len(candidates) != 0 {
		t.Fatalf("candidates = %v", candidates)
	}
}

func TestNormalizeTextureResizeDimension(t *testing.T) {
	t.Parallel()
	cases := []struct{ value, want int }{
		{0, 1024}, {512, 1024}, {1024, 1024}, {1535, 1024}, {1536, 2048}, {2048, 2048}, {4096, 4096},
	}
	for _, testCase := range cases {
		if got := normalizeTextureResizeDimension(testCase.value); got != testCase.want {
			t.Fatalf("normalize(%d) = %d, want %d", testCase.value, got, testCase.want)
		}
	}
}

func TestEncodeTexturePreservesDarkGrayInBC1ColorBlocks(t *testing.T) {
	t.Parallel()
	data := make([]byte, 4*4*4)
	for i := range 16 {
		copy(data[i*4:], []byte{45, 45, 45, 255})
	}
	copy(data[:4], []byte{0, 0, 0, 255})
	copy(data[4:8], []byte{255, 255, 255, 255})
	source := &ddsutil.SurfaceRgba8{Width: 4, Height: 4, Depth: 1, Layers: 1, Mipmaps: 1, Data: data}
	for _, format := range []ddsutil.ImageFormat{
		ddsutil.BC1RgbaUnorm, ddsutil.BC1RgbaUnormSrgb, ddsutil.BC2RgbaUnorm, ddsutil.BC3RgbaUnorm,
	} {
		t.Run(format.String(), func(t *testing.T) {
			t.Parallel()
			encoded, err := encodeTexture(t.Context(), source, format, ddsutil.MipmapsDisabled, nil)
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := encoded.DecodeRgba8()
			if err != nil {
				t.Fatal(err)
			}
			for i := 2; i < 16; i++ {
				pixel := decoded.Data[i*4:][:4]
				for _, channel := range pixel[:3] {
					if channel < 37 || channel > 53 {
						t.Fatalf("gray pixel %d = %v, want RGB within 8 of 45", i, pixel)
					}
				}
			}
		})
	}
}

func bc7AlphaRegressionBlock() [64]byte {
	var block [64]byte
	for i := range 16 {
		pixel := block[i*4:][:4]
		switch {
		case i < 3:
			copy(pixel, []byte{64, 254, 0, 0})
		case i < 14:
			copy(pixel, []byte{254, 0, 64, 0})
		default:
			copy(pixel, []byte{192, 64, 192, 254})
		}
	}
	return block
}

func bc7ColorRegressionBlock() [64]byte {
	var block [64]byte
	for i := range 12 {
		copy(block[i*4:], []byte{128, 254, 254, 255})
	}
	copy(block[48:], []byte{128, 128, 64, 255, 128, 254, 254, 255, 128, 64, 0, 255, 254, 254, 254, 255})
	return block
}

func TestEncodeTexturePreservesBC7DominantColor(t *testing.T) {
	t.Parallel()
	block := bc7ColorRegressionBlock()
	source := &ddsutil.SurfaceRgba8{Width: 4, Height: 4, Depth: 1, Layers: 1, Mipmaps: 1, Data: block[:]}
	encoded, err := encodeTexture(t.Context(), source, ddsutil.BC7RgbaUnorm, ddsutil.MipmapsDisabled, nil)
	if err != nil {
		t.Fatal(err)
	}
	decoded := assertBC7AlphaCoverage(t, encoded, block[:])
	assertBC7DominantColor(t, decoded, 4)
}

func assertBC7DominantColor(t *testing.T, rgba []byte, width int) {
	t.Helper()
	for i := range 12 {
		pixel := rgba[(i/4*width+i%4)*4:][:4]
		if pixel[0] < 120 || pixel[0] > 136 || pixel[1] < 246 || pixel[2] < 246 {
			t.Fatalf("dominant pixel %d = %v, want RGB within 8 of (128, 254, 254)", i, pixel)
		}
	}
}

func TestEncodeTexturePreservesBC7AlphaCoverage(t *testing.T) {
	t.Parallel()
	regression := bc7AlphaRegressionBlock()
	nearCutoff := regression
	for i := 3; i < len(nearCutoff); i += 4 {
		if nearCutoff[i] == 0 {
			nearCutoff[i] = 127
		} else {
			nearCutoff[i] = 128
		}
	}
	for _, tt := range []struct {
		name  string
		block [64]byte
	}{
		{"transparent mask", regression},
		{"adjacent alpha levels", nearCutoff},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			source := &ddsutil.SurfaceRgba8{Width: 4, Height: 4, Depth: 1, Layers: 1, Mipmaps: 1, Data: tt.block[:]}
			encoded, err := encodeTexture(t.Context(), source, ddsutil.BC7RgbaUnorm, ddsutil.MipmapsDisabled, nil)
			if err != nil {
				t.Fatal(err)
			}
			assertBC7AlphaCoverage(t, encoded, tt.block[:])
		})
	}
}

func assertBC7AlphaCoverage(t *testing.T, encoded *ddsutil.Surface, rgba []byte) []byte {
	t.Helper()
	decoded, err := encoded.DecodeRgba8()
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded.Data) != len(rgba) {
		t.Fatalf("decoded bytes = %d, want %d", len(decoded.Data), len(rgba))
	}
	for i := 3; i < len(rgba); i += 4 {
		if (decoded.Data[i] >= 128) != (rgba[i] >= 128) {
			t.Fatalf("pixel %d changed sides of the alpha cutoff: input %d, decoded %d", i/4, rgba[i], decoded.Data[i])
		}
	}
	return decoded.Data
}

// Mirrors the Rust sidecar's encodes_decodes_and_resizes_a_dds_texture test.
func TestExecuteTextureResizeEncodeDecodeAndResizeDDS(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	pngPath, ddsPath := filepath.Join(root, "source.png"), filepath.Join(root, "source.dds")
	pixels := make([]uint8, 2048*2048*4)
	for index := range pixels {
		pixels[index] = 127
	}
	if err := saveTexturePNG(pngPath, pixels, 2048, 2048); err != nil {
		t.Fatal(err)
	}
	if _, err := encodePNGToDDS(
		t.Context(),
		pngPath,
		ddsPath,
		"DXGI_FORMAT_BC1_UNORM",
		nil,
		false,
		true,
		nil,
	); err != nil {
		t.Fatal(err)
	}
	result, err := executeTextureResize(context.Background(), nil, textureResizeRequest{
		TargetPath: ddsPath, Mode: "percent", Operation: "resize", Percent: 50, Backup: true,
		// A stale output format from an earlier conversion must not apply to a plain resize.
		OutputFormat: "DXGI_FORMAT_R8_UNORM",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Updated != 1 || result.Failed != 0 || len(result.Files) != 1 {
		t.Fatalf("result = %#v", result)
	}
	if result.Files[0].OutputWidth != 1024 || result.Files[0].OutputHeight != 1024 {
		t.Fatalf("file result = %#v", result.Files[0])
	}
	if !regularFile(ddsPath + ".bak") {
		t.Fatal("backup DDS was not created")
	}
	decoded, err := decodeDDSToPng(ddsPath, filepath.Join(root, "decoded.png"))
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Width != 1024 || decoded.Height != 1024 || decoded.Format != "DXGI_FORMAT_BC1_UNORM" {
		t.Fatalf("decoded = %#v", decoded)
	}
	if !regularFile(filepath.Join(root, "decoded.png")) {
		t.Fatal("decoded PNG was not created")
	}
}

func TestExecuteResizeSkipsConvertWithSameFormat(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	pngPath, ddsPath := filepath.Join(root, "source.png"), filepath.Join(root, "source.dds")
	pixels := make([]uint8, 2048*2048*4)
	for index := 0; index < len(pixels); index += 4 {
		pixels[index], pixels[index+1], pixels[index+2], pixels[index+3] = 64, 96, 128, 255
	}
	if err := saveTexturePNG(pngPath, pixels, 2048, 2048); err != nil {
		t.Fatal(err)
	}
	if _, err := encodePNGToDDS(
		t.Context(),
		pngPath,
		ddsPath,
		"DXGI_FORMAT_BC1_UNORM",
		nil,
		false,
		false,
		nil,
	); err != nil {
		t.Fatal(err)
	}
	result, err := executeTextureResize(context.Background(), nil, textureResizeRequest{
		TargetPath: ddsPath, Mode: "percent", Operation: "convert",
		OutputFormat: "DXGI_FORMAT_BC1_UNORM", Backup: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Skipped != 1 || result.Updated != 0 || len(result.Files) != 1 {
		t.Fatalf("result = %#v", result)
	}
	if result.Files[0].Message == nil ||
		*result.Files[0].Message != "Selected output format matches the source format." {
		t.Fatalf("message = %v", result.Files[0].Message)
	}
	if regularFile(ddsPath + ".bak") {
		t.Fatal("convert skip must not create a backup")
	}
}

func TestExecuteResizeRejectsNonDDSFile(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "texture.png")
	if err := os.WriteFile(path, []byte("not a dds"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := executeTextureResize(context.Background(), nil, textureResizeRequest{
		TargetPath: path, Mode: "percent", Operation: "resize", Percent: 50,
	})
	if err == nil || !strings.HasPrefix(err.Error(), "Target file '") ||
		!strings.HasSuffix(err.Error(), "' must be a DDS texture.") {
		t.Fatalf("error = %v", err)
	}
}

func TestParseTextureOutputFormatRoundTrip(t *testing.T) {
	t.Parallel()

	// Spelled out independently of textureFormatsByName so a swapped, dropped, or unlisted entry fails.
	want := map[string]ddsutil.ImageFormat{
		"DXGI_FORMAT_R8_UNORM":            ddsutil.R8Unorm,
		"DXGI_FORMAT_R8_SNORM":            ddsutil.R8Snorm,
		"DXGI_FORMAT_R8G8_UNORM":          ddsutil.Rg8Unorm,
		"DXGI_FORMAT_R8G8_SNORM":          ddsutil.Rg8Snorm,
		"DXGI_FORMAT_R8G8B8A8_UNORM":      ddsutil.Rgba8Unorm,
		"DXGI_FORMAT_R8G8B8A8_UNORM_SRGB": ddsutil.Rgba8UnormSrgb,
		"DXGI_FORMAT_R8G8B8A8_SNORM":      ddsutil.Rgba8Snorm,
		"DXGI_FORMAT_R16_UNORM":           ddsutil.R16Unorm,
		"DXGI_FORMAT_R16_SNORM":           ddsutil.R16Snorm,
		"DXGI_FORMAT_R16_FLOAT":           ddsutil.R16Float,
		"DXGI_FORMAT_R16G16_UNORM":        ddsutil.Rg16Unorm,
		"DXGI_FORMAT_R16G16_SNORM":        ddsutil.Rg16Snorm,
		"DXGI_FORMAT_R16G16_FLOAT":        ddsutil.Rg16Float,
		"DXGI_FORMAT_R16G16B16A16_UNORM":  ddsutil.Rgba16Unorm,
		"DXGI_FORMAT_R16G16B16A16_SNORM":  ddsutil.Rgba16Snorm,
		"DXGI_FORMAT_R16G16B16A16_FLOAT":  ddsutil.Rgba16Float,
		"DXGI_FORMAT_R32_FLOAT":           ddsutil.R32Float,
		"DXGI_FORMAT_R32G32_FLOAT":        ddsutil.Rg32Float,
		"DXGI_FORMAT_R32G32B32_FLOAT":     ddsutil.Rgb32Float,
		"DXGI_FORMAT_R32G32B32A32_FLOAT":  ddsutil.Rgba32Float,
		"DXGI_FORMAT_B8G8R8A8_UNORM":      ddsutil.Bgra8Unorm,
		"DXGI_FORMAT_B8G8R8A8_UNORM_SRGB": ddsutil.Bgra8UnormSrgb,
		"DXGI_FORMAT_B4G4R4A4_UNORM":      ddsutil.Bgra4Unorm,
		"DXGI_FORMAT_B5G5R5A1_UNORM":      ddsutil.Bgr5A1Unorm,
		"DXGI_FORMAT_BC1_UNORM":           ddsutil.BC1RgbaUnorm,
		"DXGI_FORMAT_BC1_UNORM_SRGB":      ddsutil.BC1RgbaUnormSrgb,
		"DXGI_FORMAT_BC2_UNORM":           ddsutil.BC2RgbaUnorm,
		"DXGI_FORMAT_BC2_UNORM_SRGB":      ddsutil.BC2RgbaUnormSrgb,
		"DXGI_FORMAT_BC3_UNORM":           ddsutil.BC3RgbaUnorm,
		"DXGI_FORMAT_BC3_UNORM_SRGB":      ddsutil.BC3RgbaUnormSrgb,
		"DXGI_FORMAT_BC4_UNORM":           ddsutil.BC4RUnorm,
		"DXGI_FORMAT_BC4_SNORM":           ddsutil.BC4RSnorm,
		"DXGI_FORMAT_BC5_UNORM":           ddsutil.BC5RgUnorm,
		"DXGI_FORMAT_BC5_SNORM":           ddsutil.BC5RgSnorm,
		"DXGI_FORMAT_BC6H_UF16":           ddsutil.BC6hRgbUfloat,
		"DXGI_FORMAT_BC6H_SF16":           ddsutil.BC6hRgbSfloat,
		"DXGI_FORMAT_BC7_UNORM":           ddsutil.BC7RgbaUnorm,
		"DXGI_FORMAT_BC7_UNORM_SRGB":      ddsutil.BC7RgbaUnormSrgb,
	}
	if !maps.Equal(textureFormatsByName, want) {
		t.Fatalf("textureFormatsByName = %v, want %v", textureFormatsByName, want)
	}
	for name, format := range want {
		got, err := parseTextureOutputFormat(name)
		if err != nil || got != format {
			t.Fatalf("parseTextureOutputFormat(%q) = %v, %v, want %v", name, got, err, format)
		}
		if textureImageFormatName(format) != name {
			t.Fatalf("textureImageFormatName(%v) = %q, want %q", format, textureImageFormatName(format), name)
		}
	}

	if _, err := parseTextureOutputFormat("DXGI_FORMAT_UNKNOWN"); err == nil {
		t.Fatal("unknown format must be rejected")
	}
}

func writeDXGITestDDS(t *testing.T, path string, params ddsutil.DxgiParams, fill func(data []byte)) {
	t.Helper()
	dds, err := ddsutil.NewDxgi(params)
	if err != nil {
		t.Fatal(err)
	}
	fill(dds.Data)
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := dds.Write(file); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func convertTestDDS(t *testing.T, path, format string) *ddsutil.Dds {
	t.Helper()
	result, err := executeTextureResize(context.Background(), nil, textureResizeRequest{
		TargetPath: path, Mode: "percent", Operation: "convert", OutputFormat: format,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Updated != 1 || len(result.Files) != 1 {
		t.Fatalf("result = %#v", result)
	}
	dds, err := readDDSFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return dds
}

func TestExecuteConvertPreservesArrayAndCubeLayout(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name          string
		layers        uint32
		cube          bool
		wantArraySize uint32
	}{
		{name: "six-layer 2D array", layers: 6, wantArraySize: 6},
		{name: "cube array", layers: 12, cube: true, wantArraySize: 2},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "array.dds")
			var caps2 ddsutil.Caps2
			if testCase.cube {
				caps2 = ddsutil.Caps2CUBEMAP | ddsutil.Caps2CUBEMAP_ALLFACES
			}
			writeDXGITestDDS(t, path, ddsutil.DxgiParams{
				Height: 4, Width: 4, Format: ddsutil.DxgiFormatR8G8B8A8_UNorm, ArrayLayers: testCase.layers,
				Caps2: caps2, IsCubemap: testCase.cube, ResourceDimension: ddsutil.D3D10ResourceDimensionTexture2D,
				AlphaMode: ddsutil.AlphaModeStraight,
			}, func(data []byte) {
				for index := range data {
					data[index] = byte(index)
				}
			})

			dds := convertTestDDS(t, path, "DXGI_FORMAT_B8G8R8A8_UNORM")
			if dds.Header10 == nil {
				t.Fatal("converted DDS has no DX10 header")
			}
			if got := dds.Header10.MiscFlag.Contains(ddsutil.MiscFlagTEXTURECUBE); got != testCase.cube {
				t.Fatalf("cube flag = %v, want %v", got, testCase.cube)
			}
			if got := dds.Header.Caps2.Contains(ddsutil.Caps2CUBEMAP); got != testCase.cube {
				t.Fatalf("caps2 cube = %v, want %v", got, testCase.cube)
			}
			if dds.Header10.ArraySize != testCase.wantArraySize {
				t.Fatalf("array size = %d, want %d", dds.Header10.ArraySize, testCase.wantArraySize)
			}
		})
	}
}

func TestExecuteConvertKeepsAuthoredMipmaps(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "mips.dds")
	writeDXGITestDDS(t, path, ddsutil.DxgiParams{
		Height: 4, Width: 4, Format: ddsutil.DxgiFormatR8G8B8A8_UNorm, MipmapLevels: 2,
		ResourceDimension: ddsutil.D3D10ResourceDimensionTexture2D, AlphaMode: ddsutil.AlphaModeStraight,
	}, func(data []byte) {
		// Mip 0 is red and mip 1 green, so a regenerated mip 1 would come out red.
		for offset := 0; offset < len(data); offset += 4 {
			if offset < 4*4*4 {
				copy(data[offset:], []byte{255, 0, 0, 255})
			} else {
				copy(data[offset:], []byte{0, 255, 0, 255})
			}
		}
	})

	dds := convertTestDDS(t, path, "DXGI_FORMAT_B8G8R8A8_UNORM")
	decoded, err := ddsutil.DecodeDds(dds)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Mipmaps != 2 {
		t.Fatalf("mipmaps = %d, want 2", decoded.Mipmaps)
	}
	mip := decoded.Get(0, 0, 1)
	if len(mip) < 4 || mip[0] != 0 || mip[1] != 255 {
		t.Fatalf("mip 1 first pixel = %v, want green", mip[:min(len(mip), 4)])
	}
}
