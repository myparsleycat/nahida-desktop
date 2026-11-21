package infra

import (
	"image"
	"io"
)

// DecodeImage decodes an image in any format registered with the image package.
//
// Lossy WebP decodes to YCbCr in VP8's BT.601 limited range, which image/color's conversion, and so image/draw,
// reads as full-range JFIF, mapping white to 235 and black to 16. DecodeImage returns such an image as
// non-premultiplied RGBA expanded as libwebp does, keeping the colour of transparent texels.
func DecodeImage(r io.Reader) (image.Image, string, error) {
	img, format, err := image.Decode(r)
	if err != nil || format != "webp" {
		return img, format, err
	}
	switch src := img.(type) {
	case *image.YCbCr:
		return vp8NRGBA(src, nil), format, nil
	case *image.NYCbCrA:
		return vp8NRGBA(&src.YCbCr, src), format, nil
	}
	return img, format, nil
}

func vp8NRGBA(ycc *image.YCbCr, alpha *image.NYCbCrA) *image.NRGBA {
	bounds := ycc.Rect
	out := image.NewNRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	for y := range bounds.Dy() {
		for x := range bounds.Dx() {
			px, py := bounds.Min.X+x, bounds.Min.Y+y
			yi, ci := ycc.YOffset(px, py), ycc.COffset(px, py)
			i := y*out.Stride + x*4
			out.Pix[i], out.Pix[i+1], out.Pix[i+2] = vp8RGB(ycc.Y[yi], ycc.Cb[ci], ycc.Cr[ci])
			out.Pix[i+3] = 0xff
			if alpha != nil {
				out.Pix[i+3] = alpha.A[alpha.AOffset(px, py)]
			}
		}
	}
	return out
}

// vp8RGB is libwebp's fixed-point VP8YUVToR/G/B.
func vp8RGB(y, cb, cr uint8) (r, g, b uint8) {
	clip := func(v int) uint8 {
		return uint8(min(max(v>>6, 0), 255))
	}
	luma := int(y) * 19077 >> 8
	r = clip(luma + int(cr)*26149>>8 - 14234)
	g = clip(luma - int(cb)*6419>>8 - int(cr)*13320>>8 + 8708)
	b = clip(luma + int(cb)*33050>>8 - 17685)
	return r, g, b
}
