package tools

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"

	"github.com/woozymasta/bcn"
)

const (
	maxModelViewerTextureInputPixels  = int64(64 * 1024 * 1024)
	maxModelViewerTextureOutputPixels = int64(2048 * 2048)
)

type modelViewerTextureBinding struct {
	SectionName          string
	IBResourceName       string
	DiffuseResourceName  string
	TextureResourceNames []string
	OverrideHash         string
}

type modelViewerPreparedTexture struct {
	bytes       []byte
	mimeType    string
	name        string
	alphaMode   string
	alphaCutoff float64
	score       int
}

func collectModelViewerTextureBindings(sections []modINISection, variables map[string]any) []modelViewerTextureBinding {
	lookup := make(map[string]modINISection)
	for _, section := range sections {
		lookup[modelViewerNormalizeKey(section.Header+section.Name)] = section
	}
	var bindings []modelViewerTextureBinding
	for _, section := range sections {
		if !strings.EqualFold(section.Header, "TextureOverride") {
			continue
		}
		wanted := []string{"ib", "this", "Resource\\ZZMI\\Diffuse"}
		for index := range 11 {
			wanted = append(wanted, fmt.Sprintf("ps-t%d", index))
		}
		assignments := resolveModelViewerAssignments(section, wanted, lookup, variables, make(map[string]bool))
		ibNames := collectModelViewerSectionIBNames(section)
		if len(ibNames) == 0 && assignments["ib"] != "" {
			ibNames = append(ibNames, modelViewerTrimResourcePrefix(assignments["ib"]))
		}
		if len(ibNames) == 0 {
			continue
		}
		var textureNames []string
		for key, value := range assignments {
			lowerValue := strings.ToLower(value)
			if (strings.HasPrefix(key, "pst") || key == modelViewerNormalizeKey("Resource\\ZZMI\\Diffuse")) && (strings.HasPrefix(lowerValue, "resource") || strings.HasPrefix(lowerValue, "ref resource")) {
				name := modelViewerTrimResourcePrefix(strings.TrimPrefix(value, "ref "))
				if resolved := resolveModelViewerTextureReference(name, section, lookup, variables, make(map[string]bool)); resolved != "" {
					textureNames = appendUniqueModelViewer(textureNames, resolved)
				}
			}
		}
		direct := ""
		if value := assignments["this"]; strings.Contains(strings.ToLower(value), "resource") {
			direct = modelViewerTrimResourcePrefix(strings.TrimPrefix(value, "ref "))
		}
		for _, ibName := range ibNames {
			diffuse := ""
			for _, name := range textureNames {
				if strings.Contains(strings.ToLower(name), "diffuse") || strings.Contains(strings.ToLower(name), "basecolor") || strings.Contains(strings.ToLower(name), "albedo") {
					diffuse = name
					break
				}
			}
			if diffuse == "" {
				for _, name := range textureNames {
					lower := strings.ToLower(name)
					if !strings.Contains(lower, "normal") && !strings.Contains(lower, "light") {
						diffuse = name
						break
					}
				}
			}
			if diffuse == "" {
				diffuse = direct
			}
			bindings = append(bindings, modelViewerTextureBinding{SectionName: section.Name, IBResourceName: modelViewerTrimResourcePrefix(ibName), DiffuseResourceName: diffuse, TextureResourceNames: textureNames, OverrideHash: strings.TrimSpace(modelViewerSectionValue(section, "hash"))})
		}
	}
	return bindings
}

func resolveModelViewerAssignments(section modINISection, targets []string, lookup map[string]modINISection, variables map[string]any, visited map[string]bool) map[string]string {
	name := modelViewerNormalizeKey(section.Header + section.Name)
	if visited[name] {
		return nil
	}
	visited = cloneModelViewerVisited(visited)
	visited[name] = true
	wanted := make(map[string]bool)
	for _, target := range targets {
		wanted[modelViewerNormalizeKey(target)] = true
	}
	assignments := make(map[string]string)
	var active, matched []bool
	isActive := func() bool {
		for _, value := range active {
			if !value {
				return false
			}
		}
		return true
	}
	for _, raw := range section.Lines {
		line := strings.TrimSpace(raw)
		lower := strings.ToLower(line)
		switch {
		case strings.HasPrefix(lower, "if "):
			parent := isActive()
			result := parent && evaluateModelViewerCondition(strings.TrimSpace(line[3:]), variables)
			active, matched = append(active, result), append(matched, result)
			continue
		case strings.HasPrefix(lower, "elif "), strings.HasPrefix(lower, "else if "):
			if len(active) == 0 {
				continue
			}
			depth := len(active) - 1
			parent := true
			for _, value := range active[:depth] {
				parent = parent && value
			}
			expression := strings.TrimSpace(line[5:])
			if strings.HasPrefix(lower, "else if ") {
				expression = strings.TrimSpace(line[8:])
			}
			result := parent && !matched[depth] && evaluateModelViewerCondition(expression, variables)
			active[depth], matched[depth] = result, matched[depth] || result
			continue
		case lower == "else":
			if len(active) > 0 {
				depth := len(active) - 1
				parent := true
				for _, value := range active[:depth] {
					parent = parent && value
				}
				active[depth], matched[depth] = parent && !matched[depth], true
			}
			continue
		case lower == "endif":
			if len(active) > 0 {
				active, matched = active[:len(active)-1], matched[:len(matched)-1]
			}
			continue
		}
		if !isActive() {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		if strings.EqualFold(key, "run") {
			if nested, exists := lookup[modelViewerNormalizeKey(value)]; exists {
				for nestedKey, nestedValue := range resolveModelViewerAssignments(nested, targets, lookup, variables, visited) {
					assignments[nestedKey] = nestedValue
				}
			}
		}
		normalized := modelViewerNormalizeKey(key)
		if wanted[normalized] {
			assignments[normalized] = value
		}
	}
	return assignments
}

func resolveModelViewerTextureReference(resourceName string, section modINISection, lookup map[string]modINISection, variables map[string]any, visited map[string]bool) string {
	key := modelViewerNormalizeKey(resourceName)
	if key == "" || visited[key] {
		return resourceName
	}
	visited[key] = true
	targets := []string{resourceName}
	if !strings.HasPrefix(strings.ToLower(resourceName), "resource") {
		targets = append(targets, "Resource"+resourceName)
	}
	assignments := resolveModelViewerAssignments(section, targets, lookup, variables, make(map[string]bool))
	for _, target := range targets {
		if next := assignments[modelViewerNormalizeKey(target)]; next != "" {
			next = modelViewerTrimResourcePrefix(strings.TrimPrefix(next, "ref "))
			if modelViewerNormalizeKey(next) != key {
				return resolveModelViewerTextureReference(next, section, lookup, variables, visited)
			}
		}
	}
	return resourceName
}

func collectModelViewerSectionIBNames(section modINISection) []string {
	var names []string
	for _, line := range section.Lines {
		key, value, ok := strings.Cut(line, "=")
		if ok && modelViewerNormalizeKey(key) == "ib" {
			names = appendUniqueModelViewer(names, modelViewerTrimResourcePrefix(value))
		}
	}
	return names
}

func appendUniqueModelViewer(values []string, value string) []string {
	for _, existing := range values {
		if modelViewerNormalizeKey(existing) == modelViewerNormalizeKey(value) {
			return values
		}
	}
	if strings.TrimSpace(value) != "" {
		return append(values, strings.TrimSpace(value))
	}
	return values
}

func prepareModelViewerTexture(ctx context.Context, path, resourceName, format string, quality int) (*modelViewerPreparedTexture, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxModelViewerBufferFileBytes {
		return nil, fmt.Errorf("viewer texture file is too large or invalid: %s", path)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	width, height, dimensionErr := modelViewerTextureDimensions(raw, filepath.Ext(path))
	if dimensionErr != nil {
		return nil, dimensionErr
	}
	if int64(width)*int64(height) > maxModelViewerTextureInputPixels {
		return nil, fmt.Errorf("viewer texture dimensions exceed the input safety limit: %dx%d", width, height)
	}
	rgba, err := decodeModelViewerImage(raw, filepath.Ext(path))
	if err != nil {
		return nil, err
	}
	rgba = downscaleModelViewerTexture(rgba, maxModelViewerTextureOutputPixels)
	low, high, partial := 0, 0, 0
	lowRGB := float64(0)
	for offset := 0; offset+3 < len(rgba.Pix); offset += 4 {
		alpha := rgba.Pix[offset+3]
		switch {
		case alpha <= 16:
			low++
			lowRGB += float64(rgba.Pix[offset]+rgba.Pix[offset+1]+rgba.Pix[offset+2]) / 3
		case alpha >= 239:
			high++
		default:
			partial++
		}
	}
	pixels := max(1, low+high+partial)
	lowRatio, highRatio, partialRatio := float64(low)/float64(pixels), float64(high)/float64(pixels), float64(partial)/float64(pixels)
	key := modelViewerNormalizeKey(resourceName)
	if strings.Contains(key, "invertalpha") || strings.Contains(key, "alphainvert") || lowRatio >= .95 && highRatio <= .03 && low > 0 && lowRGB/float64(low) >= 8 {
		for offset := 3; offset < len(rgba.Pix); offset += 4 {
			rgba.Pix[offset] = 255 - rgba.Pix[offset]
		}
		lowRatio, highRatio = highRatio, lowRatio
	}
	cutout := lowRatio >= .005 && highRatio >= .5 && partialRatio <= .02
	usesAlpha := cutout || partialRatio > 0 || lowRatio >= .005
	format = normalizeModelViewerFormat(format)
	mimeType := "image/png"
	if format == "jpeg-force" || format == "jpeg-safe" && !usesAlpha {
		mimeType = "image/jpeg"
	}
	var output bytes.Buffer
	if mimeType == "image/jpeg" {
		background := image.NewRGBA(rgba.Bounds())
		draw.Draw(background, background.Bounds(), &image.Uniform{C: color.White}, image.Point{}, draw.Src)
		draw.Draw(background, background.Bounds(), rgba, rgba.Bounds().Min, draw.Over)
		if err = jpeg.Encode(&output, background, &jpeg.Options{Quality: normalizeJPEGQuality(quality)}); err != nil {
			return nil, err
		}
	} else if err = png.Encode(&output, rgba); err != nil {
		return nil, err
	}
	prepared := &modelViewerPreparedTexture{bytes: output.Bytes(), mimeType: mimeType, name: filepath.Base(path), score: modelViewerTextureNamePriority(resourceName) + 20}
	if cutout {
		prepared.alphaMode, prepared.alphaCutoff = "MASK", .5
	}
	return prepared, nil
}

func decodeModelViewerImage(raw []byte, extension string) (*image.NRGBA, error) {
	if strings.EqualFold(extension, ".dds") {
		return decodeModelViewerDDS(raw)
	}
	decoded, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	return imageToNRGBA(decoded), nil
}

func decodeModelViewerDDS(raw []byte) (*image.NRGBA, error) {
	_, rgba, err := bcn.DecodeDDS(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	if rgba == nil {
		return nil, fmt.Errorf("dds decode produced no image")
	}
	return rgba, nil
}

func imageToNRGBA(src image.Image) *image.NRGBA {
	if nrgba, ok := src.(*image.NRGBA); ok {
		return nrgba
	}
	bounds := src.Bounds()
	dst := image.NewNRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	draw.Draw(dst, dst.Bounds(), src, bounds.Min, draw.Src)
	return dst
}

func modelViewerTextureDimensions(raw []byte, extension string) (int, int, error) {
	switch strings.ToLower(extension) {
	case ".dds":
		if len(raw) < 20 || string(raw[:4]) != "DDS " {
			return 0, 0, fmt.Errorf("invalid DDS texture header")
		}
		width := int(binary.LittleEndian.Uint32(raw[16:20]))
		height := int(binary.LittleEndian.Uint32(raw[12:16]))
		if width <= 0 || height <= 0 {
			return 0, 0, fmt.Errorf("invalid DDS texture dimensions")
		}
		return width, height, nil
	case ".png":
		if len(raw) < 24 || !bytes.Equal(raw[:8], []byte{137, 80, 78, 71, 13, 10, 26, 10}) {
			return 0, 0, fmt.Errorf("invalid PNG texture header")
		}
		width := int(binary.BigEndian.Uint32(raw[16:20]))
		height := int(binary.BigEndian.Uint32(raw[20:24]))
		if width <= 0 || height <= 0 {
			return 0, 0, fmt.Errorf("invalid PNG texture dimensions")
		}
		return width, height, nil
	default:
		configuration, _, err := image.DecodeConfig(bytes.NewReader(raw))
		if err != nil {
			return 0, 0, err
		}
		return configuration.Width, configuration.Height, nil
	}
}

func viewerPreviewTextureSize(width, height int) (int, int) {
	for int64(width)*int64(height) > maxModelViewerTextureOutputPixels && (width > 1 || height > 1) {
		width = max(1, width/2)
		height = max(1, height/2)
	}
	return width, height
}

func downscaleModelViewerTexture(source *image.NRGBA, maxPixels int64) *image.NRGBA {
	if source == nil || maxPixels <= 0 {
		return source
	}
	for int64(source.Bounds().Dx())*int64(source.Bounds().Dy()) > maxPixels {
		width, height := max(1, source.Bounds().Dx()/2), max(1, source.Bounds().Dy()/2)
		source = downsampleModelViewerTextureHalf(source, width, height)
	}
	return source
}

func downsampleModelViewerTextureHalf(source *image.NRGBA, width, height int) *image.NRGBA {
	target := image.NewNRGBA(image.Rect(0, 0, width, height))
	srcStride, dstStride := source.Stride, target.Stride
	srcW := source.Bounds().Dx()
	srcH := source.Bounds().Dy()
	for y := range height {
		srcY := min(srcH-1, y*2)
		srcRow := source.Pix[srcY*srcStride:]
		dstRow := target.Pix[y*dstStride:]
		for x := range width {
			srcX := min(srcW-1, x*2) * 4
			copy(dstRow[x*4:x*4+4], srcRow[srcX:srcX+4])
		}
	}
	return target
}

func normalizeModelViewerFormat(value string) string {
	switch value {
	case "png", "jpeg-safe", "jpeg-force":
		return value
	default:
		return "jpeg-safe"
	}
}

func normalizeJPEGQuality(value int) int {
	if value == 0 {
		return 85
	}
	return max(1, min(100, value))
}

func modelViewerTextureNamePriority(name string) int {
	key, score := modelViewerNormalizeKey(name), 0
	if strings.Contains(key, "basecolor") || strings.Contains(key, "albedo") {
		score += 80
	}
	if strings.Contains(key, "diffuse") {
		score += 60
	}
	if strings.Contains(key, "color") {
		score += 25
	}
	if strings.Contains(key, "shadow") {
		score -= 20
	}
	if strings.Contains(key, "lightmap") {
		score -= 12
	}
	if strings.Contains(key, "light") {
		score -= 10
	}
	if strings.Contains(key, "metal") || strings.Contains(key, "rough") || strings.Contains(key, "ao") {
		score -= 24
	}
	if strings.Contains(key, "mask") {
		score -= 28
	}
	if strings.Contains(key, "normal") || strings.Contains(key, "bump") {
		score -= 60
	}
	return score
}
