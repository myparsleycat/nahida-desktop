package modelviewer

import (
	"context"
	"fmt"
	"os"

	"github.com/myparsleycat/ddsutil"
)

type modelViewerDDSMetadata struct {
	Format   string
	Width    uint32
	Height   uint32
	MipCount uint32
}

func prepareModelViewerDDSFallback(ctx context.Context, path string) ([]byte, error) {
	decoded, err := decodeModelViewerTextureSource(ctx, path)
	if err != nil {
		return nil, err
	}
	prepared, err := encodeModelViewerPreparedTexture(
		ctx,
		decoded,
		path,
		"",
		modelViewerTextureTransformPassthrough,
		"png",
		100,
	)
	if err != nil {
		return nil, err
	}
	return prepared.bytes, nil
}

func inspectModelViewerDDS(path string) (modelViewerDDSMetadata, error) {
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
	return modelViewerDDSMetadata{
		Format:   format,
		Width:    metadata.Width,
		Height:   metadata.Height,
		MipCount: metadata.Mipmaps,
	}, nil
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
