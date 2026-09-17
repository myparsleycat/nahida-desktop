package modelviewer

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/myparsleycat/ddsutil"
)

const modelViewerDDSPreviewMaxDimension uint32 = 2048

type modelViewerDDSPreviewPlan struct {
	baseWidth    uint32
	baseHeight   uint32
	width        uint32
	height       uint32
	sourceWidth  uint32
	sourceHeight uint32
	sourceMip    uint32
	copyMip      bool
}

type modelViewerDDSMetadata struct {
	Format          string
	Width           uint32
	Height          uint32
	MipCount        uint32
	AutoInvertAlpha bool
}

func prepareModelViewerDDSFallback(ctx context.Context, path string) ([]byte, error) {
	decoded, err := decodeModelViewerTextureSource(ctx, path)
	if err != nil {
		return nil, err
	}
	prepared, err := encodeModelViewerPreparedTextureWithAlpha(
		ctx,
		decoded,
		path,
		"",
		modelViewerTextureTransformPassthrough,
		"png",
		100,
		false,
	)
	if err != nil {
		return nil, err
	}
	return prepared.bytes, nil
}

func prepareModelViewerDDSPreview(
	ctx context.Context,
	path string,
	expected modelViewerDDSMetadata,
) ([]byte, error) {
	return prepareModelViewerDDSPreviewWithLimit(ctx, path, expected, modelViewerDDSPreviewMaxDimension)
}

func prepareModelViewerDDSPreviewWithLimit(
	ctx context.Context,
	path string,
	expected modelViewerDDSMetadata,
	maxDimension uint32,
) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()

	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxModelViewerBufferFileBytes {
		return nil, fmt.Errorf("viewer texture file is too large or invalid: %s", path)
	}
	reader, err := ddsutil.NewDdsReader(file, info.Size())
	if err != nil {
		return nil, err
	}
	metadata := reader.Metadata()
	format, supported := modelViewerDDSFormat(metadata.ImageFormat)
	if !supported || metadata.Depth != 1 || metadata.Layers != 1 ||
		format != expected.Format || metadata.Width != expected.Width || metadata.Height != expected.Height ||
		metadata.Mipmaps != expected.MipCount {
		return nil, fmt.Errorf("DDS metadata changed while preparing model viewer preview: %s", path)
	}
	plan, needed := modelViewerDDSPreviewPlanFor(expected, maxDimension)
	if !needed {
		return nil, fmt.Errorf("DDS does not need a model viewer preview: %s", path)
	}

	var surface *ddsutil.Surface
	if plan.copyMip {
		surface, err = reader.ReadMip(0, plan.sourceMip)
	} else {
		var data []byte
		data, err = readModelViewerDDSDecimatedMip(ctx, file, metadata.ImageFormat, plan)
		if err == nil {
			surface = &ddsutil.Surface{
				Width:       plan.width,
				Height:      plan.height,
				Depth:       1,
				Layers:      1,
				Mipmaps:     1,
				ImageFormat: metadata.ImageFormat,
				Data:        data,
			}
		}
	}
	if err != nil {
		return nil, err
	}
	return encodeModelViewerDDSSurface(ctx, surface)
}

func modelViewerDDSPreviewPlanFor(
	metadata modelViewerDDSMetadata,
	maxDimension uint32,
) (modelViewerDDSPreviewPlan, bool) {
	if maxDimension == 0 || metadata.Width == 0 || metadata.Height == 0 || metadata.MipCount == 0 ||
		max(metadata.Width, metadata.Height) <= maxDimension {
		return modelViewerDDSPreviewPlan{}, false
	}

	width, height := metadata.Width, metadata.Height
	for mip := range metadata.MipCount {
		if max(width, height) <= maxDimension {
			return modelViewerDDSPreviewPlan{
				baseWidth: metadata.Width, baseHeight: metadata.Height,
				width: width, height: height, sourceWidth: width, sourceHeight: height, sourceMip: mip, copyMip: true,
			}, true
		}
		if mip+1 < metadata.MipCount {
			width, height = max(1, width/2), max(1, height/2)
		}
	}

	targetWidth, targetHeight := fitModelViewerDDSPreview(width, height, maxDimension)
	return modelViewerDDSPreviewPlan{
		baseWidth:    metadata.Width,
		baseHeight:   metadata.Height,
		width:        targetWidth,
		height:       targetHeight,
		sourceWidth:  width,
		sourceHeight: height,
		sourceMip:    metadata.MipCount - 1,
	}, true
}

func fitModelViewerDDSPreview(width, height, maxDimension uint32) (uint32, uint32) {
	if max(width, height) <= maxDimension {
		return width, height
	}
	if width >= height {
		return maxDimension, max(1, uint32((uint64(height)*uint64(maxDimension)+uint64(width)/2)/uint64(width)))
	}
	return max(1, uint32((uint64(width)*uint64(maxDimension)+uint64(height)/2)/uint64(height))), maxDimension
}

// readModelViewerDDSDecimatedMip keeps one already-compressed block for each
// destination block. The lossy block-space sampling avoids both pixel decode
// and BC re-encoding on the model viewer's first load.
func readModelViewerDDSDecimatedMip(
	ctx context.Context,
	file *os.File,
	format ddsutil.ImageFormat,
	plan modelViewerDDSPreviewPlan,
) ([]byte, error) {
	blockBytes, ok := modelViewerDDSBlockBytes(format)
	if !ok {
		return nil, fmt.Errorf("unsupported DDS preview format: %v", format)
	}
	headerBytes, err := modelViewerDDSHeaderBytes(file)
	if err != nil {
		return nil, err
	}
	offset := int64(headerBytes)
	width, height := plan.baseWidth, plan.baseHeight
	for range plan.sourceMip {
		mipBytes := modelViewerDDSMipBytes(width, height, blockBytes)
		if mipBytes > uint64(maxModelViewerBufferFileBytes) {
			return nil, fmt.Errorf("DDS preview source mip is too large")
		}
		offset += int64(mipBytes)
		width, height = max(1, width/2), max(1, height/2)
	}

	sourceBlocksX := modelViewerDDSBlocks(plan.sourceWidth)
	sourceBlocksY := modelViewerDDSBlocks(plan.sourceHeight)
	targetBlocksX := modelViewerDDSBlocks(plan.width)
	targetBlocksY := modelViewerDDSBlocks(plan.height)
	sourceRowBytes := sourceBlocksX * uint64(blockBytes)
	targetRowBytes := targetBlocksX * uint64(blockBytes)
	outputBytes := targetRowBytes * targetBlocksY
	if sourceRowBytes > uint64(maxModelViewerBufferFileBytes) || outputBytes > uint64(maxModelViewerBufferFileBytes) ||
		outputBytes > uint64(^uint(0)>>1) {
		return nil, fmt.Errorf("DDS preview dimensions are too large")
	}
	sourceRow := make([]byte, int(sourceRowBytes))
	output := make([]byte, int(outputBytes))
	for targetY := range targetBlocksY {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		// Sampling block centers avoids the top-left bias of a simple stride.
		sourceY := ((2*targetY + 1) * sourceBlocksY) / (2 * targetBlocksY)
		rowOffset := offset + int64(sourceY*sourceRowBytes)
		read, readErr := file.ReadAt(sourceRow, rowOffset)
		if readErr != nil && (readErr != io.EOF || read != len(sourceRow)) {
			return nil, fmt.Errorf("read DDS preview source row: %w", readErr)
		}
		if read != len(sourceRow) {
			return nil, io.ErrUnexpectedEOF
		}
		targetRow := output[targetY*targetRowBytes : (targetY+1)*targetRowBytes]
		for targetX := range targetBlocksX {
			sourceX := ((2*targetX + 1) * sourceBlocksX) / (2 * targetBlocksX)
			sourceStart := sourceX * uint64(blockBytes)
			targetStart := targetX * uint64(blockBytes)
			copy(
				targetRow[targetStart:targetStart+uint64(blockBytes)],
				sourceRow[sourceStart:sourceStart+uint64(blockBytes)],
			)
		}
	}
	return output, nil
}

func modelViewerDDSHeaderBytes(file *os.File) (int, error) {
	header := make([]byte, 128)
	if _, err := file.ReadAt(header, 0); err != nil {
		return 0, fmt.Errorf("read DDS header: %w", err)
	}
	if binary.LittleEndian.Uint32(header[:4]) != 0x20534444 {
		return 0, fmt.Errorf("invalid DDS magic")
	}
	if string(header[84:88]) == "DX10" {
		return 148, nil
	}
	return 128, nil
}

func modelViewerDDSMipBytes(width, height uint32, blockBytes int) uint64 {
	return modelViewerDDSBlocks(width) * modelViewerDDSBlocks(height) * uint64(blockBytes)
}

func modelViewerDDSBlocks(dimension uint32) uint64 {
	return max(uint64(1), (uint64(dimension)+3)/4)
}

func modelViewerDDSBlockBytes(format ddsutil.ImageFormat) (int, bool) {
	switch format {
	case ddsutil.BC1RgbaUnorm, ddsutil.BC1RgbaUnormSrgb, ddsutil.BC4RUnorm, ddsutil.BC4RSnorm:
		return 8, true
	case ddsutil.BC2RgbaUnorm, ddsutil.BC2RgbaUnormSrgb,
		ddsutil.BC3RgbaUnorm, ddsutil.BC3RgbaUnormSrgb,
		ddsutil.BC5RgUnorm, ddsutil.BC5RgSnorm,
		ddsutil.BC6hRgbUfloat, ddsutil.BC6hRgbSfloat,
		ddsutil.BC7RgbaUnorm, ddsutil.BC7RgbaUnormSrgb:
		return 16, true
	default:
		return 0, false
	}
}

func encodeModelViewerDDSSurface(ctx context.Context, surface *ddsutil.Surface) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	dds, err := surface.ToDds()
	if err != nil {
		return nil, err
	}
	var output bytes.Buffer
	output.Grow(148 + len(surface.Data))
	if err = dds.Write(&output); err != nil {
		return nil, err
	}
	return output.Bytes(), ctx.Err()
}

func inspectModelViewerDDS(ctx context.Context, path string) (modelViewerDDSMetadata, error) {
	if err := ctx.Err(); err != nil {
		return modelViewerDDSMetadata{}, err
	}
	file, err := os.Open(path)
	if err != nil {
		return modelViewerDDSMetadata{}, err
	}
	defer func() { _ = file.Close() }()

	info, err := file.Stat()
	if err != nil {
		return modelViewerDDSMetadata{}, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxModelViewerBufferFileBytes {
		return modelViewerDDSMetadata{}, fmt.Errorf("viewer texture file is too large or invalid: %s", path)
	}
	reader, err := ddsutil.NewDdsReader(file, info.Size())
	if err != nil {
		return modelViewerDDSMetadata{}, err
	}
	metadata := reader.Metadata()
	format, _ := modelViewerDDSFormat(metadata.ImageFormat)
	if metadata.Depth != 1 || metadata.Layers != 1 {
		format = ""
	}
	autoInvertAlpha := false
	if modelViewerDDSFormatHasAlpha(format) {
		mipmap, width, height := wwmiHintMipmap(metadata.Width, metadata.Height, metadata.Mipmaps)
		rgba, decodeErr := decodeModelViewerDDSMip(reader, mipmap, width, height)
		if decodeErr == nil {
			autoInvertAlpha = modelViewerTextureShouldInvertAlpha("", analyzeModelViewerTexture(rgba))
		}
	}
	if err := ctx.Err(); err != nil {
		return modelViewerDDSMetadata{}, err
	}
	return modelViewerDDSMetadata{
		Format:          format,
		Width:           metadata.Width,
		Height:          metadata.Height,
		MipCount:        metadata.Mipmaps,
		AutoInvertAlpha: autoInvertAlpha,
	}, nil
}

func modelViewerDDSFormatHasAlpha(format string) bool {
	return strings.HasPrefix(format, "bc1-") || strings.HasPrefix(format, "bc2-") ||
		strings.HasPrefix(format, "bc3-") || strings.HasPrefix(format, "bc7-")
}

func modelViewerDDSFormat(format ddsutil.ImageFormat) (string, bool) {
	formats := map[ddsutil.ImageFormat]string{
		ddsutil.BC1RgbaUnorm:     "bc1-unorm",
		ddsutil.BC1RgbaUnormSrgb: "bc1-unorm-srgb",
		ddsutil.BC2RgbaUnorm:     "bc2-unorm",
		ddsutil.BC2RgbaUnormSrgb: "bc2-unorm-srgb",
		ddsutil.BC3RgbaUnorm:     "bc3-unorm",
		ddsutil.BC3RgbaUnormSrgb: "bc3-unorm-srgb",
		ddsutil.BC4RUnorm:        "bc4-unorm",
		ddsutil.BC4RSnorm:        "bc4-snorm",
		ddsutil.BC5RgUnorm:       "bc5-unorm",
		ddsutil.BC5RgSnorm:       "bc5-snorm",
		ddsutil.BC6hRgbUfloat:    "bc6h-ufloat",
		ddsutil.BC6hRgbSfloat:    "bc6h-sfloat",
		ddsutil.BC7RgbaUnorm:     "bc7-unorm",
		ddsutil.BC7RgbaUnormSrgb: "bc7-unorm-srgb",
	}
	value, ok := formats[format]
	return value, ok
}
