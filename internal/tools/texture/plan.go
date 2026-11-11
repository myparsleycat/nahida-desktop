package texture

import (
	"slices"
	"strings"
)

// Size is a texture's width and height in pixels.
type Size struct{ Width, Height int }

// UpscaleModelGroup lists the upscale models one engine runs, in the order the UI offers them.
type UpscaleModelGroup struct {
	Engine string
	Models []string
}

var UpscaleModelGroups = []UpscaleModelGroup{
	{Engine: "realesrgan", Models: []string{"realesr-animevideov3", "realesrgan-x4plus-anime", "realesrgan-x4plus"}},
	{Engine: "realcugan", Models: []string{"realcugan-pro", "realcugan-se", "realcugan-nose"}},
}

const upscaleMaxDimension = 8192

// UpscaleScales lists the scales model ships with.
func UpscaleScales(model string) []int {
	switch model {
	case "realesr-animevideov3", "realcugan-se":
		return []int{2, 3, 4}
	case "realcugan-pro":
		return []int{2, 3}
	case "realcugan-nose":
		return []int{2}
	}
	return []int{4}
}

// ResolveUpscaleScale returns scale when model supports it, else the model's largest scale.
func ResolveUpscaleScale(model string, scale int) int { return normalizeUpscaleScale(model, scale) }

func IsUpscaleOperation(operation string) bool { return isTextureUpscaleOperation(operation) }

// UpscaleUnsupportedFormat reports a format whose channels the upscalers would destroy.
func UpscaleUnsupportedFormat(format string) bool {
	return strings.Contains(format, "BC4") || strings.Contains(format, "BC5") || strings.Contains(format, "BC6H")
}

// UpscaleTarget is the size w×h becomes at scale, or false when the scale is not offered or the result passes
// the 8192px limit.
func UpscaleTarget(w, h, scale int) (Size, bool) {
	if w <= 0 || h <= 0 || scale < 2 || scale > 4 {
		return Size{}, false
	}
	if w*scale > upscaleMaxDimension || h*scale > upscaleMaxDimension {
		return Size{}, false
	}
	return Size{w * scale, h * scale}, true
}

// ResizeCandidates lists the smaller sizes w×h can be resized to: multiples of 1024 that keep its aspect ratio.
func ResizeCandidates(w, h int) []Size {
	var out []Size
	for _, c := range textureCandidates(w, h) {
		out = append(out, Size{c[0], c[1]})
	}
	return out
}

// PickResizeCandidate returns the largest candidate within maxW×maxH.
func PickResizeCandidate(candidates []Size, maxW, maxH int) (Size, bool) {
	var best Size
	ok := false
	for _, c := range candidates {
		if c.Width <= maxW && c.Height <= maxH && (!ok || c.Width*c.Height > best.Width*best.Height) {
			best, ok = c, true
		}
	}
	return best, ok
}

// PreviewSize is the size item gets with settings, or false when settings cannot produce one.
func PreviewSize(item TextureResizeListItem, settings TextureResizeSettings) (Size, bool) {
	switch {
	case settings.Operation == "convert":
		return Size{item.OriginalWidth, item.OriginalHeight}, true
	case IsUpscaleOperation(settings.Operation):
		return UpscaleTarget(item.OriginalWidth, item.OriginalHeight, settings.UpscaleScale)
	}
	candidates := ResizeCandidates(item.OriginalWidth, item.OriginalHeight)
	if len(candidates) == 0 {
		return Size{}, false
	}
	bounds := textureBounds(item.OriginalWidth, item.OriginalHeight, settings)
	return PickResizeCandidate(candidates, bounds[0], bounds[1])
}

func canUpscaleWith(item TextureResizeListItem, settings TextureResizeSettings) bool {
	if item.LayerCount > 1 || UpscaleUnsupportedFormat(item.Format) {
		return false
	}
	_, ok := PreviewSize(item, settings)
	return ok
}

// CanRun reports whether processing item with settings would do anything.
func CanRun(item TextureResizeListItem, settings TextureResizeSettings) bool {
	_, canResize := PreviewSize(item, settings)
	switch settings.Operation {
	case "resize":
		return canResize
	case "convert":
		return item.CanConvertFormat
	case "upscale":
		return canUpscaleWith(item, settings)
	case "upscale_and_convert":
		return item.CanConvertFormat && canUpscaleWith(item, settings)
	}
	return canResize && item.CanConvertFormat
}

// AdaptSettings fits settings to item: an output format the item cannot take falls back to its default, and a
// conversion it cannot do drops to the plain operation.
func AdaptSettings(settings TextureResizeSettings, item TextureResizeListItem) TextureResizeSettings {
	if settings.OutputFormat == "" || !slices.Contains(item.AvailableOutputFormats, settings.OutputFormat) {
		settings.OutputFormat = item.OutputFormatDefault
	}
	if !item.CanConvertFormat {
		switch settings.Operation {
		case "resize_and_convert":
			settings.Operation = "resize"
		case "upscale_and_convert":
			settings.Operation = "upscale"
		}
	}
	return settings
}

// ItemSettings are the settings one texture starts with in the process dialog: adapted to it, with the custom
// size set to the size the shared settings would give it.
func ItemSettings(settings TextureResizeSettings, item TextureResizeListItem) TextureResizeSettings {
	adapted := AdaptSettings(settings, item)
	if size, ok := PreviewSize(item, adapted); ok {
		adapted.CustomWidth, adapted.CustomHeight = size.Width, size.Height
	}
	adapted.Mode = "custom"
	return FitResize(adapted, item.OriginalWidth, item.OriginalHeight)
}

// FitResize sets the custom size of a resize of a w×h texture to its smallest candidate when no candidate fits
// within it, so the size a dialog shows for the texture is the size it is processed with.
func FitResize(settings TextureResizeSettings, w, h int) TextureResizeSettings {
	if settings.Mode != "custom" || (settings.Operation != "resize" && settings.Operation != "resize_and_convert") {
		return settings
	}
	candidates := ResizeCandidates(w, h)
	if len(candidates) == 0 {
		return settings
	}
	if _, ok := PickResizeCandidate(candidates, settings.CustomWidth, settings.CustomHeight); !ok {
		settings.CustomWidth, settings.CustomHeight = candidates[0].Width, candidates[0].Height
	}
	return settings
}

// SharedSettings are the settings applied to several textures at once, with an output format they all accept.
func SharedSettings(settings TextureResizeSettings, items []TextureResizeListItem) TextureResizeSettings {
	formats := SharedOutputFormats(items)
	if settings.OutputFormat == "" || !slices.Contains(formats, settings.OutputFormat) {
		settings.OutputFormat = ""
		if len(formats) > 0 {
			settings.OutputFormat = formats[0]
		}
	}
	return settings
}

// SharedOutputFormats lists the output formats every item accepts, in the first item's order.
func SharedOutputFormats(items []TextureResizeListItem) []string {
	if len(items) == 0 {
		return nil
	}
	var out []string
	for _, format := range items[0].AvailableOutputFormats {
		if !slices.ContainsFunc(items, func(item TextureResizeListItem) bool {
			return !slices.Contains(item.AvailableOutputFormats, format)
		}) {
			out = append(out, format)
		}
	}
	return out
}

// SharedColorSpace is linear when any item is linear, the items' color space when they agree, else "unknown".
func SharedColorSpace(items []TextureResizeListItem) string {
	if len(items) == 0 {
		return "unknown"
	}
	if slices.ContainsFunc(items, func(item TextureResizeListItem) bool { return item.ColorSpace == "linear" }) {
		return "linear"
	}
	first := items[0].ColorSpace
	for _, item := range items[1:] {
		if item.ColorSpace != first {
			return "unknown"
		}
	}
	return first
}

// SharedConversionMessage is the items' format conversion message when they all share it.
func SharedConversionMessage(items []TextureResizeListItem) *string {
	if len(items) == 0 || items[0].FormatConversionMessage == nil {
		return nil
	}
	first := *items[0].FormatConversionMessage
	for _, item := range items[1:] {
		if item.FormatConversionMessage == nil || *item.FormatConversionMessage != first {
			return nil
		}
	}
	return items[0].FormatConversionMessage
}

// FormatLabel shows a DXGI format without its prefix, as "BC7 UNORM SRGB".
func FormatLabel(format string) string {
	return strings.ReplaceAll(strings.TrimPrefix(format, "DXGI_FORMAT_"), "_", " ")
}

// SettingsPatch turns settings into a patch that sets every field.
func SettingsPatch(s TextureResizeSettings) TextureResizeSettingsPatch {
	return TextureResizeSettingsPatch{
		Mode: &s.Mode, Operation: &s.Operation, Percent: &s.Percent, CustomWidth: &s.CustomWidth,
		CustomHeight: &s.CustomHeight, OutputFormat: &s.OutputFormat, Backup: &s.Backup,
		UpscaleScale: &s.UpscaleScale, UpscaleModel: &s.UpscaleModel,
	}
}
