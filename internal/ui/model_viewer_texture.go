package ui

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/draw"
	_ "image/jpeg" // Mod textures that are not DDS arrive as PNG or JPEG.
	_ "image/png"
	"math"
	"runtime"
	"strings"
	"sync"
	"unsafe"

	"github.com/myparsleycat/fairy/d3d"

	modelviewer "nahida.live/desktop/internal/tools/model_viewer"
)

const modelViewerMaxTextureSize = 16384

// viewerTextureData is a texture decoded in memory, ready for upload.
type viewerTextureData struct {
	format uint32
	width  uint32
	height uint32
	// mips holds the levels from the largest down with their row pitches; an RGBA image has one level and
	// gets the rest from GenerateMips.
	mips    [][]byte
	pitches []uint32
	// generateMips marks an uncompressed image whose lower levels the GPU builds.
	generateMips bool
	// resample marks a compressed texture without a mip chain, which upload redraws into an uncompressed one
	// that has mips.
	resample   bool
	flags      uint32
	normalMode uint32
	// sourceFormat is the transport's lower-case format name.
	sourceFormat string
	outlineMask  bool
}

// Shader flags of a mesh, matching FLAG_* in model_viewer.hlsl.
const (
	viewerFlagDiffuse     = 1
	viewerFlagDiffuseSRGB = 2
	viewerFlagMapBC4      = 4
	viewerFlagMapSigned   = 8
	viewerFlagInvertAlpha = 16
	viewerFlagCoverage    = 32
	viewerFlagNormalMap   = 64
	viewerFlagCutout      = 128
)

// loadViewerTexture reads a transport texture through read and decodes it the way the React viewer does:
// a compressed DDS goes to the GPU as is, anything else as RGBA8 decoded from its image or PNG fallback.
func loadViewerTexture(
	ctx context.Context,
	read func(context.Context, string) ([]byte, error),
	entry modelviewer.ModelViewerTextureTransport,
	normalXYReconstruct bool,
) (*viewerTextureData, error) {
	var texture *viewerTextureData
	var directErr error
	if entry.Encoding == "dds" && entry.Width > 0 && entry.Height > 0 && entry.MipCount > 0 {
		data, err := read(ctx, entry.URL)
		if err == nil {
			texture, err = parseViewerDDS(data)
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		directErr = err
	}
	if texture == nil {
		url := entry.URL
		if entry.Encoding == "dds" {
			url = entry.FallbackURL
		}
		if url == "" {
			return nil, errors.Join(directErr, errors.New("texture has no decodable source"))
		}
		data, err := read(ctx, url)
		if err != nil {
			return nil, errors.Join(directErr, err)
		}
		texture, err = decodeViewerImage(data)
		if err != nil {
			return nil, errors.Join(directErr, err)
		}
	}

	format := strings.ToLower(entry.Format)
	texture.sourceFormat = format
	texture.outlineMask = entry.OutlineMask
	direct := texture.format != d3d.FORMAT_R8G8B8A8_UNORM
	switch texture.format {
	case formatBC1, formatBC2, formatBC3, formatBC7:
		texture.resample = len(texture.mips) == 1 && max(texture.width, texture.height) > viewerMipmapMinDimension
	}
	bc4 := direct && strings.HasPrefix(format, "bc4-")
	signed := direct && strings.HasSuffix(format, "snorm")
	if entry.Role == "diffuse" {
		texture.flags |= viewerFlagDiffuseSRGB
	}
	if bc4 {
		texture.flags |= viewerFlagMapBC4
	}
	if signed {
		texture.flags |= viewerFlagMapSigned
	}
	if entry.InvertAlpha {
		texture.flags |= viewerFlagInvertAlpha
	}
	if entry.CoverageMask {
		texture.flags |= viewerFlagCoverage
	}
	if normalXYReconstruct {
		// Converted normal maps keep their source channels, and blue may hold shading data, so Z is rebuilt
		// for them too.
		switch {
		case bc4 && signed:
			texture.normalMode = 4
		case bc4:
			texture.normalMode = 3
		case signed:
			texture.normalMode = 2
		default:
			texture.normalMode = 1
		}
	}
	return texture, nil
}

func decodeViewerImage(data []byte) (*viewerTextureData, error) {
	decoded, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode texture image: %w", err)
	}
	bounds := decoded.Bounds()
	if bounds.Dx() <= 0 || bounds.Dy() <= 0 || bounds.Dx() > modelViewerMaxTextureSize ||
		bounds.Dy() > modelViewerMaxTextureSize {
		return nil, fmt.Errorf("texture image size %dx%d is unsupported", bounds.Dx(), bounds.Dy())
	}
	rgba, ok := decoded.(*image.NRGBA)
	if !ok || rgba.Rect.Min != (image.Point{}) {
		rgba = image.NewNRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
		draw.Draw(rgba, rgba.Rect, decoded, bounds.Min, draw.Src)
	}
	return &viewerTextureData{
		format:       d3d.FORMAT_R8G8B8A8_UNORM,
		width:        uint32(bounds.Dx()),
		height:       uint32(bounds.Dy()),
		mips:         [][]byte{rgba.Pix},
		pitches:      []uint32{uint32(rgba.Stride)},
		generateMips: true,
	}, nil
}

const (
	ddsMagic        = 0x20534444
	ddsFourCCDX10   = 0x30315844
	ddsCaps2Cubemap = 0x200
	ddsCaps2Volume  = 0x200000
)

// parseViewerDDS reads a block-compressed 2D DDS into its mip levels. Levels larger than the device allows
// are dropped.
func parseViewerDDS(data []byte) (*viewerTextureData, error) {
	if len(data) < 128 || binary.LittleEndian.Uint32(data) != ddsMagic || binary.LittleEndian.Uint32(data[4:]) != 124 {
		return nil, errors.New("invalid DDS header")
	}
	height := binary.LittleEndian.Uint32(data[12:])
	width := binary.LittleEndian.Uint32(data[16:])
	mipCount := max(binary.LittleEndian.Uint32(data[28:]), 1)
	if width == 0 || height == 0 || mipCount > 32 {
		return nil, errors.New("invalid DDS dimensions")
	}
	if binary.LittleEndian.Uint32(data[112:])&(ddsCaps2Cubemap|ddsCaps2Volume) != 0 {
		return nil, errors.New("DDS cube maps and volumes are unsupported")
	}
	offset := 128
	format := ddsLegacyFormat(string(data[84:88]))
	if binary.LittleEndian.Uint32(data[84:]) == ddsFourCCDX10 {
		if len(data) < 148 || binary.LittleEndian.Uint32(data[132:]) != 3 ||
			binary.LittleEndian.Uint32(data[140:]) != 1 {
			return nil, errors.New("DDS arrays and non-2D resources are unsupported")
		}
		format = ddsDXGIFormat(binary.LittleEndian.Uint32(data[128:]))
		offset = 148
	}
	if format == 0 {
		return nil, errors.New("unsupported DDS format")
	}
	// Direct3D needs the top level of a block-compressed texture in whole blocks.
	blockBytes := ddsBlockBytes(format)
	texture := &viewerTextureData{format: format}
	w, h := width, height
	for range mipCount {
		pitch := max((w+3)/4, 1) * uint32(blockBytes)
		size := int(pitch) * int(max((h+3)/4, 1))
		if offset+size > len(data) {
			return nil, errors.New("DDS mip data is truncated")
		}
		fits := w <= modelViewerMaxTextureSize && h <= modelViewerMaxTextureSize
		if fits && (len(texture.mips) > 0 || (w%4 == 0 && h%4 == 0)) {
			if len(texture.mips) == 0 {
				texture.width, texture.height = w, h
			}
			texture.mips = append(texture.mips, data[offset:offset+size])
			texture.pitches = append(texture.pitches, pitch)
		}
		offset += size
		w, h = max(w/2, 1), max(h/2, 1)
	}
	if len(texture.mips) == 0 {
		return nil, errors.New("DDS has no level the device can sample")
	}
	return texture, nil
}

func ddsLegacyFormat(fourCC string) uint32 {
	switch fourCC {
	case "DXT1":
		return formatBC1
	case "DXT2", "DXT3":
		return formatBC2
	case "DXT4", "DXT5":
		return formatBC3
	case "ATI1", "BC4U":
		return formatBC4
	case "BC4S":
		return formatBC4Signed
	case "ATI2", "BC5U":
		return formatBC5
	case "BC5S":
		return formatBC5Signed
	}
	return 0
}

// DXGI block-compressed formats. Colour is decoded to linear in the shader, so sRGB variants map to their
// UNORM forms.
const (
	formatBC1        = 71
	formatBC2        = 74
	formatBC3        = 77
	formatBC4        = 80
	formatBC4Signed  = 81
	formatBC5        = 83
	formatBC5Signed  = 84
	formatBC6HUnsign = 95
	formatBC6HSigned = 96
	formatBC7        = 98
)

func ddsDXGIFormat(format uint32) uint32 {
	switch format {
	case 70, 71, 72:
		return formatBC1
	case 73, 74, 75:
		return formatBC2
	case 76, 77, 78:
		return formatBC3
	case 79, 80:
		return formatBC4
	case 81:
		return formatBC4Signed
	case 82, 83:
		return formatBC5
	case 84:
		return formatBC5Signed
	case 94, 95:
		return formatBC6HUnsign
	case 96:
		return formatBC6HSigned
	case 97, 98, 99:
		return formatBC7
	}
	return 0
}

func ddsBlockBytes(format uint32) int {
	if format == formatBC1 || format == formatBC4 || format == formatBC4Signed {
		return 8
	}
	return 16
}

// upload makes the texture on dev and returns a view of it. A texture that needs generated mips is filled
// and mipped through ctx.
func (t *viewerTextureData) upload(dev d3d.Device, ctx d3d.Context) (d3d.ShaderResourceView, error) {
	desc := d3d.Texture2DDesc{
		Width: t.width, Height: t.height, MipLevels: uint32(len(t.mips)), ArraySize: 1,
		Format: t.format, SampleDesc: d3d.SampleDesc{Count: 1},
		Usage: d3d.USAGE_IMMUTABLE, BindFlags: d3d.BIND_SHADER_RESOURCE,
	}
	var init []d3d.SubresourceData
	if t.generateMips {
		desc.MipLevels = 0
		desc.Usage = d3d.USAGE_DEFAULT
		desc.BindFlags |= d3d.BIND_RENDER_TARGET
		desc.MiscFlags = d3d.RESOURCE_MISC_GENERATE_MIPS
	} else {
		for level, mip := range t.mips {
			init = append(init, d3d.SubresourceData{SysMem: unsafePointer(mip), Pitch: t.pitches[level]})
		}
	}
	var first *d3d.SubresourceData
	if len(init) > 0 {
		first = &init[0]
	}
	tex, err := dev.CreateTexture2D(&desc, first)
	runtime.KeepAlive(t.mips)
	if err != nil {
		return d3d.ShaderResourceView{}, fmt.Errorf(
			"create %dx%d texture format %d: %w",
			t.width,
			t.height,
			t.format,
			err,
		)
	}
	defer tex.Release()
	view, err := dev.CreateShaderResourceView(tex, nil)
	if err != nil {
		return d3d.ShaderResourceView{}, err
	}
	if t.generateMips {
		ctx.UpdateSubresource(tex, nil, unsafePointer(t.mips[0]), t.pitches[0], 0)
		ctx.GenerateMips(view)
		runtime.KeepAlive(t.mips)
	}
	if t.resample && ctx.P != nil {
		defer view.Release()
		return resampleViewerTexture(dev, ctx, view, t.width, t.height)
	}
	return view, nil
}

// Mods often ship DDS files without a mip chain, which alias when minified, and the GPU cannot generate mips
// for a compressed texture. As the React viewer's mipmapper did, a four-channel texture larger than
// viewerMipmapMinDimension is redrawn into an uncompressed one of at most viewerMipmapMaxDimension that has
// them; the other formats keep their compressed upload, because their shaders read the packed channels.
const (
	viewerMipmapMinDimension = 512
	viewerMipmapMaxDimension = 2048
)

const viewerBlitHLSL = `
Texture2D source : register(t0);
SamplerState linearClamp : register(s0);

struct Out {
    float4 pos : SV_Position;
    float2 uv : TEXCOORD0;
};

Out vs_main(uint id : SV_VertexID) {
    Out o;
    o.uv = float2((id << 1) & 2, id & 2);
    o.pos = float4(o.uv * float2(2, -2) + float2(-1, 1), 0, 1);
    return o;
}

float4 ps_main(Out i) : SV_Target {
    return source.Sample(linearClamp, i.uv);
}
`

type viewerBlitShaders struct{ vs, ps []byte }

var viewerBlitCode = sync.OnceValues(func() (viewerBlitShaders, error) {
	var code viewerBlitShaders
	var err error
	if code.vs, err = d3d.Compile([]byte(viewerBlitHLSL), "model_viewer_blit.hlsl", "vs_main", "vs_5_0"); err != nil {
		return viewerBlitShaders{}, fmt.Errorf("compile model viewer blit shader: %w", err)
	}
	if code.ps, err = d3d.Compile([]byte(viewerBlitHLSL), "model_viewer_blit.hlsl", "ps_main", "ps_5_0"); err != nil {
		return viewerBlitShaders{}, fmt.Errorf("compile model viewer blit shader: %w", err)
	}
	return code, nil
})

// resampleViewerTexture draws source, a texture of width by height, into an uncompressed texture with a full
// mip chain and returns a view of it. The values are copied as stored, so the result decodes as source does.
func resampleViewerTexture(
	dev d3d.Device,
	ctx d3d.Context,
	source d3d.ShaderResourceView,
	width, height uint32,
) (d3d.ShaderResourceView, error) {
	code, err := viewerBlitCode()
	if err != nil {
		return d3d.ShaderResourceView{}, err
	}
	scale := min(1, viewerMipmapMaxDimension/float64(max(width, height)))
	width = max(uint32(math.Round(float64(width)*scale)), 1)
	height = max(uint32(math.Round(float64(height)*scale)), 1)

	target, err := dev.CreateTexture2D(&d3d.Texture2DDesc{
		Width: width, Height: height, ArraySize: 1, Format: d3d.FORMAT_R8G8B8A8_UNORM,
		SampleDesc: d3d.SampleDesc{Count: 1},
		BindFlags:  d3d.BIND_SHADER_RESOURCE | d3d.BIND_RENDER_TARGET, MiscFlags: d3d.RESOURCE_MISC_GENERATE_MIPS,
	}, nil)
	if err != nil {
		return d3d.ShaderResourceView{}, fmt.Errorf("create %dx%d mipped texture: %w", width, height, err)
	}
	defer target.Release()
	rtv, err := dev.CreateRenderTargetView(target, nil)
	if err != nil {
		return d3d.ShaderResourceView{}, err
	}
	defer rtv.Release()
	vs, err := dev.CreateVertexShader(code.vs)
	if err != nil {
		return d3d.ShaderResourceView{}, err
	}
	defer vs.Release()
	ps, err := dev.CreatePixelShader(code.ps)
	if err != nil {
		return d3d.ShaderResourceView{}, err
	}
	defer ps.Release()
	sampler, err := dev.CreateSamplerState(&d3d.SamplerDesc{
		Filter:   d3d.FILTER_MIN_MAG_MIP_LINEAR,
		AddressU: d3d.TEXTURE_ADDRESS_CLAMP, AddressV: d3d.TEXTURE_ADDRESS_CLAMP, AddressW: d3d.TEXTURE_ADDRESS_CLAMP,
		ComparisonFunc: d3d.COMPARISON_NEVER, MaxLOD: 3.4e38,
	})
	if err != nil {
		return d3d.ShaderResourceView{}, err
	}
	defer sampler.Release()
	view, err := dev.CreateShaderResourceView(target, nil)
	if err != nil {
		return d3d.ShaderResourceView{}, err
	}

	ctx.OMSetRenderTargets([]d3d.RenderTargetView{rtv}, d3d.DepthStencilView{})
	ctx.RSSetViewports(d3d.Viewport{Width: float32(width), Height: float32(height), MaxDepth: 1})
	ctx.RSSetState(d3d.RasterizerState{})
	ctx.OMSetBlendState(d3d.BlendState{}, [4]float32{}, 0xffffffff)
	ctx.OMSetDepthStencilState(d3d.DepthStencilState{}, 0)
	ctx.IASetInputLayout(d3d.InputLayout{})
	ctx.IASetPrimitiveTopology(d3d.PRIMITIVE_TOPOLOGY_TRIANGLELIST)
	ctx.VSSetShader(vs)
	ctx.PSSetShader(ps)
	ctx.PSSetShaderResources(0, source)
	ctx.PSSetSamplers(0, sampler)
	ctx.Draw(3, 0)
	// Neither texture may stay bound: the caller releases source, and target is sampled next.
	ctx.OMSetRenderTargets(nil, d3d.DepthStencilView{})
	ctx.PSSetShaderResources(0, d3d.ShaderResourceView{})
	ctx.GenerateMips(view)
	return view, nil
}

func unsafePointer(data []byte) unsafe.Pointer { return unsafe.Pointer(unsafe.SliceData(data)) }
