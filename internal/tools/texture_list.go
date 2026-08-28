package tools

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const (
	textureMinDimension = 1024
	textureStep         = 1024
	ddsMagic            = 0x20534444
	ddsHeaderSize       = 124
	ddpfFourCC          = 0x4
	ddpfRGB             = 0x40
	ddsMipMapFlag       = 0x00020000
	ddsCubemapFlag      = 0x200
)

var textureSettingKeys = struct {
	mode, operation, format, percent, width, height, backup, scale, model string
}{"texture_resize_mode", "texture_resize_operation", "texture_resize_output_format", "texture_resize_percent", "texture_resize_custom_width", "texture_resize_custom_height", "texture_resize_backup", "texture_resize_upscale_scale", "texture_resize_upscale_model"}

var srgbTextureFormats = []string{
	"DXGI_FORMAT_R8G8B8A8_UNORM_SRGB", "DXGI_FORMAT_B8G8R8A8_UNORM_SRGB", "DXGI_FORMAT_BC1_UNORM_SRGB",
	"DXGI_FORMAT_BC2_UNORM_SRGB", "DXGI_FORMAT_BC3_UNORM_SRGB", "DXGI_FORMAT_BC7_UNORM_SRGB",
}

var linearTextureFormats = []string{
	"DXGI_FORMAT_R8_UNORM", "DXGI_FORMAT_R8_SNORM", "DXGI_FORMAT_R8G8_UNORM", "DXGI_FORMAT_R8G8_SNORM",
	"DXGI_FORMAT_R8G8B8A8_UNORM", "DXGI_FORMAT_R8G8B8A8_SNORM", "DXGI_FORMAT_R16_UNORM", "DXGI_FORMAT_R16_SNORM",
	"DXGI_FORMAT_R16_FLOAT", "DXGI_FORMAT_R16G16_UNORM", "DXGI_FORMAT_R16G16_SNORM", "DXGI_FORMAT_R16G16_FLOAT",
	"DXGI_FORMAT_R16G16B16A16_UNORM", "DXGI_FORMAT_R16G16B16A16_SNORM", "DXGI_FORMAT_R16G16B16A16_FLOAT",
	"DXGI_FORMAT_R32_FLOAT", "DXGI_FORMAT_R32G32_FLOAT", "DXGI_FORMAT_R32G32B32_FLOAT", "DXGI_FORMAT_R32G32B32A32_FLOAT",
	"DXGI_FORMAT_B8G8R8A8_UNORM", "DXGI_FORMAT_B4G4R4A4_UNORM", "DXGI_FORMAT_B5G5R5A1_UNORM", "DXGI_FORMAT_BC1_UNORM",
	"DXGI_FORMAT_BC2_UNORM", "DXGI_FORMAT_BC3_UNORM", "DXGI_FORMAT_BC4_UNORM", "DXGI_FORMAT_BC4_SNORM",
	"DXGI_FORMAT_BC5_UNORM", "DXGI_FORMAT_BC5_SNORM", "DXGI_FORMAT_BC6H_UF16", "DXGI_FORMAT_BC6H_SF16", "DXGI_FORMAT_BC7_UNORM",
}

var dxgiNames = map[uint32]string{
	0: "DXGI_FORMAT_UNKNOWN", 2: "DXGI_FORMAT_R32G32B32A32_FLOAT", 10: "DXGI_FORMAT_R16G16B16A16_FLOAT",
	24: "DXGI_FORMAT_R10G10B10A2_UNORM", 28: "DXGI_FORMAT_R8G8B8A8_UNORM", 29: "DXGI_FORMAT_R8G8B8A8_UNORM_SRGB",
	41: "DXGI_FORMAT_R32_FLOAT", 49: "DXGI_FORMAT_R8G8_UNORM", 54: "DXGI_FORMAT_R16_FLOAT", 56: "DXGI_FORMAT_R16_UNORM",
	57: "DXGI_FORMAT_R16_UINT", 58: "DXGI_FORMAT_R16_SNORM", 60: "DXGI_FORMAT_R8G8_SNORM", 61: "DXGI_FORMAT_R8_UNORM",
	63: "DXGI_FORMAT_R8_SNORM", 71: "DXGI_FORMAT_BC1_UNORM", 72: "DXGI_FORMAT_BC1_UNORM_SRGB", 74: "DXGI_FORMAT_BC2_UNORM",
	75: "DXGI_FORMAT_BC2_UNORM_SRGB", 77: "DXGI_FORMAT_BC3_UNORM", 78: "DXGI_FORMAT_BC3_UNORM_SRGB",
	80: "DXGI_FORMAT_BC4_UNORM", 81: "DXGI_FORMAT_BC4_SNORM", 83: "DXGI_FORMAT_BC5_UNORM", 84: "DXGI_FORMAT_BC5_SNORM",
	87: "DXGI_FORMAT_B8G8R8A8_UNORM", 88: "DXGI_FORMAT_B8G8R8X8_UNORM", 91: "DXGI_FORMAT_B8G8R8A8_UNORM_SRGB",
	93: "DXGI_FORMAT_B8G8R8X8_UNORM_SRGB", 95: "DXGI_FORMAT_BC6H_UF16", 96: "DXGI_FORMAT_BC6H_SF16",
	98: "DXGI_FORMAT_BC7_UNORM", 99: "DXGI_FORMAT_BC7_UNORM_SRGB", 115: "DXGI_FORMAT_B4G4R4A4_UNORM",
}

type TextureResizeSettings struct {
	Mode         string `json:"mode"`
	Operation    string `json:"operation"`
	Percent      int    `json:"percent"`
	CustomWidth  int    `json:"customWidth"`
	CustomHeight int    `json:"customHeight"`
	OutputFormat string `json:"outputFormat"`
	Backup       bool   `json:"backup"`
	UpscaleScale int    `json:"upscaleScale"`
	UpscaleModel string `json:"upscaleModel"`
}

type TextureResizeSettingsPatch struct {
	Mode         *string `json:"mode,omitempty"`
	Operation    *string `json:"operation,omitempty"`
	Percent      *int    `json:"percent,omitempty"`
	CustomWidth  *int    `json:"customWidth,omitempty"`
	CustomHeight *int    `json:"customHeight,omitempty"`
	OutputFormat *string `json:"outputFormat,omitempty"`
	Backup       *bool   `json:"backup,omitempty"`
	UpscaleScale *int    `json:"upscaleScale,omitempty"`
	UpscaleModel *string `json:"upscaleModel,omitempty"`
}

type TextureResizeListItem struct {
	FilePath                string   `json:"filePath"`
	RelativePath            string   `json:"relativePath"`
	FileName                string   `json:"fileName"`
	FileSize                int64    `json:"fileSize"`
	Format                  string   `json:"format"`
	ColorSpace              string   `json:"colorSpace"`
	LayerCount              int      `json:"layerCount"`
	MipLevelCount           int      `json:"mipLevelCount"`
	OriginalWidth           int      `json:"originalWidth"`
	OriginalHeight          int      `json:"originalHeight"`
	TargetWidth             int      `json:"targetWidth"`
	TargetHeight            int      `json:"targetHeight"`
	CanResize               bool     `json:"canResize"`
	CanUpscale              bool     `json:"canUpscale"`
	CanConvertFormat        bool     `json:"canConvertFormat"`
	CanProcess              bool     `json:"canProcess"`
	AvailableOutputFormats  []string `json:"availableOutputFormats"`
	OutputFormatDefault     string   `json:"outputFormatDefault"`
	FormatConversionMessage *string  `json:"formatConversionMessage,omitempty"`
	Message                 *string  `json:"message,omitempty"`
}

type ddsMetadata struct {
	width, height      int
	format, colorSpace string
	layers, mipmaps    int
}
type ddsHeaderError struct{ message string }

func (e ddsHeaderError) Error() string { return e.message }

func defaultTextureSettings() TextureResizeSettings {
	return TextureResizeSettings{Mode: "custom", Operation: "resize", Percent: 50, CustomWidth: 2048, CustomHeight: 2048, Backup: true, UpscaleScale: 2, UpscaleModel: "realesr-animevideov3"}
}

func (t *Tools) GetTextureResizeSettings(ctx context.Context) (TextureResizeSettings, error) {
	client, err := t.requireClient()
	if err != nil {
		return TextureResizeSettings{}, err
	}
	keys := []string{textureSettingKeys.mode, textureSettingKeys.operation, textureSettingKeys.format, textureSettingKeys.percent,
		textureSettingKeys.width, textureSettingKeys.height, textureSettingKeys.backup, textureSettingKeys.scale, textureSettingKeys.model}
	values := make(map[string]string, len(keys))
	for _, key := range keys {
		value, getErr := client.Settings.GetValue(ctx, key)
		if getErr != nil {
			return TextureResizeSettings{}, getErr
		}
		if value != nil {
			values[key] = *value
		}
	}
	s := defaultTextureSettings()
	s.Mode = normalizeTextureMode(values[textureSettingKeys.mode])
	s.Operation = normalizeTextureOperation(values[textureSettingKeys.operation])
	s.OutputFormat = normalizeTextureFormat(values[textureSettingKeys.format])
	if value, err := strconv.Atoi(values[textureSettingKeys.percent]); err == nil {
		s.Percent = normalizeTexturePercent(value)
	}
	if value, err := strconv.Atoi(values[textureSettingKeys.width]); err == nil {
		s.CustomWidth = normalizeTextureDimension(value)
	}
	if value, err := strconv.Atoi(values[textureSettingKeys.height]); err == nil {
		s.CustomHeight = normalizeTextureDimension(value)
	}
	if raw := values[textureSettingKeys.backup]; raw != "" {
		s.Backup = raw == "1" || strings.EqualFold(raw, "true")
	}
	s.UpscaleModel = normalizeUpscaleModel(values[textureSettingKeys.model])
	requestedScale := s.UpscaleScale
	if raw := values[textureSettingKeys.scale]; raw != "" {
		requestedScale = parseInt(raw)
	}
	s.UpscaleScale = normalizeUpscaleScale(s.UpscaleModel, requestedScale)
	return s, nil
}

func (t *Tools) SaveTextureResizeSettings(ctx context.Context, patch TextureResizeSettingsPatch) (TextureResizeSettings, error) {
	current, err := t.GetTextureResizeSettings(ctx)
	if err != nil {
		return TextureResizeSettings{}, err
	}
	if patch.Mode != nil {
		current.Mode = normalizeTextureMode(*patch.Mode)
	}
	if patch.Operation != nil {
		current.Operation = normalizeTextureOperation(*patch.Operation)
	}
	if patch.Percent != nil {
		current.Percent = normalizeTexturePercent(*patch.Percent)
	}
	if patch.CustomWidth != nil {
		current.CustomWidth = normalizeTextureDimension(*patch.CustomWidth)
	}
	if patch.CustomHeight != nil {
		current.CustomHeight = normalizeTextureDimension(*patch.CustomHeight)
	}
	if patch.OutputFormat != nil {
		current.OutputFormat = normalizeTextureFormat(*patch.OutputFormat)
	}
	if patch.Backup != nil {
		current.Backup = *patch.Backup
	}
	if patch.UpscaleModel != nil {
		current.UpscaleModel = normalizeUpscaleModel(*patch.UpscaleModel)
	}
	if patch.UpscaleScale != nil {
		current.UpscaleScale = normalizeUpscaleScale(current.UpscaleModel, *patch.UpscaleScale)
	} else {
		current.UpscaleScale = normalizeUpscaleScale(current.UpscaleModel, current.UpscaleScale)
	}
	client, _ := t.requireClient()
	values := map[string]string{textureSettingKeys.mode: current.Mode, textureSettingKeys.operation: current.Operation, textureSettingKeys.format: current.OutputFormat,
		textureSettingKeys.percent: strconv.Itoa(current.Percent), textureSettingKeys.width: strconv.Itoa(current.CustomWidth), textureSettingKeys.height: strconv.Itoa(current.CustomHeight),
		textureSettingKeys.backup: map[bool]string{true: "1", false: "0"}[current.Backup], textureSettingKeys.scale: strconv.Itoa(current.UpscaleScale), textureSettingKeys.model: current.UpscaleModel}
	for key, value := range values {
		if err := client.Settings.Upsert(ctx, key, &value); err != nil {
			return TextureResizeSettings{}, err
		}
	}
	return current, nil
}

func (t *Tools) ListTextureFolder(ctx context.Context, targetPath string, patch *TextureResizeSettingsPatch) ([]TextureResizeListItem, error) {
	settings, err := t.GetTextureResizeSettings(ctx)
	if err != nil {
		return nil, err
	}
	if patch != nil {
		settings = mergeTexturePatch(settings, *patch)
	}
	trimmed := strings.TrimSpace(targetPath)
	if trimmed == "" {
		return nil, contractError("Target path is required.")
	}
	root, err := filepath.Abs(trimmed)
	if err != nil {
		return nil, err
	}
	files, err := resolveDDSFiles(root)
	if err != nil {
		return nil, err
	}
	items := make([]TextureResizeListItem, 0, len(files))
	for _, path := range files {
		item, itemErr := buildTextureItem(path, root, settings)
		var headerErr ddsHeaderError
		if errors.As(itemErr, &headerErr) {
			continue
		}
		if itemErr != nil {
			return nil, itemErr
		}
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		pi := items[i].OriginalWidth * items[i].OriginalHeight
		pj := items[j].OriginalWidth * items[j].OriginalHeight
		if pi != pj {
			return pi > pj
		}
		if items[i].FileName != items[j].FileName {
			return items[i].FileName < items[j].FileName
		}
		return items[i].RelativePath < items[j].RelativePath
	})
	return items, nil
}

func (t *Tools) ListTextureMod(ctx context.Context, modPath string, patch *TextureResizeSettingsPatch) ([]TextureResizeListItem, error) {
	return t.ListTextureFolder(ctx, modPath, patch)
}

func mergeTexturePatch(s TextureResizeSettings, p TextureResizeSettingsPatch) TextureResizeSettings {
	if p.Mode != nil {
		s.Mode = normalizeTextureMode(*p.Mode)
	}
	if p.Operation != nil {
		s.Operation = normalizeTextureOperation(*p.Operation)
	}
	if p.Percent != nil {
		s.Percent = normalizeTexturePercent(*p.Percent)
	}
	if p.CustomWidth != nil {
		s.CustomWidth = normalizeTextureDimension(*p.CustomWidth)
	}
	if p.CustomHeight != nil {
		s.CustomHeight = normalizeTextureDimension(*p.CustomHeight)
	}
	if p.OutputFormat != nil {
		s.OutputFormat = normalizeTextureFormat(*p.OutputFormat)
	}
	if p.Backup != nil {
		s.Backup = *p.Backup
	}
	if p.UpscaleModel != nil {
		s.UpscaleModel = normalizeUpscaleModel(*p.UpscaleModel)
	}
	if p.UpscaleScale != nil {
		s.UpscaleScale = *p.UpscaleScale
	}
	s.UpscaleScale = normalizeUpscaleScale(s.UpscaleModel, s.UpscaleScale)
	return s
}

func resolveDDSFiles(path string) ([]string, error) {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, contractError(fmt.Sprintf("Target path '%s' does not exist.", path))
	}
	if err != nil {
		return nil, err
	}
	if info.Mode().IsRegular() {
		if !strings.EqualFold(filepath.Ext(path), ".dds") {
			return nil, contractError(fmt.Sprintf("Target file '%s' is not a DDS texture.", path))
		}
		return []string{path}, nil
	}
	if !info.IsDir() {
		return nil, contractError(fmt.Sprintf("Target path '%s' must be a directory or DDS file.", path))
	}
	var files []string
	err = filepath.WalkDir(path, func(p string, e fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil //nolint:nilerr // Electron glob skips unreadable descendants.
		}
		if !e.IsDir() && strings.EqualFold(filepath.Ext(e.Name()), ".dds") {
			files = append(files, p)
		}
		return nil
	})
	sort.Strings(files)
	return files, err
}

func buildTextureItem(path, root string, s TextureResizeSettings) (TextureResizeListItem, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return TextureResizeListItem{}, err
	}
	m, err := parseDDS(raw)
	if err != nil {
		return TextureResizeListItem{}, err
	}
	candidates := textureCandidates(m.width, m.height)
	target, canResize := pickTextureCandidate(candidates, textureBounds(m.width, m.height, s))
	tw, th := m.width, m.height
	if s.Operation == "upscale" || s.Operation == "upscale_and_convert" {
		if m.width*s.UpscaleScale <= 8192 && m.height*s.UpscaleScale <= 8192 {
			tw, th = m.width*s.UpscaleScale, m.height*s.UpscaleScale
		}
	} else if s.Operation != "convert" && canResize {
		tw, th = target[0], target[1]
	}
	formats := availableFormats(m.colorSpace)
	defaultFormat := m.format
	if !contains(formats, defaultFormat) {
		if contains(formats, s.OutputFormat) {
			defaultFormat = s.OutputFormat
		} else if len(formats) > 0 {
			defaultFormat = formats[0]
		}
	}
	upscaleReason := textureUpscaleSkipReason(m, s.UpscaleScale)
	canUpscale := upscaleReason == nil
	var conversionMessage *string
	if m.colorSpace == "unknown" {
		v := "DDS color space could not be detected. Choose either an sRGB or Linear output format before converting."
		conversionMessage = &v
	}
	var message *string
	if (s.Operation == "upscale" || s.Operation == "upscale_and_convert") && !canUpscale {
		message = upscaleReason
	} else if s.Operation != "convert" && s.Operation != "upscale" && s.Operation != "upscale_and_convert" && !canResize {
		v := "No valid downscale candidate matched the requested bounds."
		message = &v
	}
	rel, _ := filepath.Rel(root, path)
	if rel == "." || strings.HasPrefix(rel, "..") {
		rel = filepath.Base(path)
	}
	return TextureResizeListItem{FilePath: path, RelativePath: rel, FileName: filepath.Base(path), FileSize: int64(len(raw)), Format: m.format, ColorSpace: m.colorSpace, LayerCount: m.layers, MipLevelCount: m.mipmaps, OriginalWidth: m.width, OriginalHeight: m.height, TargetWidth: tw, TargetHeight: th, CanResize: canResize, CanUpscale: canUpscale, CanConvertFormat: len(formats) > 0, CanProcess: canUpscale || len(formats) > 0 || len(candidates) > 0, AvailableOutputFormats: formats, OutputFormatDefault: defaultFormat, FormatConversionMessage: conversionMessage, Message: message}, nil
}

func textureUpscaleSkipReason(m ddsMetadata, scale int) *string {
	if m.layers > 1 {
		return stringPointer("Cubemap and layered DDS textures cannot be upscaled.")
	}
	if strings.Contains(m.format, "BC4") || strings.Contains(m.format, "BC5") || strings.Contains(m.format, "BC6H") {
		return stringPointer("This DDS format cannot be upscaled without destroying channel data.")
	}
	if m.width*scale > 8192 || m.height*scale > 8192 {
		return stringPointer("Upscaled dimensions would exceed the 8192px limit.")
	}
	return nil
}

func parseDDS(b []byte) (ddsMetadata, error) {
	if len(b) < 132 {
		return ddsMetadata{}, ddsHeaderError{"DDS header could not be parsed: file is too small."}
	}
	u := func(o int) uint32 { return binary.LittleEndian.Uint32(b[o : o+4]) }
	if u(0) != ddsMagic {
		return ddsMetadata{}, ddsHeaderError{"DDS header could not be parsed: invalid magic."}
	}
	if u(4) != ddsHeaderSize {
		return ddsMetadata{}, ddsHeaderError{"DDS header could not be parsed: invalid header size."}
	}
	w, h := int(u(16)), int(u(12))
	if w == 0 || h == 0 {
		return ddsMetadata{}, ddsHeaderError{"DDS header could not be parsed: invalid dimensions."}
	}
	mips := 1
	if u(8)&ddsMipMapFlag != 0 {
		mips = max(1, int(u(28)))
	}
	layers := 1
	if u(112)&ddsCubemapFlag != 0 {
		layers = 6
	}
	format := detectDDSFormat(u)
	color := "unknown"
	if strings.HasSuffix(format, "_SRGB") {
		color = "srgb"
	} else if u(80)&ddpfFourCC != 0 && u(84) == 0x30315844 {
		if containsUint([]uint32{72, 75, 78, 29, 91, 93, 99}, u(128)) {
			color = "srgb"
		} else if containsUint([]uint32{71, 74, 77, 80, 81, 83, 84, 95, 96, 28, 87, 88, 98}, u(128)) {
			color = "linear"
		}
	}
	return ddsMetadata{w, h, format, color, layers, mips}, nil
}

func detectDDSFormat(u func(int) uint32) string {
	flags, four := u(80), u(84)
	if flags&ddpfFourCC != 0 {
		if four == 0x30315844 {
			if name, ok := dxgiNames[u(128)]; ok {
				return name
			}
			return fmt.Sprintf("DXGI_FORMAT_%d", u(128))
		}
		legacy := map[uint32]string{0x31545844: "DXGI_FORMAT_BC1_UNORM", 0x33545844: "DXGI_FORMAT_BC2_UNORM", 0x35545844: "DXGI_FORMAT_BC3_UNORM", 0x31495441: "DXGI_FORMAT_BC4_UNORM", 0x55344342: "DXGI_FORMAT_BC4_UNORM", 0x53344342: "DXGI_FORMAT_BC4_SNORM", 0x32495441: "DXGI_FORMAT_BC5_UNORM", 0x55354342: "DXGI_FORMAT_BC5_UNORM", 0x53354342: "DXGI_FORMAT_BC5_SNORM"}
		if name := legacy[four]; name != "" {
			return name
		}
	}
	if flags&ddpfRGB != 0 {
		bits, r, g, b, a := u(88), u(92), u(96), u(100), u(104)
		if bits == 32 && r == 0xff0000 && g == 0xff00 && b == 0xff && a == 0xff000000 {
			return "DXGI_FORMAT_B8G8R8A8_UNORM"
		}
		if bits == 32 && r == 0xff && g == 0xff00 && b == 0xff0000 && a == 0xff000000 {
			return "DXGI_FORMAT_R8G8B8A8_UNORM"
		}
		if bits == 32 && r == 0xff0000 && g == 0xff00 && b == 0xff && a == 0 {
			return "DXGI_FORMAT_B8G8R8X8_UNORM"
		}
	}
	return "UNKNOWN_DDS_FORMAT"
}

func textureCandidates(w, h int) [][2]int {
	if w < 1024 || h < 1024 {
		return nil
	}
	g := gcdInt(w, h)
	rw, rh := w/g, h/g
	maxScale := min(w/(rw*1024), h/(rh*1024))
	var out [][2]int
	for scale := 1; scale <= maxScale; scale++ {
		cw, ch := rw*1024*scale, rh*1024*scale
		if cw < w || ch < h {
			out = append(out, [2]int{cw, ch})
		}
	}
	return out
}
func textureBounds(w, h int, s TextureResizeSettings) [2]int {
	if s.Mode == "percent" {
		return [2]int{w * s.Percent / 100, h * s.Percent / 100}
	}
	return [2]int{s.CustomWidth, s.CustomHeight}
}
func pickTextureCandidate(c [][2]int, b [2]int) ([2]int, bool) {
	var best [2]int
	ok := false
	for _, v := range c {
		if v[0] <= b[0] && v[1] <= b[1] && (!ok || v[0]*v[1] > best[0]*best[1]) {
			best = v
			ok = true
		}
	}
	return best, ok
}
func gcdInt(a, b int) int {
	for b != 0 {
		a, b = b, a%b
	}
	return a
}
func availableFormats(color string) []string {
	if color == "srgb" {
		return append([]string{}, srgbTextureFormats...)
	}
	if color == "linear" {
		return append([]string{}, linearTextureFormats...)
	}
	return append(append([]string{}, srgbTextureFormats...), linearTextureFormats...)
}
func contains(values []string, target string) bool {
	for _, v := range values {
		if v == target {
			return true
		}
	}
	return false
}
func containsUint(values []uint32, target uint32) bool {
	for _, v := range values {
		if v == target {
			return true
		}
	}
	return false
}
func parseInt(v string) int { n, _ := strconv.Atoi(v); return n }
func normalizeTextureMode(v string) string {
	if v == "percent" {
		return v
	}
	return "custom"
}
func normalizeTextureOperation(v string) string {
	switch v {
	case "convert", "resize_and_convert", "upscale", "upscale_and_convert":
		return v
	}
	return "resize"
}
func normalizeTexturePercent(v int) int {
	return max(1, min(99, v))
}
func normalizeTextureDimension(v int) int {
	v = max(1024, v)
	r := v % 1024
	if r == 0 {
		return v
	}
	if r >= 512 {
		return v + (1024 - r)
	}
	return v - r
}
func normalizeTextureFormat(v string) string {
	if contains(srgbTextureFormats, v) || contains(linearTextureFormats, v) {
		return v
	}
	return ""
}
func normalizeUpscaleModel(v string) string {
	if contains([]string{"realesr-animevideov3", "realesrgan-x4plus-anime", "realesrgan-x4plus", "realcugan-pro", "realcugan-se", "realcugan-nose"}, v) {
		return v
	}
	return "realesr-animevideov3"
}
func normalizeUpscaleScale(model string, scale int) int {
	available := []int{4}
	switch model {
	case "realesr-animevideov3", "realcugan-se":
		available = []int{2, 3, 4}
	case "realcugan-pro":
		available = []int{2, 3}
	case "realcugan-nose":
		available = []int{2}
	}
	for _, v := range available {
		if v == scale {
			return scale
		}
	}
	return available[len(available)-1]
}
