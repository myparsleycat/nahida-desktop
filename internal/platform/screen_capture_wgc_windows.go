//go:build windows

package platform

import (
	"fmt"
	"image"
	"runtime"
	"syscall"
	"time"
	"unsafe"

	"github.com/rodrigocfd/windigo/win"
	"golang.org/x/sys/windows"
)

var (
	modCombase = windows.NewLazySystemDLL("combase.dll")
	modD3D11   = windows.NewLazySystemDLL("d3d11.dll")

	procRoInitialize           = modCombase.NewProc("RoInitialize")
	procRoUninitialize         = modCombase.NewProc("RoUninitialize")
	procRoGetActivationFactory = modCombase.NewProc("RoGetActivationFactory")
	procWindowsCreateString    = modCombase.NewProc("WindowsCreateString")
	procWindowsDeleteString    = modCombase.NewProc("WindowsDeleteString")

	procD3D11CreateDevice                    = modD3D11.NewProc("D3D11CreateDevice")
	procCreateDirect3D11DeviceFromDXGIDevice = modD3D11.NewProc("CreateDirect3D11DeviceFromDXGIDevice")
)

// WinRT runtime class names resolved through RoGetActivationFactory.
const (
	wgcClassCaptureItem = "Windows.Graphics.Capture.GraphicsCaptureItem"
	wgcClassFramePool   = "Windows.Graphics.Capture.Direct3D11CaptureFramePool"
)

// Interface IDs verified against the Windows SDK 10.0.26100.0 headers:
// um/Windows.Graphics.Capture.Interop.h, um/windows.graphics.directx.direct3d11.interop.h,
// winrt/windows.graphics.capture.h, winrt/windows.graphics.directx.direct3d11.h,
// winrt/windows.foundation.h, um/d3d11.h, shared/dxgi.h.
var (
	iidGraphicsCaptureItemInterop  = mustParseGUID("3628E81B-3CAC-4C60-B7F4-23CE0E0C3356")
	iidGraphicsCaptureItem         = mustParseGUID("79c3f95b-31f7-4ec2-a464-632ef5d30760")
	iidFramePoolStatics2           = mustParseGUID("589b103f-6bbc-5df5-a991-02e28b3b66d5")
	iidDirect3DDevice              = mustParseGUID("a37624ab-8d5f-4650-9d3e-9eae3d9bc670")
	iidIDXGIDevice                 = mustParseGUID("54ec77fa-1377-44e6-8c32-88fd5f44c84c")
	iidDirect3DDxgiInterfaceAccess = mustParseGUID("A9B3D012-3DF2-4EE3-B8D1-8695F457D3C1")
	iidD3D11Texture2D              = mustParseGUID("6f15aaf2-d208-4e89-9ab4-489535d34f9c")
	iidClosable                    = mustParseGUID("30d5a829-7fa4-4026-83bb-d75bae4ea99e")
	iidGraphicsCaptureSession2     = mustParseGUID("2c39ae40-7d2e-5044-804e-8b6799d4cf9e")
	iidGraphicsCaptureSession3     = mustParseGUID("f2cdd966-22ae-5ea1-9596-3a289344c3be")
)

// COM vtable slots, 0-based. IUnknown contributes slots 0-2 and IInspectable
// slots 3-5, so derived WinRT methods start at 6 unless the interface derives
// from IUnknown directly. ID3D11DeviceChild contributes slots 3-6, so
// ID3D11DeviceContext methods start at 7.
const (
	slotQueryInterface = 0
	slotRelease        = 2

	slotCaptureItemInteropCreateForWindow = 3
	slotDxgiInterfaceAccessGetInterface   = 3

	slotCaptureItemGetSize             = 7
	slotFramePoolStaticsCreateFree     = 6
	slotFramePoolTryGetNextFrame       = 7
	slotFramePoolCreateCaptureSession  = 10
	slotSessionStartCapture            = 6
	slotSessionPutCursorCaptureEnabled = 7
	slotSessionPutBorderRequired       = 7
	slotFrameGetSurface                = 6
	slotClosableClose                  = 6

	slotDeviceCreateTexture2D = 5
	slotContextMap            = 14
	slotContextUnmap          = 15
	slotContextCopyResource   = 47
)

const (
	roInitMultithreaded = 1

	d3dDriverTypeHardware   = 1
	d3dCreateDeviceBGRA     = 0x20
	d3dSDKVersion           = 7
	dxgiFormatB8G8R8A8Unorm = 87
	directXPixelFormatBGRA  = 87

	d3d11UsageStaging  = 3
	d3d11CPUAccessRead = 0x20000
	d3d11MapRead       = 1
	rpcEChangedMode    = 0x80010106

	wgcPollInterval = 2 * time.Millisecond
	wgcFrameBuffers = 1
)

var wgcFeatureLevels = []uint32{0xb100, 0xb000, 0xa100, 0xa000}

// wgcSizeInt32 mirrors ABI::Windows::Graphics::SizeInt32: two int32 values
// passed by value as one 64-bit argument.
type wgcSizeInt32 struct {
	width  int32
	height int32
}

// wgcTexture2DDesc mirrors D3D11_TEXTURE2D_DESC: ten consecutive UINT values.
type wgcTexture2DDesc struct {
	width          uint32
	height         uint32
	mipLevels      uint32
	arraySize      uint32
	format         uint32
	sampleCount    uint32
	sampleQuality  uint32
	usage          uint32
	bindFlags      uint32
	cpuAccessFlags uint32
	miscFlags      uint32
}

// wgcMappedSubresource mirrors D3D11_MAPPED_SUBRESOURCE.
type wgcMappedSubresource struct {
	data       unsafe.Pointer
	rowPitch   uint32
	depthPitch uint32
}

// wgcCaptureResult is one compositor frame: opaque pixels and their size.
type wgcCaptureResult struct {
	frame  *image.RGBA
	width  int
	height int
}

// captureWindowWGC grabs one compositor frame of a non-minimized window
// without bringing it to the foreground. Every WinRT and D3D11 call runs on a
// dedicated thread because RoInitialize binds the apartment to the calling
// thread. A nil frame with a nil error means the compositor had no frame for
// the window, in which case the caller falls back to PrintWindow.
func captureWindowWGC(handle win.HWND, timeout time.Duration) (wgcCaptureResult, error) {
	type outcome struct {
		result wgcCaptureResult
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()

		result, err := wgcGrabFrame(handle, timeout)
		done <- outcome{result: result, err: err}
	}()

	select {
	case got := <-done:
		return got.result, got.err
	case <-time.After(timeout):
		return wgcCaptureResult{}, fmt.Errorf(
			"%w: the compositor did not deliver a frame within %s",
			ErrCaptureUnavailable,
			timeout,
		)
	}
}

// wgcGrabFrame runs the whole WGC sequence on the caller's thread: initialize
// WinRT, build a D3D11 device, capture one frame of the window, and copy it
// into CPU memory.
func wgcGrabFrame(handle win.HWND, timeout time.Duration) (wgcCaptureResult, error) {
	hr, _, _ := procRoInitialize.Call(roInitMultithreaded)
	if isFailed(hr) && uint32(hr) != rpcEChangedMode {
		return wgcCaptureResult{}, fmt.Errorf("%w: RoInitialize: %s", ErrCaptureUnavailable, wgcHResult(hr))
	}
	// S_FALSE means the thread already had the apartment initialized, which is a
	// success that still must be balanced; RPC_E_CHANGED_MODE initialized nothing
	// and must not be unbalanced.
	if !isFailed(hr) {
		defer func() { _, _, _ = procRoUninitialize.Call() }()
	}

	device, context, err := wgcCreateDevice()
	if err != nil {
		return wgcCaptureResult{}, err
	}
	defer device.release()
	defer context.release()

	directDevice, err := wgcWrapDevice(device)
	if err != nil {
		return wgcCaptureResult{}, err
	}
	defer directDevice.release()

	item, size, err := wgcCaptureItem(handle)
	if err != nil {
		return wgcCaptureResult{}, err
	}
	defer item.release()
	if size.width <= 0 || size.height <= 0 {
		return wgcCaptureResult{}, fmt.Errorf(
			"%w: window 0x%X reports an empty %d×%d capture size",
			ErrCaptureUnavailable,
			uintptr(handle),
			size.width,
			size.height,
		)
	}

	pool, err := wgcFramePool(directDevice, size)
	if err != nil {
		return wgcCaptureResult{}, err
	}
	defer pool.close()

	session, err := wgcCaptureSession(pool, item)
	if err != nil {
		return wgcCaptureResult{}, err
	}
	defer session.close()

	frame, err := wgcPollFrame(pool, timeout)
	if err != nil {
		return wgcCaptureResult{}, err
	}
	if frame.ptr == 0 {
		return wgcCaptureResult{}, nil
	}
	defer frame.close()

	return wgcReadFrame(device, context, frame, int(size.width), int(size.height))
}

// wgcCreateDevice builds a hardware D3D11 device with BGRA support, which the
// capture pool requires, and returns it with its immediate context.
func wgcCreateDevice() (device, context wgcComObject, err error) {
	var dev, ctx uintptr
	var level uint32
	hr, _, _ := procD3D11CreateDevice.Call(
		0,
		d3dDriverTypeHardware,
		0,
		d3dCreateDeviceBGRA,
		uintptr(unsafe.Pointer(&wgcFeatureLevels[0])),
		uintptr(len(wgcFeatureLevels)),
		d3dSDKVersion,
		uintptr(unsafe.Pointer(&dev)),
		uintptr(unsafe.Pointer(&level)),
		uintptr(unsafe.Pointer(&ctx)),
	)
	if isFailed(hr) || dev == 0 || ctx == 0 {
		return wgcComObject{}, wgcComObject{}, fmt.Errorf(
			"%w: D3D11CreateDevice: %s",
			ErrCaptureUnavailable,
			wgcHResult(hr),
		)
	}
	return wgcComObject{ptr: dev}, wgcComObject{ptr: ctx}, nil
}

// wgcWrapDevice exposes the D3D11 device to WinRT so the frame pool can
// allocate its surfaces on it.
func wgcWrapDevice(device wgcComObject) (wgcComObject, error) {
	dxgiDevice, err := device.queryInterface(&iidIDXGIDevice)
	if err != nil {
		return wgcComObject{}, fmt.Errorf("%w: query the DXGI device: %w", ErrCaptureUnavailable, err)
	}
	defer dxgiDevice.release()

	var inspectable uintptr
	hr, _, _ := procCreateDirect3D11DeviceFromDXGIDevice.Call(
		dxgiDevice.ptr,
		uintptr(unsafe.Pointer(&inspectable)),
	)
	if isFailed(hr) || inspectable == 0 {
		return wgcComObject{}, fmt.Errorf(
			"%w: CreateDirect3D11DeviceFromDXGIDevice: %s",
			ErrCaptureUnavailable,
			wgcHResult(hr),
		)
	}
	defer wgcComObject{ptr: inspectable}.release()

	directDevice, err := wgcComObject{ptr: inspectable}.queryInterface(&iidDirect3DDevice)
	if err != nil {
		return wgcComObject{}, fmt.Errorf("%w: query the Direct3D device: %w", ErrCaptureUnavailable, err)
	}
	return directDevice, nil
}

// wgcCaptureItem addresses the window for the capture session and reports the
// compositor size of the window, which matches the DWM extended frame bounds.
func wgcCaptureItem(handle win.HWND) (wgcComObject, wgcSizeInt32, error) {
	interop, err := wgcActivationFactory(wgcClassCaptureItem, &iidGraphicsCaptureItemInterop)
	if err != nil {
		return wgcComObject{}, wgcSizeInt32{}, fmt.Errorf(
			"%w: resolve the capture item factory: %w",
			ErrCaptureUnavailable,
			err,
		)
	}
	defer interop.release()

	var item uintptr
	hr := interop.call(
		slotCaptureItemInteropCreateForWindow,
		uintptr(handle),
		uintptr(unsafe.Pointer(&iidGraphicsCaptureItem)),
		uintptr(unsafe.Pointer(&item)),
	)
	if isFailed(hr) || item == 0 {
		return wgcComObject{}, wgcSizeInt32{}, fmt.Errorf(
			"%w: CreateForWindow: %s",
			ErrCaptureUnavailable,
			wgcHResult(hr),
		)
	}

	var size wgcSizeInt32
	if hr := (wgcComObject{ptr: item}).call(slotCaptureItemGetSize, uintptr(unsafe.Pointer(&size))); isFailed(hr) {
		wgcComObject{ptr: item}.release()
		return wgcComObject{}, wgcSizeInt32{}, fmt.Errorf(
			"%w: read the capture size: %s",
			ErrCaptureUnavailable,
			wgcHResult(hr),
		)
	}
	return wgcComObject{ptr: item}, size, nil
}

// wgcFramePool creates a free-threaded single-buffer pool: no DispatcherQueue
// exists in this process, and one still needs exactly one buffer.
func wgcFramePool(device wgcComObject, size wgcSizeInt32) (wgcComObject, error) {
	statics, err := wgcActivationFactory(wgcClassFramePool, &iidFramePoolStatics2)
	if err != nil {
		return wgcComObject{}, fmt.Errorf("%w: resolve the frame pool factory: %w", ErrCaptureUnavailable, err)
	}
	defer statics.release()

	sizeArg := uint64(uint32(size.width)) | uint64(uint32(size.height))<<32
	var pool uintptr
	hr := statics.call(
		slotFramePoolStaticsCreateFree,
		device.ptr,
		directXPixelFormatBGRA,
		wgcFrameBuffers,
		uintptr(sizeArg),
		uintptr(unsafe.Pointer(&pool)),
	)
	if isFailed(hr) || pool == 0 {
		return wgcComObject{}, fmt.Errorf("%w: CreateFreeThreaded: %s", ErrCaptureUnavailable, wgcHResult(hr))
	}
	return wgcComObject{ptr: pool}, nil
}

// wgcCaptureSession binds the pool to the window and starts the capture. The
// cursor and border settings are probed per interface, so older builds that
// lack them keep working; without borderless consent the border setting is
// accepted but ignored by the system.
func wgcCaptureSession(pool, item wgcComObject) (wgcComObject, error) {
	var session uintptr
	hr := pool.call(slotFramePoolCreateCaptureSession, item.ptr, uintptr(unsafe.Pointer(&session)))
	if isFailed(hr) || session == 0 {
		return wgcComObject{}, fmt.Errorf("%w: CreateCaptureSession: %s", ErrCaptureUnavailable, wgcHResult(hr))
	}
	capture := wgcComObject{ptr: session}

	if session2, err := capture.queryInterface(&iidGraphicsCaptureSession2); err == nil {
		session2.call(slotSessionPutCursorCaptureEnabled, 0)
		session2.release()
	}
	if session3, err := capture.queryInterface(&iidGraphicsCaptureSession3); err == nil {
		session3.call(slotSessionPutBorderRequired, 0)
		session3.release()
	}

	if hr := capture.call(slotSessionStartCapture); isFailed(hr) {
		capture.close()
		return wgcComObject{}, fmt.Errorf("%w: StartCapture: %s", ErrCaptureUnavailable, wgcHResult(hr))
	}
	return capture, nil
}

// wgcPollFrame pulls the next compositor frame without subscribing to
// FrameArrived. TryGetNextFrame returns success with a null frame while the
// pool is empty, and a minimized or cloaked window never delivers one, so the
// poll budget expiring yields a null frame rather than an error.
func wgcPollFrame(pool wgcComObject, timeout time.Duration) (wgcComObject, error) {
	deadline := time.Now().Add(timeout)
	for {
		var frame uintptr
		if hr := pool.call(slotFramePoolTryGetNextFrame, uintptr(unsafe.Pointer(&frame))); isFailed(hr) {
			return wgcComObject{}, fmt.Errorf("%w: TryGetNextFrame: %s", ErrCaptureUnavailable, wgcHResult(hr))
		} else if frame != 0 {
			return wgcComObject{ptr: frame}, nil
		}

		if !time.Now().Before(deadline) {
			return wgcComObject{}, nil
		}
		time.Sleep(wgcPollInterval)
	}
}

// wgcReadFrame copies the frame surface into a CPU-readable staging texture
// and swizzles its BGRA pixels into an opaque RGBA image. The mapped rows may
// be padded, so every row is copied by its own pitch.
func wgcReadFrame(device, context, frame wgcComObject, width, height int) (wgcCaptureResult, error) {
	var surface uintptr
	if hr := frame.call(slotFrameGetSurface, uintptr(unsafe.Pointer(&surface))); isFailed(hr) || surface == 0 {
		return wgcCaptureResult{}, fmt.Errorf("%w: read the frame surface: %s", ErrCaptureUnavailable, wgcHResult(hr))
	}
	defer wgcComObject{ptr: surface}.release()

	access, err := wgcComObject{ptr: surface}.queryInterface(&iidDirect3DDxgiInterfaceAccess)
	if err != nil {
		return wgcCaptureResult{}, fmt.Errorf("%w: query the DXGI surface access: %w", ErrCaptureUnavailable, err)
	}
	defer access.release()

	var texture uintptr
	hr := access.call(
		slotDxgiInterfaceAccessGetInterface,
		uintptr(unsafe.Pointer(&iidD3D11Texture2D)),
		uintptr(unsafe.Pointer(&texture)),
	)
	if isFailed(hr) || texture == 0 {
		return wgcCaptureResult{}, fmt.Errorf("%w: read the frame texture: %s", ErrCaptureUnavailable, wgcHResult(hr))
	}
	defer wgcComObject{ptr: texture}.release()

	desc := wgcTexture2DDesc{
		width: uint32(width), height: uint32(height),
		mipLevels: 1, arraySize: 1, format: dxgiFormatB8G8R8A8Unorm,
		sampleCount: 1, sampleQuality: 0,
		usage: d3d11UsageStaging, bindFlags: 0, cpuAccessFlags: d3d11CPUAccessRead, miscFlags: 0,
	}
	var staging uintptr
	hr = device.call(
		slotDeviceCreateTexture2D,
		uintptr(unsafe.Pointer(&desc)),
		0,
		uintptr(unsafe.Pointer(&staging)),
	)
	if isFailed(hr) || staging == 0 {
		return wgcCaptureResult{}, fmt.Errorf(
			"%w: create the staging texture: %s",
			ErrCaptureUnavailable,
			wgcHResult(hr),
		)
	}
	defer wgcComObject{ptr: staging}.release()

	if hr := context.call(slotContextCopyResource, staging, texture); isFailed(hr) {
		return wgcCaptureResult{}, fmt.Errorf("%w: copy the frame texture: %s", ErrCaptureUnavailable, wgcHResult(hr))
	}

	var mapped wgcMappedSubresource
	hr = context.call(slotContextMap, staging, 0, d3d11MapRead, 0, uintptr(unsafe.Pointer(&mapped)))
	if isFailed(hr) || mapped.data == nil {
		return wgcCaptureResult{}, fmt.Errorf("%w: map the staging texture: %s", ErrCaptureUnavailable, wgcHResult(hr))
	}

	captured := image.NewRGBA(image.Rect(0, 0, width, height))
	rows := unsafe.Slice((*byte)(mapped.data), mapped.rowPitch*uint32(height))
	for y := range height {
		source := rows[y*int(mapped.rowPitch) : y*int(mapped.rowPitch)+width*captureBytesPerPixel]
		target := captured.Pix[y*captured.Stride : y*captured.Stride+width*captureBytesPerPixel]
		for x := range width {
			pixel := x * captureBytesPerPixel
			target[pixel] = source[pixel+2]
			target[pixel+1] = source[pixel+1]
			target[pixel+2] = source[pixel]
			target[pixel+3] = 0xFF
		}
	}
	context.call(slotContextUnmap, staging, 0)
	return wgcCaptureResult{frame: captured, width: width, height: height}, nil
}

func mustParseGUID(value string) windows.GUID {
	guid, err := windows.GUIDFromString("{" + value + "}")
	if err != nil {
		panic(fmt.Sprintf("parse GUID %s: %v", value, err))
	}
	return guid
}

// wgcComObject is one reference-counted COM or WinRT interface pointer. Calls
// go through the vtable by slot, mirroring the SDK header method order.
type wgcComObject struct {
	ptr uintptr
}

func (o wgcComObject) method(slot int) uintptr {
	table := (*[64]uintptr)(unsafe.Pointer(*(*uintptr)(unsafe.Pointer(o.ptr))))
	return table[slot]
}

func (o wgcComObject) call(slot int, args ...uintptr) uintptr {
	method := o.method(slot)
	full := append([]uintptr{o.ptr}, args...)
	var ret uintptr
	switch len(full) {
	case 1:
		ret, _, _ = syscall.SyscallN(method, full[0])
	case 2:
		ret, _, _ = syscall.SyscallN(method, full[0], full[1])
	case 3:
		ret, _, _ = syscall.SyscallN(method, full[0], full[1], full[2])
	case 4:
		ret, _, _ = syscall.SyscallN(method, full[0], full[1], full[2], full[3])
	case 5:
		ret, _, _ = syscall.SyscallN(method, full[0], full[1], full[2], full[3], full[4])
	case 6:
		ret, _, _ = syscall.SyscallN(method, full[0], full[1], full[2], full[3], full[4], full[5])
	default:
		panic(fmt.Sprintf("wgc: %d arguments exceed the call helper", len(full)))
	}
	return ret
}

func (o wgcComObject) queryInterface(id *windows.GUID) (wgcComObject, error) {
	var out uintptr
	if hr := o.call(
		slotQueryInterface,
		uintptr(unsafe.Pointer(id)),
		uintptr(unsafe.Pointer(&out)),
	); isFailed(hr) ||
		out == 0 {
		return wgcComObject{}, fmt.Errorf("QueryInterface: %s", wgcHResult(hr))
	}
	return wgcComObject{ptr: out}, nil
}

func (o wgcComObject) release() {
	o.call(slotRelease)
}

// close releases a WinRT Closable after calling Close, which returns pooled
// buffers such as capture frames.
func (o wgcComObject) close() {
	if closable, err := o.queryInterface(&iidClosable); err == nil {
		closable.call(slotClosableClose)
		closable.release()
	}
	o.release()
}

func isFailed(hr uintptr) bool {
	return int32(hr) < 0
}

func wgcHResult(hr uintptr) string {
	return fmt.Sprintf("0x%08X", uint32(hr))
}

// wgcActivationFactory resolves a WinRT runtime class to the requested factory
// interface. The HSTRING length counts UTF-16 code units without the
// terminator.
func wgcActivationFactory(class string, id *windows.GUID) (wgcComObject, error) {
	encoded, err := windows.UTF16FromString(class)
	if err != nil {
		return wgcComObject{}, err
	}
	var handle uintptr
	hr, _, _ := procWindowsCreateString.Call(
		uintptr(unsafe.Pointer(&encoded[0])),
		uintptr(len(encoded)-1),
		uintptr(unsafe.Pointer(&handle)),
	)
	if isFailed(hr) {
		return wgcComObject{}, fmt.Errorf("WindowsCreateString: %s", wgcHResult(hr))
	}
	defer func() { _, _, _ = procWindowsDeleteString.Call(handle) }()

	var factory uintptr
	hr, _, _ = procRoGetActivationFactory.Call(
		handle,
		uintptr(unsafe.Pointer(id)),
		uintptr(unsafe.Pointer(&factory)),
	)
	if isFailed(hr) || factory == 0 {
		return wgcComObject{}, fmt.Errorf("RoGetActivationFactory(%s): %s", class, wgcHResult(hr))
	}
	return wgcComObject{ptr: factory}, nil
}
