package infra

import (
	"bytes"
	"image"
	"image/jpeg"
	"testing"
)

// JPEG's YCbCr is already full range; expanding it as WebP would crush its blacks and whites.
func TestDecodeImageKeepsJPEG(t *testing.T) {
	var data bytes.Buffer
	src := image.NewYCbCr(image.Rect(0, 0, 8, 8), image.YCbCrSubsampleRatio420)
	if err := jpeg.Encode(&data, src, nil); err != nil {
		t.Fatal(err)
	}
	img, format, err := DecodeImage(&data)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := img.(*image.YCbCr); format != "jpeg" || !ok {
		t.Errorf("DecodeImage = %T, %q; want the decoder's *image.YCbCr, \"jpeg\"", img, format)
	}
}

// Lossy WebP stores BT.601 limited-range YCbCr; read as full range, its images come out about 8% darker and greyer
// than libwebp shows them.
func TestVP8NRGBALimitedRange(t *testing.T) {
	img := image.NewNYCbCrA(image.Rect(0, 0, 2, 2), image.YCbCrSubsampleRatio420)
	img.Y[0], img.Y[1] = 235, 16
	img.Cb[0], img.Cr[0] = 128, 128
	img.A[0], img.A[1] = 7, 255

	out := vp8NRGBA(&img.YCbCr, img)
	for i, want := range [][4]uint8{{255, 255, 255, 7}, {0, 0, 0, 255}} {
		if got := [4]uint8(out.Pix[i*4 : i*4+4]); got != want {
			t.Errorf("pixel %d = %v, want %v", i, got, want)
		}
	}
}
