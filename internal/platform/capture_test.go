package platform

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func TestResolveCaptureRequestValidatesTargetAndBudget(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		request CaptureRequest
		wantErr error
	}{
		{name: "no target", request: CaptureRequest{}, wantErr: ErrInputTargetRequired},
		{
			name:    "blank target fields",
			request: CaptureRequest{Target: WindowTarget{Title: "  "}},
			wantErr: ErrInputTargetRequired,
		},
		{
			name:    "budget without a target",
			request: CaptureRequest{MaxBytes: 1 << 20},
			wantErr: ErrInputTargetRequired,
		},
		{name: "full size", request: CaptureRequest{Target: WindowTarget{Process: "game.exe"}}},
		{
			name:    "smallest budget",
			request: CaptureRequest{Target: WindowTarget{PID: 1}, MaxBytes: minCaptureBytes},
		},
		{
			name:    "budget below the floor",
			request: CaptureRequest{Target: WindowTarget{PID: 1}, MaxBytes: minCaptureBytes - 1},
			wantErr: ErrInputOptionInvalid,
		},
		{
			name:    "budget above the ceiling",
			request: CaptureRequest{Target: WindowTarget{PID: 1}, MaxBytes: maxCaptureBytes + 1},
			wantErr: ErrInputOptionInvalid,
		},
	}

	for _, testCase := range cases {
		err := resolveCaptureRequest(testCase.request)
		if testCase.wantErr == nil && err != nil {
			t.Fatalf("%s: resolveCaptureRequest = %v", testCase.name, err)
		}
		if testCase.wantErr != nil && !errors.Is(err, testCase.wantErr) {
			t.Fatalf("%s: resolveCaptureRequest = %v, want %v", testCase.name, err, testCase.wantErr)
		}
	}
}

func TestEncodeCapturedPNGKeepsFullSizeWithoutBudget(t *testing.T) {
	t.Parallel()

	source := noisyCapture(400, 300)
	frame, err := encodeCapturedPNG(source, 0)
	if err != nil {
		t.Fatalf("encodeCapturedPNG = %v", err)
	}
	if frame.scale != 1 || frame.width != 400 || frame.height != 300 {
		t.Fatalf("frame = %d×%d at scale %v, want 400×300 at scale 1", frame.width, frame.height, frame.scale)
	}

	decoded := decodePNG(t, frame.png)
	if decoded.Bounds().Dx() != 400 || decoded.Bounds().Dy() != 300 {
		t.Fatalf("decoded = %v, want a 400×300 image", decoded.Bounds())
	}
	if _, _, _, alpha := decoded.At(10, 10).RGBA(); alpha != 0xFFFF {
		t.Fatalf("decoded alpha = 0x%X, want an opaque pixel", alpha)
	}
	if got, want := decoded.At(10, 10), source.At(10, 10); got != want {
		t.Fatalf("decoded pixel = %v, want the captured pixel %v", got, want)
	}
}

func TestEncodeCapturedPNGDownscalesToFitBudget(t *testing.T) {
	t.Parallel()

	source := noisyCapture(1024, 1024)
	full, err := encodeCapturedPNG(source, 0)
	if err != nil {
		t.Fatalf("encodeCapturedPNG = %v", err)
	}
	budget := len(full.png) / 3
	if budget < minCaptureBytes {
		t.Fatalf("budget %d is below the floor", budget)
	}

	frame, err := encodeCapturedPNG(source, budget)
	if err != nil {
		t.Fatalf("encodeCapturedPNG(%d bytes) = %v", budget, err)
	}
	if len(frame.png) > budget {
		t.Fatalf("encoded %d bytes, want at most %d", len(frame.png), budget)
	}
	if frame.scale >= 1 || frame.width != int(1024*frame.scale) || frame.height != int(1024*frame.scale) {
		t.Fatalf("frame = %d×%d at scale %v, want a halved image", frame.width, frame.height, frame.scale)
	}
	if decoded := decodePNG(t, frame.png); decoded.Bounds().Dx() != frame.width {
		t.Fatalf("decoded width = %d, want %d", decoded.Bounds().Dx(), frame.width)
	}
}

func TestEncodeCapturedPNGReportsUnreachableBudget(t *testing.T) {
	t.Parallel()

	_, err := encodeCapturedPNG(noisyCapture(512, 512), minCaptureBytes)
	if !errors.Is(err, ErrCaptureTooLarge) {
		t.Fatalf("encodeCapturedPNG = %v, want %v", err, ErrCaptureTooLarge)
	}
}

func TestHalveImageAveragesBlocks(t *testing.T) {
	t.Parallel()

	source := image.NewRGBA(image.Rect(0, 0, 4, 4))
	paint := func(x, y int, value byte) {
		offset := source.PixOffset(x, y)
		source.Pix[offset], source.Pix[offset+1] = value, value
		source.Pix[offset+2], source.Pix[offset+3] = value, 0xFF
	}
	for y := range 4 {
		for x := range 4 {
			paint(x, y, byte((x+y*4)*16))
		}
	}

	halved := halveImage(source)
	if halved.Bounds().Dx() != 2 || halved.Bounds().Dy() != 2 {
		t.Fatalf("halved = %v, want a 2×2 image", halved.Bounds())
	}
	// The top-left block averages pixels 0, 16, 64, and 80.
	if got, want := halved.At(0, 0), (color.RGBA{R: 40, G: 40, B: 40, A: 0xFF}); got != want {
		t.Fatalf("halved pixel = %v, want %v", got, want)
	}
	// The bottom-right block averages pixels 160, 176, 224, and 240.
	if got, want := halved.At(1, 1), (color.RGBA{R: 200, G: 200, B: 200, A: 0xFF}); got != want {
		t.Fatalf("halved bottom-right pixel = %v, want %v", got, want)
	}
}

// noisyCapture builds a deterministic high-entropy image, which PNG cannot
// compress, so a byte budget behaves like it would on a real screenshot.
func noisyCapture(width, height int) *image.RGBA {
	frame := image.NewRGBA(image.Rect(0, 0, width, height))
	seed := uint32(0x9E3779B9)
	for y := range height {
		for x := range width {
			seed = seed*1664525 + 1013904223
			value := uint8(seed >> 24)
			offset := frame.PixOffset(x, y)
			frame.Pix[offset] = value
			frame.Pix[offset+1] = value ^ 0x5A
			frame.Pix[offset+2] = value ^ 0xA5
			frame.Pix[offset+3] = 0xFF
		}
	}
	return frame
}

func decodePNG(t *testing.T, encoded []byte) image.Image {
	t.Helper()
	decoded, err := png.Decode(bytes.NewReader(encoded))
	if err != nil {
		t.Fatalf("decode capture: %v", err)
	}
	return decoded
}
