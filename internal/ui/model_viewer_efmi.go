package ui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"math"
	"path"
	"sync"

	"github.com/myparsleycat/fairy/d3d"

	"nahida.live/desktop/internal/infra"
	modelviewer "nahida.live/desktop/internal/tools/model_viewer"
)

type efmiLighting struct {
	Yaw, Pitch, Bias, Ambient, Strength  float64
	Light, SkinLight, Shadow, SkinShadow vec3
	Rim                                  []efmiRimLight
}

type efmiRimLight struct {
	Position, Direction, Color    vec3
	Cone                          [2]float64
	Range, Falloff, Width, Albedo float64
	Bone                          string
}

type efmiOverlay struct {
	Color              [4]float32
	Stencil            uint32
	OverHair, PreDepth bool
	Mask               string
}

type efmiStockMaterial struct {
	NoCast, NoRamp, UV1 bool
	Tile                [2]int
	Cutoff              float32
}

var efmiStockMaterials = sync.OnceValue(func() map[string]efmiStockMaterial {
	var materials map[string]efmiStockMaterial
	_ = readEFMIData("efmi_materials.json", &materials)
	return materials
})

var efmiPresets = sync.OnceValues(func() (map[string]efmiLighting, error) {
	var presets map[string]efmiLighting
	err := readEFMIData("efmi_lighting.json", &presets)
	return presets, err
})

var efmiOverlays = sync.OnceValues(func() (map[string]efmiOverlay, error) {
	var overlays map[string]efmiOverlay
	err := readEFMIData("efmi_overlays.json", &overlays)
	return overlays, err
})

func readEFMIData(name string, dst any) error {
	data, err := characterToonFiles.ReadFile("assets/" + characterToonDir + name)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, dst); err != nil {
		return fmt.Errorf("read EFMI %s: %w", name, err)
	}
	return nil
}

func efmiPreset(id string) efmiLighting {
	presets, _ := efmiPresets()
	if preset, ok := presets[id]; ok {
		return preset
	}
	return efmiLighting{
		Yaw: 10, Pitch: 32, Bias: -0.4, Ambient: 0.8, Strength: 0.9,
		Light: vec3{1, 1, 1}, SkinLight: vec3{1, 1, 1},
		Shadow: vec3{0.878, 0.93, 1.122}, SkinShadow: vec3{1.187, 0.927, 0.813},
	}
}

func (p efmiLighting) frame(frame *viewerFrameConstants, scene *viewerScene, opts viewerDrawOptions) {
	frame.toonParams[0][3] -= float32(p.Bias + 0.4)
	frame.efmiLight = efmiVector(p.Light.scale(p.Strength), 0)
	frame.efmiSkinLight = efmiVector(p.SkinLight.scale(p.Strength), p.Ambient)
	frame.efmiShadowTint = efmiVector(p.Shadow, 0)
	frame.efmiSkinShadowTint = efmiVector(p.SkinShadow, 0)
	if frame.toonParams[1][1] == 0 {
		return
	}
	camera := scene.world
	// Lighting presets are in the stock scene's upright coordinates. Undo the
	// viewer's upright transform before applying the model's current rotation.
	toModel := func(p vec3) vec3 {
		return vec3{
			camera[0]*p[0] + camera[4]*p[1] + camera[8]*p[2],
			camera[1]*p[0] + camera[5]*p[1] + camera[9]*p[2],
			camera[2]*p[0] + camera[6]*p[1] + camera[10]*p[2],
		}
	}
	origin := opts.world.point(vec3{})
	count := min(len(p.Rim), len(frame.efmiRimLights)/4)
	frame.efmiLight[3] = float32(count)
	for i, rim := range p.Rim[:count] {
		position, direction := toModel(rim.Position), toModel(rim.Direction)
		if motion := opts.skin; motion != nil && rim.Bone != "" {
			for node, bone := range motion.bones {
				if bone == viewerMotionHidden || motion.motion.NodeName(node) != rim.Bone {
					continue
				}
				var moved vec3
				for row := range 3 {
					moved[row] = float64(bone[row*4])*position[0] + float64(bone[row*4+1])*position[1] +
						float64(bone[row*4+2])*position[2] + float64(bone[row*4+3])
				}
				position, direction = moved, motion.turnBy(int32(node), direction)
				break
			}
		}
		cosOuter := math.Cos(rim.Cone[0] * math.Pi / 360)
		invCone := 0.0
		if rim.Cone[0] != 0 {
			invCone = 1 / max(0.0001, math.Cos(rim.Cone[1]*math.Pi/360)-cosOuter)
		}
		frame.efmiRimLights[i*4] = efmiVector(opts.world.point(position), 1/max(rim.Range, 0.0001))
		frame.efmiRimLights[i*4+1] = efmiVector(rim.Color, rim.Falloff)
		frame.efmiRimLights[i*4+2] = efmiVector(opts.world.point(direction).sub(origin).normalize(), rim.Width)
		frame.efmiRimLights[i*4+3] = efmiVector(vec3{cosOuter, invCone, rim.Albedo}, 0)
	}
}

func efmiVector(v vec3, w float64) [4]float32 {
	return [4]float32{float32(v[0]), float32(v[1]), float32(v[2]), float32(w)}
}

func (m *viewerMesh) efmiFaceAxes() (forward, right vec3) {
	forward, right = m.faceForward, m.efmiRight
	if forward.dot(forward) == 0 {
		forward = vec3{0, -1, 0}
	}
	if right.dot(right) == 0 {
		right = vec3{0, 0, 1}.cross(forward).normalize()
		if right.dot(right) == 0 {
			right = vec3{1, 0, 0}
		}
	}
	return forward, right
}

func (m *viewerMesh) measureEFMIFace() {
	var left, right, normal vec3
	var nl, nr int
	for v := range min(len(m.positions)/3, len(m.uvs)/4, len(m.normals)/3) {
		u, y := m.uvs[v*4], m.uvs[v*4+1]
		if u < 0.2 || u > 0.8 || y < 0.25 || y > 0.7 {
			continue
		}
		p := m.positions[v*3:]
		point := vec3{float64(p[0]), float64(p[1]), float64(p[2])}
		switch {
		case u < 0.4:
			left = left.add(point)
			nl++
		case u > 0.6:
			right = right.add(point)
			nr++
		default:
			n := m.normals[v*3:]
			normal = normal.add(vec3{float64(n[0]), float64(n[1]), float64(n[2])})
		}
	}
	if nl == 0 || nr == 0 || normal.dot(normal) == 0 {
		return
	}
	forward := normal.normalize()
	side := right.scale(1 / float64(nr)).sub(left.scale(1 / float64(nl)))
	m.faceForward = forward
	m.efmiRight = side.sub(forward.scale(side.dot(forward))).normalize()
}

func (r *viewerRenderer) initEFMITextures(dev d3d.Device) (err error) {
	if r.efmiCutouts.P != nil {
		return nil
	}
	overlays, err := efmiOverlays()
	if err != nil {
		return err
	}
	r.efmiMasks = make(map[string]d3d.ShaderResourceView)
	defer func() {
		if err != nil {
			for _, mask := range r.efmiMasks {
				mask.Release()
			}
			r.efmiMasks = nil
		}
	}()
	for _, overlay := range overlays {
		if overlay.Mask == "" || r.efmiMasks[overlay.Mask].P != nil {
			continue
		}
		data, err := characterToonFiles.ReadFile(path.Join("assets", characterToonDir, "efmi", overlay.Mask+".webp"))
		if err != nil {
			return err
		}
		texture, err := decodeViewerImage(data)
		if err != nil {
			return err
		}
		texture.generateMips = false
		view, err := texture.upload(dev, d3d.Context{})
		if err != nil {
			return err
		}
		r.efmiMasks[overlay.Mask] = view
	}
	data, err := characterToonFiles.ReadFile("assets/" + characterToonDir + "efmi_cutouts.png")
	if err != nil {
		return err
	}
	decoded, _, err := infra.DecodeImage(bytes.NewReader(data))
	if err != nil {
		return err
	}
	mask, ok := decoded.(*image.Gray)
	if !ok || mask.Rect.Dx() != 8*512 || mask.Rect.Dy() != 6*512 {
		return fmt.Errorf("invalid EFMI cutout atlas")
	}
	r.efmiCutouts, err = (&viewerTextureData{
		format: d3d.FORMAT_R8_UNORM, width: uint32(mask.Rect.Dx()), height: uint32(mask.Rect.Dy()),
		mips: [][]byte{mask.Pix}, pitches: []uint32{uint32(mask.Stride)},
	}).upload(dev, d3d.Context{})
	return err
}

const efmiShadowSize = 1024

func (r *viewerRenderer) efmiCutout(mesh *viewerMesh, c *viewerMeshConstants) d3d.ShaderResourceView {
	m := mesh.efmiMaterial
	if m.Cutoff <= 0 || c.flags&viewerFlagCutout != 0 {
		return d3d.ShaderResourceView{}
	}
	c.flags |= viewerFlagCutout | viewerFlagEFMIStockCutout
	c.efmiCutout = [4]float32{m.Cutoff, float32(m.Tile[0]), float32(m.Tile[1]), viewerFlag(m.UV1 && mesh.hasUV1)}
	return r.efmiCutouts
}

type viewerDepthTexture struct {
	texture d3d.Texture2D
	dsv     d3d.DepthStencilView
	srv     d3d.ShaderResourceView
}

func (d *viewerDepthTexture) release() {
	d.srv.Release()
	d.dsv.Release()
	d.texture.Release()
	*d = viewerDepthTexture{}
}

func (d *viewerDepthTexture) ensure(dev d3d.Device, width, height uint32) (err error) {
	if d.srv.P != nil {
		return nil
	}
	defer func() {
		if err != nil {
			d.release()
		}
	}()
	d.texture, err = dev.CreateTexture2D(&d3d.Texture2DDesc{
		Width: width, Height: height, MipLevels: 1, ArraySize: 1,
		Format: d3d.FORMAT_R32_TYPELESS, SampleDesc: d3d.SampleDesc{Count: 1},
		BindFlags: d3d.BIND_DEPTH_STENCIL | d3d.BIND_SHADER_RESOURCE,
	}, nil)
	if err != nil {
		return err
	}
	d.dsv, err = dev.CreateDepthStencilView(d.texture, &d3d.DSVDesc{
		Format: d3d.FORMAT_D32_FLOAT, ViewDimension: d3d.DSV_DIMENSION_TEXTURE2D,
	})
	if err != nil {
		return err
	}
	d.srv, err = dev.CreateShaderResourceView(d.texture, &d3d.SRVDesc{
		Format: d3d.FORMAT_R32_FLOAT, ViewDimension: d3d.SRV_DIMENSION_TEXTURE2D, U: [4]uint32{0, 1},
	})
	return err
}

func (r *viewerRenderer) initEFMIDepthStates(dev d3d.Device) (err error) {
	op := d3d.DepthStencilOpDesc{
		StencilFailOp: d3d.STENCIL_OP_KEEP, StencilDepthFailOp: d3d.STENCIL_OP_KEEP,
		StencilPassOp: d3d.STENCIL_OP_REPLACE, StencilFunc: d3d.COMPARISON_ALWAYS,
	}
	desc := d3d.DepthStencilDesc{
		DepthEnable: 1, DepthWriteMask: d3d.DEPTH_WRITE_MASK_ALL, DepthFunc: d3d.COMPARISON_LESS_EQUAL,
		StencilEnable: 1, StencilReadMask: 0xff, StencilWriteMask: 0xff, FrontFace: op, BackFace: op,
	}
	if r.efmiDepthStencil, err = dev.CreateDepthStencilState(&desc); err != nil {
		return err
	}
	op.StencilPassOp, op.StencilFunc = d3d.STENCIL_OP_KEEP, d3d.COMPARISON_EQUAL
	desc.FrontFace, desc.BackFace, desc.StencilWriteMask = op, op, 0
	desc.StencilReadMask = 4 | 16
	if r.efmiPreDepth, err = dev.CreateDepthStencilState(&desc); err != nil {
		return err
	}
	desc.DepthWriteMask = d3d.DEPTH_WRITE_MASK_ZERO
	for i, mask := range []uint8{2 | 4 | 16, 4 | 16} {
		desc.StencilReadMask = mask
		if r.efmiOverlayDepth[i], err = dev.CreateDepthStencilState(&desc); err != nil {
			return err
		}
	}
	raster := d3d.RasterizerDesc{
		FillMode: d3d.FILL_SOLID, CullMode: d3d.CULL_NONE, FrontCounterClockwise: 1,
		DepthClipEnable: 1, MultisampleEnable: 1, DepthBias: -1, SlopeScaledDepthBias: -1,
	}
	if r.efmiPreDepthRaster, err = dev.CreateRasterizerState(&raster); err != nil {
		return err
	}
	raster.DepthBias = -6
	r.efmiOverlayRaster, err = dev.CreateRasterizerState(&raster)
	return err
}

func efmiStencil(mesh *viewerMesh) uint32 {
	switch {
	case mesh.toonEye:
		return 4 | 16
	case mesh.toonHair:
		return 4 | 2
	case mesh.efmiFace || mesh.efmiSkin || mesh.efmiHead:
		return 4
	default:
		return 0
	}
}

func efmiDepthGeometry(
	scene *viewerScene, skin *viewerSkinGPU, originals []originalDraw,
) []originalDraw {
	var draws []originalDraw
	for i, mesh := range scene.meshes {
		if !mesh.drawn() || i >= len(scene.gpu.meshes) || scene.gpu.meshes[i].indexCount == 0 {
			continue
		}
		state := scene.original.fillState(i, mesh.state, scene.gpu.textures)
		blend, skinned := skin.blend(i, mesh.vertexCount)
		draws = append(
			draws,
			originalDraw{mesh: mesh, state: state, gpu: scene.gpu.meshes[i], blend: blend, skinned: skinned},
		)
	}
	draws = append(draws, originals...)
	opaque := draws[:0]
	for _, draw := range draws {
		tex, _ := scene.texture(draw.state.TexKey)
		if draw.mesh.overlayShadow || draw.mesh.layer != nil || draw.mesh.efmiMaterial.NoCast ||
			tex.flags&viewerFlagCoverage != 0 {
			continue
		}
		opaque = append(opaque, draw)
	}
	return opaque
}

func fitEFMIShadow(frame *viewerFrameConstants, casters []originalDraw, opts viewerDrawOptions) bool {
	minimum := vec3{math.Inf(1), math.Inf(1), math.Inf(1)}
	maximum := vec3{math.Inf(-1), math.Inf(-1), math.Inf(-1)}
	posed := false
	for _, draw := range casters {
		if draw.mesh.toonEye {
			continue
		}
		if draw.skinned {
			posed = true
		} else if b := draw.mesh.info.Bounds; b != nil {
			extendBounds(&minimum, &maximum, b.Min, b.Max, opts.world)
		}
	}
	if posed {
		extendBounds(&minimum, &maximum, opts.minimum, opts.maximum, identity4())
	}
	if math.IsInf(minimum[0], 0) {
		return false
	}
	center := minimum.add(maximum).scale(0.5)
	radius := maximum.sub(minimum).length() * 0.5
	light := vec3{float64(frame.toonLight[0]), float64(frame.toonLight[1]), float64(frame.toonLight[2])}
	up := vec3{0, 1, 0}
	if math.Abs(light[1]) > 0.99 {
		up = vec3{0, 0, 1}
	}
	lightView := lookAt(center.add(light.scale(radius+0.1)), center, up)
	hx, hy := 0.0, 0.0
	for corner := range 8 {
		point := minimum
		for axis := range 3 {
			if corner&(1<<axis) != 0 {
				point[axis] = maximum[axis]
			}
		}
		p := lightView.point(point)
		hx, hy = max(hx, math.Abs(p[0])), max(hy, math.Abs(p[1]))
	}
	hx, hy = hx+0.1, hy+0.1
	tx, ty := 2*hx/efmiShadowSize, 2*hy/efmiShadowSize
	lightView[3] = math.Round(lightView[3]/tx) * tx
	lightView[7] = math.Round(lightView[7]/ty) * ty
	projection := orthographic(hy, 0, radius*2+4.1)
	projection[0] = 1 / hx
	frame.shadowViewProj = projection.mul(lightView).float32s()
	frame.toonShadow = [4]float32{1, float32(3 * tx), float32(6 * tx), efmiShadowSize}
	return true
}

func (r *viewerRenderer) drawEFMIDepth(
	ctx d3d.Context, scene *viewerScene, draws []originalDraw, opts viewerDrawOptions,
	shadow bool, target d3d.DepthStencilView, width, height uint32,
) error {
	ctx.OMSetRenderTargets(nil, target)
	ctx.ClearDepthStencilView(target, d3d.CLEAR_DEPTH, 1, 0)
	ctx.RSSetViewports(d3d.Viewport{Width: float32(width), Height: float32(height), MaxDepth: 1})
	ctx.OMSetDepthStencilState(r.depthOn, 0)
	ctx.RSSetState(r.raster)
	ctx.PSSetShader(r.efmiDepthPS)
	if shadow {
		ctx.VSSetShader(r.shadowVS)
	} else {
		ctx.VSSetShader(r.rimVS)
	}
	for _, draw := range draws {
		if shadow && draw.mesh.toonEye {
			continue
		}
		c := viewerMeshConstants{morphWeight: morphWeight(draw.mesh, draw.gpu, opts.morph)}
		cutout, ok := scene.texture(draw.state.CutoutMapKey)
		if ok {
			c.flags |= viewerFlagCutout
		}
		if draw.skinned {
			c.flags |= viewerFlagSkinned
			ctx.VSSetShaderResources(14, draw.blend)
		}
		view := cutout.view
		if mask := r.efmiCutout(draw.mesh, &c); mask.P != nil {
			view = mask
		}
		ctx.PSSetShaderResources(2, view)
		if err := d3d.WriteData(ctx, r.meshCB, []viewerMeshConstants{c}); err != nil {
			return err
		}
		r.drawMesh(ctx, draw.gpu)
	}
	return nil
}

func (r *viewerRenderer) drawEFMIOverlays(
	ctx d3d.Context, scene *viewerScene, opts viewerDrawOptions, skin *viewerSkinGPU, originals []originalDraw,
) error {
	var draws []originalDraw
	for _, draw := range originals {
		if draw.mesh.efmiOverlay != nil {
			draws = append(draws, draw)
		}
	}
	if len(draws) == 0 {
		for i, mesh := range scene.meshes {
			if mesh.efmiOverlay == nil || !mesh.state.Visible || i >= len(scene.gpu.meshes) ||
				scene.gpu.meshes[i].indexCount == 0 {
				continue
			}
			blend, skinned := skin.blend(i, mesh.vertexCount)
			draws = append(draws, originalDraw{mesh: mesh, gpu: scene.gpu.meshes[i], blend: blend, skinned: skinned})
		}
	}
	if len(draws) == 0 {
		return nil
	}

	ctx.VSSetShader(r.vs)
	ctx.OMSetBlendState(r.multiply, [4]float32{}, 0xffffffff)
	for _, stencil := range []uint32{4, 20} {
		for _, draw := range draws {
			overlay := draw.mesh.efmiOverlay
			if overlay == nil || overlay.Stencil != stencil {
				continue
			}
			c := viewerMeshConstants{
				tint:        overlay.Color,
				flags:       viewerFlagEFMIOverlay,
				morphWeight: morphWeight(draw.mesh, draw.gpu, opts.morph),
			}
			c.tint[3] *= c.tint[3]
			mask := r.efmiMasks[overlay.Mask]
			if mask.P != nil {
				c.flags |= viewerFlagDiffuse
			}
			if draw.skinned {
				c.flags |= viewerFlagSkinned
				ctx.VSSetShaderResources(14, draw.blend)
			}
			if err := d3d.WriteData(ctx, r.meshCB, []viewerMeshConstants{c}); err != nil {
				return err
			}
			if overlay.PreDepth {
				ctx.OMSetDepthStencilState(r.efmiPreDepth, stencil)
				ctx.RSSetState(r.efmiPreDepthRaster)
				ctx.PSSetShader(d3d.PixelShader{})
				r.drawMesh(ctx, draw.gpu)
			}
			depth := r.efmiOverlayDepth[0]
			if overlay.OverHair {
				depth = r.efmiOverlayDepth[1]
			}
			ctx.OMSetDepthStencilState(depth, stencil)
			ctx.RSSetState(r.efmiOverlayRaster)
			ctx.PSSetShader(r.ps)
			ctx.PSSetShaderResources(0, mask)
			r.drawMesh(ctx, draw.gpu)
		}
	}
	ctx.OMSetBlendState(r.blend, [4]float32{}, 0xffffffff)
	return nil
}

func inferredEFMIOverlay(indices int) *efmiOverlay {
	overlays, _ := efmiOverlays()
	name := "M_hairshadow_common_01"
	if indices <= efmiEyeShadowMaxIndices {
		name = "M_eyeshadow_common_03"
	}
	if overlay, ok := overlays[name]; ok {
		return &overlay
	}
	return nil
}

func efmiOriginalMaterial(mesh *viewerMesh, part modelviewer.CharacterOriginalPart) {
	mesh.efmiMaterial = efmiStockMaterials()[part.Material]
	mesh.efmiSkin = part.Shader == "Qebg"
	mesh.toonHair = mesh.toonHair || part.Shader == "Qktu"
	var bounds viewerMotionBox
	for i := 0; i+2 < len(part.Positions); i += 3 {
		p := part.Positions[i:]
		bounds.add(vec3{float64(p[0]), float64(p[1]), float64(p[2])})
	}
	if bounds.ok {
		mesh.info.Bounds = &modelviewer.ModelViewerBounds{Min: bounds.minimum, Max: bounds.maximum}
	}
	if part.Role == modelviewer.OriginalRoleOverlay {
		mesh.overlayShadow = true
		overlays, _ := efmiOverlays()
		if overlay, ok := overlays[part.Material]; ok {
			mesh.efmiOverlay = &overlay
		}
	}
}
