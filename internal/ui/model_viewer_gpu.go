package ui

import (
	_ "embed"
	"fmt"
	"math"
	"slices"
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/d3d"

	modelviewer "nahida.live/desktop/internal/tools/model_viewer"
)

//go:embed shaders/model_viewer.hlsl
var modelViewerHLSL []byte

type viewerShaders struct {
	vs, ps, outlineVS, outlinePS, shadowVS, ringVS, ringPS, overlayVS, overlayPS []byte
}

// warmViewerRenderer compiles the viewer's shaders and computes its environment lighting while a model loads. The
// first draw otherwise does both on the UI thread, holding the device lock that every window draws under.
func warmViewerRenderer() {
	_, _ = viewerShaderCode()
	_, _ = viewerBlitCode()
	viewerEnvironmentSH()
	viewerPMREMBands()
	_, _ = bundledToonMaps()
}

var viewerShaderCode = sync.OnceValues(func() (viewerShaders, error) {
	var code viewerShaders
	for _, entry := range []struct {
		code          *[]byte
		name, profile string
	}{
		{&code.vs, "vs_main", "vs_5_0"},
		{&code.ps, "ps_main", "ps_5_0"},
		{&code.outlineVS, "vs_outline", "vs_5_0"},
		{&code.outlinePS, "ps_outline", "ps_5_0"},
		{&code.shadowVS, "vs_shadow", "vs_5_0"},
		{&code.ringVS, "vs_ring", "vs_5_0"},
		{&code.ringPS, "ps_ring", "ps_5_0"},
		{&code.overlayVS, "vs_overlay", "vs_5_0"},
		{&code.overlayPS, "ps_overlay", "ps_5_0"},
	} {
		compiled, err := d3d.Compile(modelViewerHLSL, "model_viewer.hlsl", entry.name, entry.profile)
		if err != nil {
			return viewerShaders{}, fmt.Errorf("compile model viewer shader %s: %w", entry.name, err)
		}
		*entry.code = compiled
	}
	return code, nil
})

// viewerGPUSet is a scene's buffers and textures on one device generation.
type viewerGPUSet struct {
	generation uint64
	meshes     []viewerMeshGPU
	textures   map[string]viewerTextureGPU
}

type viewerMeshGPU struct {
	// vertices interleaves position and normal and follows the mesh's current geometry; uvs never change, and
	// extra changes only for the body shape editor's colours.
	vertices d3d.Buffer
	// next holds the mesh's geometry in the animation's next frame, laid out as vertices. It is made when the
	// mesh first has one.
	next       d3d.Buffer
	uvs        d3d.Buffer
	extra      d3d.Buffer
	indices    d3d.Buffer
	indexCount uint32
}

type viewerTextureGPU struct {
	view       d3d.ShaderResourceView
	flags      uint32
	normalMode uint32
	// format is the transport's lower-case format name, such as "bc1-unorm".
	format string
	// outlineMask marks the flat outline width map EFMI binds in the light map slot.
	outlineMask bool
}

func (m *viewerMeshGPU) release() {
	m.vertices.Release()
	m.next.Release()
	m.uvs.Release()
	m.extra.Release()
	m.indices.Release()
	*m = viewerMeshGPU{}
}

func (s *viewerGPUSet) release() {
	if s == nil {
		return
	}
	for i := range s.meshes {
		s.meshes[i].release()
	}
	for _, texture := range s.textures {
		texture.view.Release()
	}
	s.meshes, s.textures = nil, nil
}

func newViewerMeshGPU(dev d3d.Device, upload viewerMeshUpload) (viewerMeshGPU, error) {
	var mesh viewerMeshGPU
	if len(upload.current) == 0 || len(upload.indices) == 0 {
		return mesh, nil
	}
	fail := func(err error) (viewerMeshGPU, error) {
		mesh.vertices.Release()
		mesh.uvs.Release()
		mesh.extra.Release()
		return viewerMeshGPU{}, err
	}
	var err error
	if mesh.vertices, err = d3d.CreateBufferData(
		dev, d3d.BIND_VERTEX_BUFFER, d3d.USAGE_DEFAULT, upload.current,
	); err != nil {
		return fail(err)
	}
	if mesh.uvs, err = d3d.CreateBufferData(dev, d3d.BIND_VERTEX_BUFFER, d3d.USAGE_IMMUTABLE, upload.uvs); err != nil {
		return fail(err)
	}
	if mesh.extra, err = d3d.CreateBufferData(
		dev, d3d.BIND_VERTEX_BUFFER, d3d.USAGE_DEFAULT, upload.extra,
	); err != nil {
		return fail(err)
	}
	if mesh.indices, err = d3d.CreateBufferData(
		dev, d3d.BIND_INDEX_BUFFER, d3d.USAGE_IMMUTABLE, upload.indices,
	); err != nil {
		return fail(err)
	}
	mesh.indexCount = uint32(len(upload.indices))
	return mesh, nil
}

type viewerFrameConstants struct {
	viewProj       [16]float32
	world          [16]float32
	eye            [4]float32
	keyLight       [4]float32
	fillLight      [4]float32
	skyLight       [4]float32
	groundLight    [4]float32
	shading        [4]float32
	surface        [4]float32
	envSH          [9][4]float32
	envLevels      [pmremLevels][4]float32
	view           [16]float32
	proj           [16]float32
	shadowViewProj [16]float32
	toonLight      [4]float32
	toonShadow     [4]float32
	toonParams     [3][4]float32
	toonState      [4]float32
	viewport       [4]float32
}

type viewerMeshConstants struct {
	tint         [4]float32
	faceForward  [4]float32
	flags        uint32
	normalMode   uint32
	characterUse uint32
	morphWeight  float32
	// characterScale.x scales the character's specular values.
	characterScale [4]float32
}

// Shader flags the renderer adds per mesh, matching FLAG_* in model_viewer.hlsl.
const (
	viewerFlagVertexTangent = 256
	viewerFlagFace          = 512
	viewerFlagVertexColor   = 1024
	viewerFlagEmission      = 2048
	viewerFlagLightMap      = 4096
	viewerFlagOverlayColor  = 8192
	viewerFlagHair          = 1 << 14
	viewerFlagEye           = 1 << 15
	viewerFlagUV1           = 1 << 16
	viewerFlagMaterialMap   = 1 << 17
	viewerFlagDataMap       = 1 << 18
	viewerFlagHighlightMap  = 1 << 19
	viewerFlagRampMap       = 1 << 20
	viewerFlagLightBC4      = 1 << 21
	viewerFlagLightSigned   = 1 << 22
	viewerFlagPackedMask    = 1 << 23
	viewerFlagOutlineMask   = 1 << 24
	viewerFlagSkinned       = 1 << 25
)

// viewerShadowMapSize is the React viewer's key light shadow map size.
const viewerShadowMapSize = 4096

// viewerShadowInterval is how long the shadow map keeps the pose it was drawn with while a playback draws between
// frames, which would otherwise draw it again on every frame.
const viewerShadowInterval = time.Second / 60

// viewerToonDraw is the toon shading a draw applies, or nil without Toon Shadows.
type viewerToonDraw struct {
	profile  string
	style    viewerToonStyle
	textures viewerToonTextures
	// materials are the character's stock materials, applied as characterUse allows.
	materials      *characterToonMaterials
	characterUse   uint32
	characterScale float32
}

// viewerDrawOptions are the view settings a draw takes from the screen.
type viewerDrawOptions struct {
	camera orbitCamera
	// world places the model: the scene's upright correction under the user's rotation.
	world       mat4
	background  [4]float32
	normalMaps  bool
	doubleSided bool
	toneMapping string
	environment string
	exposure    float64
	metalness   float64
	roughness   float64
	profile     string
	toon        *viewerToonDraw
	// minimum and maximum bound the model in viewer space, which the shadow map covers.
	minimum, maximum vec3
	// untextured replaces the linear tint of the meshes without a diffuse texture, or is nil.
	untextured *[3]float32
	// ring is the body shape editor's brush cursor, or nil.
	ring *viewerRing
	// overlay is a tool's markers over the model, or nil.
	overlay *viewerOverlayDraw
	// skin is the character motion that poses the meshes, or nil.
	skin *viewerMotion
	// morph is the share of their next animation frame's geometry the meshes that have one are drawn with.
	morph float32
	// interpolating is set while a playback draws between frames, so the meshes move on every draw.
	interpolating bool
}

// viewerRing is a circle drawn over the model without depth testing, in model space.
type viewerRing struct {
	center, normal vec3
	radius         float64
	// color is premultiplied and display-referred.
	color [4]float32
}

// viewerRingConstants matches the Ring cbuffer in model_viewer.hlsl.
type viewerRingConstants struct {
	center, u, v, color [4]float32
}

// viewerRingSegments matches RING_SEGMENTS in model_viewer.hlsl.
const viewerRingSegments = 64

// viewerRenderer holds the pipeline and the render targets of one viewer surface. Its resources belong to
// one device generation and are made again after the device is lost.
type viewerRenderer struct {
	generation uint64
	err        error

	vs        d3d.VertexShader
	ps        d3d.PixelShader
	outlineVS d3d.VertexShader
	outlinePS d3d.PixelShader
	shadowVS  d3d.VertexShader
	ringVS    d3d.VertexShader
	ringPS    d3d.PixelShader
	ringCB    d3d.Buffer
	overlayVS d3d.VertexShader
	overlayPS d3d.PixelShader
	// overlayLayout and overlayVB draw viewerOverlayVertex lists; overlayVB grows to the largest list.
	overlayLayout d3d.InputLayout
	overlayVB     d3d.Buffer
	layout        d3d.InputLayout
	frameCB       d3d.Buffer
	meshCB        d3d.Buffer
	// characterCB holds the characterMaterial of the meshes drawn since it was last written.
	characterCB d3d.Buffer
	sampler     d3d.SamplerState
	clamp       d3d.SamplerState
	dfg         d3d.ShaderResourceView
	raster      d3d.RasterizerState
	culled      d3d.RasterizerState
	// frontCulled draws back faces only, for the outline hull and the shadows of single-sided meshes.
	frontCulled d3d.RasterizerState
	compare     d3d.SamplerState
	// toonMaps holds the neutral light map, built-in ramp, bundled metal map, a white texture and the bundled
	// face shadow.
	toonMaps [5]d3d.ShaderResourceView
	// shadow is the key light's depth map, made when self shadows are first drawn.
	shadow    d3d.Texture2D
	shadowDSV d3d.DepthStencilView
	shadowSRV d3d.ShaderResourceView
	// shadowKey, shadowCasters and shadowBuffers are what the shadow map was last drawn with; a draw that matches
	// them and uploads no vertices keeps the map. A released buffer's address can be reused by a new one, so the
	// casters are compared by index as well as by buffer. The scratch slices hold the next draw's casters.
	shadowKey     viewerShadowKey
	shadowCasters []int
	shadowBuffers []d3d.Buffer
	casterScratch []int
	bufferScratch []d3d.Buffer
	shadowDrawn   bool
	// shadowAt is the surface time the shadow map was last drawn at.
	shadowAt  time.Duration
	depthOn   d3d.DepthStencilState
	depthRead d3d.DepthStencilState
	blend     d3d.BlendState

	width, height int
	samples       uint32
	color         d3d.Texture2D
	colorRTV      d3d.RenderTargetView
	depth         d3d.Texture2D
	dsv           d3d.DepthStencilView
}

func (r *viewerRenderer) release() {
	r.releaseTargets()
	for _, release := range []func(){
		r.vs.Release, r.ps.Release, r.outlineVS.Release, r.outlinePS.Release, r.shadowVS.Release, r.ringVS.Release,
		r.ringPS.Release, r.ringCB.Release, r.overlayVS.Release, r.overlayPS.Release, r.overlayLayout.Release,
		r.overlayVB.Release, r.layout.Release,
		r.frameCB.Release, r.meshCB.Release, r.characterCB.Release, r.sampler.Release, r.clamp.Release, r.dfg.Release, r.raster.Release,
		r.culled.Release, r.frontCulled.Release, r.compare.Release, r.depthOn.Release, r.depthRead.Release,
		r.blend.Release, r.shadowSRV.Release, r.shadowDSV.Release, r.shadow.Release,
	} {
		release()
	}
	for _, view := range r.toonMaps {
		view.Release()
	}
	*r = viewerRenderer{}
}

func (r *viewerRenderer) releaseTargets() {
	r.dsv.Release()
	r.depth.Release()
	r.colorRTV.Release()
	r.color.Release()
	r.color, r.colorRTV, r.depth, r.dsv = d3d.Texture2D{}, d3d.RenderTargetView{}, d3d.Texture2D{}, d3d.DepthStencilView{}
	r.width, r.height = 0, 0
}

func (r *viewerRenderer) init(g fairy.GPU) error {
	// A failed create can leave some objects made, so the error is checked first.
	if r.generation == g.Generation && r.err != nil {
		return r.err
	}
	if r.generation == g.Generation && r.vs.P != nil {
		return nil
	}
	r.release()
	r.generation = g.Generation
	r.err = r.create(g.Device)
	return r.err
}

func (r *viewerRenderer) create(dev d3d.Device) error {
	code, err := viewerShaderCode()
	if err != nil {
		return err
	}
	if r.vs, err = dev.CreateVertexShader(code.vs); err != nil {
		return err
	}
	if r.ps, err = dev.CreatePixelShader(code.ps); err != nil {
		return err
	}
	if r.outlineVS, err = dev.CreateVertexShader(code.outlineVS); err != nil {
		return err
	}
	if r.outlinePS, err = dev.CreatePixelShader(code.outlinePS); err != nil {
		return err
	}
	if r.shadowVS, err = dev.CreateVertexShader(code.shadowVS); err != nil {
		return err
	}
	if r.ringVS, err = dev.CreateVertexShader(code.ringVS); err != nil {
		return err
	}
	if r.ringPS, err = dev.CreatePixelShader(code.ringPS); err != nil {
		return err
	}
	if r.overlayVS, err = dev.CreateVertexShader(code.overlayVS); err != nil {
		return err
	}
	if r.overlayPS, err = dev.CreatePixelShader(code.overlayPS); err != nil {
		return err
	}
	if r.overlayLayout, err = dev.CreateInputLayout([]d3d.InputElement{
		{SemanticName: "POSITION", Format: d3d.FORMAT_R32G32B32_FLOAT},
		{SemanticName: "COLOR", Format: d3d.FORMAT_R32G32B32A32_FLOAT, AlignedByteOffset: 12},
	}, code.overlayVS); err != nil {
		return err
	}
	if r.layout, err = dev.CreateInputLayout([]d3d.InputElement{
		{SemanticName: "POSITION", Format: d3d.FORMAT_R32G32B32_FLOAT},
		{SemanticName: "NORMAL", Format: d3d.FORMAT_R32G32B32_FLOAT, AlignedByteOffset: 12},
		{SemanticName: "TEXCOORD", Format: d3d.FORMAT_R32G32_FLOAT, InputSlot: 1},
		{
			SemanticName:      "TEXCOORD",
			SemanticIndex:     2,
			Format:            d3d.FORMAT_R32G32_FLOAT,
			InputSlot:         1,
			AlignedByteOffset: 8,
		},
		{SemanticName: "TANGENT", Format: d3d.FORMAT_R32G32B32A32_FLOAT, InputSlot: 2},
		{
			SemanticName:      "TEXCOORD",
			SemanticIndex:     1,
			Format:            d3d.FORMAT_R32G32B32_FLOAT,
			InputSlot:         2,
			AlignedByteOffset: 16,
		},
		{SemanticName: "COLOR", Format: d3d.FORMAT_R8G8B8A8_UNORM, InputSlot: 2, AlignedByteOffset: 28},
		{SemanticName: "POSITION", SemanticIndex: 1, Format: d3d.FORMAT_R32G32B32_FLOAT, InputSlot: 3},
		{
			SemanticName:      "NORMAL",
			SemanticIndex:     1,
			Format:            d3d.FORMAT_R32G32B32_FLOAT,
			InputSlot:         3,
			AlignedByteOffset: 12,
		},
	}, code.vs); err != nil {
		return err
	}
	constants := func(size uintptr) (d3d.Buffer, error) {
		return dev.CreateBuffer(&d3d.BufferDesc{
			ByteWidth: uint32(size), Usage: d3d.USAGE_DYNAMIC,
			BindFlags: d3d.BIND_CONSTANT_BUFFER, CPUAccessFlags: d3d.CPU_ACCESS_WRITE,
		}, nil)
	}
	if r.frameCB, err = constants(unsafe.Sizeof(viewerFrameConstants{})); err != nil {
		return err
	}
	if r.meshCB, err = constants(unsafe.Sizeof(viewerMeshConstants{})); err != nil {
		return err
	}
	if r.characterCB, err = constants(unsafe.Sizeof(characterMaterial{})); err != nil {
		return err
	}
	if r.ringCB, err = constants(unsafe.Sizeof(viewerRingConstants{})); err != nil {
		return err
	}
	if r.sampler, err = dev.CreateSamplerState(&d3d.SamplerDesc{
		Filter: d3d.FILTER_ANISOTROPIC, MaxAnisotropy: 8,
		AddressU: d3d.TEXTURE_ADDRESS_WRAP, AddressV: d3d.TEXTURE_ADDRESS_WRAP, AddressW: d3d.TEXTURE_ADDRESS_WRAP,
		ComparisonFunc: d3d.COMPARISON_NEVER, MaxLOD: 3.4e38,
	}); err != nil {
		return err
	}
	if r.clamp, err = dev.CreateSamplerState(&d3d.SamplerDesc{
		Filter:   d3d.FILTER_MIN_MAG_MIP_LINEAR,
		AddressU: d3d.TEXTURE_ADDRESS_CLAMP, AddressV: d3d.TEXTURE_ADDRESS_CLAMP, AddressW: d3d.TEXTURE_ADDRESS_CLAMP,
		ComparisonFunc: d3d.COMPARISON_NEVER, MaxLOD: 3.4e38,
	}); err != nil {
		return err
	}
	if r.compare, err = dev.CreateSamplerState(&d3d.SamplerDesc{
		Filter:   d3d.FILTER_COMPARISON_MIN_MAG_MIP_LINEAR,
		AddressU: d3d.TEXTURE_ADDRESS_CLAMP, AddressV: d3d.TEXTURE_ADDRESS_CLAMP, AddressW: d3d.TEXTURE_ADDRESS_CLAMP,
		ComparisonFunc: d3d.COMPARISON_LESS_EQUAL, MaxLOD: 3.4e38,
	}); err != nil {
		return err
	}
	if r.dfg, err = createViewerDFG(dev); err != nil {
		return err
	}
	for i, texture := range []struct {
		size   uint32
		format uint32
		pixels []byte
	}{
		{1, d3d.FORMAT_R8G8B8A8_UNORM, []byte{0, 128, 0, 77}},
		{genshinRampWidth, d3d.FORMAT_R8G8B8A8_UNORM_SRGB, genshinProceduralRamp()},
	} {
		height := uint32(len(texture.pixels)) / (texture.size * 4)
		if r.toonMaps[i], err = (&viewerTextureData{
			format: texture.format, width: texture.size, height: height,
			mips: [][]byte{texture.pixels}, pitches: []uint32{texture.size * 4},
		}).upload(dev, d3d.Context{}); err != nil {
			return err
		}
	}
	if r.toonMaps[3], err = (&viewerTextureData{
		format: d3d.FORMAT_R8G8B8A8_UNORM, width: 1, height: 1,
		mips: [][]byte{{255, 255, 255, 255}}, pitches: []uint32{4},
	}).upload(dev, d3d.Context{}); err != nil {
		return err
	}
	bundled, err := bundledToonMaps()
	if err != nil {
		return err
	}
	if r.toonMaps[2], err = bundled[0].upload(dev, d3d.Context{}); err != nil {
		return err
	}
	if r.toonMaps[4], err = bundled[1].upload(dev, d3d.Context{}); err != nil {
		return err
	}
	// Front faces wind counter-clockwise, as in three.js; SV_IsFrontFace follows the same rule.
	if r.raster, err = dev.CreateRasterizerState(&d3d.RasterizerDesc{
		FillMode: d3d.FILL_SOLID, CullMode: d3d.CULL_NONE, FrontCounterClockwise: 1, DepthClipEnable: 1,
		MultisampleEnable: 1,
	}); err != nil {
		return err
	}
	if r.culled, err = dev.CreateRasterizerState(&d3d.RasterizerDesc{
		FillMode: d3d.FILL_SOLID, CullMode: d3d.CULL_BACK, FrontCounterClockwise: 1, DepthClipEnable: 1,
		MultisampleEnable: 1,
	}); err != nil {
		return err
	}
	if r.frontCulled, err = dev.CreateRasterizerState(&d3d.RasterizerDesc{
		FillMode: d3d.FILL_SOLID, CullMode: d3d.CULL_FRONT, FrontCounterClockwise: 1, DepthClipEnable: 1,
		MultisampleEnable: 1,
	}); err != nil {
		return err
	}
	if r.depthOn, err = dev.CreateDepthStencilState(&d3d.DepthStencilDesc{
		DepthEnable: 1, DepthWriteMask: d3d.DEPTH_WRITE_MASK_ALL, DepthFunc: d3d.COMPARISON_LESS_EQUAL,
	}); err != nil {
		return err
	}
	if r.depthRead, err = dev.CreateDepthStencilState(&d3d.DepthStencilDesc{
		DepthEnable: 1, DepthWriteMask: d3d.DEPTH_WRITE_MASK_ZERO, DepthFunc: d3d.COMPARISON_LESS_EQUAL,
	}); err != nil {
		return err
	}
	blend := d3d.BlendDesc{}
	blend.RenderTarget[0] = d3d.RenderTargetBlendDesc{
		BlendEnable: 1, SrcBlend: d3d.BLEND_ONE, DestBlend: d3d.BLEND_INV_SRC_ALPHA, BlendOp: d3d.BLEND_OP_ADD,
		SrcBlendAlpha: d3d.BLEND_ONE, DestBlendAlpha: d3d.BLEND_INV_SRC_ALPHA, BlendOpAlpha: d3d.BLEND_OP_ADD,
		RenderTargetWriteMask: d3d.COLOR_WRITE_ALL,
	}
	r.blend, err = dev.CreateBlendState(&blend)
	return err
}

// targets makes the multisampled colour and depth buffers for a frame size.
func (r *viewerRenderer) targets(dev d3d.Device, width, height int) error {
	if r.width == width && r.height == height && r.dsv.P != nil {
		return nil
	}
	r.releaseTargets()
	r.samples = 1
	// 8x would double the targets' memory at large sizes for little visible gain.
	for _, count := range []uint32{4, 2} {
		if dev.MultisampleQualityLevels(d3d.FORMAT_B8G8R8A8_UNORM, count) > 0 &&
			dev.MultisampleQualityLevels(d3d.FORMAT_D32_FLOAT, count) > 0 {
			r.samples = count
			break
		}
	}
	desc := d3d.Texture2DDesc{
		Width: uint32(width), Height: uint32(height), MipLevels: 1, ArraySize: 1,
		Format: d3d.FORMAT_B8G8R8A8_UNORM, SampleDesc: d3d.SampleDesc{Count: r.samples},
		BindFlags: d3d.BIND_RENDER_TARGET,
	}
	var err error
	if r.samples > 1 {
		if r.color, err = dev.CreateTexture2D(&desc, nil); err != nil {
			return err
		}
		if r.colorRTV, err = dev.CreateRenderTargetView(r.color, &d3d.RTVDesc{
			Format: d3d.FORMAT_B8G8R8A8_UNORM, ViewDimension: d3d.RTV_DIMENSION_TEXTURE2DMS,
		}); err != nil {
			return err
		}
	}
	desc.Format, desc.BindFlags = d3d.FORMAT_D32_FLOAT, d3d.BIND_DEPTH_STENCIL
	if r.depth, err = dev.CreateTexture2D(&desc, nil); err != nil {
		return err
	}
	dsvDesc := d3d.DSVDesc{Format: d3d.FORMAT_D32_FLOAT, ViewDimension: d3d.DSV_DIMENSION_TEXTURE2D}
	if r.samples > 1 {
		dsvDesc.ViewDimension = d3d.DSV_DIMENSION_TEXTURE2DMS
	}
	if r.dsv, err = dev.CreateDepthStencilView(r.depth, &dsvDesc); err != nil {
		return err
	}
	r.width, r.height = width, height
	return nil
}

// draw renders scene into the frame. A scene whose GPU set belongs to another device generation draws
// nothing; the caller uploads it again.
func (r *viewerRenderer) draw(f fairy.SurfaceFrame, scene *viewerScene, opts viewerDrawOptions) error {
	g := f.GPU()
	if err := r.init(g); err != nil {
		return err
	}
	if err := r.targets(g.Device, f.Width, f.Height); err != nil {
		r.releaseTargets()
		return err
	}
	ctx := g.Context
	frameTexture, frameRTV := f.RenderTarget()
	rtv := frameRTV
	if r.samples > 1 {
		rtv = r.colorRTV
	}
	ctx.ClearRenderTargetView(rtv, opts.background)
	ctx.ClearDepthStencilView(r.dsv, d3d.CLEAR_DEPTH, 1, 0)
	if scene != nil && scene.gpu != nil && scene.gpu.generation == f.Generation {
		if err := r.drawScene(ctx, rtv, f, scene, opts); err != nil {
			return err
		}
	}
	if r.samples > 1 {
		ctx.ResolveSubresource(frameTexture, 0, r.color, 0, d3d.FORMAT_B8G8R8A8_UNORM)
	}
	return nil
}

type viewerShadowKey struct {
	gpu             *viewerGPUSet
	viewProj, world [16]float32
	doubleSided     bool
	// posed is the pose of the character motion that posed the casters, or 0 without one.
	posed uint64
	morph float32
}

func (r *viewerRenderer) releaseShadow() {
	r.shadowSRV.Release()
	r.shadowDSV.Release()
	r.shadow.Release()
	r.shadow, r.shadowDSV, r.shadowSRV = d3d.Texture2D{}, d3d.DepthStencilView{}, d3d.ShaderResourceView{}
	r.shadowCasters, r.shadowBuffers, r.shadowDrawn = nil, nil, false
}

// shadowTargets makes the key light's depth map on first use.
func (r *viewerRenderer) shadowTargets(dev d3d.Device) (err error) {
	if r.shadowSRV.P != nil {
		return nil
	}
	defer func() {
		if err != nil {
			r.releaseShadow()
		}
	}()
	if r.shadow, err = dev.CreateTexture2D(&d3d.Texture2DDesc{
		Width: viewerShadowMapSize, Height: viewerShadowMapSize, MipLevels: 1, ArraySize: 1,
		Format: d3d.FORMAT_R32_TYPELESS, SampleDesc: d3d.SampleDesc{Count: 1},
		BindFlags: d3d.BIND_DEPTH_STENCIL | d3d.BIND_SHADER_RESOURCE,
	}, nil); err != nil {
		return fmt.Errorf("create model viewer shadow map: %w", err)
	}
	if r.shadowDSV, err = dev.CreateDepthStencilView(r.shadow, &d3d.DSVDesc{
		Format: d3d.FORMAT_D32_FLOAT, ViewDimension: d3d.DSV_DIMENSION_TEXTURE2D,
	}); err != nil {
		return err
	}
	r.shadowSRV, err = dev.CreateShaderResourceView(r.shadow, &d3d.SRVDesc{
		Format: d3d.FORMAT_R32_FLOAT, ViewDimension: d3d.SRV_DIMENSION_TEXTURE2D, U: [4]uint32{0, 1},
	})
	return err
}

// toonFrame fills the toon constants of frame and reports whether the key light casts the shadow map.
func toonFrame(frame *viewerFrameConstants, toon *viewerToonDraw, opts viewerDrawOptions) bool {
	style := toon.style
	light := vec3{6, 8, 10}.normalize()
	if style.LightFollowsCamera {
		light = toonLightDirection(opts.camera.forward())
	}
	frame.toonLight = [4]float32{
		float32(light[0]), float32(light[1]), float32(light[2]), viewerToonProfiles[toon.profile].mode,
	}
	frame.toonParams = packToonParams(toon.profile, style.Shading)
	frame.toonState = [4]float32{
		float32(2.414 * math.Tan(opts.camera.lens()/2)), 1, viewerFlag(toon.textures.faceLightMap.P != nil), 1,
	}
	if !style.SelfShadow {
		return false
	}

	// The shadow camera fits the bounding sphere of the model's box, as the React viewer's
	// fitKeyLightShadowToObject does, and sits two radii towards the light.
	center := opts.minimum.add(opts.maximum).scale(0.5)
	radius := opts.maximum.sub(opts.minimum).length() * 0.5 * 1.2
	if radius <= 0 || math.IsNaN(radius) || math.IsInf(radius, 0) {
		radius = 1
	}
	up := vec3{0, 1, 0}
	if math.Abs(light[1]) > 0.999 {
		up = vec3{0, 0, 1}
	}
	lightView := lookAt(center.add(light.scale(2*radius)), center, up)
	frame.shadowViewProj = orthographic(radius, radius*0.1, radius*4).mul(lightView).float32s()
	frame.toonShadow = [4]float32{1, -0.0005, float32(radius * 0.003), viewerShadowMapSize}
	return true
}

func viewerFlag(on bool) float32 {
	if on {
		return 1
	}
	return 0
}

func (r *viewerRenderer) drawScene(
	ctx d3d.Context,
	rtv d3d.RenderTargetView,
	f fairy.SurfaceFrame,
	scene *viewerScene,
	opts viewerDrawOptions,
) error {
	cam := opts.camera
	aspect := float64(f.Width) / float64(max(f.Height, 1))
	eye := cam.eye()
	view, proj := cam.view(), cam.projection(aspect)
	frame := viewerFrameConstants{
		viewProj: proj.mul(view).float32s(),
		world:    opts.world.float32s(),
		eye:      [4]float32{float32(eye[0]), float32(eye[1]), float32(eye[2]), 1},
		surface:  [4]float32{float32(opts.metalness), float32(opts.roughness)},
		view:     view.float32s(),
		proj:     proj.float32s(),
		viewport: [4]float32{float32(f.Width), float32(f.Height)},
	}
	lights := viewerLighting(opts.environment)
	direction := func(position vec3, intensity float64) [4]float32 {
		d := position.normalize()
		return [4]float32{float32(d[0]), float32(d[1]), float32(d[2]), float32(intensity)}
	}
	frame.keyLight = direction(vec3{6, 8, 10}, lights.key)
	frame.fillLight = direction(vec3{-6, 4, -8}, lights.fill)
	ground := [3]float64{srgbToLinear(0xb9 / 255.0), srgbToLinear(0xbe / 255.0), srgbToLinear(0xc7 / 255.0)}
	for i := range 3 {
		frame.skyLight[i] = float32(lights.hemisphere)
		frame.groundLight[i] = float32(ground[i] * lights.hemisphere)
	}
	var tone float32
	switch opts.toneMapping {
	case viewerToneACES:
		tone = 1
	case viewerToneNone:
		tone = 2
	}
	frame.shading = [4]float32{float32(lights.ambient), 1, float32(opts.exposure), tone}
	sh := viewerEnvironmentSH()
	switch opts.environment {
	case viewerEnvironmentNone:
		frame.shading[1] = 0
	case viewerEnvironmentSoft:
		for i, c := range sh[1] {
			frame.envSH[i][0] = float32(c)
		}
	default:
		for i, c := range sh[0] {
			frame.envSH[i][0] = float32(c)
		}
	}
	for i, bands := range viewerPMREMBands() {
		frame.envLevels[i] = [4]float32{float32(bands[0]), float32(bands[1]), float32(bands[2])}
	}
	shadowed := opts.toon != nil && toonFrame(&frame, opts.toon, opts)
	if err := d3d.WriteData(ctx, r.frameCB, []viewerFrameConstants{frame}); err != nil {
		return err
	}

	gpu := scene.gpu
	for i, mesh := range scene.meshes {
		if i >= len(gpu.meshes) {
			break
		}
		if err := r.uploadGeometry(ctx, f.GPU().Device, mesh, &gpu.meshes[i]); err != nil {
			return err
		}
	}

	ctx.IASetInputLayout(r.layout)
	ctx.IASetPrimitiveTopology(d3d.PRIMITIVE_TOPOLOGY_TRIANGLELIST)
	ctx.VSSetConstantBuffers(0, r.frameCB, r.meshCB)
	ctx.PSSetConstantBuffers(0, r.frameCB, r.meshCB)
	ctx.PSSetConstantBuffers(3, r.characterCB)
	// A mod's meshes share a few stock materials, so the character constants are written only when they change.
	var written *characterMaterial
	// Without its buffers the motion leaves the meshes unposed; the viewer turns it off once the draw returns.
	var skin *viewerSkinGPU
	if opts.skin != nil {
		if skin = opts.skin.skinGPU(ctx, f.GPU().Device, f.Generation); skin != nil {
			ctx.VSSetShaderResources(15, skin.bonesView)
		}
	}
	var skinMotion *viewerMotion
	if skin != nil {
		skinMotion = opts.skin
	}
	originals := scene.original.draws(f.GPU().Device, skinMotion, f.Generation)
	for i := range originals {
		if err := r.uploadGeometry(ctx, f.GPU().Device, originals[i].mesh, &originals[i].gpu); err != nil {
			return err
		}
	}
	if shadowed {
		if err := r.drawShadowMap(ctx, f.GPU().Device, scene, opts, skin, originals, &frame, f.Time); err != nil {
			return err
		}
	} else if r.shadowSRV.P != nil {
		r.releaseShadow()
	}

	ctx.OMSetRenderTargets([]d3d.RenderTargetView{rtv}, r.dsv)
	ctx.RSSetViewports(d3d.Viewport{Width: float32(f.Width), Height: float32(f.Height), MaxDepth: 1})
	ctx.PSSetSamplers(0, r.sampler, r.clamp, r.compare)
	ctx.VSSetSamplers(0, r.sampler)
	ctx.PSSetShaderResources(3, r.dfg)
	if opts.toon != nil {
		textures := opts.toon.textures
		shadow := d3d.ShaderResourceView{}
		if shadowed {
			shadow = r.shadowSRV
		}
		ctx.PSSetShaderResources(6,
			cmpView(textures.metalMap, r.toonMaps[2]),
			cmpView(textures.faceLightMap, r.toonMaps[3]),
			cmpView(textures.faceShadow, r.toonMaps[4]),
			shadow,
		)
	}

	// Coverage cards overlap in no fixed order, which one ink colour hides as long as they blend over the
	// opaque meshes without writing depth over each other. Outlines draw with the opaque meshes, before them.
	for _, pass := range []string{"opaque", "outline", "blended"} {
		switch pass {
		case "opaque":
			ctx.OMSetDepthStencilState(r.depthOn, 0)
			ctx.OMSetBlendState(d3d.BlendState{}, [4]float32{}, 0xffffffff)
			ctx.VSSetShader(r.vs)
			ctx.PSSetShader(r.ps)
			if opts.doubleSided {
				ctx.RSSetState(r.raster)
			} else {
				ctx.RSSetState(r.culled)
			}
		case "outline":
			if opts.toon == nil || !opts.toon.style.Outlines {
				continue
			}
			ctx.VSSetShader(r.outlineVS)
			ctx.PSSetShader(r.outlinePS)
			ctx.RSSetState(r.frontCulled)
		case "blended":
			ctx.OMSetDepthStencilState(r.depthRead, 0)
			ctx.OMSetBlendState(r.blend, [4]float32{}, 0xffffffff)
			ctx.VSSetShader(r.vs)
			ctx.PSSetShader(r.ps)
			if opts.doubleSided {
				ctx.RSSetState(r.raster)
			} else {
				ctx.RSSetState(r.culled)
			}
		}
		textures := scene.texture
		drawOne := func(
			mesh *viewerMesh,
			state modelviewer.ViewerMeshState,
			buffers viewerMeshGPU,
			blend d3d.ShaderResourceView,
			anchor int32,
		) error {
			// EFMI draws no hull around eyes and overlay shadows.
			if pass == "outline" && opts.toon.profile == efmiProfile && (mesh.toonEye || mesh.overlayShadow) {
				return nil
			}
			material := meshMaterial(mesh, state, textures, opts)
			coverage := material.constants.flags&viewerFlagCoverage != 0
			if coverage != (pass == "blended") {
				return nil
			}
			if blend.P != nil {
				material.constants.flags |= viewerFlagSkinned
				if material.constants.flags&viewerFlagFace != 0 {
					forward := opts.skin.turnBy(anchor, mesh.faceForward)
					material.constants.faceForward = [4]float32{
						float32(forward[0]), float32(forward[1]), float32(forward[2]),
					}
				}
				ctx.VSSetShaderResources(14, blend)
			}
			material.constants.morphWeight = morphWeight(mesh, buffers, opts.morph)
			if err := d3d.WriteData(ctx, r.meshCB, []viewerMeshConstants{material.constants}); err != nil {
				return err
			}
			if material.character != nil && material.character != written {
				if err := d3d.WriteData(ctx, r.characterCB, []characterMaterial{*material.character}); err != nil {
					return err
				}
				written = material.character
			}
			ctx.PSSetShaderResources(0, material.views[:3]...)
			if opts.toon != nil {
				white := r.toonMaps[3]
				ctx.PSSetShaderResources(4, cmpView(material.views[3], r.toonMaps[0]),
					cmpView(material.views[4], r.toonMaps[1]))
				ctx.PSSetShaderResources(10, cmpView(material.views[5], white), cmpView(material.views[6], white),
					cmpView(material.views[7], white))
				ctx.VSSetShaderResources(13, cmpView(material.outlineMask, white))
			} else {
				ctx.PSSetShaderResources(4, material.views[3])
				ctx.PSSetShaderResources(10, material.views[5])
			}
			r.drawMesh(ctx, buffers)
			return nil
		}
		for i, mesh := range scene.meshes {
			if !mesh.drawn(opts.untextured != nil) || i >= len(gpu.meshes) || gpu.meshes[i].indexCount == 0 {
				continue
			}
			blend, _ := skin.blend(i, mesh.vertexCount)
			state := mesh.state
			if opts.untextured == nil {
				state = scene.original.fillState(i, state, gpu.textures)
			}
			if err := drawOne(mesh, state, gpu.meshes[i], blend, opts.skin.anchor(i)); err != nil {
				return err
			}
		}
		for _, part := range originals {
			blend := d3d.ShaderResourceView{}
			if part.skinned {
				blend = part.blend
			}
			if err := drawOne(part.mesh, part.state, part.gpu, blend, part.anchor); err != nil {
				return err
			}
		}
	}
	if opts.overlay != nil {
		if err := r.drawOverlay(ctx, f.GPU().Device, rtv, opts.overlay, f.Time.Seconds()); err != nil {
			return err
		}
	}
	if opts.ring != nil {
		return r.drawRing(ctx, rtv, *opts.ring)
	}
	return nil
}

// uploadGeometry brings the mesh's buffers up to date with its current and next geometry.
func (r *viewerRenderer) uploadGeometry(
	ctx d3d.Context,
	dev d3d.Device,
	mesh *viewerMesh,
	buffers *viewerMeshGPU,
) error {
	if buffers.indexCount == 0 {
		return nil
	}
	if mesh.swapped {
		mesh.swapped = false
		if buffers.next.P != nil {
			buffers.vertices, buffers.next = buffers.next, buffers.vertices
		} else {
			mesh.dirty, mesh.nextDirty = mesh.current != nil, mesh.next != nil
		}
		r.shadowDrawn = false
	}
	size := int(buffers.vertices.Desc().ByteWidth) / 4
	if mesh.dirty && len(mesh.current) == size {
		d3d.UpdateData(ctx, buffers.vertices, mesh.current)
		mesh.dirty, r.shadowDrawn = false, false
	}
	if mesh.nextDirty && len(mesh.next) == size {
		if buffers.next.P == nil {
			next, err := d3d.CreateBufferData(dev, d3d.BIND_VERTEX_BUFFER, d3d.USAGE_DEFAULT, mesh.next)
			if err != nil {
				return fmt.Errorf("create model viewer next frame buffer: %w", err)
			}
			buffers.next = next
		} else {
			d3d.UpdateData(ctx, buffers.next, mesh.next)
		}
		mesh.nextDirty, r.shadowDrawn = false, false
	}
	if mesh.extraDirty && len(mesh.extra) == int(buffers.extra.Desc().ByteWidth) {
		d3d.UpdateData(ctx, buffers.extra, mesh.extra)
		mesh.extraDirty, r.shadowDrawn = false, false
	}
	return nil
}

// morphWeight is the share of its next frame's geometry the mesh draws with: weight once that geometry is on the
// GPU, and 0 without one.
func morphWeight(mesh *viewerMesh, buffers viewerMeshGPU, weight float32) float32 {
	if mesh.next == nil || mesh.nextDirty || buffers.next.P == nil {
		return 0
	}
	return weight
}

// drawRing draws the ring as a line loop over everything, as three.js draws a LineLoop without depth test.
func (r *viewerRenderer) drawRing(ctx d3d.Context, rtv d3d.RenderTargetView, ring viewerRing) error {
	normal := ring.normal.normalize()
	if normal.length() == 0 {
		normal = vec3{0, 0, 1}
	}
	helper := vec3{0, 1, 0}
	if math.Abs(normal[1]) > 0.9 {
		helper = vec3{1, 0, 0}
	}
	u := helper.cross(normal).normalize().scale(ring.radius)
	v := normal.cross(u.normalize()).scale(ring.radius)
	four := func(a vec3) [4]float32 { return [4]float32{float32(a[0]), float32(a[1]), float32(a[2]), 0} }
	if err := d3d.WriteData(ctx, r.ringCB, []viewerRingConstants{{
		center: four(ring.center), u: four(u), v: four(v), color: ring.color,
	}}); err != nil {
		return err
	}
	ctx.OMSetRenderTargets([]d3d.RenderTargetView{rtv}, d3d.DepthStencilView{})
	ctx.OMSetBlendState(r.blend, [4]float32{}, 0xffffffff)
	ctx.RSSetState(r.raster)
	ctx.IASetInputLayout(d3d.InputLayout{})
	ctx.IASetPrimitiveTopology(d3d.PRIMITIVE_TOPOLOGY_LINESTRIP)
	ctx.VSSetConstantBuffers(2, r.ringCB)
	ctx.PSSetConstantBuffers(2, r.ringCB)
	ctx.VSSetShader(r.ringVS)
	ctx.PSSetShader(r.ringPS)
	ctx.Draw(viewerRingSegments+1, 0)
	return nil
}

// drawShadowMap renders the depth of the meshes that cast shadows into the key light's map, unless the map already
// holds them as frame's light sees them. While a playback draws between frames, a map drawn less than
// viewerShadowInterval before now stays as long as only the pose or the share of the next frame changed.
func (r *viewerRenderer) drawShadowMap(
	ctx d3d.Context,
	dev d3d.Device,
	scene *viewerScene,
	opts viewerDrawOptions,
	skin *viewerSkinGPU,
	originals []originalDraw,
	frame *viewerFrameConstants,
	now time.Duration,
) error {
	if err := r.shadowTargets(dev); err != nil {
		return err
	}
	gpu := scene.gpu
	casters, buffers := r.casterScratch[:0], r.bufferScratch[:0]
	for i, mesh := range scene.meshes {
		if !mesh.drawn(false) || i >= len(gpu.meshes) || gpu.meshes[i].indexCount == 0 {
			continue
		}
		// Coverage cards are flat ink decals that block no light.
		if texture, ok := scene.texture(mesh.state.TexKey); ok && texture.flags&viewerFlagCoverage != 0 {
			continue
		}
		casters = append(casters, i)
		buffers = append(buffers, gpu.meshes[i].vertices)
	}
	for _, part := range originals {
		buffers = append(buffers, part.gpu.vertices)
	}
	key := viewerShadowKey{
		gpu: gpu, viewProj: frame.shadowViewProj, world: frame.world, doubleSided: opts.doubleSided,
		morph: opts.morph,
	}
	if skin != nil {
		key.posed = skin.posed
	}
	moved := key
	moved.posed, moved.morph = r.shadowKey.posed, r.shadowKey.morph
	// The surface's time starts again after a frame that left it out.
	elapsed := now - r.shadowAt
	recent := opts.interpolating && moved == r.shadowKey && elapsed >= 0 && elapsed < viewerShadowInterval
	if r.shadowDrawn && (key == r.shadowKey || recent) && slices.Equal(casters, r.shadowCasters) &&
		slices.Equal(buffers, r.shadowBuffers) {
		r.casterScratch, r.bufferScratch = casters, buffers
		return nil
	}
	r.casterScratch, r.bufferScratch = r.shadowCasters, r.shadowBuffers
	r.shadowKey, r.shadowCasters, r.shadowBuffers, r.shadowDrawn = key, casters, buffers, true
	r.shadowAt = now

	ctx.PSSetShaderResources(9, d3d.ShaderResourceView{})
	ctx.OMSetRenderTargets(nil, r.shadowDSV)
	ctx.ClearDepthStencilView(r.shadowDSV, d3d.CLEAR_DEPTH, 1, 0)
	ctx.RSSetViewports(d3d.Viewport{Width: viewerShadowMapSize, Height: viewerShadowMapSize, MaxDepth: 1})
	ctx.OMSetDepthStencilState(r.depthOn, 0)
	ctx.OMSetBlendState(d3d.BlendState{}, [4]float32{}, 0xffffffff)
	// three.js renders the shadows of single-sided materials from their back faces.
	if opts.doubleSided {
		ctx.RSSetState(r.raster)
	} else {
		ctx.RSSetState(r.frontCulled)
	}
	ctx.VSSetShader(r.shadowVS)
	ctx.PSSetShader(d3d.PixelShader{})
	// The shadow's vertex shader reads only whether the mesh is skinned and how far it morphs, so the constants
	// change only with those.
	written := false
	var constants viewerMeshConstants
	for _, i := range casters {
		var flags uint32
		if blend, ok := skin.blend(i, scene.meshes[i].vertexCount); ok {
			flags = viewerFlagSkinned
			ctx.VSSetShaderResources(14, blend)
		}
		morph := morphWeight(scene.meshes[i], gpu.meshes[i], opts.morph)
		if !written || flags != constants.flags || morph != constants.morphWeight {
			constants.flags, constants.morphWeight = flags, morph
			if err := d3d.WriteData(ctx, r.meshCB, []viewerMeshConstants{constants}); err != nil {
				return err
			}
			written = true
		}
		r.drawMesh(ctx, gpu.meshes[i])
	}
	for _, part := range originals {
		var flags uint32
		if part.skinned {
			flags = viewerFlagSkinned
			ctx.VSSetShaderResources(14, part.blend)
		}
		if !written || flags != constants.flags || constants.morphWeight != 0 {
			constants.flags, constants.morphWeight = flags, 0
			if err := d3d.WriteData(ctx, r.meshCB, []viewerMeshConstants{constants}); err != nil {
				return err
			}
			written = true
		}
		r.drawMesh(ctx, part.gpu)
	}
	return nil
}

func (r *viewerRenderer) drawMesh(ctx d3d.Context, buffers viewerMeshGPU) {
	next := buffers.next
	if next.P == nil {
		next = buffers.vertices
	}
	ctx.IASetVertexBuffers(0, []d3d.Buffer{buffers.vertices, buffers.uvs, buffers.extra, next},
		[]uint32{24, 16, viewerVertexExtraStride, 24}, []uint32{0, 0, 0, 0})
	ctx.IASetIndexBuffer(buffers.indices, d3d.FORMAT_R32_UINT, 0)
	ctx.DrawIndexed(buffers.indexCount, 0, 0)
}

func cmpView(view, fallback d3d.ShaderResourceView) d3d.ShaderResourceView {
	if view.P != nil {
		return view
	}
	return fallback
}

// derivativeTangentProfiles are the material profiles whose normal maps may use a tangent frame derived in the
// shader when the mesh has no usable vertex tangents.
var derivativeTangentProfiles = []string{"efmi", "wwmi", "wuwa:rabbitfx"}

// viewerMeshMaterial is what one mesh draws with: its constants and the views of t0-t2, then its light map and
// toon ramp (t4, t5), then its material map, normal map read as data and highlight map (t10-t12).
type viewerMeshMaterial struct {
	constants viewerMeshConstants
	// character is the stock material characterUse applies, or nil.
	character *characterMaterial
	views     [8]d3d.ShaderResourceView
	// outlineMask is EFMI's outline width mask, read by the hull's vertex shader.
	outlineMask d3d.ShaderResourceView
}

// meshMaterial picks the textures and shader flags state selects for mesh, looking them up with textures.
func meshMaterial(
	mesh *viewerMesh,
	state modelviewer.ViewerMeshState,
	textures func(string) (viewerTextureGPU, bool),
	opts viewerDrawOptions,
) viewerMeshMaterial {
	m := viewerMeshMaterial{constants: viewerMeshConstants{tint: [4]float32{1, 1, 1, 1}}}
	c := &m.constants
	diffuse, hasDiffuse := textures(state.TexKey)
	if hasDiffuse && state.TexKey != "" {
		m.views[0] = diffuse.view
		c.flags |= viewerFlagDiffuse | diffuse.flags
	} else {
		hasDiffuse = false
	}
	// A normal map needs a frame: the mesh's tangents, or one derived in the shader for profiles that allow it.
	usable := mesh.tangentUsable || slices.Contains(derivativeTangentProfiles, opts.profile)
	if texture, ok := textures(state.NormalMapKey); ok && opts.normalMaps && usable &&
		state.NormalMapKey != "" && !mesh.normalMapUnused {
		m.views[1] = texture.view
		c.flags |= viewerFlagNormalMap
		c.normalMode = texture.normalMode
		if mesh.tangentUsable {
			c.flags |= viewerFlagVertexTangent
		}
	}
	if texture, ok := textures(state.CutoutMapKey); ok && state.CutoutMapKey != "" {
		m.views[2] = texture.view
		c.flags |= viewerFlagCutout
	}
	if !hasDiffuse && opts.untextured != nil {
		c.tint = [4]float32{opts.untextured[0], opts.untextured[1], opts.untextured[2], 1}
	}
	if overlay := mesh.overlay; overlay != nil {
		c.tint = [4]float32{overlay.tint[0], overlay.tint[1], overlay.tint[2], 1}
		if overlay.colors {
			c.flags |= viewerFlagOverlayColor
		}
	}
	lookup := func(key string) (viewerTextureGPU, bool) {
		texture, ok := textures(key)
		return texture, ok && key != ""
	}
	if opts.toon == nil {
		// Without toon shading ZZMI's packed maps drive the standard material: green of the light map is
		// metalness and green of the material map smoothness.
		if opts.profile == zzmiProfile {
			if texture, ok := lookup(state.LightMapKey); ok {
				m.views[3] = texture.view
				c.flags |= viewerFlagLightMap
			}
			if texture, ok := lookup(state.MaterialMapKey); ok {
				m.views[5] = texture.view
				c.flags |= viewerFlagMaterialMap
			}
		}
		return m
	}

	profile := opts.toon.profile
	mode := viewerToonProfiles[profile].mode
	optional := func(key *string) string {
		if key == nil {
			return ""
		}
		return *key
	}
	for _, flag := range []struct {
		on   bool
		flag uint32
	}{
		{mesh.hasColors, viewerFlagVertexColor},
		{mesh.hasUV1, viewerFlagUV1},
		{mesh.toonHair, viewerFlagHair},
		{mesh.toonEye, viewerFlagEye},
	} {
		if flag.on {
			c.flags |= flag.flag
		}
	}

	light, hasLight := lookup(state.LightMapKey)
	// One- and two-channel light maps have no material ID or specular mask.
	packed := hasLight && !strings.HasPrefix(light.format, "bc4-") && !strings.HasPrefix(light.format, "bc5-")
	switch mode {
	case toonModeWWMI:
		// WWMI reads its shadow mask from any light map, from red in one-channel containers.
		if hasLight {
			m.views[3] = light.view
			c.flags |= viewerFlagLightMap
			if light.flags&viewerFlagMapBC4 != 0 {
				c.flags |= viewerFlagLightBC4
			}
			if light.flags&viewerFlagMapSigned != 0 {
				c.flags |= viewerFlagLightSigned
			}
			if packed && !strings.HasPrefix(light.format, "bc6h-") {
				c.flags |= viewerFlagPackedMask
			}
		}
	case toonModeEFMI:
		if hasLight && light.outlineMask {
			m.outlineMask = light.view
			c.flags |= viewerFlagOutlineMask
		} else if packed {
			m.views[3] = light.view
			c.flags |= viewerFlagLightMap
		}
	default:
		if packed {
			m.views[3] = light.view
			c.flags |= viewerFlagLightMap
		}
	}

	if values := opts.toon.materials.forMesh(mesh); values != nil && opts.toon.characterUse != 0 {
		m.character = values
		c.characterUse = opts.toon.characterUse
		c.characterScale = [4]float32{opts.toon.characterScale}
	}

	face := mesh.toonFace
	if mode == toonModeEFMI {
		// A flat face lights the head meshes that bind no light map of their own from the face mask instead of N.L.
		face = opts.toon.style.FlatFace && mesh.efmiHead && c.flags&viewerFlagLightMap == 0
	}
	if face {
		c.flags |= viewerFlagFace
		c.faceForward = [4]float32{
			float32(mesh.faceForward[0]),
			float32(mesh.faceForward[1]),
			float32(mesh.faceForward[2]),
		}
	}
	// BC1 and opaque images carry no emission mask; their alpha would light everything. Star Rail has no
	// emission.
	if profile == gimiProfile && hasDiffuse && !strings.HasPrefix(diffuse.format, "bc1-") && !face {
		c.flags |= viewerFlagEmission
	}

	switch mode {
	case toonModeGIMI:
		m.views[4] = opts.toon.textures.bodyRamp
		if mesh.toonHair {
			m.views[4] = cmpView(opts.toon.textures.hairRamp, opts.toon.textures.bodyRamp)
		}
	case toonModeSRMI:
		if ramp := opts.toon.textures.characterRamp(mesh); ramp.P != nil {
			m.views[4] = ramp
			c.flags |= viewerFlagRampMap
		}
	case toonModeZZMI, toonModeWWMI:
		// The material map and the normal map hold shading data, which one- and two-channel containers lack.
		data := func(key string, view int, flag uint32) {
			if texture, ok := lookup(key); ok && !strings.HasPrefix(texture.format, "bc4-") &&
				!strings.HasPrefix(texture.format, "bc5-") {
				m.views[view] = texture.view
				c.flags |= flag
			}
		}
		data(state.MaterialMapKey, 5, viewerFlagMaterialMap)
		data(state.NormalMapKey, 6, viewerFlagDataMap)
	case toonModeEFMI:
		for _, slot := range []struct {
			key  string
			view int
			flag uint32
		}{
			{state.MaterialMapKey, 5, viewerFlagMaterialMap},
			{optional(mesh.info.HighlightMapKey), 7, viewerFlagHighlightMap},
			{optional(mesh.info.RampMapKey), 4, viewerFlagRampMap},
		} {
			if texture, ok := lookup(slot.key); ok {
				m.views[slot.view] = texture.view
				c.flags |= slot.flag
			}
		}
		// The character's stock ramp stands in for a _DiffRampMap the mod does not bind.
		if ramp := opts.toon.textures.characterRamp(mesh); c.flags&viewerFlagRampMap == 0 && ramp.P != nil {
			m.views[4] = ramp
			c.flags |= viewerFlagRampMap
		}
	}
	return m
}

func createViewerDFG(dev d3d.Device) (d3d.ShaderResourceView, error) {
	tex, err := dev.CreateTexture2D(&d3d.Texture2DDesc{
		Width: 16, Height: 16, MipLevels: 1, ArraySize: 1, Format: d3d.FORMAT_R16G16_FLOAT,
		SampleDesc: d3d.SampleDesc{Count: 1}, Usage: d3d.USAGE_IMMUTABLE, BindFlags: d3d.BIND_SHADER_RESOURCE,
	}, &d3d.SubresourceData{SysMem: unsafe.Pointer(&viewerDFGLUT[0]), Pitch: 16 * 4})
	if err != nil {
		return d3d.ShaderResourceView{}, fmt.Errorf("create model viewer DFG table: %w", err)
	}
	defer tex.Release()
	return dev.CreateShaderResourceView(tex, nil)
}
