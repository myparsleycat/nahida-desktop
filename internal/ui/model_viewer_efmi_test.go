package ui

import (
	"math"
	"testing"
	"unsafe"

	"github.com/myparsleycat/fairy/d3d"

	modelviewer "nahida.live/desktop/internal/tools/model_viewer"
)

func TestEFMIDepthPassCutout(t *testing.T) {
	dev, ctx, _, err := d3d.CreateDeviceWithOptions(d3d.DeviceOptions{MinFeatureLevel: d3d.FEATURE_LEVEL_11_0})
	if err != nil {
		t.Fatal(err)
	}
	defer dev.Release()
	defer ctx.Release()

	var renderer viewerRenderer
	defer renderer.release()
	if err := renderer.create(dev); err != nil {
		t.Fatal(err)
	}
	if err := renderer.initEFMITextures(dev); err != nil {
		t.Fatal(err)
	}
	gpu, err := newViewerMeshGPU(dev, viewerMeshUpload{
		current: []float32{-1, -1, 0.5, 0, 0, 1, -1, 1, 0.5, 0, 0, 1, 1, -1, 0.5, 0, 0, 1},
		uvs:     make([]float32, 12), extra: make([]byte, 3*viewerVertexExtraStride), indices: []uint32{0, 1, 2},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer gpu.vertices.Release()
	defer gpu.uvs.Release()
	defer gpu.extra.Release()
	defer gpu.indices.Release()
	mask, err := (&viewerTextureData{
		format: d3d.FORMAT_R8_UNORM, width: 1, height: 1, mips: [][]byte{{0}}, pitches: []uint32{1},
	}).upload(dev, ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer mask.Release()
	scene := &viewerScene{gpu: &viewerGPUSet{textures: map[string]viewerTextureGPU{"mask": {view: mask}}}}

	const size = 4
	var depth viewerDepthTexture
	defer depth.release()
	if err := depth.ensure(dev, size, size); err != nil {
		t.Fatal(err)
	}
	readback, err := dev.CreateTexture2D(&d3d.Texture2DDesc{
		Width: size, Height: size, MipLevels: 1, ArraySize: 1,
		Format: d3d.FORMAT_R32_TYPELESS, SampleDesc: d3d.SampleDesc{Count: 1},
		Usage: d3d.USAGE_STAGING, CPUAccessFlags: d3d.CPU_ACCESS_READ,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer readback.Release()

	mat := identity4().float32s()
	frame := viewerFrameConstants{viewProj: mat, world: mat, shadowViewProj: mat}
	if err := d3d.WriteData(ctx, renderer.frameCB, []viewerFrameConstants{frame}); err != nil {
		t.Fatal(err)
	}
	ctx.IASetInputLayout(renderer.layout)
	ctx.IASetPrimitiveTopology(d3d.PRIMITIVE_TOPOLOGY_TRIANGLELIST)
	ctx.VSSetConstantBuffers(0, renderer.frameCB, renderer.meshCB)
	ctx.PSSetConstantBuffers(0, renderer.frameCB, renderer.meshCB)
	ctx.PSSetSamplers(0, renderer.sampler, renderer.clamp)

	for _, pass := range []struct {
		name   string
		shadow bool
	}{{"camera", false}, {"key light", true}} {
		for _, sample := range []struct {
			texture string
			want    float64
		}{{"", 0.5}, {"mask", 1}} {
			draw := originalDraw{
				mesh: &viewerMesh{}, state: modelviewer.ViewerMeshState{CutoutMapKey: sample.texture}, gpu: gpu,
			}
			if err := renderer.drawEFMIDepth(ctx, scene, []originalDraw{draw}, viewerDrawOptions{},
				pass.shadow, depth.dsv, size, size); err != nil {
				t.Fatal(err)
			}
			ctx.OMSetRenderTargets(nil, d3d.DepthStencilView{})
			ctx.CopyResource(readback, depth.texture)
			mapped, err := ctx.Map(readback, d3d.MAP_READ)
			if err != nil {
				t.Fatal(err)
			}
			got := float64(*(*float32)(unsafe.Add(mapped.Data, 2*mapped.RowPitch)))
			ctx.Unmap(readback)
			if math.Abs(got-sample.want) > 0.0001 {
				t.Errorf("%s depth with %q = %g, want %g", pass.name, sample.texture, got, sample.want)
			}
		}
	}
}
