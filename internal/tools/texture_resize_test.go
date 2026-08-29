package tools

import (
	"context"
	"encoding/binary"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestTextureUpscaleSkipsCubemapWithoutInstallingRuntime(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "cube.dds")
	data := makeTestDDS(1024, 1024, 72)
	binary.LittleEndian.PutUint32(data[112:116], ddsCubemapFlag)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	service := New()
	service.UseClient(openToolsTestDB(t))
	result, err := service.ResizeTextureFile(context.Background(), TextureResizeFileRunInput{
		FilePath: path,
		Settings: TextureResizeSettings{Mode: "custom", Operation: "upscale", CustomWidth: 2048, CustomHeight: 2048, Backup: true, UpscaleScale: 2, UpscaleModel: "realesr-animevideov3"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Skipped != 1 || result.Updated != 0 || len(result.Files) != 1 || result.Files[0].Message == nil || *result.Files[0].Message != "Cubemap and layered DDS textures cannot be upscaled." {
		t.Fatalf("result = %#v", result)
	}
}

func TestTextureFolderUpscaleIsRejected(t *testing.T) {
	t.Parallel()
	service := New()
	service.UseClient(openToolsTestDB(t))
	_, err := service.ResizeTextureFolder(context.Background(), TextureResizeRunInput{
		TargetPath: t.TempDir(),
		Settings:   TextureResizeSettings{Mode: "custom", Operation: "upscale", CustomWidth: 2048, CustomHeight: 2048, UpscaleScale: 2, UpscaleModel: "realesr-animevideov3"},
	})
	if err == nil || err.Error() != "Folder upscale is not supported." {
		t.Fatalf("error = %v", err)
	}
}

func TestBuildNCNNUpscalerArgs(t *testing.T) {
	t.Parallel()
	settings := TextureResizeSettings{UpscaleScale: 3, UpscaleModel: "realcugan-pro"}
	args := buildNCNNUpscalerArgs("realcugan", `C:\runtime`, "in.png", "out.png", settings)
	wantTail := filepath.Join(`C:\runtime`, "models-pro")
	if len(args) != 16 || args[5] != "0" || args[7] != "3" || args[len(args)-1] != wantTail {
		t.Fatalf("realcugan args = %v", args)
	}
	settings.UpscaleModel = "realesr-animevideov3"
	args = buildNCNNUpscalerArgs("realesrgan", `C:\models`, "in.png", "out.png", settings)
	if len(args) != 14 || args[5] != settings.UpscaleModel || args[len(args)-1] != `C:\models` {
		t.Fatalf("realesrgan args = %v", args)
	}
}

func TestTextureProcessErrorsUseElectronDisplayNames(t *testing.T) {
	t.Parallel()
	if got, want := formatTextureProcessTimeout("Real-ESRGAN", ""), "Real-ESRGAN timed out after 600000ms"; got != want {
		t.Fatalf("timeout = %q, want %q", got, want)
	}
	if got, want := formatTextureProcessExit("Real-CUGAN", 7, "bad model"), "Real-CUGAN exited with code 7: bad model"; got != want {
		t.Fatalf("exit error = %q, want %q", got, want)
	}
}

func TestResizeTextureFileProcessesDDS(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	pngPath, ddsPath := filepath.Join(root, "source.png"), filepath.Join(root, "source.dds")
	imageData := image.NewNRGBA(image.Rect(0, 0, 2048, 2048))
	for index := range imageData.Pix {
		imageData.Pix[index] = 127
	}
	pngFile, err := os.Create(pngPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(pngFile, imageData); err != nil {
		_ = pngFile.Close()
		t.Fatal(err)
	}
	if err := pngFile.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := encodePNGToDDS(pngPath, ddsPath, "DXGI_FORMAT_BC1_UNORM", false, true); err != nil {
		t.Fatalf("encode fixture: %v", err)
	}
	client := openToolsTestDB(t)
	var events []string
	var progress []TextureResizeProgressEvent
	service := NewWithOptions(Options{
		EventEmit: func(name string, data ...any) {
			events = append(events, name)
			if name == "tools:textureResizeProgress" && len(data) == 1 {
				if event, ok := data[0].(TextureResizeProgressEvent); ok {
					progress = append(progress, event)
				}
			}
		},
	})
	service.UseClient(client)
	result, err := service.ResizeTextureFile(context.Background(), TextureResizeFileRunInput{
		FilePath: ddsPath,
		Settings: TextureResizeSettings{
			Mode: "percent", Operation: "resize", Percent: 50,
			CustomWidth: 2048, CustomHeight: 2048, Backup: true,
			UpscaleScale: 2, UpscaleModel: "realesr-animevideov3",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Processed != 1 || result.Updated != 1 || result.Failed != 0 || len(result.Files) != 1 {
		t.Fatalf("result = %#v", result)
	}
	if result.Files[0].OutputWidth != 1024 || result.Files[0].OutputHeight != 1024 || !result.Files[0].BackupCreated {
		t.Fatalf("file result = %#v", result.Files[0])
	}
	resized, err := os.ReadFile(ddsPath)
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := parseDDS(resized)
	if err != nil || metadata.width != 1024 || metadata.height != 1024 {
		t.Fatalf("resized metadata = %#v, %v", metadata, err)
	}
	if !regularFile(ddsPath + ".bak") {
		t.Fatal("backup DDS was not created")
	}
	if state := service.GetTextureResizeState(); state.Status != "idle" {
		t.Fatalf("state = %#v, want idle", state)
	}
	if len(events) != 2 || events[0] != "tools:textureResizeProgress" || events[1] != "tools:textureResizeProgress" {
		t.Fatalf("progress events = %v", events)
	}
	if len(progress) == 0 || progress[0].TotalFiles == nil || *progress[0].TotalFiles != 1 || progress[0].ProcessedFiles == nil || *progress[0].ProcessedFiles != 0 {
		t.Fatalf("initial file progress = %#v", progress)
	}
}

func TestTextureJobOwnershipKeepsRunningStateWhenALaterFileFinishesFirst(t *testing.T) {
	service := New()
	folder := service.beginTextureJob(TextureResizeProgressEvent{Status: "running", FilePath: stringPointer("folder")})
	file := service.beginTextureJob(TextureResizeProgressEvent{Status: "running", FilePath: stringPointer("file")})
	service.settleTextureJob(file, TextureResizeProgressEvent{Status: "completed", FilePath: stringPointer("file")})
	state := service.GetTextureResizeState()
	if state.Status != "running" || state.FilePath == nil || *state.FilePath != "folder" {
		t.Fatalf("state after later file = %#v", state)
	}
	service.settleTextureJob(folder, TextureResizeProgressEvent{Status: "completed", FilePath: stringPointer("folder")})
	if got := service.GetTextureResizeState(); got.Status != "idle" {
		t.Fatalf("final = %#v", got)
	}
}

func TestTextureJobOwnershipDoesNotIdleAfterFailedResizeWhileAnotherJobRuns(t *testing.T) {
	service := New()
	first := service.beginTextureJob(TextureResizeProgressEvent{Status: "running", FilePath: stringPointer("a")})
	second := service.beginTextureJob(TextureResizeProgressEvent{Status: "running", FilePath: stringPointer("b")})
	service.settleTextureJob(first, TextureResizeProgressEvent{Status: "failed", Error: stringPointer("boom")})
	state := service.GetTextureResizeState()
	if state.Status != "running" || state.FilePath == nil || *state.FilePath != "b" {
		t.Fatalf("state after failure = %#v", state)
	}
	service.settleTextureJob(second, TextureResizeProgressEvent{Status: "completed", FilePath: stringPointer("b")})
	if got := service.GetTextureResizeState(); got.Status != "idle" {
		t.Fatalf("final = %#v", got)
	}
}

func TestTextureJobOwnershipKeepsFolderActivityWhenAFileResizeFinishesFirst(t *testing.T) {
	service := New()
	folder := service.beginTextureJob(TextureResizeProgressEvent{Status: "running", FilePath: stringPointer("mods")})
	file := service.beginTextureJob(TextureResizeProgressEvent{Status: "running", FilePath: stringPointer("one.dds")})
	service.settleTextureJob(file, TextureResizeProgressEvent{Status: "completed", FilePath: stringPointer("one.dds")})
	state := service.GetTextureResizeState()
	if state.Status != "running" || state.FilePath == nil || *state.FilePath != "mods" {
		t.Fatalf("folder activity lost: %#v", state)
	}
	service.settleTextureJob(folder, TextureResizeProgressEvent{Status: "completed", FilePath: stringPointer("mods")})
}
