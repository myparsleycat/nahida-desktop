//go:build windows

package platform

import (
	"context"
	"errors"
	"fmt"
	"image"
	"time"
	"unsafe"

	"github.com/rodrigocfd/windigo/co"
	"github.com/rodrigocfd/windigo/win"
)

// PrintWindow flags. The window renders itself with PW_RENDERFULLCONTENT, which
// only covers GDI applications: GPU-rendered game windows never answer it.
const (
	pwClientOnly        = 0x0000_0001
	pwRenderFullContent = 0x0000_0002
)

const (
	// captureTimeout bounds a capture, because PrintWindow sends a message a
	// busy game window can stall for a long time.
	captureTimeout = 5 * time.Second

	// wgcFrameTimeout bounds the compositor poll inside a capture, leaving
	// room for the PrintWindow fallback and the PNG encode.
	wgcFrameTimeout = 3 * time.Second

	captureBytesPerPixel = 4
)

var procPrintWindow = modUser32.NewProc("PrintWindow")

// Compositor capture outcomes that are not failures: the window was skipped
// because it cannot yield a frame, or the compositor simply had none. Both
// are joined into the final error only to name the attempted path.
var (
	errWGCSkipped = errors.New("wgc: skipped")
	errWGCNoFrame = errors.New("wgc: no frame")
)

// grabTarget is one resolved capture: the window and the screen rectangle to
// copy when the window is visible.
type grabTarget struct {
	handle     win.HWND
	rect       win.RECT
	clientOnly bool
}

// Screen captures the image of another window. Window targets are resolved per
// request, so a caller may pass a window title, a process name, a PID, or any
// combination of them.
type Screen struct {
	diagnostic func(error, string, map[string]any)

	windows windowScanner
	grab    func(grabTarget) (*image.RGBA, error)
}

func NewScreen() *Screen {
	return &Screen{windows: defaultWindowScanner(), grab: captureWindowPixels}
}

//wails:ignore
func (s *Screen) UseDiagnostic(report func(error, string, map[string]any)) {
	s.diagnostic = report
}

// CaptureWindow returns the current image of the resolved target window as a
// PNG. A visible window in the foreground is copied from the screen, which
// keeps exactly the pixels the user sees; a background window is composited
// without disturbing it, and a window the compositor has no frame for is
// asked to render itself as a GDI fallback.
func (s *Screen) CaptureWindow(ctx context.Context, request CaptureRequest) (CaptureResult, error) {
	if err := ctx.Err(); err != nil {
		return CaptureResult{}, err
	}
	if err := resolveCaptureRequest(request); err != nil {
		return CaptureResult{}, err
	}

	record, err := selectWindow(s.windows.records(), request.Target)
	if err != nil {
		s.report(err, "resolve", request.Target.resolveDiagnosticFields())
		return CaptureResult{}, err
	}
	rect, err := captureWindowRect(record.handle, request.ClientOnly)
	if err != nil {
		s.report(err, "geometry", map[string]any{"window": record.describe()})
		return CaptureResult{}, err
	}

	captured, stage, err := s.captureWithinTimeout(ctx, grabTarget{
		handle: record.handle, rect: rect, clientOnly: request.ClientOnly,
	}, request.MaxBytes)
	if err != nil {
		s.report(err, stage, map[string]any{
			"window": record.describe(), "clientOnly": request.ClientOnly,
			"foreground": record.isForeground, "maxBytes": request.MaxBytes,
		})
		return CaptureResult{}, err
	}
	return CaptureResult{
		Window: record.info(), Width: captured.width, Height: captured.height,
		Scale: captured.scale, PNG: captured.png,
	}, nil
}

// captureWithinTimeout runs the Win32 grab and the PNG encode on their own
// goroutine, so a window that never answers its print message cannot block the
// caller and a large frame cannot outlive the deadline. The goroutine keeps
// running until it finishes, then releases the bitmap it allocated. The stage
// names the step that failed, and is empty on success.
func (s *Screen) captureWithinTimeout(
	ctx context.Context, target grabTarget, maxBytes int,
) (captureFrame, string, error) {
	type outcome struct {
		frame captureFrame
		stage string
		err   error
	}
	done := make(chan outcome, 1)
	go func() {
		pixels, err := s.grab(target)
		if err != nil {
			done <- outcome{stage: "grab", err: err}
			return
		}
		frame, err := encodeCapturedPNG(pixels, maxBytes)
		done <- outcome{frame: frame, stage: "encode", err: err}
	}()

	timer := time.NewTimer(captureTimeout)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return captureFrame{}, "capture", ctx.Err()
	case <-timer.C:
		return captureFrame{}, "capture", fmt.Errorf(
			"%w: the capture did not finish within %s",
			ErrCaptureUnavailable,
			captureTimeout,
		)
	case got := <-done:
		return got.frame, got.stage, got.err
	}
}

func (s *Screen) report(err error, stage string, fields map[string]any) {
	if err == nil || s.diagnostic == nil {
		return
	}
	s.diagnostic(err, stage, fields)
}

// captureWindowRect returns the screen rectangle of the window, or of its
// client area with clientOnly. The window frame comes from the DWM bounds,
// which exclude the drop shadow and the invisible resize border that
// GetWindowRect includes.
func captureWindowRect(handle win.HWND, clientOnly bool) (win.RECT, error) {
	rect, err := probeWindowRect(handle, clientOnly)
	if err != nil {
		return win.RECT{}, err
	}

	width, height := rectSize(rect)
	switch {
	case width <= 0 || height <= 0:
		return win.RECT{}, fmt.Errorf(
			"%w: window 0x%X has an empty %d×%d rectangle",
			ErrCaptureUnavailable,
			uintptr(handle),
			width,
			height,
		)
	case int64(width)*int64(height) > maxCapturePixels:
		return win.RECT{}, fmt.Errorf(
			"%w: window 0x%X spans %d×%d pixels",
			ErrCaptureUnavailable,
			uintptr(handle),
			width,
			height,
		)
	}
	return rect, nil
}

func probeWindowRect(handle win.HWND, clientOnly bool) (win.RECT, error) {
	if clientOnly {
		client, err := handle.GetClientRect()
		if err != nil {
			return win.RECT{}, fmt.Errorf("%w: read the client rectangle: %w", ErrCaptureUnavailable, err)
		}
		if err := handle.ClientToScreenRc(&client); err != nil {
			return win.RECT{}, fmt.Errorf(
				"%w: map the client rectangle onto the screen: %w",
				ErrCaptureUnavailable,
				err,
			)
		}
		return client, nil
	}

	if attribute, err := handle.DwmGetWindowAttribute(co.DWMWA_EXTENDED_FRAME_BOUNDS); err == nil {
		if bounds, ok := attribute.ExtendedFrameBounds(); ok {
			return bounds, nil
		}
	}
	bounds, err := handle.GetWindowRect()
	if err != nil {
		return win.RECT{}, fmt.Errorf("%w: read the window rectangle: %w", ErrCaptureUnavailable, err)
	}
	return bounds, nil
}

// captureWindowPixels renders the target into an opaque 32-bit bitmap. A
// foreground window is copied from the screen so overlays on top of it stay in
// the picture; a background window is composited by Windows Graphics Capture,
// which is the only path that covers GPU-rendered game windows; anything that
// yields no frame falls back to PrintWindow for GDI applications. Foreground
// and minimized state are read here rather than taken from the enumeration, so
// a window that moved between the two cannot be copied from the wrong pixels.
func captureWindowPixels(target grabTarget) (*image.RGBA, error) {
	screen := func(target grabTarget) (*image.RGBA, error) { return copyScreenRegion(target.rect) }
	return captureWindowPixelsWith(target, screen, captureCompositorFrame, printWindow)
}

// captureAttempt grabs the target through one capture path: the screen copy,
// the compositor, or the window itself.
type captureAttempt func(grabTarget) (*image.RGBA, error)

// captureWindowPixelsWith runs the capture fallbacks in order: the screen copy
// for a foreground window, the compositor for a window that can yield a frame,
// and PrintWindow for GDI applications. The attempts are parameters so tests
// can script each path without touching the screen.
func captureWindowPixelsWith(
	target grabTarget, screen, compositor, print captureAttempt,
) (*image.RGBA, error) {
	var screenErr error
	if !target.handle.IsIconic() && win.GetForegroundWindow() == target.handle {
		frame, err := screen(target)
		if err == nil {
			return frame, nil
		}
		screenErr = err
	}

	wgcErr := errWGCSkipped
	if !target.handle.IsIconic() && !windowCloaked(target.handle) {
		frame, err := compositor(target)
		if err == nil {
			if frame != nil {
				return frame, nil
			}
			wgcErr = errWGCNoFrame
		} else {
			wgcErr = err
		}
	}

	frame, err := print(target)
	if err == nil {
		return frame, nil
	}
	return nil, errors.Join(screenErr, wgcErr, err)
}

// windowCloaked reports whether DWM hides the window from the screen, for
// example on another virtual desktop. The compositor keeps no frame for such
// a window, so capturing it would only burn the poll budget.
func windowCloaked(handle win.HWND) bool {
	attribute, err := handle.DwmGetWindowAttribute(co.DWMWA_CLOAKED)
	if err != nil {
		return false
	}
	cloaked, ok := attribute.Cloaked()
	return ok && cloaked != 0
}

// captureCompositorFrame grabs one compositor frame of the window and crops it
// to the requested rectangle. A nil frame with a nil error means the
// compositor had no frame for the window, in which case the caller falls back
// to PrintWindow. The compositor always delivers the whole window, so a
// client-only request crops the client rectangle out of the full frame.
func captureCompositorFrame(target grabTarget) (*image.RGBA, error) {
	full, err := probeWindowRect(target.handle, false)
	if err != nil {
		return nil, err
	}
	fullWidth, fullHeight := rectSize(full)
	if int64(fullWidth)*int64(fullHeight) > maxCapturePixels {
		return nil, fmt.Errorf(
			"%w: window 0x%X spans %d×%d pixels",
			ErrCaptureUnavailable,
			uintptr(target.handle),
			fullWidth,
			fullHeight,
		)
	}

	captured, err := captureWindowWGC(target.handle, wgcFrameTimeout)
	if err != nil {
		return nil, err
	}
	if captured.frame == nil {
		return nil, nil
	}
	if captured.width != fullWidth || captured.height != fullHeight {
		return nil, fmt.Errorf(
			"%w: window 0x%X delivered %d×%d pixels for a %d×%d capture",
			ErrCaptureUnavailable,
			uintptr(target.handle),
			captured.width,
			captured.height,
			fullWidth,
			fullHeight,
		)
	}

	wantWidth, wantHeight := rectSize(target.rect)
	return cropCapturedImage(captured.frame, image.Rect(
		int(target.rect.Left-full.Left),
		int(target.rect.Top-full.Top),
		int(target.rect.Left-full.Left)+wantWidth,
		int(target.rect.Top-full.Top)+wantHeight,
	))
}

// cropCapturedImage cuts rect out of a compositor frame. The rectangle is
// rejected unless it fits the frame exactly, so a window that moved mid
// capture cannot produce a shifted image.
func cropCapturedImage(frame *image.RGBA, rect image.Rectangle) (*image.RGBA, error) {
	if rect.Empty() || !rect.In(frame.Bounds()) {
		return nil, fmt.Errorf(
			"%w: cannot crop %v out of a %v capture",
			ErrCaptureUnavailable,
			rect,
			frame.Bounds(),
		)
	}

	cropped := image.NewRGBA(image.Rect(0, 0, rect.Dx(), rect.Dy()))
	for y := range rect.Dy() {
		source := frame.Pix[frame.PixOffset(rect.Min.X, rect.Min.Y+y):]
		copy(cropped.Pix[y*cropped.Stride:y*cropped.Stride+rect.Dx()*captureBytesPerPixel], source)
	}
	return cropped, nil
}

// copyScreenRegion copies the on-screen pixels of rect, which only works while
// the window is visible and nothing covers it.
func copyScreenRegion(rect win.RECT) (*image.RGBA, error) {
	screen, err := win.HWND(0).GetDC()
	if err != nil {
		return nil, fmt.Errorf("%w: open the screen device context: %w", ErrCaptureUnavailable, err)
	}
	defer func() { _ = win.HWND(0).ReleaseDC(screen) }()

	width, height := rectSize(rect)
	surface, err := newCaptureSurface(width, height)
	if err != nil {
		return nil, err
	}
	defer surface.close()

	err = surface.dc.BitBlt(
		win.POINT{X: 0, Y: 0},
		win.SIZE{Cx: int32(width), Cy: int32(height)},
		screen,
		win.POINT{X: rect.Left, Y: rect.Top},
		co.ROP_SRCCOPY,
	)
	if err != nil {
		return nil, fmt.Errorf("%w: copy the screen region: %w", ErrCaptureUnavailable, err)
	}
	return surface.image(), nil
}

// printWindow asks the window to draw itself into the bitmap, which only GDI
// applications answer. GPU-rendered windows never do, so this stays a
// fallback behind the compositor capture.
func printWindow(target grabTarget) (*image.RGBA, error) {
	width, height := rectSize(target.rect)
	surface, err := newCaptureSurface(width, height)
	if err != nil {
		return nil, err
	}
	defer surface.close()

	flags := uintptr(pwRenderFullContent)
	if target.clientOnly {
		flags |= pwClientOnly
	}
	result, _, _ := procPrintWindow.Call(uintptr(target.handle), uintptr(surface.dc), flags)
	if result == 0 {
		return nil, fmt.Errorf(
			"%w: window 0x%X refused to render itself",
			ErrCaptureUnavailable,
			uintptr(target.handle),
		)
	}
	return surface.image(), nil
}

// captureSurface is the screen-compatible memory bitmap one capture renders
// into.
type captureSurface struct {
	dc       win.HDC
	bitmap   win.HBITMAP
	previous win.HBITMAP
	bits     *byte
	width    int
	height   int
}

func newCaptureSurface(width, height int) (captureSurface, error) {
	memoryDC, err := win.HDC(0).CreateCompatibleDC()
	if err != nil {
		return captureSurface{}, fmt.Errorf(
			"%w: create a memory device context: %w",
			ErrCaptureUnavailable,
			err,
		)
	}

	info := win.BITMAPINFO{}
	info.BmiHeader.Width = int32(width)
	// A negative height asks for a top-down bitmap, so every row is stored in
	// the order image.RGBA expects.
	info.BmiHeader.Height = int32(-height)
	info.BmiHeader.Planes = 1
	info.BmiHeader.BitCount = co.BITCOUNT_32
	info.BmiHeader.Compression = co.BI_RGB
	info.BmiHeader.SetBiSize()

	bitmap, bits, err := memoryDC.CreateDIBSection(&info, co.DIB_COLORS_RGB, 0, 0)
	if err != nil {
		_ = memoryDC.DeleteDC()
		return captureSurface{}, fmt.Errorf(
			"%w: create a %d×%d capture bitmap: %w",
			ErrCaptureUnavailable,
			width,
			height,
			err,
		)
	}
	previous, err := memoryDC.SelectObjectBmp(bitmap)
	if err != nil {
		_ = bitmap.DeleteObject()
		_ = memoryDC.DeleteDC()
		return captureSurface{}, fmt.Errorf("%w: select the capture bitmap: %w", ErrCaptureUnavailable, err)
	}
	return captureSurface{
		dc: memoryDC, bitmap: bitmap, previous: previous, bits: bits, width: width, height: height,
	}, nil
}

func (s captureSurface) close() {
	_, _ = s.dc.SelectObjectBmp(s.previous)
	_ = s.bitmap.DeleteObject()
	_ = s.dc.DeleteDC()
}

// image copies the rendered pixels into an opaque RGBA image. GDI leaves the
// fourth byte of every pixel at zero, which would otherwise produce an
// invisible PNG.
func (s captureSurface) image() *image.RGBA {
	frame := image.NewRGBA(image.Rect(0, 0, s.width, s.height))
	rows := unsafe.Slice(s.bits, s.width*s.height*captureBytesPerPixel)
	for y := range s.height {
		source := rows[y*s.width*captureBytesPerPixel : (y+1)*s.width*captureBytesPerPixel]
		target := frame.Pix[y*frame.Stride : y*frame.Stride+s.width*captureBytesPerPixel]
		for x := range s.width {
			pixel := x * captureBytesPerPixel
			target[pixel] = source[pixel+2]
			target[pixel+1] = source[pixel+1]
			target[pixel+2] = source[pixel]
			target[pixel+3] = 0xFF
		}
	}
	return frame
}

// rectSize returns the extent of a rectangle. The edges widen before they are
// subtracted, so a damaged or overflowing rectangle cannot wrap into a small
// positive size.
func rectSize(rect win.RECT) (int, int) {
	return int(rect.Right) - int(rect.Left), int(rect.Bottom) - int(rect.Top)
}
