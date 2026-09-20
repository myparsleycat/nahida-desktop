//go:build windows

package platform

import (
	"context"
	"errors"
	"fmt"
	"image"
	"os"
	"slices"
	"testing"

	"github.com/rodrigocfd/windigo/win"
)

func TestScreenCaptureWindowResolvesTargetAndReturnsPNG(t *testing.T) {
	title := "Nahida Screen Test " + t.Name()
	window := newTestWindow(t, title)
	screen := NewScreen()
	window.restrictScreen(screen)

	var grabbed []grabTarget
	screen.grab = func(target grabTarget) (*image.RGBA, error) {
		grabbed = append(grabbed, target)
		return noisyCapture(64, 40), nil
	}

	ctx := context.Background()
	result, err := screen.CaptureWindow(ctx, CaptureRequest{Target: WindowTarget{Title: title}})
	if err != nil {
		t.Fatalf("CaptureWindow = %v", err)
	}
	if result.Window.Title != title || result.Window.PID != uint32(os.Getpid()) {
		t.Fatalf("window = %+v, want the test window", result.Window)
	}
	if len(grabbed) != 1 {
		t.Fatalf("grabbed %d targets, want 1", len(grabbed))
	}
	if target := grabbed[0]; target.handle != window.handle || target.clientOnly {
		t.Fatalf("grab target = %+v, want the hidden test window in window mode", target)
	}
	if width, height := rectSize(grabbed[0].rect); width <= 0 || height <= 0 {
		t.Fatalf("grab rectangle = %+v, want a visible area", grabbed[0].rect)
	}
	if result.Width != 64 || result.Height != 40 || result.Scale != 1 {
		t.Fatalf("result = %d×%d at scale %v, want 64×40 at scale 1",
			result.Width, result.Height, result.Scale)
	}
	if decoded := decodePNG(t, result.PNG); decoded.Bounds().Dx() != 64 || decoded.Bounds().Dy() != 40 {
		t.Fatalf("decoded = %v, want a 64×40 image", decoded.Bounds())
	}

	// A client-only request narrows the copy to the client area.
	clientRequest := CaptureRequest{Target: WindowTarget{Title: title}, ClientOnly: true}
	if _, err := screen.CaptureWindow(ctx, clientRequest); err != nil {
		t.Fatalf("CaptureWindow(client only) = %v", err)
	}
	if target := grabbed[1]; !target.clientOnly {
		t.Fatalf("grab target = %+v, want a client-only capture", target)
	}
	windowWidth, _ := rectSize(grabbed[0].rect)
	clientWidth, _ := rectSize(grabbed[1].rect)
	if clientWidth <= 0 || clientWidth >= windowWidth {
		t.Fatalf("client rectangle is %d px wide, want less than the %d px window", clientWidth, windowWidth)
	}
}

func TestScreenCaptureWindowReportsMissingWindow(t *testing.T) {
	title := "Nahida Screen Test " + t.Name()
	window := newTestWindow(t, title)
	screen := NewScreen()
	window.restrictScreen(screen)
	screen.grab = func(grabTarget) (*image.RGBA, error) {
		t.Error("capture grabbed a window that does not exist")
		return nil, nil
	}

	_, err := screen.CaptureWindow(context.Background(), CaptureRequest{
		Target: WindowTarget{Title: title + " missing"},
	})
	if !errors.Is(err, ErrWindowNotFound) {
		t.Fatalf("CaptureWindow = %v, want %v", err, ErrWindowNotFound)
	}
}

func TestScreenCaptureWindowReportsGrabFailure(t *testing.T) {
	title := "Nahida Screen Test " + t.Name()
	window := newTestWindow(t, title)
	screen := NewScreen()
	window.restrictScreen(screen)

	var stages []string
	screen.UseDiagnostic(func(_ error, stage string, _ map[string]any) {
		stages = append(stages, stage)
	})
	screen.grab = func(grabTarget) (*image.RGBA, error) {
		return nil, fmt.Errorf("%w: protected content", ErrCaptureUnavailable)
	}

	_, err := screen.CaptureWindow(context.Background(), CaptureRequest{Target: WindowTarget{Title: title}})
	if !errors.Is(err, ErrCaptureUnavailable) {
		t.Fatalf("CaptureWindow = %v, want %v", err, ErrCaptureUnavailable)
	}
	if !slices.Contains(stages, "grab") {
		t.Fatalf("diagnostic stages = %v, want the grab stage", stages)
	}
}

func TestScreenCaptureWindowHonorsByteBudget(t *testing.T) {
	title := "Nahida Screen Test " + t.Name()
	window := newTestWindow(t, title)
	screen := NewScreen()
	window.restrictScreen(screen)
	screen.grab = func(grabTarget) (*image.RGBA, error) {
		return noisyCapture(512, 512), nil
	}

	const budget = 64 << 10
	result, err := screen.CaptureWindow(context.Background(), CaptureRequest{
		Target: WindowTarget{Title: title}, MaxBytes: budget,
	})
	if err != nil {
		t.Fatalf("CaptureWindow = %v", err)
	}
	if len(result.PNG) > budget {
		t.Fatalf("capture is %d bytes, want at most %d", len(result.PNG), budget)
	}
	if result.Scale >= 1 || result.Width != int(512*result.Scale) {
		t.Fatalf("result = %d×%d at scale %v, want a halved image", result.Width, result.Height, result.Scale)
	}
}

// TestCaptureWindowPixelsPrintsHiddenWindow covers the real GDI path: a window
// that is never shown on screen still renders itself into the capture bitmap.
func TestCaptureWindowPixelsPrintsHiddenWindow(t *testing.T) {
	title := "Nahida Screen Test " + t.Name()
	window := newTestWindow(t, title)

	rect, err := captureWindowRect(window.handle, false)
	if err != nil {
		t.Fatalf("captureWindowRect = %v", err)
	}
	frame, err := captureWindowPixels(grabTarget{handle: window.handle, rect: rect})
	if err != nil {
		t.Fatalf("captureWindowPixels = %v", err)
	}

	width, height := rectSize(rect)
	if frame.Bounds().Dx() != width || frame.Bounds().Dy() != height {
		t.Fatalf("frame = %v, want %d×%d", frame.Bounds(), width, height)
	}
	for offset := 3; offset < len(frame.Pix); offset += 4 {
		if frame.Pix[offset] != 0xFF {
			t.Fatalf("pixel %d has alpha 0x%X, want an opaque capture", offset/4, frame.Pix[offset])
		}
	}
}

// TestScreenCaptureWindowCapturesRealWindow runs the whole path, from target
// resolution to the encoded PNG, against a real window.
func TestScreenCaptureWindowCapturesRealWindow(t *testing.T) {
	title := "Nahida Screen Test " + t.Name()
	window := newTestWindow(t, title)
	screen := NewScreen()
	window.restrictScreen(screen)

	result, err := screen.CaptureWindow(context.Background(), CaptureRequest{
		Target: WindowTarget{Title: title},
	})
	if err != nil {
		t.Fatalf("CaptureWindow = %v", err)
	}
	if result.Scale != 1 || result.Width <= 0 || result.Height <= 0 {
		t.Fatalf("result = %d×%d at scale %v, want a full-size capture", result.Width, result.Height, result.Scale)
	}
	if decoded := decodePNG(t, result.PNG); decoded.Bounds().Dx() != result.Width ||
		decoded.Bounds().Dy() != result.Height {
		t.Fatalf("decoded = %v, want %d×%d", decoded.Bounds(), result.Width, result.Height)
	}
}

// restrictScreen points the service at this window only, standing in for the
// visibility filter that a hidden window would never pass.
func (w *testWindow) restrictScreen(screen *Screen) {
	scanner := defaultWindowScanner()
	scanner.enumerate = func() []win.HWND { return []win.HWND{w.handle} }
	scanner.isVisible = func(win.HWND) bool { return true }
	screen.windows = scanner
}
