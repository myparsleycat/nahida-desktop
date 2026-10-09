package texture

import (
	"context"
	"encoding/binary"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/myparsleycat/ddsutil"
)

func writeTestDDS(t *testing.T, path string, width, height uint32, format ddsutil.ImageFormat) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	data := make([]float32, int(width)*int(height)*4)
	for index := range data {
		data[index] = 0.5
	}
	surface := &ddsutil.SurfaceRgba32Float{Width: width, Height: height, Depth: 1, Layers: 1, Mipmaps: 1, Data: data}
	encoded, err := surface.Encode(format, ddsutil.QualityFast, ddsutil.MipmapsDisabled)
	if err != nil {
		t.Fatal(err)
	}
	dds, err := encoded.ToDds()
	if err != nil {
		t.Fatal(err)
	}
	if err := writeDDSAtomically(path, dds); err != nil {
		t.Fatal(err)
	}
}

// writeLegacyBGRA writes the DX9-style header most mod tools produce, which has no DXGI format.
func writeLegacyBGRA(t *testing.T, path string, width, height uint32) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	raw := make([]byte, 128+int(width)*int(height)*4)
	put := func(offset int, value uint32) { binary.LittleEndian.PutUint32(raw[offset:offset+4], value) }
	put(0, ddsMagic)
	put(4, ddsHeaderSize)
	put(8, 0x100f)
	put(12, height)
	put(16, width)
	put(20, width*4)
	put(76, 32)
	put(80, 0x41)
	put(88, 32)
	put(92, 0x00ff0000)
	put(96, 0x0000ff00)
	put(100, 0x000000ff)
	put(104, 0xff000000)
	put(108, 0x1000)
	for index := 128; index < len(raw); index++ {
		raw[index] = 128
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

func firstDDSPixel(t *testing.T, path string) []byte {
	t.Helper()
	dds, err := readDDSFile(path)
	if err != nil {
		t.Fatal(err)
	}
	surface, err := ddsutil.SurfaceFromDds(dds)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := surface.DecodeLayersMipmapsRgba8(0, 1, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	return decoded.Data[:4]
}

func linkTestJunction(t *testing.T, link, target string) {
	t.Helper()
	if output, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput(); err != nil {
		t.Fatalf("mklink: %v: %s", err, output)
	}
}

func TestFindAndCompressUncompressedTextures(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	// Importing user data in link mode makes the Mods folder a junction, and mods are often linked in.
	storage, root := filepath.Join(base, "storage"), filepath.Join(base, "Mods")
	linked := filepath.Join(root, "Mod C", "Linked.dds")
	writeTestDDS(t, filepath.Join(base, "elsewhere", "Linked.dds"), 1024, 1024, ddsutil.Rgba8Unorm)
	if err := os.MkdirAll(filepath.Join(storage, "Mod A"), 0o755); err != nil {
		t.Fatal(err)
	}
	linkTestJunction(t, root, storage)
	linkTestJunction(t, filepath.Join(root, "Mod C"), filepath.Join(base, "elsewhere"))
	linkTestJunction(t, filepath.Join(root, "Mod A", "Loop"), storage)
	srgb := filepath.Join(root, "Mod A", "Diffuse.dds")
	legacy := filepath.Join(root, "Mod A", "Textures", "Legacy.dds")
	disabled := filepath.Join(root, "DISABLED Mod B", "Diffuse.dds")
	small := filepath.Join(root, "Mod A", "Icon.dds")
	odd := filepath.Join(root, "Mod A", "Odd.dds")
	compressed := filepath.Join(root, "Mod A", "Compressed.dds")
	writeTestDDS(t, srgb, 1024, 1024, ddsutil.Rgba8UnormSrgb)
	writeLegacyBGRA(t, legacy, 1024, 2048)
	writeTestDDS(t, disabled, 1024, 1024, ddsutil.Rgba8Unorm)
	writeTestDDS(t, small, 256, 256, ddsutil.Rgba8Unorm)
	writeTestDDS(t, odd, 1026, 1026, ddsutil.Rgba8Unorm)
	writeTestDDS(t, compressed, 1024, 1024, ddsutil.BC7RgbaUnorm)
	untouched := map[string][]byte{}
	for _, path := range []string{disabled, small, odd, compressed} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		untouched[path] = data
	}

	found, err := FindUncompressed(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 3 || found[0].Path != legacy || found[1].Path != srgb || found[2].Path != linked {
		t.Fatalf("found = %+v", found)
	}
	before := firstDDSPixel(t, srgb)
	if found[0].RelativePath != filepath.Join("Mod A", "Textures", "Legacy.dds") || found[0].Height != 2048 {
		t.Fatalf("legacy texture = %+v", found[0])
	}

	var reported []int
	result, err := CompressUncompressed(context.Background(), found, true, func(done, total int, _ string) {
		reported = append(reported, done, total)
	})
	if err != nil || result.Updated != 3 || result.Failed != 0 {
		t.Fatalf("result = %+v, %v", result, err)
	}
	if len(reported) != 6 || reported[0] != 1 || reported[4] != 3 || reported[5] != 3 {
		t.Fatalf("progress = %v", reported)
	}
	for channel, value := range firstDDSPixel(t, srgb) {
		if diff := int(value) - int(before[channel]); diff < -3 || diff > 3 {
			t.Fatalf("pixel changed from %v to %v", before, firstDDSPixel(t, srgb))
		}
	}
	for path, want := range map[string]string{
		srgb: "DXGI_FORMAT_BC7_UNORM_SRGB", legacy: "DXGI_FORMAT_BC7_UNORM", linked: "DXGI_FORMAT_BC7_UNORM",
	} {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		metadata, err := parseDDS(raw)
		if err != nil || metadata.format != want || metadata.width != 1024 {
			t.Fatalf("%s = %+v, %v; want %s", path, metadata, err, want)
		}
		if !regularFile(path + ".bak") {
			t.Fatalf("%s has no backup", path)
		}
	}
	for path, want := range untouched {
		data, err := os.ReadFile(path)
		if err != nil || string(data) != string(want) {
			t.Fatalf("%s was rewritten (%v)", path, err)
		}
	}

	found, err = FindUncompressed(context.Background(), root)
	if err != nil || len(found) != 0 {
		t.Fatalf("found after compressing = %+v, %v", found, err)
	}
}

func TestCompressUncompressedKeepsAuthoredMipmaps(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "Diffuse.dds")
	data := make([]byte, (1024*1024+512*512)*4)
	for index := 0; index < len(data); index += 4 {
		data[index+3] = 255
		if index < 1024*1024*4 {
			data[index] = 255
		} else {
			data[index+2] = 255
		}
	}
	source := &ddsutil.SurfaceRgba8{Width: 1024, Height: 1024, Depth: 1, Layers: 1, Mipmaps: 2, Data: data}
	encoded, err := source.Encode(ddsutil.Rgba8Unorm, ddsutil.QualityFast, ddsutil.MipmapsFromSurface)
	if err != nil {
		t.Fatal(err)
	}
	dds, err := encoded.ToDds()
	if err != nil {
		t.Fatal(err)
	}
	if err := writeDDSAtomically(path, dds); err != nil {
		t.Fatal(err)
	}

	result, err := CompressUncompressed(context.Background(), []UncompressedTexture{{Path: path}}, false, nil)
	if err != nil || result.Updated != 1 {
		t.Fatalf("result = %+v, %v", result, err)
	}
	compressed, err := readDDSFile(path)
	if err != nil {
		t.Fatal(err)
	}
	surface, err := ddsutil.SurfaceFromDds(compressed)
	if err != nil {
		t.Fatal(err)
	}
	if surface.Mipmaps != 2 || surface.ImageFormat != ddsutil.BC7RgbaUnorm {
		t.Fatalf("compressed to %d levels of %v", surface.Mipmaps, surface.ImageFormat)
	}
	level, err := surface.DecodeLayersMipmapsRgba8(0, 1, 1, 2)
	if err != nil || level.Data[0] > 8 || level.Data[2] < 247 {
		t.Fatalf("second level starts with %v, %v; want the authored blue", level.Data[:4], err)
	}
}

func TestReplaceUncompressedFileKeepsTextureRewrittenDuringEncode(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "Diffuse.dds")
	writeTestDDS(t, path, 1024, 1024, ddsutil.Rgba8Unorm)
	current, dds, err := loadUncompressedTexture(path)
	if err != nil || dds == nil {
		t.Fatalf("load = %+v, %v", current, err)
	}
	writeLegacyBGRA(t, path, 1024, 1024)
	rewritten, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	result, err := replaceUncompressedFile(context.Background(), current, dds, "DXGI_FORMAT_BC7_UNORM", true)
	if err != nil || result.Status != "skipped" {
		t.Fatalf("result = %+v, %v", result, err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != string(rewritten) {
		t.Fatalf("the rewritten texture was replaced (%v)", err)
	}
	if regularFile(path + ".bak") {
		t.Fatal("a skipped texture got a backup")
	}
}

func TestCompressUncompressedStopsWhenCancelled(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "Diffuse.dds")
	writeTestDDS(t, path, 1024, 1024, ddsutil.Rgba8Unorm)
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := CompressUncompressed(ctx, []UncompressedTexture{{Path: path}}, true, nil)
	if !errors.Is(err, context.Canceled) || len(result.Files) != 0 {
		t.Fatalf("result = %+v, %v", result, err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != string(original) {
		t.Fatalf("cancelled run rewrote the texture (%v)", err)
	}
}
