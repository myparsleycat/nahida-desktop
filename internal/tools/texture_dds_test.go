package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
		candidates[0] != [2]int{1024, 1024} || candidates[1] != [2]int{2048, 2048} || candidates[2] != [2]int{3072, 3072} {
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
	if _, err := encodePNGToDDS(pngPath, ddsPath, "DXGI_FORMAT_BC1_UNORM", false, true); err != nil {
		t.Fatal(err)
	}
	result, err := executeTextureResize(context.Background(), textureResizeRequest{
		TargetPath: ddsPath, Mode: "percent", Operation: "resize", Percent: 50, Backup: true,
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
	if _, err := encodePNGToDDS(pngPath, ddsPath, "DXGI_FORMAT_BC1_UNORM", false, false); err != nil {
		t.Fatal(err)
	}
	result, err := executeTextureResize(context.Background(), textureResizeRequest{
		TargetPath: ddsPath, Mode: "percent", Operation: "convert",
		OutputFormat: "DXGI_FORMAT_BC1_UNORM", Backup: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Skipped != 1 || result.Updated != 0 || len(result.Files) != 1 {
		t.Fatalf("result = %#v", result)
	}
	if result.Files[0].Message == nil || *result.Files[0].Message != "Selected output format matches the source format." {
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
	_, err := executeTextureResize(context.Background(), textureResizeRequest{
		TargetPath: path, Mode: "percent", Operation: "resize", Percent: 50,
	})
	if err == nil || !strings.HasPrefix(err.Error(), "Target file '") || !strings.HasSuffix(err.Error(), "' must be a DDS texture.") {
		t.Fatalf("error = %v", err)
	}
}

func TestParseTextureOutputFormatRoundTrip(t *testing.T) {
	t.Parallel()
	for name, format := range textureFormatsByName {
		if textureImageFormatName(format) != name {
			t.Fatalf("format %v maps back to %q, want %q", format, textureImageFormatName(format), name)
		}
	}
	if _, err := parseTextureOutputFormat("DXGI_FORMAT_UNKNOWN"); err == nil {
		t.Fatal("unknown format must be rejected")
	}
}
