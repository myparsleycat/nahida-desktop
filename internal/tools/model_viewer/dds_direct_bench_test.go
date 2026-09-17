package modelviewer

import (
	"context"
	"testing"

	"github.com/myparsleycat/ddsutil"
)

func BenchmarkPrepareModelViewerDDSPreview(b *testing.B) {
	const width, height = uint32(8192), uint32(8192)
	data := make([]byte, int(modelViewerDDSMipBytes(width, height, 16)))
	path := writeModelViewerDDSurface(b, width, height, ddsutil.BC7RgbaUnorm, data)
	metadata, err := inspectModelViewerDDS(context.Background(), path)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))

	var preview []byte
	for b.Loop() {
		preview, err = prepareModelViewerDDSPreview(context.Background(), path, metadata)
		if err != nil {
			b.Fatal(err)
		}
	}
	if len(preview) < 2048*2048 {
		b.Fatalf("preview bytes = %d", len(preview))
	}
}
