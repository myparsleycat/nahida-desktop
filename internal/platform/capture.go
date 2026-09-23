package platform

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/png"
)

// Capture sentinel errors. UPPER_SNAKE prefixes are part of the renderer and
// agent contract, so keep them stable and add detail after the prefix.
var (
	ErrCaptureUnavailable = errors.New("CAPTURE_UNAVAILABLE")
	ErrCaptureTooLarge    = errors.New("CAPTURE_TOO_LARGE")
)

const (
	minCaptureBytes = 16 << 10
	maxCaptureBytes = 32 << 20

	// maxCapturePixels rejects a window whose rectangle is too large to copy
	// into memory, which also covers a damaged or overflowing rectangle.
	maxCapturePixels = 64_000_000

	// minCaptureDimension stops the byte budget from shrinking a capture into
	// an unreadable thumbnail.
	minCaptureDimension = 160
)

// CaptureRequest asks for the pixels of one window. Target is resolved per
// request with the same selector Input uses, so a caller may pass a window
// title, a process name, a PID, or any combination of them.
type CaptureRequest struct {
	Target     WindowTarget `json:"target"`
	ClientOnly bool         `json:"clientOnly,omitempty"`
	MaxBytes   int          `json:"maxBytes,omitempty"`
}

// CaptureResult is the captured image of one window. Scale is the applied
// downscale factor, where 1 keeps the window's own pixel size.
type CaptureResult struct {
	Window WindowInfo `json:"window"`
	Width  int        `json:"width"`
	Height int        `json:"height"`
	Scale  float64    `json:"scale"`
	// PNG holds the encoded image. The Wails binding surfaces byte slices as
	// base64 strings.
	PNG []byte `json:"png"`
}

// PixelCapture is an unencoded window capture for internal high-frequency
// consumers. It is never exposed through Wails.
type PixelCapture struct {
	Window WindowInfo
	Image  *image.RGBA
}

// captureFrame is one encoded capture: its PNG bytes, their pixel size, and the
// downscale that was applied to fit the byte budget.
type captureFrame struct {
	png    []byte
	width  int
	height int
	scale  float64
}

func resolveCaptureRequest(request CaptureRequest) error {
	if request.Target.empty() {
		return ErrInputTargetRequired
	}
	if request.MaxBytes == 0 {
		return nil
	}
	if request.MaxBytes < minCaptureBytes || request.MaxBytes > maxCaptureBytes {
		return fmt.Errorf(
			"%w: maxBytes must be 0 or between %d and %d",
			ErrInputOptionInvalid,
			minCaptureBytes,
			maxCaptureBytes,
		)
	}
	return nil
}

// encodeCapturedPNG encodes captured pixels as PNG. With a byte budget the
// image is halved until the encoding fits, and the applied scale is reported
// with the result.
func encodeCapturedPNG(source *image.RGBA, maxBytes int) (captureFrame, error) {
	frame, scale := source, 1.0
	for {
		encoded, err := encodePNG(frame)
		if err != nil {
			return captureFrame{}, err
		}
		width, height := frame.Rect.Dx(), frame.Rect.Dy()
		if maxBytes == 0 || len(encoded) <= maxBytes {
			return captureFrame{png: encoded, width: width, height: height, scale: scale}, nil
		}
		if width <= minCaptureDimension || height <= minCaptureDimension {
			return captureFrame{}, fmt.Errorf(
				"%w: %d bytes at %d×%d cannot fit %d bytes",
				ErrCaptureTooLarge,
				len(encoded),
				width,
				height,
				maxBytes,
			)
		}
		frame, scale = halveImage(frame), scale/2
	}
}

func encodePNG(frame *image.RGBA) ([]byte, error) {
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, frame); err != nil {
		return nil, fmt.Errorf("encode capture: %w", err)
	}
	return buffer.Bytes(), nil
}

// halveImage averages 2x2 pixel blocks. Captures are opaque, so averaging the
// channels directly is correct and no alpha premultiplication is involved.
func halveImage(source *image.RGBA) *image.RGBA {
	width := max(source.Rect.Dx()/2, 1)
	height := max(source.Rect.Dy()/2, 1)
	target := image.NewRGBA(image.Rect(0, 0, width, height))

	for y := range height {
		for x := range width {
			var sums [4]int
			count := 0
			for dy := range 2 {
				for dx := range 2 {
					sourceX, sourceY := source.Rect.Min.X+x*2+dx, source.Rect.Min.Y+y*2+dy
					if sourceX >= source.Rect.Max.X || sourceY >= source.Rect.Max.Y {
						continue
					}
					offset := source.PixOffset(sourceX, sourceY)
					for channel := range 4 {
						sums[channel] += int(source.Pix[offset+channel])
					}
					count++
				}
			}
			offset := target.PixOffset(x, y)
			for channel := range 4 {
				target.Pix[offset+channel] = uint8(sums[channel] / count)
			}
		}
	}
	return target
}
