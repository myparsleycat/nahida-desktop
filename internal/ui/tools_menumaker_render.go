package ui

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"strings"
	"sync"

	"golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/f64"
	"golang.org/x/image/math/fixed"
	"golang.org/x/image/vector"

	"nahida.live/desktop/internal/tools/menumaker"
)

const (
	menuMakerBaseSlotSize       = 64
	menuMakerBasePanelImageSize = 256
	menuMakerCropPreviewSize    = 256
)

// errMenuMakerFont stops an export when the embedded Inter font cannot be read, as the React frontend did when
// its web font failed to load.
var errMenuMakerFont = errors.New("MENU_MAKER_INTER_FONT_LOAD_FAILED")

var menuMakerFont = sync.OnceValues(func() (*opentype.Font, error) {
	data, err := fontFiles.ReadFile("fonts/Inter-Medium.ttf")
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errMenuMakerFont, err)
	}
	parsed, err := opentype.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errMenuMakerFont, err)
	}
	return parsed, nil
})

// menuMakerCanvas draws the way the React frontend's 2D canvas did, so that exported menus look the same.
type menuMakerCanvas struct {
	img   *image.RGBA
	font  *opentype.Font
	faces map[float64]font.Face
}

func newMenuMakerCanvas(width, height int, fnt *opentype.Font) *menuMakerCanvas {
	return &menuMakerCanvas{
		img:   image.NewRGBA(image.Rect(0, 0, max(1, width), max(1, height))),
		font:  fnt,
		faces: map[float64]font.Face{},
	}
}

func (c *menuMakerCanvas) close() {
	for _, face := range c.faces {
		_ = face.Close()
	}
}

func (c *menuMakerCanvas) face(size float64) (font.Face, error) {
	if face, ok := c.faces[size]; ok {
		return face, nil
	}
	face, err := opentype.NewFace(c.font, &opentype.FaceOptions{Size: size, DPI: 72, Hinting: font.HintingNone})
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errMenuMakerFont, err)
	}
	c.faces[size] = face
	return face, nil
}

func (c *menuMakerCanvas) paint(mask image.Image, col color.NRGBA) {
	draw.DrawMask(c.img, c.img.Bounds(), image.NewUniform(col), image.Point{}, mask, image.Point{}, draw.Over)
}

func (c *menuMakerCanvas) fillRect(col color.NRGBA) {
	draw.Draw(c.img, c.img.Bounds(), image.NewUniform(col), image.Point{}, draw.Over)
}

// strokeRect strokes the rectangle centred on its edges, as CanvasRenderingContext2D.strokeRect does.
func (c *menuMakerCanvas) strokeRect(x, y, w, h, lineWidth float32, col color.NRGBA) {
	b := c.img.Bounds()
	var r vector.Rasterizer
	r.Reset(b.Dx(), b.Dy())
	half := lineWidth / 2
	rect := func(x0, y0, x1, y1 float32, clockwise bool) {
		if x1 <= x0 || y1 <= y0 {
			return
		}
		r.MoveTo(x0, y0)
		if clockwise {
			r.LineTo(x1, y0)
			r.LineTo(x1, y1)
			r.LineTo(x0, y1)
		} else {
			r.LineTo(x0, y1)
			r.LineTo(x1, y1)
			r.LineTo(x1, y0)
		}
		r.ClosePath()
	}
	rect(x-half, y-half, x+w+half, y+h+half, true)
	rect(x+half, y+half, x+w-half, y+h-half, false)
	mask := image.NewAlpha(b)
	r.Draw(mask, b, image.Opaque, image.Point{})
	c.paint(mask, col)
}

// drawImage draws src stretched over the rectangle with the opacity alpha.
func (c *menuMakerCanvas) drawImage(src image.Image, x, y, w, h float64, alpha float64) {
	sb := src.Bounds()
	if sb.Empty() || w <= 0 || h <= 0 {
		return
	}
	sx, sy := w/float64(sb.Dx()), h/float64(sb.Dy())
	transform := f64.Aff3{sx, 0, x - float64(sb.Min.X)*sx, 0, sy, y - float64(sb.Min.Y)*sy}
	var options *draw.Options
	if alpha < 1 {
		options = &draw.Options{SrcMask: image.NewUniform(color.Alpha{A: uint8(math.Round(alpha * 255))})}
	}
	draw.CatmullRom.Transform(c.img, transform, src, sb, draw.Over, options)
}

// drawIcon tints the vector's mask with col inside the square at x, y.
func (c *menuMakerCanvas) drawIcon(mask *image.Alpha, x, y int, col color.NRGBA) {
	if mask == nil {
		return
	}
	r := mask.Bounds().Add(image.Pt(x, y))
	draw.DrawMask(c.img, r, image.NewUniform(col), image.Point{}, mask, image.Point{}, draw.Over)
}

type menuMakerText struct {
	size     float64
	align    string // left, center, right
	baseline string // alphabetic, middle, top
	fill     color.NRGBA
	shadow   color.NRGBA
	blur     float64
}

// fillText draws text with the canvas's textAlign, textBaseline and shadowBlur semantics.
func (c *menuMakerCanvas) fillText(text string, x, y float64, style menuMakerText) error {
	if text == "" {
		return nil
	}
	face, err := c.face(style.size)
	if err != nil {
		return err
	}
	width := float64(font.MeasureString(face, text)) / 64
	switch style.align {
	case "center":
		x -= width / 2
	case "right":
		x -= width
	}
	metrics := face.Metrics()
	ascent, descent := float64(metrics.Ascent)/64, float64(metrics.Descent)/64
	switch style.baseline {
	case "middle":
		y += (ascent - descent) / 2
	case "top":
		y += ascent
	}

	mask := image.NewAlpha(c.img.Bounds())
	drawer := font.Drawer{Dst: mask, Src: image.Opaque, Face: face, Dot: fixed.Point26_6{
		X: fixed.Int26_6(math.Round(x * 64)), Y: fixed.Int26_6(math.Round(y * 64)),
	}}
	drawer.DrawString(text)
	if style.blur > 0 && style.shadow.A > 0 {
		c.paint(gaussianBlur(mask, style.blur/2), style.shadow)
	}
	c.paint(mask, style.fill)
	return nil
}

// gaussianBlur approximates a Gaussian blur of the given sigma with three box blurs.
func gaussianBlur(src *image.Alpha, sigma float64) *image.Alpha {
	if sigma <= 0 {
		return src
	}
	w, h := src.Rect.Dx(), src.Rect.Dy()
	values := make([]float64, len(src.Pix))
	for i, v := range src.Pix {
		values[i] = float64(v)
	}
	scratch := make([]float64, len(values))
	for _, size := range boxSizes(sigma, 3) {
		radius := (size - 1) / 2
		boxBlur(values, scratch, w, h, radius, true)
		boxBlur(scratch, values, w, h, radius, false)
	}
	out := image.NewAlpha(src.Rect)
	for i, v := range values {
		out.Pix[i] = uint8(min(255, max(0, math.Round(v))))
	}
	return out
}

func boxSizes(sigma float64, n int) []int {
	ideal := math.Sqrt(12*sigma*sigma/float64(n) + 1)
	lower := int(math.Floor(ideal))
	if lower%2 == 0 {
		lower--
	}
	upper := lower + 2
	m := int(math.Round((12*sigma*sigma - float64(n*lower*lower) - float64(4*n*lower) - float64(3*n)) /
		float64(-4*lower-4)))
	sizes := make([]int, n)
	for i := range sizes {
		if i < m {
			sizes[i] = lower
		} else {
			sizes[i] = upper
		}
	}
	return sizes
}

func boxBlur(src, dst []float64, w, h, radius int, horizontal bool) {
	if radius <= 0 {
		copy(dst, src)
		return
	}
	lines, length := h, w
	if !horizontal {
		lines, length = w, h
	}
	at := func(line, i int) int {
		if horizontal {
			return line*w + i
		}
		return i*w + line
	}
	scale := 1 / float64(2*radius+1)
	for line := range lines {
		sum := 0.0
		for i := -radius; i <= radius; i++ {
			if i >= 0 && i < length {
				sum += src[at(line, i)]
			}
		}
		for i := range length {
			dst[at(line, i)] = sum * scale
			if out := i - radius; out >= 0 {
				sum -= src[at(line, out)]
			}
			if in := i + radius + 1; in < length {
				sum += src[at(line, in)]
			}
		}
	}
}

func (c *menuMakerCanvas) png() ([]byte, error) {
	var out bytes.Buffer
	if err := png.Encode(&out, c.img); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// menuMakerColor parses #rgb or #rrggbb with an alpha of 0 to 255, as the frontend's rgba helper did.
func menuMakerColor(hex string, alpha int) color.NRGBA {
	value := strings.TrimPrefix(hex, "#")
	if len(value) == 3 {
		value = string([]byte{value[0], value[0], value[1], value[1], value[2], value[2]})
	}
	var r, g, b uint8
	if len(value) == 6 {
		_, _ = fmt.Sscanf(value, "%02x%02x%02x", &r, &g, &b)
	}
	return color.NRGBA{R: r, G: g, B: b, A: uint8(min(255, max(0, alpha)))}
}

func truncateMenuMakerText(value string, length int) string {
	runes := []rune(value)
	if len(runes) <= length {
		return value
	}
	return string(runes[:length-1]) + "…"
}

// decodeDataURL decodes a base64 data URL such as the PNGs the crop dialog produces.
func decodeDataURL(dataURL string) (image.Image, error) {
	_, payload, ok := strings.Cut(dataURL, ";base64,")
	if !ok {
		return nil, errors.New("menu maker: not a base64 data URL")
	}
	data, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return nil, err
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	return img, err
}

func pngDataURL(data []byte) string {
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(data)
}

// renderMenuMakerAssets draws the textures a generated menu uses: the shader, the panel background, the title
// and an idle and a hover image of every slot state.
func renderMenuMakerAssets(
	slots []menuMakerSlot,
	settings menuMakerSettings,
	geometry menumaker.MenuMakerGeometry,
	slotStates []menumaker.MenuMakerSlotStateGroup,
) ([]menumaker.MenuMakerGeneratedAsset, error) {
	fnt, err := menuMakerFont()
	if err != nil {
		return nil, err
	}
	palette := settings.Palette
	assets := []menumaker.MenuMakerGeneratedAsset{
		{RelativePath: "res_gui/draw_2d.hlsl", Data: []byte(menumaker.Shader)},
	}
	add := func(path string, canvas *menuMakerCanvas) error {
		defer canvas.close()
		data, err := canvas.png()
		if err != nil {
			return err
		}
		assets = append(assets, menumaker.MenuMakerGeneratedAsset{RelativePath: path, Data: data})
		return nil
	}

	background := newMenuMakerCanvas(geometry.PanelWidth, geometry.PanelHeight, fnt)
	background.fillRect(menuMakerColor(palette.PanelBackground, palette.PanelBackgroundAlpha))
	if settings.PanelImageDataURL != "" {
		panel, err := decodeDataURL(settings.PanelImageDataURL)
		if err != nil {
			return nil, fmt.Errorf("decode panel image: %w", err)
		}
		bounds := background.img.Bounds()
		background.drawImage(panel, 0, 0, float64(bounds.Dx()), float64(bounds.Dy()),
			float64(palette.PanelBackgroundAlpha)/255)
	}
	bounds := background.img.Bounds()
	background.strokeRect(
		1,
		1,
		float32(bounds.Dx()-2),
		float32(bounds.Dy()-2),
		float32(
			max(1, math.Round(2*settings.PanelScale)),
		),
		menuMakerColor(palette.PanelBorder, palette.PanelBorderAlpha),
	)
	if err := add("res_gui/bg.png", background); err != nil {
		return nil, err
	}

	if title := strings.TrimSpace(settings.Title); title != "" && geometry.TitleHeight > 0 {
		scale := settings.PanelScale
		canvas := newMenuMakerCanvas(
			max(1, geometry.PanelWidth-int(math.Round(6*scale))), geometry.TitleHeight, fnt)
		err := canvas.fillText(title, math.Round(10*scale), float64(geometry.TitleHeight)/2, menuMakerText{
			size: max(12, math.Round(18*scale)), baseline: "middle",
			fill:   menuMakerColor(palette.Title, 255),
			shadow: menuMakerColor(palette.TitleShadow, 255), blur: math.Round(3 * scale),
		})
		if err != nil {
			canvas.close()
			return nil, err
		}
		if err := add("res_gui/title.png", canvas); err != nil {
			return nil, err
		}
	}

	index := 0
	for _, slot := range slots {
		if slot.Skip {
			continue
		}
		index++
		states := []menumaker.MenuMakerSlotValueState{{}}
		for _, group := range slotStates {
			if group.SlotID == slot.ID && len(group.States) > 0 {
				states = group.States
				break
			}
		}
		for _, state := range states {
			for _, hover := range []bool{false, true} {
				canvas, err := renderMenuMakerSlot(slot, settings, geometry.SlotSize, hover, state, fnt)
				if err != nil {
					return nil, err
				}
				prefix := "slot_"
				if hover {
					prefix = "slot_hover_"
				}
				if err := add(
					fmt.Sprintf("res_gui/%s%02d%s.png", prefix, index, state.FileSuffix),
					canvas,
				); err != nil {
					return nil, err
				}
			}
		}
	}
	return assets, nil
}

func renderMenuMakerSlot(
	slot menuMakerSlot,
	settings menuMakerSettings,
	size int,
	hover bool,
	state menumaker.MenuMakerSlotValueState,
	fnt *opentype.Font,
) (*menuMakerCanvas, error) {
	palette := settings.Palette
	px := func(value float64) float64 { return math.Round(value * float64(size) / menuMakerBaseSlotSize) }
	canvas := newMenuMakerCanvas(size, size, fnt)
	fail := func(err error) (*menuMakerCanvas, error) {
		canvas.close()
		return nil, err
	}

	if hover || state.Active {
		canvas.fillRect(menuMakerColor(palette.SlotHover, palette.SlotHoverAlpha))
	} else {
		canvas.fillRect(menuMakerColor(palette.SlotBackground, palette.SlotBackgroundAlpha))
	}
	border := menuMakerColor(palette.SlotBorder, palette.SlotBorderAlpha)
	if state.Active {
		border = menuMakerColor(palette.Accent, 255)
	}
	lineWidth := px(2)
	if state.Active && hover {
		lineWidth = px(3)
	}
	canvas.strokeRect(float32(px(1)), float32(px(1)), float32(float64(size)-px(2)), float32(float64(size)-px(2)),
		float32(max(1, lineWidth)), border)

	iconSize := int(max(1, px(36)))
	iconColor := menuMakerColor(slot.Icon.Color, 255)
	switch slot.Icon.Kind {
	case "upload":
		img, err := decodeDataURL(slot.Icon.DataURL)
		if err != nil {
			return fail(fmt.Errorf("decode icon of slot %s: %w", slot.Name, err))
		}
		canvas.drawImage(img, px(12), px(8), max(1, px(40)), max(1, px(40)), 1)
	default:
		if vec := menuMakerIconVector(slot.Icon); vec != nil {
			canvas.drawIcon(vec.Mask(iconSize, iconSize), int(px(14)), int(px(8)), iconColor)
			break
		}
		initial := ""
		if runes := []rune(slot.Name); len(runes) > 0 {
			initial = strings.ToUpper(string(runes[0]))
		}
		err := canvas.fillText(initial, px(32), px(27), menuMakerText{
			size: max(8, px(22)), align: "center", baseline: "middle", fill: iconColor,
		})
		if err != nil {
			return fail(err)
		}
	}

	shadow := color.NRGBA{A: 255}
	label := truncateMenuMakerText(slot.Name, 14)
	if slot.Icon.Kind == "upload" && settings.HideUploadLabel {
		label = ""
	}
	labelX := px(32)
	switch settings.SlotAlignment {
	case "left":
		labelX = px(5)
	case "right":
		labelX = float64(size) - px(5)
	}
	labelY := px(58)
	if settings.ShowKeyHint {
		labelY = px(52)
	}
	err := canvas.fillText(label, labelX, labelY, menuMakerText{
		size: max(6, px(8)), align: settings.SlotAlignment, baseline: "alphabetic",
		fill: color.NRGBA{R: 255, G: 255, B: 255, A: 255}, shadow: shadow, blur: px(2),
	})
	if err == nil && settings.ShowKeyHint {
		err = canvas.fillText(truncateMenuMakerText(slot.Key, 16), labelX, px(61), menuMakerText{
			size: max(5, px(7)), align: settings.SlotAlignment, baseline: "alphabetic",
			fill: menuMakerColor(palette.Accent, 255), shadow: shadow, blur: px(2),
		})
	}
	if err == nil && state.Value != "" {
		err = canvas.fillText(truncateMenuMakerText(state.Value, 5), float64(size)-px(4), px(3), menuMakerText{
			size: max(7, px(11)), align: "right", baseline: "top", fill: menuMakerColor(palette.Accent, 255),
		})
	}
	if err != nil {
		return fail(err)
	}
	return canvas, nil
}

// coveredImageRect is where an image of the given size is drawn so that it covers a square of outputSize, zoomed
// and moved by an offset measured in crop preview pixels.
func coveredImageRect(imageW, imageH, outputSize int, zoom, offsetX, offsetY float64) (x, y, w, h float64) {
	width, height := float64(max(1, imageW)), float64(max(1, imageH))
	size := float64(outputSize)
	scale := max(size/width, size/height) * zoom
	w, h = width*scale, height*scale
	x = (size-w)/2 + offsetX*size/menuMakerCropPreviewSize
	y = (size-h)/2 + offsetY*size/menuMakerCropPreviewSize
	return x, y, w, h
}

// cropMenuMakerImage renders the crop of src as a square PNG data URL of outputSize pixels.
func cropMenuMakerImage(src image.Image, outputSize int, zoom, offsetX, offsetY float64) (string, error) {
	canvas := newMenuMakerCanvas(outputSize, outputSize, nil)
	b := src.Bounds()
	x, y, w, h := coveredImageRect(b.Dx(), b.Dy(), outputSize, zoom, offsetX, offsetY)
	canvas.drawImage(src, x, y, w, h, 1)
	data, err := canvas.png()
	if err != nil {
		return "", err
	}
	return pngDataURL(data), nil
}
