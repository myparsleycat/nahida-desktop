package tools

import (
	"context"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestModelViewerTextureBindingAndPreparation(t *testing.T) {
	sections := parseModINI(`[TextureOverrideBody]
ib = ResourceBodyIB
ps-t0 = ResourceBodyDiffuse

[ResourceBodyDiffuse]
filename = body.png`)
	bindings := collectModelViewerTextureBindings(sections, nil)
	if len(bindings) != 1 || bindings[0].IBResourceName != "BodyIB" || bindings[0].DiffuseResourceName != "BodyDiffuse" {
		t.Fatalf("bindings = %#v", bindings)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "body.png")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	input := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	input.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 255})
	input.SetNRGBA(1, 0, color.NRGBA{G: 255, A: 0})
	if err = png.Encode(file, input); err != nil {
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
	prepared, err := prepareModelViewerTexture(context.Background(), path, "BodyDiffuse", "jpeg-safe", 85)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.mimeType != "image/png" || prepared.alphaMode != "MASK" || len(prepared.bytes) == 0 {
		t.Fatalf("prepared = %#v", prepared)
	}
}

func TestPrepareModelViewerTextureDecodesUncompressedDDS(t *testing.T) {
	path := filepath.Join(t.TempDir(), "body.dds")
	if err := os.WriteFile(path, encodeUncompressedDDS(color.NRGBA{R: 12, G: 34, B: 56, A: 255}), 0o600); err != nil {
		t.Fatal(err)
	}
	prepared, err := prepareModelViewerTexture(context.Background(), path, "BodyDiffuse", "jpeg-safe", 85)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.mimeType != "image/jpeg" || len(prepared.bytes) == 0 {
		t.Fatalf("prepared = %#v", prepared)
	}
}

func encodeUncompressedDDS(pixel color.NRGBA) []byte {
	header := make([]byte, 128)
	copy(header[:4], "DDS ")
	binary.LittleEndian.PutUint32(header[4:8], 124)
	binary.LittleEndian.PutUint32(header[8:12], 0x100f)
	binary.LittleEndian.PutUint32(header[12:16], 1)
	binary.LittleEndian.PutUint32(header[16:20], 1)
	binary.LittleEndian.PutUint32(header[20:24], 4)
	binary.LittleEndian.PutUint32(header[76:80], 32)
	binary.LittleEndian.PutUint32(header[80:84], 0x41)
	binary.LittleEndian.PutUint32(header[88:92], 32)
	binary.LittleEndian.PutUint32(header[92:96], 0x00ff0000)
	binary.LittleEndian.PutUint32(header[96:100], 0x0000ff00)
	binary.LittleEndian.PutUint32(header[100:104], 0x000000ff)
	binary.LittleEndian.PutUint32(header[104:108], 0xff000000)
	binary.LittleEndian.PutUint32(header[108:112], 0x1000)
	return append(header, pixel.B, pixel.G, pixel.R, pixel.A)
}

func TestModelViewerTextureRejectsDeclaredOversizedPNG(t *testing.T) {
	raw := make([]byte, 24)
	copy(raw, []byte{137, 80, 78, 71, 13, 10, 26, 10})
	copy(raw[12:16], "IHDR")
	binary.BigEndian.PutUint32(raw[16:20], 100_000)
	binary.BigEndian.PutUint32(raw[20:24], 100_000)
	path := filepath.Join(t.TempDir(), "oversized.png")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := prepareModelViewerTexture(context.Background(), path, "Diffuse", "png", 85)
	if err == nil || !strings.Contains(err.Error(), "input safety limit") {
		t.Fatalf("err = %v", err)
	}
}

func TestViewerPreviewTextureSize(t *testing.T) {
	t.Parallel()
	cases := []struct {
		width, height, wantW, wantH int
	}{
		{8192, 8192, 2048, 2048},
		{2048, 8192, 1024, 4096},
		{4096, 4096, 2048, 2048},
		{2048, 2048, 2048, 2048},
	}
	for _, test := range cases {
		gotW, gotH := viewerPreviewTextureSize(test.width, test.height)
		if gotW != test.wantW || gotH != test.wantH {
			t.Fatalf("viewerPreviewTextureSize(%d, %d) = %d×%d, want %d×%d",
				test.width, test.height, gotW, gotH, test.wantW, test.wantH)
		}
	}
}

func TestModelViewerTextureDownscalesToPreviewBudget(t *testing.T) {
	input := image.NewNRGBA(image.Rect(0, 0, 3000, 2000))
	output := downscaleModelViewerTexture(input, maxModelViewerTextureOutputPixels)
	if int64(output.Bounds().Dx())*int64(output.Bounds().Dy()) > maxModelViewerTextureOutputPixels || output.Bounds().Dx() >= input.Bounds().Dx() {
		t.Fatalf("input=%v output=%v", input.Bounds(), output.Bounds())
	}
}
