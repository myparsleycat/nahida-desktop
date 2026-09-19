package infra

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/gabriel-vasile/mimetype"
)

func TestDetectMediaTypeReadsContentNotNames(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		content []byte
		want    string
	}{
		{name: "png", content: pngContent(t), want: "image/png"},
		{name: "webp", content: webpContent(), want: "image/webp"},
		{name: "avif", content: isoBMFFContent("avif", "avif", "mif1"), want: "image/avif"},
		{name: "heic", content: isoBMFFContent("heic", "heic", "mif1"), want: "image/heic"},
		{name: "mp4 with an isom brand", content: isoBMFFContent("isom", "isom"), want: "video/mp4"},
		{name: "quicktime", content: isoBMFFContent("qt  ", "qt  "), want: "video/quicktime"},
		{name: "glb", content: glbContent(), want: "model/gltf-binary"},
		{name: "matroska", content: ebmlContent("matroska"), want: "video/x-matroska"},
		{name: "webm", content: ebmlContent("webm"), want: "video/webm"},
		{name: "avi", content: aviContent(), want: "video/x-msvideo"},
		{name: "zip", content: zipContent(t, map[string]string{"mod.ini": "[Constants]"}), want: "application/zip"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, ok := DetectMediaType(test.content)
			if !ok || got != test.want {
				t.Fatalf("DetectMediaType = %q, %v, want %q", got, ok, test.want)
			}
		})
	}
}

func TestDetectMediaTypeReportsTextAndUnknownContentAsUnrecognized(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		content []byte
	}{
		{name: "short text", content: []byte("0123456789")},
		{name: "json", content: []byte(`{"asset":{"version":"2.0"}}`)},
		{name: "scriptable svg", content: []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script/></svg>`)},
		{name: "dds texture", content: append([]byte("DDS "), make([]byte, 124)...)},
		{name: "empty", content: nil},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got, ok := DetectMediaType(test.content); ok {
				t.Fatalf("DetectMediaType = %q, true, want unrecognized", got)
			}
		})
	}
}

func TestDetectFileMediaTypeLeavesTheFileOffset(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "payload.bin")
	if err := os.WriteFile(path, isoBMFFContent("avif", "avif", "mif1"), 0o644); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()

	got, ok := DetectFileMediaType(file)
	if !ok || got != "image/avif" {
		t.Fatalf("DetectFileMediaType = %q, %v", got, ok)
	}
	offset, err := file.Seek(0, 0)
	if err != nil || offset != 0 {
		t.Fatalf("offset = %d, %v", offset, err)
	}
}

func TestDetectFormatExtensionReportsContainers(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	archivePath := filepath.Join(directory, "payload.bin")
	content := zipContent(t, map[string]string{"mod.ini": "[Constants]"})
	if err := os.WriteFile(archivePath, content, 0o644); err != nil {
		t.Fatal(err)
	}

	extension, err := DetectFormatExtension(archivePath)
	if err != nil || extension != "zip" {
		t.Fatalf("DetectFormatExtension = %q, %v, want zip", extension, err)
	}
	missing, err := DetectFormatExtension(filepath.Join(directory, "missing.bin"))
	if !errors.Is(err, os.ErrNotExist) || missing != "" {
		t.Fatalf("missing file = %q, %v", missing, err)
	}
}

func TestServedContentTypeKeepsProtocolStrings(t *testing.T) {
	t.Parallel()
	// Every container the protocol pins explicitly must keep the exact string the
	// model viewer and the WebView2 media stack expect, whatever name mimetype
	// uses for it.
	containers := []struct {
		detected string
		want     string
	}{
		{detected: "image/png", want: "image/png"},
		{detected: "video/matroska", want: "video/x-matroska"},
		{detected: "video/x-matroska", want: "video/x-matroska"},
		{detected: "video/webm", want: "video/webm"},
		{detected: "video/quicktime", want: "video/quicktime"},
		{detected: "video/x-msvideo", want: "video/x-msvideo"},
		{detected: "model/gltf-binary", want: "model/gltf-binary"},
		{detected: "model/gltf+json", want: "model/gltf+json"},
	}
	for _, container := range containers {
		detected := mimetype.Lookup(container.detected)
		if detected == nil {
			t.Fatalf("mimetype does not know %s", container.detected)
		}
		if got := servedContentType(detected); got != container.want {
			t.Fatalf("servedContentType(%s) = %q, want %q", container.detected, got, container.want)
		}
	}
}

func pngContent(t *testing.T) []byte {
	t.Helper()
	square := image.NewRGBA(image.Rect(0, 0, 1, 1))
	square.Set(0, 0, color.RGBA{R: 255, A: 255})
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, square); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func webpContent() []byte {
	content := append([]byte("RIFF\x00\x00\x00\x00WEBPVP8 "), make([]byte, 16)...)
	return content
}

func glbContent() []byte {
	return []byte{'g', 'l', 'T', 'F', 0x02, 0, 0, 0, 12, 0, 0, 0, 0, 0, 0, 0}
}

// ebmlContent builds the EBML header whose DocType identifies matroska and webm
// content.
func ebmlContent(docType string) []byte {
	content := []byte{0x1A, 0x45, 0xDF, 0xA3, 0x42, 0x82, byte(len(docType) | 0x80)}
	return append(content, docType...)
}

func aviContent() []byte {
	return append([]byte("RIFF\x00\x00\x00\x00AVI LIST"), make([]byte, 8)...)
}

// isoBMFFContent builds the ftyp box that identifies mp4, quicktime, avif, or
// heic content by its major and compatible brands.
func isoBMFFContent(major string, compatible ...string) []byte {
	content := []byte{0, 0, 0, 0, 'f', 't', 'y', 'p'}
	content = append(content, major...)
	content = append(content, 0, 0, 0, 0)
	for _, brand := range compatible {
		content = append(content, brand...)
	}
	binary.BigEndian.PutUint32(content, uint32(len(content)))
	return content
}

func zipContent(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for name, content := range entries {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}
