package tools

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"

	"github.com/myparsleycat/ddsutil"

	"nahida.live/desktop/internal/infra"
)

const textureDefaultResizePercent = 50

const (
	textureOperationResize           = "resize"
	textureOperationResizeAndConvert = "resize_and_convert"
	textureOperationConvert          = "convert"
	unknownTextureFormatName         = "UNKNOWN_DDS_FORMAT"
	missingBaseMipTextureError       = "Missing base mip image data."
)

var textureTempCounter atomic.Uint64

// textureError wraps the former sidecar's user-facing message text so the
// exact capitalisation and punctuation survive the port.
func textureError(format string, args ...any) error {
	var err error = contractError(fmt.Sprintf(format, args...))
	for _, arg := range args {
		if cause, ok := arg.(error); ok {
			err = infra.WithCause(err, cause)
		}
	}
	return err
}

type textureResizeRequest struct {
	TargetPath   string
	Mode         string
	Operation    string
	Percent      int
	CustomWidth  int
	CustomHeight int
	OutputFormat string
	Backup       bool
}

type textureResizeMode struct {
	percent      bool
	percentValue int
	maxWidth     int
	maxHeight    int
}

type normalizedTextureResizeRequest struct {
	targetPath   string
	mode         textureResizeMode
	operation    string
	outputFormat ddsutil.ImageFormat
	hasFormat    bool
	backup       bool
}

// executeTextureResize mirrors the sidecar's `resize` subcommand.
func executeTextureResize(ctx context.Context, request textureResizeRequest) (TextureResizeResult, error) {
	normalized, err := normalizeTextureResizeRequest(request)
	if err != nil {
		return TextureResizeResult{}, err
	}
	resolved, err := filepath.Abs(normalized.targetPath)
	if err != nil {
		return TextureResizeResult{}, textureError("Failed to resolve target path '%s': %s", normalized.targetPath, err)
	}
	if resolved, err = filepath.EvalSymlinks(resolved); err != nil {
		return TextureResizeResult{}, textureError("Failed to resolve target path '%s': %s", normalized.targetPath, err)
	}
	info, statErr := os.Stat(resolved)
	if statErr != nil || (!info.IsDir() && !info.Mode().IsRegular()) {
		return TextureResizeResult{}, infra.WithCause(contractError(fmt.Sprintf("Target path '%s' must be a directory or DDS file.", resolved)), statErr)
	}
	var files []string
	if info.IsDir() {
		if files, err = collectDDSFiles(resolved); err != nil {
			return TextureResizeResult{}, textureError("Failed to scan target directory '%s': %s", resolved, err)
		}
	} else if !isDDSFilePath(resolved) {
		return TextureResizeResult{}, contractError(fmt.Sprintf("Target file '%s' must be a DDS texture.", resolved))
	} else {
		files = []string{resolved}
	}
	sort.Strings(files)
	result := TextureResizeResult{TargetPath: resolved, Files: make([]TextureResizeFileResult, 0, len(files))}
	for _, filePath := range files {
		if err := ctx.Err(); err != nil {
			return TextureResizeResult{}, err
		}
		fileResult := resizeDDSFile(filePath, &normalized)
		switch fileResult.Status {
		case "updated":
			result.Updated++
		case "skipped":
			result.Skipped++
		default:
			result.Failed++
		}
		result.Files = append(result.Files, fileResult)
	}
	result.Processed = len(result.Files)
	return result, nil
}

func normalizeTextureResizeRequest(request textureResizeRequest) (normalizedTextureResizeRequest, error) {
	trimmed := strings.TrimSpace(request.TargetPath)
	if trimmed == "" {
		return normalizedTextureResizeRequest{}, contractError("Target path is required.")
	}
	var mode textureResizeMode
	switch request.Mode {
	case "percent":
		percent := request.Percent
		if percent < 1 || percent > 99 {
			percent = textureDefaultResizePercent
		}
		mode = textureResizeMode{percent: true, percentValue: percent}
	case "custom":
		mode = textureResizeMode{
			maxWidth:  normalizeTextureResizeDimension(request.CustomWidth),
			maxHeight: normalizeTextureResizeDimension(request.CustomHeight),
		}
	default:
		return normalizedTextureResizeRequest{}, textureError("Unsupported resize mode '%s'.", request.Mode)
	}
	operation := request.Operation
	switch operation {
	case textureOperationResizeAndConvert, textureOperationConvert:
	case textureOperationResize, "":
		operation = textureOperationResize
	default:
		return normalizedTextureResizeRequest{}, textureError("Unsupported texture operation '%s'.", request.Operation)
	}
	normalized := normalizedTextureResizeRequest{
		targetPath: trimmed,
		mode:       mode,
		operation:  operation,
		backup:     request.Backup,
	}
	if strings.TrimSpace(request.OutputFormat) != "" {
		format, formatErr := parseTextureOutputFormat(request.OutputFormat)
		if formatErr != nil {
			return normalizedTextureResizeRequest{}, formatErr
		}
		normalized.outputFormat, normalized.hasFormat = format, true
	}
	return normalized, nil
}

func normalizeTextureResizeDimension(value int) int {
	if value <= textureMinDimension {
		return textureMinDimension
	}
	remainder := value % textureStep
	if remainder == 0 {
		return value
	}
	if remainder >= textureStep/2 {
		return value + (textureStep - remainder)
	}
	return value - remainder
}

func isDDSFilePath(path string) bool {
	return strings.EqualFold(filepath.Ext(path), ".dds")
}

func collectDDSFiles(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() && entry.Type().IsRegular() && isDDSFilePath(path) {
			files = append(files, path)
		}
		return nil
	})
	return files, err
}

// resizeDDSFile never fails: processing errors become failed file results,
// mirroring the sidecar's per-file unwrap_or_else behaviour.
func resizeDDSFile(path string, request *normalizedTextureResizeRequest) TextureResizeFileResult {
	result, err := processResizeDDSFile(path, request)
	if err != nil {
		return TextureResizeFileResult{
			FilePath:       path,
			Status:         "failed",
			OriginalFormat: unknownTextureFormatName,
			OutputFormat:   unknownTextureFormatName,
			Message:        stringPointer(err.Error()),
		}
	}
	return result
}

func processResizeDDSFile(path string, request *normalizedTextureResizeRequest) (TextureResizeFileResult, error) {
	dds, err := readDDSFile(path)
	if err != nil {
		return TextureResizeFileResult{}, err
	}
	surface, err := ddsutil.SurfaceFromDds(dds)
	if err != nil {
		return TextureResizeFileResult{}, textureError("Failed to decode DDS metadata '%s': %s", path, err)
	}
	originalWidth, originalHeight := int(surface.Width), int(surface.Height)
	originalFormat := textureImageFormatName(surface.ImageFormat)
	outputFormat := surface.ImageFormat
	if request.hasFormat {
		outputFormat = request.outputFormat
	}
	outputFormatName := textureImageFormatName(outputFormat)
	var targetWidth, targetHeight int
	if request.operation == textureOperationConvert {
		targetWidth, targetHeight = originalWidth, originalHeight
		if outputFormat == surface.ImageFormat {
			return skippedResizeFileResult(path, originalWidth, originalHeight, originalFormat, outputFormatName,
				"Selected output format matches the source format."), nil
		}
	} else {
		width, height, ok := calculateTextureResizeTarget(surface.Width, surface.Height, &request.mode)
		if !ok {
			return skippedResizeFileResult(path, originalWidth, originalHeight, originalFormat, outputFormatName,
				"No valid downscale candidate matched the requested bounds."), nil
		}
		targetWidth, targetHeight = width, height
	}
	decoded, err := surface.DecodeRgbaf32()
	if err != nil {
		return TextureResizeFileResult{}, textureError("Failed to decode DDS pixels '%s': %s", path, err)
	}
	output := decoded
	if targetWidth != originalWidth || targetHeight != originalHeight {
		if output, err = resizeTextureSurfaceRgba32F(decoded, surface.Layers, surface.Depth, uint32(targetWidth), uint32(targetHeight)); err != nil {
			return TextureResizeFileResult{}, textureError("Failed to resize DDS '%s': %s", path, err)
		}
	}
	mipmaps := ddsutil.MipmapsDisabled
	if surface.Mipmaps > 1 {
		mipmaps = ddsutil.MipmapsGeneratedAutomatic
	}
	encoded, err := output.Encode(outputFormat, ddsutil.QualityFast, mipmaps)
	if err != nil {
		return TextureResizeFileResult{}, textureError("Failed to encode processed DDS '%s': %s", path, err)
	}
	resizedDDS, err := encoded.ToDds()
	if err != nil {
		return TextureResizeFileResult{}, textureError("Failed to create DDS '%s': %s", path, err)
	}
	backupCreated := false
	if request.backup {
		if backupCreated, err = createBackupIfMissing(path); err != nil {
			return TextureResizeFileResult{}, err
		}
	}
	if err := writeDDSAtomically(path, resizedDDS); err != nil {
		return TextureResizeFileResult{}, err
	}
	var message *string
	switch {
	case request.operation == textureOperationConvert:
		message = stringPointer("Format changed without resizing.")
	case outputFormat != surface.ImageFormat && targetWidth == originalWidth && targetHeight == originalHeight:
		message = stringPointer("Texture format changed without resizing.")
	}
	return TextureResizeFileResult{
		FilePath:       path,
		Status:         "updated",
		OriginalWidth:  originalWidth,
		OriginalHeight: originalHeight,
		OutputWidth:    targetWidth,
		OutputHeight:   targetHeight,
		OriginalFormat: originalFormat,
		OutputFormat:   outputFormatName,
		BackupCreated:  backupCreated,
		Message:        message,
	}, nil
}

func skippedResizeFileResult(path string, width, height int, originalFormat, outputFormat, message string) TextureResizeFileResult {
	return TextureResizeFileResult{
		FilePath:       path,
		Status:         "skipped",
		OriginalWidth:  width,
		OriginalHeight: height,
		OutputWidth:    width,
		OutputHeight:   height,
		OriginalFormat: originalFormat,
		OutputFormat:   outputFormat,
		Message:        stringPointer(message),
	}
}

// calculateTextureResizeTarget mirrors the sidecar's calculate_target_dimensions.
func calculateTextureResizeTarget(width, height uint32, mode *textureResizeMode) (int, int, bool) {
	candidates := textureCandidates(int(width), int(height))
	if len(candidates) == 0 {
		return 0, 0, false
	}
	var bounds [2]int
	if mode.percent {
		bounds = [2]int{int(width) * mode.percentValue / 100, int(height) * mode.percentValue / 100}
	} else {
		bounds = [2]int{mode.maxWidth, mode.maxHeight}
	}
	best, ok := pickTextureCandidate(candidates, bounds)
	return best[0], best[1], ok
}

// resizeTextureSurfaceRgba32F resizes the base mip of every layer and depth
// level with a triangle filter, mirroring the sidecar's resize_surface_rgba32f.
func resizeTextureSurfaceRgba32F(surface *ddsutil.SurfaceRgba32Float, layers, depth, targetWidth, targetHeight uint32) (*ddsutil.SurfaceRgba32Float, error) {
	data := make([]float32, 0, uint64(layers)*uint64(depth)*uint64(targetWidth)*uint64(targetHeight)*4)
	for layer := range layers {
		for level := range depth {
			base := surface.Get(layer, level, 0)
			if base == nil {
				return nil, contractError(missingBaseMipTextureError)
			}
			data = append(data, resizeTextureRgba32F(base, surface.Width, surface.Height, targetWidth, targetHeight)...)
		}
	}
	return &ddsutil.SurfaceRgba32Float{
		Width:   targetWidth,
		Height:  targetHeight,
		Depth:   depth,
		Layers:  layers,
		Mipmaps: 1,
		Data:    data,
	}, nil
}

// decodeDDSToPng mirrors the sidecar's `decode` subcommand.
func decodeDDSToPng(input, output string) (textureDecodedMetadata, error) {
	path := strings.TrimSpace(input)
	if path == "" {
		return textureDecodedMetadata{}, contractError("Target path is required.")
	}
	dds, err := readDDSFile(path)
	if err != nil {
		return textureDecodedMetadata{}, err
	}
	surface, err := ddsutil.SurfaceFromDds(dds)
	if err != nil {
		return textureDecodedMetadata{}, textureError("Failed to decode DDS metadata '%s': %s", path, err)
	}
	decoded, err := surface.DecodeRgbaf32()
	if err != nil {
		return textureDecodedMetadata{}, textureError("Failed to decode DDS pixels '%s': %s", path, err)
	}
	base := decoded.Get(0, 0, 0)
	if base == nil {
		return textureDecodedMetadata{}, contractError(missingBaseMipTextureError)
	}
	pixels := make([]uint8, len(base))
	for index, channel := range base {
		pixels[index] = quantizeTextureChannel(channel)
	}
	if err := saveTexturePNG(output, pixels, surface.Width, surface.Height); err != nil {
		return textureDecodedMetadata{}, textureError("Failed to write decoded PNG '%s': %s", output, err)
	}
	return textureDecodedMetadata{
		Width:   int(surface.Width),
		Height:  int(surface.Height),
		Layers:  int(surface.Layers),
		Mipmaps: int(surface.Mipmaps),
		Format:  textureImageFormatName(surface.ImageFormat),
	}, nil
}

// encodePNGToDDS mirrors the sidecar's `encode` subcommand.
func encodePNGToDDS(input, target, outputFormatName string, backup, generateMipmaps bool) (textureEncodedMetadata, error) {
	pixels, width, height, err := loadTexturePNG(input)
	if err != nil {
		return textureEncodedMetadata{}, textureError("Failed to read upscaled PNG '%s': %s", input, err)
	}
	if width == 0 || height == 0 {
		return textureEncodedMetadata{}, contractError("Encoded texture dimensions must be greater than zero.")
	}
	outputFormat, err := parseTextureOutputFormat(outputFormatName)
	if err != nil {
		return textureEncodedMetadata{}, err
	}
	data := make([]float32, len(pixels))
	for index, channel := range pixels {
		data[index] = float32(channel) / 255.0
	}
	surface := &ddsutil.SurfaceRgba32Float{
		Width:   uint32(width),
		Height:  uint32(height),
		Depth:   1,
		Layers:  1,
		Mipmaps: 1,
		Data:    data,
	}
	mipmaps := ddsutil.MipmapsDisabled
	if generateMipmaps {
		mipmaps = ddsutil.MipmapsGeneratedAutomatic
	}
	encoded, err := surface.Encode(outputFormat, ddsutil.QualityFast, mipmaps)
	if err != nil {
		return textureEncodedMetadata{}, textureError("Failed to encode processed DDS '%s': %s", target, err)
	}
	outputDDS, err := encoded.ToDds()
	if err != nil {
		return textureEncodedMetadata{}, textureError("Failed to create DDS '%s': %s", target, err)
	}
	backupCreated := false
	if backup {
		if backupCreated, err = createBackupIfMissing(target); err != nil {
			return textureEncodedMetadata{}, err
		}
	}
	if err := writeDDSAtomically(target, outputDDS); err != nil {
		return textureEncodedMetadata{}, err
	}
	return textureEncodedMetadata{
		Path:          target,
		Width:         width,
		Height:        height,
		OutputFormat:  textureImageFormatName(outputFormat),
		BackupCreated: backupCreated,
	}, nil
}

// loadTexturePNG decodes a PNG into straight-alpha RGBA8 bytes, matching the
// Rust image crate's into_rgba8 layout. PNG data is straight alpha, so
// premultiplying conversions must be avoided.
func loadTexturePNG(path string) ([]uint8, int, int, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, 0, 0, err
	}
	defer func() { _ = file.Close() }()
	decoded, err := png.Decode(file)
	if err != nil {
		return nil, 0, 0, err
	}
	bounds := decoded.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	pixels := make([]uint8, width*height*4)
	switch source := decoded.(type) {
	case *image.NRGBA:
		for y := range height {
			sourceOffset := (y+bounds.Min.Y)*source.Stride + bounds.Min.X*4
			copy(pixels[y*width*4:(y+1)*width*4], source.Pix[sourceOffset:sourceOffset+width*4])
		}
	case *image.RGBA:
		for y := range height {
			sourceOffset := (y+bounds.Min.Y)*source.Stride + bounds.Min.X*4
			copy(pixels[y*width*4:(y+1)*width*4], source.Pix[sourceOffset:sourceOffset+width*4])
		}
	default:
		for y := range height {
			for x := range width {
				pixel := color.NRGBAModel.Convert(decoded.At(bounds.Min.X+x, bounds.Min.Y+y)).(color.NRGBA)
				offset := (y*width + x) * 4
				pixels[offset] = pixel.R
				pixels[offset+1] = pixel.G
				pixels[offset+2] = pixel.B
				pixels[offset+3] = pixel.A
			}
		}
	}
	return pixels, width, height, nil
}

func saveTexturePNG(path string, pixels []uint8, width, height uint32) error {
	out := image.NewNRGBA(image.Rect(0, 0, int(width), int(height)))
	if len(out.Pix) != len(pixels) {
		return contractError(missingBaseMipTextureError)
	}
	copy(out.Pix, pixels)
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := png.Encode(file, out); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func quantizeTextureChannel(value float32) uint8 {
	if value < 0 {
		value = 0
	} else if value > 1 {
		value = 1
	}
	return uint8(math.Round(float64(value * 255.0)))
}

// resizeTextureRgba32F resizes a flat RGBA f32 image with a separable
// triangle filter, mirroring image::imageops::resize(FilterType::Triangle).
func resizeTextureRgba32F(source []float32, sourceWidth, sourceHeight, targetWidth, targetHeight uint32) []float32 {
	if sourceWidth == targetWidth && sourceHeight == targetHeight {
		return source
	}
	vertical := resizeTextureRgba32FVertical(source, sourceWidth, sourceHeight, targetHeight)
	return resizeTextureRgba32FHorizontal(vertical, sourceWidth, targetHeight, targetWidth)
}

type textureResizeWeights struct {
	start   uint32
	weights []float32
}

func textureResizeWeightSets(sourceLength, targetLength uint32) []textureResizeWeights {
	ratio := float32(sourceLength) / float32(targetLength)
	sratio := max(ratio, 1.0)
	support := sratio
	sets := make([]textureResizeWeights, targetLength)
	for out := range targetLength {
		center := (float32(out) + 0.5) * ratio
		left := int(math.Floor(float64(center - support)))
		left = min(max(left, 0), int(sourceLength)-1)
		right := int(math.Ceil(float64(center + support)))
		right = min(max(right, left+1), int(sourceLength))
		center -= 0.5
		weights := make([]float32, 0, right-left)
		sum := float32(0)
		for i := left; i < right; i++ {
			weight := textureTriangleKernel((float32(i) - center) / sratio)
			weights = append(weights, weight)
			sum += weight
		}
		for index := range weights {
			weights[index] /= sum
		}
		sets[out] = textureResizeWeights{start: uint32(left), weights: weights}
	}
	return sets
}

func textureTriangleKernel(x float32) float32 {
	if x < 0 {
		x = -x
	}
	if x < 1 {
		return 1 - x
	}
	return 0
}

// resizeTextureRgba32FVertical maps a width×height image to width×targetHeight.
func resizeTextureRgba32FVertical(source []float32, width, height, targetHeight uint32) []float32 {
	out := make([]float32, uint64(width)*uint64(targetHeight)*4)
	sets := textureResizeWeightSets(height, targetHeight)
	for outY, set := range sets {
		for x := range width {
			var accumulated [4]float32
			for offset, weight := range set.weights {
				sourceIndex := (uint64(set.start+uint32(offset))*uint64(width) + uint64(x)) * 4
				accumulated[0] += source[sourceIndex] * weight
				accumulated[1] += source[sourceIndex+1] * weight
				accumulated[2] += source[sourceIndex+2] * weight
				accumulated[3] += source[sourceIndex+3] * weight
			}
			outIndex := (uint64(outY)*uint64(width) + uint64(x)) * 4
			out[outIndex] = accumulated[0]
			out[outIndex+1] = accumulated[1]
			out[outIndex+2] = accumulated[2]
			out[outIndex+3] = accumulated[3]
		}
	}
	return out
}

// resizeTextureRgba32FHorizontal maps a width×height image to targetWidth×height.
func resizeTextureRgba32FHorizontal(source []float32, width, height, targetWidth uint32) []float32 {
	out := make([]float32, uint64(targetWidth)*uint64(height)*4)
	sets := textureResizeWeightSets(width, targetWidth)
	for outX, set := range sets {
		for y := range height {
			var accumulated [4]float32
			for offset, weight := range set.weights {
				sourceIndex := (uint64(y)*uint64(width) + uint64(set.start+uint32(offset))) * 4
				accumulated[0] += source[sourceIndex] * weight
				accumulated[1] += source[sourceIndex+1] * weight
				accumulated[2] += source[sourceIndex+2] * weight
				accumulated[3] += source[sourceIndex+3] * weight
			}
			outIndex := (uint64(y)*uint64(targetWidth) + uint64(outX)) * 4
			out[outIndex] = accumulated[0]
			out[outIndex+1] = accumulated[1]
			out[outIndex+2] = accumulated[2]
			out[outIndex+3] = accumulated[3]
		}
	}
	return out
}

func readDDSFile(path string) (*ddsutil.Dds, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, textureError("Failed to open DDS file '%s': %s", path, err)
	}
	defer func() { _ = file.Close() }()
	dds, err := ddsutil.Read(file)
	if err != nil {
		return nil, textureError("Failed to read DDS file '%s': %s", path, err)
	}
	return dds, nil
}

// writeDDSAtomically writes via an exclusive sibling temp file and an atomic
// replace, mirroring the sidecar's write_dds_atomically.
func writeDDSAtomically(path string, dds *ddsutil.Dds) error {
	tempFile, tempPath, err := createSiblingDDSTemp(path)
	if err != nil {
		return textureError("Failed to overwrite DDS file '%s': %s", path, err)
	}
	writeErr := func() error {
		writer := bufio.NewWriter(tempFile)
		if err := dds.Write(writer); err != nil {
			return textureError("Failed to write DDS file '%s': %s", path, err)
		}
		if err := writer.Flush(); err != nil {
			return textureError("Failed to write DDS file '%s': %s", path, err)
		}
		if err := tempFile.Sync(); err != nil {
			return textureError("Failed to write DDS file '%s': %s", path, err)
		}
		// Close before replacing: Go opens files without FILE_SHARE_DELETE on
		// Windows, so the rename would fail while the handle is held.
		if err := tempFile.Close(); err != nil {
			return textureError("Failed to finalize DDS file '%s': %s", path, err)
		}
		if err := replaceAtomic(tempPath, path); err != nil {
			return textureError("Failed to write DDS file '%s': %s", path, err)
		}
		return nil
	}()
	if writeErr != nil {
		return infra.WithCause(writeErr, os.Remove(tempPath))
	}
	return nil
}

func createSiblingDDSTemp(path string) (*os.File, string, error) {
	dir, name := filepath.Split(path)
	for {
		tempPath := filepath.Join(dir, fmt.Sprintf("%s.%d.%d.tmp", name, os.Getpid(), textureTempCounter.Add(1)))
		file, err := os.OpenFile(tempPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o666)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return nil, "", err
		}
		return file, tempPath, nil
	}
}

func createBackupIfMissing(path string) (bool, error) {
	backupPath := path + ".bak"
	if _, err := os.Stat(backupPath); err == nil {
		return false, nil
	}
	source, err := os.Open(path)
	if err != nil {
		return false, textureError("Failed to create backup file '%s': %s", backupPath, err)
	}
	defer func() { _ = source.Close() }()
	destination, err := os.OpenFile(backupPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o666)
	if err != nil {
		return false, textureError("Failed to create backup file '%s': %s", backupPath, err)
	}
	_, copyErr := io.Copy(destination, source)
	closeErr := destination.Close()
	if copyErr == nil {
		copyErr = closeErr
	}
	if copyErr != nil {
		return false, infra.WithCause(textureError("Failed to create backup file '%s': %s", backupPath, copyErr), os.Remove(backupPath))
	}
	return true, nil
}

func parseTextureOutputFormat(value string) (ddsutil.ImageFormat, error) {
	if format, ok := textureFormatsByName[value]; ok {
		return format, nil
	}
	return 0, textureError("Unsupported output format '%s'.", value)
}

func textureImageFormatName(format ddsutil.ImageFormat) string {
	if name, ok := textureNamesByFormat[format]; ok {
		return name
	}
	return unknownTextureFormatName
}

var textureFormatsByName = map[string]ddsutil.ImageFormat{
	"DXGI_FORMAT_R8_UNORM":            ddsutil.R8Unorm,
	"DXGI_FORMAT_R8_SNORM":            ddsutil.R8Snorm,
	"DXGI_FORMAT_R8G8_UNORM":          ddsutil.Rg8Unorm,
	"DXGI_FORMAT_R8G8_SNORM":          ddsutil.Rg8Snorm,
	"DXGI_FORMAT_R8G8B8A8_UNORM":      ddsutil.Rgba8Unorm,
	"DXGI_FORMAT_R8G8B8A8_UNORM_SRGB": ddsutil.Rgba8UnormSrgb,
	"DXGI_FORMAT_R8G8B8A8_SNORM":      ddsutil.Rgba8Snorm,
	"DXGI_FORMAT_R16_UNORM":           ddsutil.R16Unorm,
	"DXGI_FORMAT_R16_SNORM":           ddsutil.R16Snorm,
	"DXGI_FORMAT_R16_FLOAT":           ddsutil.R16Float,
	"DXGI_FORMAT_R16G16_UNORM":        ddsutil.Rg16Unorm,
	"DXGI_FORMAT_R16G16_SNORM":        ddsutil.Rg16Snorm,
	"DXGI_FORMAT_R16G16_FLOAT":        ddsutil.Rg16Float,
	"DXGI_FORMAT_R16G16B16A16_UNORM":  ddsutil.Rgba16Unorm,
	"DXGI_FORMAT_R16G16B16A16_SNORM":  ddsutil.Rgba16Snorm,
	"DXGI_FORMAT_R16G16B16A16_FLOAT":  ddsutil.Rgba16Float,
	"DXGI_FORMAT_R32_FLOAT":           ddsutil.R32Float,
	"DXGI_FORMAT_R32G32_FLOAT":        ddsutil.Rg32Float,
	"DXGI_FORMAT_R32G32B32_FLOAT":     ddsutil.Rgb32Float,
	"DXGI_FORMAT_R32G32B32A32_FLOAT":  ddsutil.Rgba32Float,
	"DXGI_FORMAT_B8G8R8A8_UNORM":      ddsutil.Bgra8Unorm,
	"DXGI_FORMAT_B8G8R8A8_UNORM_SRGB": ddsutil.Bgra8UnormSrgb,
	"DXGI_FORMAT_B4G4R4A4_UNORM":      ddsutil.Bgra4Unorm,
	"DXGI_FORMAT_B5G5R5A1_UNORM":      ddsutil.Bgr5A1Unorm,
	"DXGI_FORMAT_BC1_UNORM":           ddsutil.BC1RgbaUnorm,
	"DXGI_FORMAT_BC1_UNORM_SRGB":      ddsutil.BC1RgbaUnormSrgb,
	"DXGI_FORMAT_BC2_UNORM":           ddsutil.BC2RgbaUnorm,
	"DXGI_FORMAT_BC2_UNORM_SRGB":      ddsutil.BC2RgbaUnormSrgb,
	"DXGI_FORMAT_BC3_UNORM":           ddsutil.BC3RgbaUnorm,
	"DXGI_FORMAT_BC3_UNORM_SRGB":      ddsutil.BC3RgbaUnormSrgb,
	"DXGI_FORMAT_BC4_UNORM":           ddsutil.BC4RUnorm,
	"DXGI_FORMAT_BC4_SNORM":           ddsutil.BC4RSnorm,
	"DXGI_FORMAT_BC5_UNORM":           ddsutil.BC5RgUnorm,
	"DXGI_FORMAT_BC5_SNORM":           ddsutil.BC5RgSnorm,
	"DXGI_FORMAT_BC6H_UF16":           ddsutil.BC6hRgbUfloat,
	"DXGI_FORMAT_BC6H_SF16":           ddsutil.BC6hRgbSfloat,
	"DXGI_FORMAT_BC7_UNORM":           ddsutil.BC7RgbaUnorm,
	"DXGI_FORMAT_BC7_UNORM_SRGB":      ddsutil.BC7RgbaUnormSrgb,
}

var textureNamesByFormat = func() map[ddsutil.ImageFormat]string {
	names := make(map[ddsutil.ImageFormat]string, len(textureFormatsByName))
	for name, format := range textureFormatsByName {
		names[format] = name
	}
	return names
}()
