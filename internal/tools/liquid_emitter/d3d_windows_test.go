//go:build windows

package liquidemitter

import (
	"encoding/binary"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The runtime's HLSL only compiles inside the game, through the d3d11.dll the
// importer installs. These helpers compile it with the compiler every Windows
// install carries in System32, and run small fixtures of it on WARP, the
// software rasterizer, so no GPU, driver, SDK or game is involved.
var (
	d3dCompilerDLL         = windows.NewLazySystemDLL("d3dcompiler_47.dll")
	procD3DCompileFromFile = d3dCompilerDLL.NewProc("D3DCompileFromFile")
	d3d11DLL               = windows.NewLazySystemDLL("d3d11.dll")
	procD3D11CreateDevice  = d3d11DLL.NewProc("D3D11CreateDevice")
)

const (
	// 3DMigoto compiles custom shaders at this level, which also decides
	// which loops must unroll.
	d3dCompileOptimizationLevel3 = 1 << 15
	// D3D_COMPILE_STANDARD_FILE_INCLUDE resolves includes against the directory
	// of the file being compiled, as the game does for the runtime folder.
	d3dStandardFileInclude = 1

	formatR32G32B32A32Float = 2
	formatR16G16B16A16Float = 10
	formatR32G32Float       = 16
	formatR16G16Float       = 34
	formatR32Float          = 41
	formatR32Uint           = 42
	formatR16Float          = 54

	bindConstantBuffer   = 0x4
	bindShaderResource   = 0x8
	bindRenderTarget     = 0x20
	bindUnorderedAccess  = 0x80
	miscBufferStructured = 0x40
	usageStaging         = 3
	cpuAccessRead        = 0x20000
	mapRead              = 1
	viewDimensionBuffer  = 1
	topologyTriangleList = 4
)

type d3dMacro struct{ name, definition *byte }

// shaderStage names the 3DMigoto stage of an INI key with the macro the game
// defines for it.
var shaderStages = map[string]struct{ target, macro string }{
	"vs": {"vs_5_0", "VERTEX_SHADER"},
	"ps": {"ps_5_0", "PIXEL_SHADER"},
	"cs": {"cs_5_0", "COMPUTE_SHADER"},
	"gs": {"gs_5_0", "GEOMETRY_SHADER"},
	"hs": {"hs_5_0", "HULL_SHADER"},
	"ds": {"ds_5_0", "DOMAIN_SHADER"},
}

// compileShader compiles the file at path for stage with the extra macros and
// returns its bytecode, or the compiler's messages as the error.
func compileShader(path, stage string, defines ...string) ([]byte, error) {
	if err := procD3DCompileFromFile.Find(); err != nil {
		return nil, fmt.Errorf("system shader compiler unavailable: %w", err)
	}
	kind, ok := shaderStages[stage]
	if !ok {
		return nil, fmt.Errorf("unknown shader stage %q", stage)
	}
	file, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	entry, _ := windows.BytePtrFromString("main")
	target, _ := windows.BytePtrFromString(kind.target)
	macros := make([]d3dMacro, 0, len(defines)+2)
	for _, define := range append([]string{kind.macro}, defines...) {
		name, value, _ := cutDefine(define)
		namePtr, _ := windows.BytePtrFromString(name)
		valuePtr, _ := windows.BytePtrFromString(value)
		macros = append(macros, d3dMacro{namePtr, valuePtr})
	}
	macros = append(macros, d3dMacro{})

	var code, messages unsafe.Pointer
	hr, _, _ := syscall.SyscallN(
		procD3DCompileFromFile.Addr(),
		uintptr(unsafe.Pointer(file)),
		uintptr(unsafe.Pointer(&macros[0])),
		d3dStandardFileInclude,
		uintptr(unsafe.Pointer(entry)),
		uintptr(unsafe.Pointer(target)),
		d3dCompileOptimizationLevel3,
		0,
		uintptr(unsafe.Pointer(&code)),
		uintptr(unsafe.Pointer(&messages)),
	)
	defer comRelease(messages)
	defer comRelease(code)
	if int32(hr) < 0 {
		return nil, fmt.Errorf("compile %s (%s): hr 0x%08x\n%s", path, stage, uint32(hr), blobBytes(messages))
	}
	return blobBytes(code), nil
}

func cutDefine(define string) (string, string, bool) {
	for index := range len(define) {
		if define[index] == '=' {
			return define[:index], define[index+1:], true
		}
	}
	return define, "1", false
}

// comMethod returns entry index of the vtable of the COM object at object.
func comMethod(object unsafe.Pointer, index int) uintptr {
	vtable := *(*unsafe.Pointer)(object)
	return *(*uintptr)(unsafe.Add(vtable, index*int(unsafe.Sizeof(uintptr(0)))))
}

func comRelease(object unsafe.Pointer) {
	if object != nil {
		_, _, _ = syscall.SyscallN(comMethod(object, 2), uintptr(object))
	}
}

func blobBytes(blob unsafe.Pointer) []byte {
	if blob == nil {
		return nil
	}
	pointer, _, _ := syscall.SyscallN(comMethod(blob, 3), uintptr(blob))
	size, _, _ := syscall.SyscallN(comMethod(blob, 4), uintptr(blob))
	if size == 0 {
		return nil
	}
	data := make([]byte, size)
	copy(data, unsafe.Slice((*byte)(unsafe.Pointer(pointer)), size)) //nolint:govet // a blob's buffer is not Go memory
	return data
}

func hresult(operation string, hr uintptr) error {
	if int32(hr) < 0 {
		return fmt.Errorf("%s: hr 0x%08x", operation, uint32(hr))
	}
	return nil
}

// warpDevice is a D3D11 device on WARP with its immediate context. Every
// object it creates is released with it.
type warpDevice struct {
	t       *testing.T
	device  unsafe.Pointer
	context unsafe.Pointer
	objects []unsafe.Pointer
}

func newWARPDevice(t *testing.T) *warpDevice {
	t.Helper()
	if err := procD3D11CreateDevice.Find(); err != nil {
		t.Fatalf("D3D11 unavailable: %v", err)
	}
	const driverWARP, featureLevel11 = 5, 0xb000
	levels := []uint32{featureLevel11}
	var device, context unsafe.Pointer
	var level uint32
	hr, _, _ := syscall.SyscallN(
		procD3D11CreateDevice.Addr(),
		0, driverWARP, 0, 0,
		uintptr(unsafe.Pointer(&levels[0])), 1, 7,
		uintptr(unsafe.Pointer(&device)),
		uintptr(unsafe.Pointer(&level)),
		uintptr(unsafe.Pointer(&context)),
	)
	if err := hresult("create WARP device", hr); err != nil {
		t.Fatal(err)
	}
	d := &warpDevice{t: t, device: device, context: context}
	t.Cleanup(func() {
		for index := len(d.objects) - 1; index >= 0; index-- {
			comRelease(d.objects[index])
		}
		comRelease(d.context)
		comRelease(d.device)
	})
	return d
}

func (d *warpDevice) keep(object unsafe.Pointer, operation string, hr uintptr) unsafe.Pointer {
	d.t.Helper()
	if err := hresult(operation, hr); err != nil {
		d.t.Fatal(err)
	}
	d.objects = append(d.objects, object)
	return object
}

type bufferDesc struct {
	byteWidth, usage, bindFlags, cpuAccess, misc, stride uint32
}

type subresourceData struct {
	data       unsafe.Pointer
	pitch      uint32
	slicePitch uint32
}

type bufferViewDesc struct {
	format, dimension uint32
	first, count      uint32
	flags             uint32
	_                 uint32
}

// gpuBuffer is a buffer and its views: a structured one when stride is
// set, otherwise a typed one of format.
type gpuBuffer struct {
	resource, srv, uav unsafe.Pointer
	size               int
}

func (d *warpDevice) buffer(data []byte, stride int, format uint32) gpuBuffer {
	d.t.Helper()
	desc := bufferDesc{byteWidth: uint32(len(data)), bindFlags: bindShaderResource | bindUnorderedAccess}
	elements := uint32(len(data) / 4)
	if stride > 0 {
		desc.misc, desc.stride = miscBufferStructured, uint32(stride)
		elements = uint32(len(data) / stride)
	}
	initial := subresourceData{data: unsafe.Pointer(&data[0])}
	var resource unsafe.Pointer
	hr, _, _ := syscall.SyscallN(comMethod(d.device, 3), uintptr(d.device),
		uintptr(unsafe.Pointer(&desc)), uintptr(unsafe.Pointer(&initial)), uintptr(unsafe.Pointer(&resource)))
	d.keep(resource, "create buffer", hr)

	view := bufferViewDesc{format: format, dimension: viewDimensionBuffer, count: elements}
	var srv, uav unsafe.Pointer
	hr, _, _ = syscall.SyscallN(comMethod(d.device, 7), uintptr(d.device),
		uintptr(resource), uintptr(unsafe.Pointer(&view)), uintptr(unsafe.Pointer(&srv)))
	d.keep(srv, "create buffer SRV", hr)
	uavDesc := view
	hr, _, _ = syscall.SyscallN(comMethod(d.device, 8), uintptr(d.device),
		uintptr(resource), uintptr(unsafe.Pointer(&uavDesc)), uintptr(unsafe.Pointer(&uav)))
	d.keep(uav, "create buffer UAV", hr)
	return gpuBuffer{resource: resource, srv: srv, uav: uav, size: len(data)}
}

func (d *warpDevice) constants(values []float32) unsafe.Pointer {
	d.t.Helper()
	data := float32Bytes(values)
	desc := bufferDesc{byteWidth: uint32(len(data)), bindFlags: bindConstantBuffer}
	initial := subresourceData{data: unsafe.Pointer(&data[0])}
	var resource unsafe.Pointer
	hr, _, _ := syscall.SyscallN(comMethod(d.device, 3), uintptr(d.device),
		uintptr(unsafe.Pointer(&desc)), uintptr(unsafe.Pointer(&initial)), uintptr(unsafe.Pointer(&resource)))
	return d.keep(resource, "create constant buffer", hr)
}

func (d *warpDevice) structured(values []float32, stride int) gpuBuffer {
	d.t.Helper()
	return d.buffer(float32Bytes(values), stride, 0)
}

func (d *warpDevice) uints(values []uint32) gpuBuffer {
	d.t.Helper()
	data := make([]byte, 4*len(values))
	for index, value := range values {
		binary.LittleEndian.PutUint32(data[4*index:], value)
	}
	return d.buffer(data, 0, formatR32Uint)
}

type texture1DDesc struct {
	width, mips, array, format, usage, bind, cpu, misc uint32
}

// iniParams is the Texture1D 3DMigoto binds at t120: 256 float4 rows.
func (d *warpDevice) iniParams(rows map[int][4]float32) unsafe.Pointer {
	d.t.Helper()
	values := make([]float32, 256*4)
	for row, value := range rows {
		copy(values[4*row:], value[:])
	}
	desc := texture1DDesc{width: 256, mips: 1, array: 1, format: formatR32G32B32A32Float, bind: bindShaderResource}
	initial := subresourceData{data: unsafe.Pointer(&values[0]), pitch: uint32(4 * len(values))}
	var texture, srv unsafe.Pointer
	hr, _, _ := syscall.SyscallN(comMethod(d.device, 4), uintptr(d.device),
		uintptr(unsafe.Pointer(&desc)), uintptr(unsafe.Pointer(&initial)), uintptr(unsafe.Pointer(&texture)))
	d.keep(texture, "create IniParams", hr)
	hr, _, _ = syscall.SyscallN(comMethod(d.device, 7), uintptr(d.device),
		uintptr(texture), 0, uintptr(unsafe.Pointer(&srv)))
	return d.keep(srv, "create IniParams SRV", hr)
}

type texture2DDesc struct {
	width, height, mips, array, format, samples, quality, usage, bind, cpu, misc uint32
}

type gpuTexture struct {
	resource, srv, rtv unsafe.Pointer
	width, height      int
	format             uint32
}

// texture2D creates a texture of 4-byte or 8-byte texels, filled with data
// when given.
func (d *warpDevice) texture2D(width, height int, format uint32, texel int, data []byte, target bool) gpuTexture {
	d.t.Helper()
	desc := texture2DDesc{
		width: uint32(width), height: uint32(height), mips: 1, array: 1, format: format, samples: 1,
		bind: bindShaderResource,
	}
	if target {
		desc.bind |= bindRenderTarget
	}
	var initial *subresourceData
	if data != nil {
		initial = &subresourceData{data: unsafe.Pointer(&data[0]), pitch: uint32(width * texel)}
	}
	var texture, srv, rtv unsafe.Pointer
	hr, _, _ := syscall.SyscallN(comMethod(d.device, 5), uintptr(d.device),
		uintptr(unsafe.Pointer(&desc)), uintptr(unsafe.Pointer(initial)), uintptr(unsafe.Pointer(&texture)))
	d.keep(texture, "create texture", hr)
	hr, _, _ = syscall.SyscallN(comMethod(d.device, 7), uintptr(d.device),
		uintptr(texture), 0, uintptr(unsafe.Pointer(&srv)))
	d.keep(srv, "create texture SRV", hr)
	if target {
		hr, _, _ = syscall.SyscallN(comMethod(d.device, 9), uintptr(d.device),
			uintptr(texture), 0, uintptr(unsafe.Pointer(&rtv)))
		d.keep(rtv, "create RTV", hr)
	}
	return gpuTexture{resource: texture, srv: srv, rtv: rtv, width: width, height: height, format: format}
}

func (d *warpDevice) floatTexture(width, height int, values []float32) gpuTexture {
	d.t.Helper()
	return d.texture2D(width, height, formatR32Float, 4, float32Bytes(values), false)
}

func (d *warpDevice) colorTexture(width, height int, values []float32) gpuTexture {
	d.t.Helper()
	return d.texture2D(width, height, formatR32G32B32A32Float, 16, float32Bytes(values), false)
}

func (d *warpDevice) shader(stage string, code []byte) unsafe.Pointer {
	d.t.Helper()
	index := map[string]int{"vs": 12, "ps": 15, "cs": 18}[stage]
	var object unsafe.Pointer
	hr, _, _ := syscall.SyscallN(comMethod(d.device, index), uintptr(d.device),
		uintptr(unsafe.Pointer(&code[0])), uintptr(len(code)), 0, uintptr(unsafe.Pointer(&object)))
	return d.keep(object, "create "+stage+" shader", hr)
}

// bindings maps registers to views for one stage.
type bindings struct {
	srv map[int]unsafe.Pointer
	uav map[int]unsafe.Pointer
	// cb holds constant buffers, which only dispatch binds.
	cb map[int]unsafe.Pointer
}

func (d *warpDevice) setResources(method int, views map[int]unsafe.Pointer) {
	for slot, view := range views {
		list := []unsafe.Pointer{view}
		_, _, _ = syscall.SyscallN(comMethod(d.context, method), uintptr(d.context),
			uintptr(slot), 1, uintptr(unsafe.Pointer(&list[0])))
	}
}

func (d *warpDevice) unbind(method int, views map[int]unsafe.Pointer, uav bool) {
	for slot := range views {
		list := []unsafe.Pointer{nil}
		if uav {
			_, _, _ = syscall.SyscallN(comMethod(d.context, method), uintptr(d.context),
				uintptr(slot), 1, uintptr(unsafe.Pointer(&list[0])), 0)
			continue
		}
		_, _, _ = syscall.SyscallN(comMethod(d.context, method), uintptr(d.context),
			uintptr(slot), 1, uintptr(unsafe.Pointer(&list[0])))
	}
}

// dispatch runs a compute shader with the views bound, then unbinds them.
func (d *warpDevice) dispatch(shader unsafe.Pointer, views bindings, groups [3]int) {
	d.t.Helper()
	const csSetShaderResources, csSetUAVs, csSetShader, csSetConstantBuffers, dispatch = 67, 68, 69, 71, 41
	d.setResources(csSetShaderResources, views.srv)
	d.setResources(csSetConstantBuffers, views.cb)
	for slot, view := range views.uav {
		list := []unsafe.Pointer{view}
		_, _, _ = syscall.SyscallN(comMethod(d.context, csSetUAVs), uintptr(d.context),
			uintptr(slot), 1, uintptr(unsafe.Pointer(&list[0])), 0)
	}
	_, _, _ = syscall.SyscallN(comMethod(d.context, csSetShader), uintptr(d.context), uintptr(shader), 0, 0)
	_, _, _ = syscall.SyscallN(comMethod(d.context, dispatch), uintptr(d.context),
		uintptr(groups[0]), uintptr(groups[1]), uintptr(groups[2]))
	d.unbind(csSetShaderResources, views.srv, false)
	d.unbind(csSetConstantBuffers, views.cb, false)
	d.unbind(csSetUAVs, views.uav, true)
}

type viewport struct{ x, y, width, height, minDepth, maxDepth float32 }

type rasterizerDesc struct {
	fill, cull, frontCCW, depthBias            uint32
	depthBiasClamp, slopeDepthBias             float32
	depthClip, scissor, multisample, antialias uint32
}

// draw runs a vertex and pixel shader over vertices with the pixel shader's
// views bound and the targets as MRTs, without blending or depth.
func (d *warpDevice) draw(vs, ps unsafe.Pointer, vsViews, psViews bindings, targets []gpuTexture, vertices int) {
	d.t.Helper()
	const (
		psSetShaderResources = 8
		psSetShader          = 9
		vsSetShader          = 11
		drawMethod           = 13
		setTopology          = 24
		vsSetShaderResources = 25
		omSetRenderTargets   = 33
		rsSetState           = 43
		rsSetViewports       = 44
	)
	// Every 3DMigoto pass of the runtime draws with cull = none.
	const fillSolid, cullNone = 3, 1
	raster := rasterizerDesc{fill: fillSolid, cull: cullNone, depthClip: 1}
	var state unsafe.Pointer
	hr, _, _ := syscall.SyscallN(comMethod(d.device, 22), uintptr(d.device),
		uintptr(unsafe.Pointer(&raster)), uintptr(unsafe.Pointer(&state)))
	d.keep(state, "create rasterizer state", hr)
	_, _, _ = syscall.SyscallN(comMethod(d.context, rsSetState), uintptr(d.context), uintptr(state))
	views := make([]unsafe.Pointer, len(targets))
	for index, target := range targets {
		views[index] = target.rtv
	}
	_, _, _ = syscall.SyscallN(comMethod(d.context, omSetRenderTargets), uintptr(d.context),
		uintptr(len(views)), uintptr(unsafe.Pointer(&views[0])), 0)
	area := []viewport{{width: float32(targets[0].width), height: float32(targets[0].height), maxDepth: 1}}
	_, _, _ = syscall.SyscallN(comMethod(d.context, rsSetViewports), uintptr(d.context),
		1, uintptr(unsafe.Pointer(&area[0])))
	_, _, _ = syscall.SyscallN(comMethod(d.context, setTopology), uintptr(d.context), topologyTriangleList)
	d.setResources(vsSetShaderResources, vsViews.srv)
	d.setResources(psSetShaderResources, psViews.srv)
	_, _, _ = syscall.SyscallN(comMethod(d.context, vsSetShader), uintptr(d.context), uintptr(vs), 0, 0)
	_, _, _ = syscall.SyscallN(comMethod(d.context, psSetShader), uintptr(d.context), uintptr(ps), 0, 0)
	_, _, _ = syscall.SyscallN(comMethod(d.context, drawMethod), uintptr(d.context), uintptr(vertices), 0)
	d.unbind(vsSetShaderResources, vsViews.srv, false)
	d.unbind(psSetShaderResources, psViews.srv, false)
	none := []unsafe.Pointer{nil}
	_, _, _ = syscall.SyscallN(comMethod(d.context, omSetRenderTargets), uintptr(d.context),
		1, uintptr(unsafe.Pointer(&none[0])), 0)
}

func (d *warpDevice) clearTarget(target gpuTexture, value [4]float32) {
	const clearRenderTargetView = 50
	color := []float32{value[0], value[1], value[2], value[3]}
	_, _, _ = syscall.SyscallN(comMethod(d.context, clearRenderTargetView), uintptr(d.context),
		uintptr(target.rtv), uintptr(unsafe.Pointer(&color[0])))
}

type mappedSubresource struct {
	data       unsafe.Pointer
	rowPitch   uint32
	depthPitch uint32
}

// readback copies resource through a staging copy made from desc, rows of
// rowBytes each.
func (d *warpDevice) readback(resource unsafe.Pointer, staging unsafe.Pointer, rows, rowBytes int) []byte {
	d.t.Helper()
	const copyResource, mapMethod, unmapMethod = 47, 14, 15
	_, _, _ = syscall.SyscallN(comMethod(d.context, copyResource), uintptr(d.context), uintptr(staging),
		uintptr(resource))
	var mapped mappedSubresource
	hr, _, _ := syscall.SyscallN(comMethod(d.context, mapMethod), uintptr(d.context), uintptr(staging), 0, mapRead,
		0, uintptr(unsafe.Pointer(&mapped)))
	if err := hresult("map staging copy", hr); err != nil {
		d.t.Fatal(err)
	}
	out := make([]byte, rows*rowBytes)
	pitch := int(mapped.rowPitch)
	if rows == 1 {
		pitch = rowBytes
	}
	for row := range rows {
		copy(out[row*rowBytes:(row+1)*rowBytes], unsafe.Slice((*byte)(unsafe.Add(mapped.data, row*pitch)), rowBytes))
	}
	_, _, _ = syscall.SyscallN(comMethod(d.context, unmapMethod), uintptr(d.context), uintptr(staging), 0)
	return out
}

func (d *warpDevice) readBuffer(buffer gpuBuffer) []byte {
	d.t.Helper()
	desc := bufferDesc{byteWidth: uint32(buffer.size), usage: usageStaging, cpuAccess: cpuAccessRead}
	var staging unsafe.Pointer
	hr, _, _ := syscall.SyscallN(comMethod(d.device, 3), uintptr(d.device),
		uintptr(unsafe.Pointer(&desc)), 0, uintptr(unsafe.Pointer(&staging)))
	d.keep(staging, "create staging buffer", hr)
	return d.readback(buffer.resource, staging, 1, buffer.size)
}

func (d *warpDevice) readFloats(buffer gpuBuffer) []float32 {
	d.t.Helper()
	return bytesFloat32(d.readBuffer(buffer))
}

func (d *warpDevice) readUints(buffer gpuBuffer) []uint32 {
	d.t.Helper()
	data := d.readBuffer(buffer)
	out := make([]uint32, len(data)/4)
	for index := range out {
		out[index] = binary.LittleEndian.Uint32(data[4*index:])
	}
	return out
}

// readTexture returns a texture's texels; texel is their byte size.
func (d *warpDevice) readTexture(texture gpuTexture, texel int) []byte {
	d.t.Helper()
	desc := texture2DDesc{
		width: uint32(texture.width), height: uint32(texture.height), mips: 1, array: 1, format: texture.format,
		samples: 1, usage: usageStaging, cpu: cpuAccessRead,
	}
	var staging unsafe.Pointer
	hr, _, _ := syscall.SyscallN(comMethod(d.device, 5), uintptr(d.device),
		uintptr(unsafe.Pointer(&desc)), 0, uintptr(unsafe.Pointer(&staging)))
	d.keep(staging, "create staging texture", hr)
	return d.readback(texture.resource, staging, texture.height, texture.width*texel)
}

func float32Bytes(values []float32) []byte {
	data := make([]byte, 4*max(len(values), 1))
	for index, value := range values {
		binary.LittleEndian.PutUint32(data[4*index:], math.Float32bits(value))
	}
	return data
}

func bytesFloat32(data []byte) []float32 {
	out := make([]float32, len(data)/4)
	for index := range out {
		out[index] = math.Float32frombits(binary.LittleEndian.Uint32(data[4*index:]))
	}
	return out
}

// shaderBundle is the runtime of one game written out as a game installs it,
// with harness shaders beside it so their includes resolve the same way.
type shaderBundle struct {
	t    *testing.T
	root string
	// compiled caches bytecode by file, stage and macros for the test run.
	compiled map[string][]byte
}

func newShaderBundle(t *testing.T, dir string) *shaderBundle {
	t.Helper()
	files, err := runtimeFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	err = fs.WalkDir(files, ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, err := fs.ReadFile(files, name)
		if err != nil {
			return err
		}
		target := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o600)
	})
	if err != nil {
		t.Fatal(err)
	}
	return &shaderBundle{t: t, root: root, compiled: map[string][]byte{}}
}

// harness writes source next to the runtime's fluid shaders under name.
func (b *shaderBundle) harness(name, source string) string {
	b.t.Helper()
	path := filepath.Join(b.root, "fluid", name)
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		b.t.Fatal(err)
	}
	return path
}

func (b *shaderBundle) compile(path, stage string, defines ...string) []byte {
	b.t.Helper()
	key := fmt.Sprint(path, stage, defines)
	if code, ok := b.compiled[key]; ok {
		return code
	}
	code, err := compileShader(path, stage, defines...)
	if err != nil {
		b.t.Fatal(err)
	}
	b.compiled[key] = code
	return code
}
