package texture

import (
	"slices"
	"testing"
)

func TestUpscaleTargetRejectsOversizedAndUnofferedScales(t *testing.T) {
	if got, ok := UpscaleTarget(1024, 1024, 2); !ok || got != (Size{2048, 2048}) {
		t.Fatalf("UpscaleTarget(1024, 1024, 2) = %v, %v", got, ok)
	}
	if _, ok := UpscaleTarget(4096, 2048, 3); ok {
		t.Fatal("a 12288px result must be rejected")
	}
	if _, ok := UpscaleTarget(2048, 2048, 5); ok {
		t.Fatal("scale 5 is not offered")
	}
}

func TestUpscaleScalesMatchShippedModels(t *testing.T) {
	cases := []struct {
		model     string
		scales    []int
		requested int
		resolved  int
	}{
		{"realesr-animevideov3", []int{2, 3, 4}, 3, 3},
		{"realesrgan-x4plus", []int{4}, 2, 4},
		{"realcugan-pro", []int{2, 3}, 4, 3},
		{"realcugan-nose", []int{2}, 3, 2},
	}
	for _, c := range cases {
		if got := UpscaleScales(c.model); !slices.Equal(got, c.scales) {
			t.Errorf("UpscaleScales(%q) = %v, want %v", c.model, got, c.scales)
		}
		if got := ResolveUpscaleScale(c.model, c.requested); got != c.resolved {
			t.Errorf("ResolveUpscaleScale(%q, %d) = %d, want %d", c.model, c.requested, got, c.resolved)
		}
	}
	for _, group := range UpscaleModelGroups {
		for _, model := range group.Models {
			if engine := textureUpscaleEngine(model); engine != group.Engine {
				t.Errorf("model %q is listed under %q but runs on %q", model, group.Engine, engine)
			}
		}
	}
}

func TestCanRunFollowsOperationLimits(t *testing.T) {
	bc5 := TextureResizeListItem{
		Format:         "DXGI_FORMAT_BC5_UNORM",
		OriginalWidth:  2048,
		OriginalHeight: 2048,
		LayerCount:     1,
	}
	upscale := TextureResizeSettings{Operation: "upscale", UpscaleScale: 2}
	if CanRun(bc5, upscale) {
		t.Fatal("BC5 textures must not be upscaled")
	}
	bc7 := bc5
	bc7.Format = "DXGI_FORMAT_BC7_UNORM"
	if !CanRun(bc7, upscale) {
		t.Fatal("a 2048px BC7 texture can be upscaled 2x")
	}
	resize := TextureResizeSettings{Operation: "resize", Mode: "custom", CustomWidth: 1024, CustomHeight: 1024}
	if !CanRun(bc7, resize) {
		t.Fatal("a 2048px texture can be resized to 1024px")
	}
	small := bc7
	small.OriginalWidth, small.OriginalHeight = 1024, 1024
	if CanRun(small, resize) {
		t.Fatal("a 1024px texture has no smaller size")
	}
}

func TestItemSettingsFallsBackToSmallestCandidate(t *testing.T) {
	wide := TextureResizeListItem{
		Format:         "DXGI_FORMAT_BC7_UNORM",
		OriginalWidth:  8192,
		OriginalHeight: 2048,
		LayerCount:     1,
	}
	base := TextureResizeSettings{Operation: "resize", Mode: "custom", CustomWidth: 2048, CustomHeight: 2048}
	settings := ItemSettings(base, wide)
	if settings.CustomWidth != 4096 || settings.CustomHeight != 1024 {
		t.Fatalf("custom size = %dx%d, want the smallest candidate 4096x1024", settings.CustomWidth,
			settings.CustomHeight)
	}
	if !CanRun(wide, settings) {
		t.Fatal("the size the dialog shows must be one the texture can be processed with")
	}
}
