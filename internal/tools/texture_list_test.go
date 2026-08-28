package tools

import (
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func makeTestDDS(width, height uint32, dxgi uint32) []byte {
	buffer := make([]byte, 132)
	put := func(offset int, value uint32) { binary.LittleEndian.PutUint32(buffer[offset:offset+4], value) }
	put(0, ddsMagic)
	put(4, ddsHeaderSize)
	put(8, ddsMipMapFlag)
	put(12, height)
	put(16, width)
	put(28, 4)
	put(80, ddpfFourCC)
	put(84, 0x30315844)
	put(128, dxgi)
	return buffer
}

func TestTextureSettingsRoundTripAndNormalize(t *testing.T) {
	ctx := context.Background()
	client := openToolsTestDB(t)
	service := New()
	service.UseClient(client)
	defaults, err := service.GetTextureResizeSettings(ctx)
	if err != nil || defaults.CustomWidth != 2048 || !defaults.Backup || defaults.UpscaleScale != 2 {
		t.Fatalf("defaults = %#v, %v", defaults, err)
	}
	mode, operation, model := "percent", "upscale", "realesrgan-x4plus"
	percent, width, scale, backup := 150, 1500, 2, false
	saved, err := service.SaveTextureResizeSettings(ctx, TextureResizeSettingsPatch{
		Mode: &mode, Operation: &operation, Percent: &percent, CustomWidth: &width,
		Backup: &backup, UpscaleScale: &scale, UpscaleModel: &model,
	})
	if err != nil {
		t.Fatal(err)
	}
	if saved.Percent != 99 || saved.CustomWidth != 1024 || saved.Backup || saved.UpscaleScale != 4 {
		t.Fatalf("normalized settings = %#v", saved)
	}
	loaded, err := service.GetTextureResizeSettings(ctx)
	if err != nil || loaded != saved {
		t.Fatalf("loaded = %#v, %v; want %#v", loaded, err, saved)
	}
}

func TestTextureSettingsNormalizeStoredZeroLikeElectron(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	client := openToolsTestDB(t)
	for key, value := range map[string]string{
		textureSettingKeys.percent: "0",
		textureSettingKeys.width:   "0",
		textureSettingKeys.height:  "0",
	} {
		if err := client.Settings.Upsert(ctx, key, &value); err != nil {
			t.Fatal(err)
		}
	}
	service := New()
	service.UseClient(client)
	settings, err := service.GetTextureResizeSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if settings.Percent != 1 || settings.CustomWidth != 1024 || settings.CustomHeight != 1024 {
		t.Fatalf("stored zero normalized to %#v", settings)
	}
}

func TestListTextureFolderParsesDDSAndSortsByPixels(t *testing.T) {
	ctx := context.Background()
	client := openToolsTestDB(t)
	service := New()
	service.UseClient(client)
	root := t.TempDir()
	large := filepath.Join(root, "z.dds")
	small := filepath.Join(root, "a.dds")
	if err := os.WriteFile(large, makeTestDDS(4096, 2048, 72), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(small, makeTestDDS(2048, 1024, 28), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "broken.dds"), []byte("bad"), 0o600); err != nil {
		t.Fatal(err)
	}
	mode := "percent"
	percent := 50
	items, err := service.ListTextureFolder(ctx, root, &TextureResizeSettingsPatch{Mode: &mode, Percent: &percent})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].FilePath != large || items[0].Format != "DXGI_FORMAT_BC1_UNORM_SRGB" || items[0].ColorSpace != "srgb" {
		t.Fatalf("items = %#v", items)
	}
	if items[0].TargetWidth != 2048 || items[0].TargetHeight != 1024 || !items[0].CanResize || items[0].MipLevelCount != 4 {
		t.Fatalf("large item = %#v", items[0])
	}
	if items[1].ColorSpace != "linear" || items[1].FormatConversionMessage != nil {
		t.Fatalf("small item = %#v", items[1])
	}
}

func TestParseDDSRejectsCubemapUpscale(t *testing.T) {
	buffer := makeTestDDS(1024, 1024, 72)
	binary.LittleEndian.PutUint32(buffer[112:116], ddsCubemapFlag)
	metadata, err := parseDDS(buffer)
	if err != nil {
		t.Fatal(err)
	}
	reason := textureUpscaleSkipReason(metadata, 2)
	if reason == nil || *reason != "Cubemap and layered DDS textures cannot be upscaled." {
		t.Fatalf("skip reason = %v", reason)
	}
}

func TestTextureRuntimeStatusRequiresEveryModelPair(t *testing.T) {
	ctx := context.Background()
	client := openToolsTestDB(t)
	service := New()
	service.UseClient(client)
	userData := useToolsTestAppData(t, service, t.TempDir())
	root := filepath.Join(userData, "tools", realesrganSpec.dirName)
	models := filepath.Join(root, "models")
	if err := os.MkdirAll(models, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, realesrganSpec.binaryName), []byte("exe"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, model := range realesrganSpec.requiredModels {
		for _, extension := range []string{".param", ".bin"} {
			if err := os.WriteFile(filepath.Join(models, model+extension), []byte("model"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	statuses, err := service.GetTextureUpscaleRuntimeStatus(ctx)
	if err != nil || !statuses.Realesrgan.Installed || statuses.Realesrgan.Version == nil || *statuses.Realesrgan.Version != realesrganSpec.version {
		t.Fatalf("statuses = %#v, %v", statuses, err)
	}
	if statuses.Realcugan.Installed || !statuses.Realcugan.NeedsInstall {
		t.Fatalf("realcugan status = %#v", statuses.Realcugan)
	}
	if err := os.Remove(filepath.Join(models, realesrganSpec.requiredModels[0]+".bin")); err != nil {
		t.Fatal(err)
	}
	statuses, err = service.GetTextureUpscaleRuntimeStatus(ctx)
	if err != nil || statuses.Realesrgan.Installed || !statuses.Realesrgan.NeedsInstall {
		t.Fatalf("incomplete status = %#v, %v", statuses.Realesrgan, err)
	}
}
