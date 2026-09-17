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
