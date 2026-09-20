//go:build windows

package platform

import (
	"errors"
	"image"
	"testing"
	"time"

	"github.com/rodrigocfd/windigo/win"
)

// TestCaptureWindowWGCRejectsInvalidHandles exercises the real WGC path
// against handles that cannot yield a frame: the call fails fast with
// CAPTURE_UNAVAILABLE instead of burning the poll budget.
func TestCaptureWindowWGCRejectsInvalidHandles(t *testing.T) {
	start := time.Now()
	_, err := captureWindowWGC(win.HWND(0), 3*time.Second)
	if !errors.Is(err, ErrCaptureUnavailable) {
		t.Fatalf("captureWindowWGC(null) = %v, want %v", err, ErrCaptureUnavailable)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("captureWindowWGC(null) took %v, want a fast rejection", elapsed)
	}
}

func TestCaptureWindowPixelsWithFallsBackInOrder(t *testing.T) {
	t.Parallel()

	target := grabTarget{handle: win.HWND(0), rect: win.RECT{Right: 64, Bottom: 40}}
	compositor := noisyCapture(64, 40)
	printed := noisyCapture(64, 40)
	screenErr := errors.New("screen unavailable")
	wgcErr := errors.New("wgc unavailable")
	printErr := errors.New("print unavailable")

	cases := []struct {
		name       string
		compositor func(grabTarget) (*image.RGBA, error)
		print      func(grabTarget) (*image.RGBA, error)
		want       *image.RGBA
		wantErrs   []error
	}{
		{
			name:       "compositor frame wins",
			compositor: func(grabTarget) (*image.RGBA, error) { return compositor, nil },
			print: func(grabTarget) (*image.RGBA, error) {
				t.Error("print ran although the compositor delivered a frame")
				return printed, nil
			},
			want: compositor,
		},
		{
			name:       "no frame falls back to print",
			compositor: func(grabTarget) (*image.RGBA, error) { return nil, nil },
			print:      func(grabTarget) (*image.RGBA, error) { return printed, nil },
			want:       printed,
		},
		{
			name:       "compositor error falls back to print",
			compositor: func(grabTarget) (*image.RGBA, error) { return nil, wgcErr },
			print:      func(grabTarget) (*image.RGBA, error) { return printed, nil },
			want:       printed,
		},
		{
			name:       "every failure joins the attempts",
			compositor: func(grabTarget) (*image.RGBA, error) { return nil, wgcErr },
			print:      func(grabTarget) (*image.RGBA, error) { return nil, printErr },
			wantErrs:   []error{wgcErr, printErr},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			frame, err := captureWindowPixelsWith(target,
				func(grabTarget) (*image.RGBA, error) {
					t.Error("screen copy ran for a background test handle")
					return nil, screenErr
				},
				testCase.compositor,
				testCase.print,
			)
			if testCase.want != nil {
				if err != nil {
					t.Fatalf("captureWindowPixelsWith = %v", err)
				}
				if frame != testCase.want {
					t.Fatal("captureWindowPixelsWith did not return the expected attempt")
				}
				return
			}

			if err == nil {
				t.Fatal("captureWindowPixelsWith = nil, want the joined attempts")
			}
			for _, want := range testCase.wantErrs {
				if !errors.Is(err, want) {
					t.Fatalf("captureWindowPixelsWith = %v, want %v joined", err, want)
				}
			}
		})
	}
}

func TestCropCapturedImageCutsAndRejects(t *testing.T) {
	t.Parallel()

	frame := image.NewRGBA(image.Rect(0, 0, 8, 6))
	for y := range 6 {
		for x := range 8 {
			offset := frame.PixOffset(x, y)
			frame.Pix[offset] = uint8(x)
			frame.Pix[offset+1] = uint8(y)
			frame.Pix[offset+2] = 0x7F
			frame.Pix[offset+3] = 0xFF
		}
	}

	cropped, err := cropCapturedImage(frame, image.Rect(2, 1, 6, 4))
	if err != nil {
		t.Fatalf("cropCapturedImage = %v", err)
	}
	if cropped.Bounds().Dx() != 4 || cropped.Bounds().Dy() != 3 {
		t.Fatalf("cropped = %v, want a 4×3 image", cropped.Bounds())
	}
	if got := cropped.RGBAAt(0, 0); got.R != 2 || got.G != 1 {
		t.Fatalf("cropped top-left = %v, want the (2,1) pixel", got)
	}
	if got := cropped.RGBAAt(3, 2); got.R != 5 || got.G != 3 {
		t.Fatalf("cropped bottom-right = %v, want the (5,3) pixel", got)
	}

	for _, rect := range []image.Rectangle{
		image.Rect(0, 0, 0, 6),
		image.Rect(-1, 0, 4, 4),
		image.Rect(0, 0, 9, 6),
		image.Rect(4, 4, 9, 9),
	} {
		if _, err := cropCapturedImage(frame, rect); !errors.Is(err, ErrCaptureUnavailable) {
			t.Fatalf("cropCapturedImage(%v) = %v, want %v", rect, err, ErrCaptureUnavailable)
		}
	}
}
