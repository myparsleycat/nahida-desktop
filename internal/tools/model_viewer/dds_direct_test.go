package modelviewer

import (
	"bytes"
	"context"
	"encoding/binary"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/myparsleycat/ddsutil"

	"nahida.live/desktop/internal/infra"
)

func TestInspectModelViewerDDS(t *testing.T) {
	path := filepath.Join(t.TempDir(), "body.dds")
	if err := os.WriteFile(path, encodeModelViewerBC1DDS(t, 4096, 2048, 3, 0), 0o600); err != nil {
		t.Fatal(err)
	}
	metadata, err := inspectModelViewerDDS(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if metadata.Format != "bc1-unorm" || metadata.Width != 4096 || metadata.Height != 2048 ||
		metadata.MipCount != 3 {
		t.Fatalf("metadata = %#v", metadata)
	}
}

func TestModelViewerDDSFormat(t *testing.T) {
	tests := map[ddsutil.ImageFormat]string{
		ddsutil.BC1RgbaUnorm:     "bc1-unorm",
		ddsutil.BC1RgbaUnormSrgb: "bc1-unorm-srgb",
		ddsutil.BC2RgbaUnorm:     "bc2-unorm",
		ddsutil.BC2RgbaUnormSrgb: "bc2-unorm-srgb",
		ddsutil.BC3RgbaUnorm:     "bc3-unorm",
		ddsutil.BC3RgbaUnormSrgb: "bc3-unorm-srgb",
		ddsutil.BC4RUnorm:        "bc4-unorm",
		ddsutil.BC4RSnorm:        "bc4-snorm",
		ddsutil.BC5RgUnorm:       "bc5-unorm",
		ddsutil.BC5RgSnorm:       "bc5-snorm",
		ddsutil.BC6hRgbUfloat:    "bc6h-ufloat",
		ddsutil.BC6hRgbSfloat:    "bc6h-sfloat",
		ddsutil.BC7RgbaUnorm:     "bc7-unorm",
		ddsutil.BC7RgbaUnormSrgb: "bc7-unorm-srgb",
	}
	for input, expected := range tests {
		if actual, ok := modelViewerDDSFormat(input); !ok || actual != expected {
			t.Errorf("modelViewerDDSFormat(%v) = %q, %t; want %q, true", input, actual, ok, expected)
		}
	}
}

func TestPrepareModelViewerDDSPreviewCopiesCenteredCompressedBlocks(t *testing.T) {
	const width, height = uint32(32), uint32(16)
	blocksX, blocksY := (width+3)/4, (height+3)/4
	data := make([]byte, int(blocksX*blocksY*16))
	for y := range blocksY {
		for x := range blocksX {
			value := byte(y*blocksX + x)
			start := int((y*blocksX + x) * 16)
			for offset := range 16 {
				data[start+offset] = value
			}
		}
	}
	path := writeModelViewerDDSurface(t, width, height, ddsutil.BC7RgbaUnorm, data)
	metadata, err := inspectModelViewerDDS(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := prepareModelViewerDDSPreviewWithLimit(context.Background(), path, metadata, 8)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := ddsutil.NewDdsReader(bytes.NewReader(preview), int64(len(preview)))
	if err != nil {
		t.Fatal(err)
	}
	actual := reader.Metadata()
	if actual.Width != 8 || actual.Height != 4 || actual.Mipmaps != 1 || actual.ImageFormat != ddsutil.BC7RgbaUnorm {
		t.Fatalf("preview metadata = %#v", actual)
	}
	surface, err := reader.ReadMip(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	want := append(bytes.Repeat([]byte{18}, 16), bytes.Repeat([]byte{22}, 16)...)
	if !bytes.Equal(surface.Data, want) {
		t.Fatalf("preview blocks = %v, want %v", surface.Data, want)
	}
}

func TestPrepareModelViewerDDSPreviewSamplesAcrossReadWindows(t *testing.T) {
	const width, height = uint32(65_536), uint32(4)
	blocksX := (width + 3) / 4
	data := make([]byte, int(blocksX*16))
	for x := range blocksX {
		data[x*16] = byte(x / 4096)
	}
	path := writeModelViewerDDSurface(t, width, height, ddsutil.BC7RgbaUnorm, data)
	metadata, err := inspectModelViewerDDS(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := prepareModelViewerDDSPreviewWithLimit(context.Background(), path, metadata, 16)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := ddsutil.NewDdsReader(bytes.NewReader(preview), int64(len(preview)))
	if err != nil {
		t.Fatal(err)
	}
	surface, err := reader.ReadMip(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{0, 1, 2, 3}
	for block, expected := range want {
		if actual := surface.Data[block*16]; actual != expected {
			t.Fatalf("preview block %d = %d, want %d", block, actual, expected)
		}
	}
}

func TestPrepareModelViewerDDSPreviewReusesExistingMip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mipped.dds")
	if err := os.WriteFile(path, encodeModelViewerBC1DDS(t, 16, 16, 3, 1), 0o600); err != nil {
		t.Fatal(err)
	}
	metadata, err := inspectModelViewerDDS(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	plan, needed := modelViewerDDSPreviewPlanFor(metadata, 8)
	if !needed || !plan.copyMip || plan.sourceMip != 1 || plan.width != 8 || plan.height != 8 {
		t.Fatalf("preview plan = %#v, needed=%t", plan, needed)
	}

	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		t.Fatal(err)
	}
	sourceReader, err := ddsutil.NewDdsReader(file, info.Size())
	if err != nil {
		t.Fatal(err)
	}
	source, err := sourceReader.ReadMip(0, 1)
	if err != nil {
		t.Fatal(err)
	}

	preview, err := prepareModelViewerDDSPreviewWithLimit(context.Background(), path, metadata, 8)
	if err != nil {
		t.Fatal(err)
	}
	previewReader, err := ddsutil.NewDdsReader(bytes.NewReader(preview), int64(len(preview)))
	if err != nil {
		t.Fatal(err)
	}
	actual, err := previewReader.ReadMip(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if actual.Width != 8 || actual.Height != 8 || !bytes.Equal(actual.Data, source.Data) {
		t.Fatalf("preview mip = %dx%d data=%v, source=%v", actual.Width, actual.Height, actual.Data, source.Data)
	}
}

func TestPrepareModelViewerDDSPreviewDecimatesOddFinalMip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "odd-mips.dds")
	if err := os.WriteFile(path, encodeModelViewerBC1DDS(t, 15, 9, 2, 1), 0o600); err != nil {
		t.Fatal(err)
	}
	metadata, err := inspectModelViewerDDS(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := prepareModelViewerDDSPreviewWithLimit(context.Background(), path, metadata, 3)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := ddsutil.NewDdsReader(bytes.NewReader(preview), int64(len(preview)))
	if err != nil {
		t.Fatal(err)
	}
	actual, err := reader.ReadMip(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if actual.Width != 3 || actual.Height != 2 || len(actual.Data) != 8 || actual.Data[0] != 0xff ||
		actual.Data[1] != 0xff {
		t.Fatalf("preview mip = %dx%d data=%v", actual.Width, actual.Height, actual.Data)
	}
}

func writeModelViewerDDSurface(
	tb testing.TB,
	width, height uint32,
	format ddsutil.ImageFormat,
	data []byte,
) string {
	tb.Helper()
	surface := &ddsutil.Surface{
		Width: width, Height: height, Depth: 1, Layers: 1, Mipmaps: 1, ImageFormat: format, Data: data,
	}
	dds, err := surface.ToDds()
	if err != nil {
		tb.Fatal(err)
	}
	var encoded bytes.Buffer
	if err = dds.Write(&encoded); err != nil {
		tb.Fatal(err)
	}
	path := filepath.Join(tb.TempDir(), "surface.dds")
	if err = os.WriteFile(path, encoded.Bytes(), 0o600); err != nil {
		tb.Fatal(err)
	}
	return path
}

func TestInspectModelViewerDDSMarksArrayForFallback(t *testing.T) {
	raw := encodeModelViewerBC1DDS(t, 4, 4, 1, 0)
	binary.LittleEndian.PutUint32(raw[140:144], 2)
	raw = append(raw, make([]byte, 8)...)
	path := filepath.Join(t.TempDir(), "array.dds")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	metadata, err := inspectModelViewerDDS(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if metadata.Format != "" || metadata.Width != 4 || metadata.Height != 4 || metadata.MipCount != 1 {
		t.Fatalf("metadata = %#v", metadata)
	}
}

func TestRunModelViewerTextureJobsKeepsDDSCompressed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "body.dds")
	if err := os.WriteFile(path, encodeModelViewerBC1DDS(t, 4, 4, 1, 0), 0o600); err != nil {
		t.Fatal(err)
	}
	output, stats, err := runModelViewerTextureJobs(
		context.Background(),
		modelViewerTextureSettings{TextureFormat: "jpeg-safe", JPEGQuality: 85, MaterialProfile: "zzmi"},
		1,
		[]modelViewerTextureJob{{
			path:         path,
			resourceName: "BodyDiffuseInvertAlpha",
			keys:         []string{"body"},
			role:         "diffuse",
			canonicalKey: "body",
		}},
	)
	if err != nil {
		t.Fatal(err)
	}
	payload := output[0]["body"]
	if payload.DDS == nil || payload.DDS.Format != "bc1-unorm" || payload.Path != path || !payload.InvertAlpha {
		t.Fatalf("payload = %#v", payload)
	}
	if len(payload.Bytes) != 0 || stats.DirectDDS != 1 || stats.Decodes != 0 || stats.Encodes != 0 ||
		stats.PreparedImages != 0 {
		t.Fatalf("stats = %#v payload bytes=%d", stats, len(payload.Bytes))
	}
}

func TestRunModelViewerTextureJobsPreservesDetectedDDSAlphaInversion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "body.dds")
	if err := os.WriteFile(path, encodeModelViewerTransparentWhiteBC3DDS(t, 4, 4), 0o600); err != nil {
		t.Fatal(err)
	}
	output, stats, err := runModelViewerTextureJobs(
		context.Background(),
		modelViewerTextureSettings{TextureFormat: "jpeg-safe", JPEGQuality: 85},
		1,
		[]modelViewerTextureJob{{
			path:         path,
			resourceName: "BodyDiffuse",
			keys:         []string{"body"},
			role:         "diffuse",
			canonicalKey: "body",
		}},
	)
	if err != nil {
		t.Fatal(err)
	}
	payload := output[0]["body"]
	if payload.DDS == nil || payload.DDS.Format != "bc3-unorm" || !payload.InvertAlpha {
		t.Fatalf("payload = %#v", payload)
	}
	if stats.DirectDDS != 1 || stats.Decodes != 0 || stats.Encodes != 0 {
		t.Fatalf("stats = %#v", stats)
	}
	fallback, err := prepareModelViewerDDSFallback(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := png.Decode(bytes.NewReader(fallback))
	if err != nil {
		t.Fatal(err)
	}
	if alpha := color.NRGBAModel.Convert(decoded.At(0, 0)).(color.NRGBA).A; alpha != 0 {
		t.Fatalf("fallback alpha = %d, want 0 before renderer inversion", alpha)
	}
}

func TestWriteModelViewerPayloadServesCompressedDDSPreview(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wide.dds")
	if err := os.WriteFile(path, encodeModelViewerBC1DDS(t, 4096, 4, 1, 0), 0o600); err != nil {
		t.Fatal(err)
	}
	metadata, err := inspectModelViewerDDS(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	protocol := infra.NewProtocol()
	sessionID := protocol.CreateMemorySession()
	service := NewWithOptions(Options{Protocol: protocol})
	transport := ModelViewerTransport{Textures: make(map[string]ModelViewerTextureTransport)}
	var decodeChecks atomic.Int32
	modelViewerTextureIOHook = func() { decodeChecks.Add(1) }
	t.Cleanup(func() { modelViewerTextureIOHook = nil })
	if err = writeModelViewerPayload(
		context.Background(),
		service,
		sessionID,
		&transport,
		nil,
		map[string]modelViewerTexturePayload{
			"wide": {Key: "wide", Role: "diffuse", Path: path, DDS: &metadata},
		},
		modelViewerPayloadOptions{ddsPreviewMaxDimension: modelViewerDDSPreviewMaxDimension},
	); err != nil {
		t.Fatal(err)
	}
	texture := transport.Textures["wide"]
	if texture.Encoding != "dds" || texture.Width != 2048 || texture.Height != 2 || texture.MipCount != 1 ||
		!strings.Contains(texture.URL, "/protocol/memory/") {
		t.Fatalf("transport texture = %#v", texture)
	}
	if decodeChecks.Load() != 0 {
		t.Fatal("DDS preview decoded pixels eagerly")
	}

	request := httptest.NewRequest(http.MethodGet, texture.URL, nil)
	response := httptest.NewRecorder()
	protocol.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "application/octet-stream" {
		t.Fatalf("preview status=%d type=%q", response.Code, response.Header().Get("Content-Type"))
	}
	reader, err := ddsutil.NewDdsReader(bytes.NewReader(response.Body.Bytes()), int64(response.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	actual := reader.Metadata()
	if actual.Width != 2048 || actual.Height != 2 || actual.Mipmaps != 1 ||
		actual.ImageFormat != ddsutil.BC1RgbaUnorm {
		t.Fatalf("preview metadata = %#v", actual)
	}
	if decodeChecks.Load() != 0 {
		t.Fatal("DDS preview decoded compressed pixels")
	}
}

func TestWriteModelViewerPayloadKeepsLargeDDSDirectWhenPreviewDisabled(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wide.dds")
	if err := os.WriteFile(path, encodeModelViewerBC1DDS(t, 4096, 4, 1, 0), 0o600); err != nil {
		t.Fatal(err)
	}
	metadata, err := inspectModelViewerDDS(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	protocol := infra.NewProtocol()
	sessionID := protocol.CreateMemorySession()
	service := NewWithOptions(Options{Protocol: protocol})
	transport := ModelViewerTransport{Textures: make(map[string]ModelViewerTextureTransport)}
	if err = writeModelViewerPayload(
		context.Background(),
		service,
		sessionID,
		&transport,
		nil,
		map[string]modelViewerTexturePayload{
			"wide": {Key: "wide", Role: "diffuse", Path: path, DDS: &metadata},
		},
		modelViewerPayloadOptions{},
	); err != nil {
		t.Fatal(err)
	}

	texture := transport.Textures["wide"]
	if texture.Encoding != "dds" || texture.Width != 4096 || texture.Height != 4 || texture.MipCount != 1 ||
		!strings.Contains(texture.URL, "/protocol/local?") {
		t.Fatalf("transport texture = %#v", texture)
	}
}

func TestWriteModelViewerPayloadDefersDDSFallback(t *testing.T) {
	path := filepath.Join(t.TempDir(), "body.dds")
	if err := os.WriteFile(path, encodeModelViewerBC1DDS(t, 4, 4, 1, 0), 0o600); err != nil {
		t.Fatal(err)
	}
	metadata, err := inspectModelViewerDDS(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	protocol := infra.NewProtocol()
	sessionID := protocol.CreateMemorySession()
	service := NewWithOptions(Options{Protocol: protocol})
	transport := ModelViewerTransport{Textures: make(map[string]ModelViewerTextureTransport)}
	var decodeChecks atomic.Int32
	modelViewerTextureIOHook = func() { decodeChecks.Add(1) }
	t.Cleanup(func() { modelViewerTextureIOHook = nil })
	err = writeModelViewerPayload(
		context.Background(),
		service,
		sessionID,
		&transport,
		nil,
		map[string]modelViewerTexturePayload{
			"body": {Key: "body", Role: "diffuse", Path: path, DDS: &metadata, InvertAlpha: true},
		},
		modelViewerPayloadOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	texture := transport.Textures["body"]
	if texture.Encoding != "dds" || texture.Format != "bc1-unorm" || texture.FallbackURL == "" ||
		!texture.InvertAlpha || !strings.Contains(texture.URL, "/protocol/local?") {
		t.Fatalf("transport texture = %#v", texture)
	}
	if decodeChecks.Load() != 0 {
		t.Fatal("DDS fallback decoded eagerly")
	}

	request := httptest.NewRequest(http.MethodGet, texture.FallbackURL, nil)
	response := httptest.NewRecorder()
	protocol.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "image/png" ||
		!strings.HasPrefix(response.Body.String(), "\x89PNG") {
		t.Fatalf(
			"fallback status=%d type=%q body=%x",
			response.Code,
			response.Header().Get("Content-Type"),
			response.Body.Bytes(),
		)
	}
	if decodeChecks.Load() == 0 {
		t.Fatal("DDS fallback was not decoded on demand")
	}
}
