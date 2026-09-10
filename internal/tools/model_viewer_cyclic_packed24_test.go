package tools

import (
	"context"
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

var packedObjectAnimShader24 = strings.Replace(packedObjectAnimShader,
	"uint texcoord;\n    uint tangent;",
	"uint tangent;\n    uint texcoord;\n    uint texcoord1;", 1)

func TestModelViewerCyclicPacked24LoadsAnimationAndDiffuseUVs(t *testing.T) {
	dir := t.TempDir()
	position := make([]byte, 3*24)
	for vertex := range 3 {
		offset := vertex * 24
		writePackedObjectVertex(position, offset, float32(vertex+1), 2, 3, 0, 0, 0, 0, 127)

		// Tangent bytes must never become UVs; the two UV sets follow them.
		binary.LittleEndian.PutUint32(position[offset+12:], 0x7f007f00)
		binary.LittleEndian.PutUint16(position[offset+16:], modelViewerFloatToHalfBits(0.25))
		binary.LittleEndian.PutUint16(position[offset+18:], modelViewerFloatToHalfBits(0.75))
		binary.LittleEndian.PutUint16(position[offset+20:], modelViewerFloatToHalfBits(0.5))
		binary.LittleEndian.PutUint16(position[offset+22:], modelViewerFloatToHalfBits(0.5))
	}
	ini := `[Constants]
global $VG_count = 2
global $Freq = 0
global $dt
global $start_frame = 1
global $end_frame = 5
global persist $light_on = 1
post ResourceWashroomPosition = copy_desc ResourceWashroomPosition.1
post run = CustomShaderComputeAnim
[KeyLight]
key = alt ]
type = cycle
$light_on = 0,1
[CustomShaderComputeAnim]
$Freq = $Freq + 24 * $dt
if $Freq > $end_frame-2
    $Freq = $start_frame
endif
x88 = $Freq
x89 = $VG_count
cs-t50 = copy ResourceWashroomPosition.1
cs-t51 = copy ResourceWashroomBlend
cs-t52 = copy ResourceWashroomPose
cs = anim.hlsl
cs-u5 = copy ResourceWashroomPosition.1
ResourceWashroomPosition = ref cs-u5
Dispatch = 3, 1, 1
[TextureOverrideWashroom]
hash = 11111111
vb0 = ResourceWashroomPosition
[TextureOverrideWashroomA]
hash = 22222222
ib = ResourceWashroomAIB
ps-t5 = ResourceWashroomADiffuse
ps-t6 = ResourceWashroomANormalMap
ps-t7 = ResourceWashroomALightMap
if $light_on == 1
    run = CustomShaderLight
else
    drawindexed = auto
endif
[CustomShaderLight]
handling = skip
drawindexed = auto
[ResourceWashroomPosition]
[ResourceWashroomPosition.1]
stride = 24
filename = Washroom.buf
[ResourceWashroomBlend]
stride = 32
filename = blend.buf
[ResourceWashroomPose]
stride = 48
filename = pose.buf
[ResourceWashroomAIB]
format = DXGI_FORMAT_R32_UINT
filename = WashroomA.ib
[ResourceWashroomADiffuse]
filename = WashroomADiffuse.png
[ResourceWashroomANormalMap]
filename = WashroomANormalMap.png
[ResourceWashroomALightMap]
filename = WashroomALightMap.png
`
	for name, data := range map[string][]byte{
		"mod.ini": []byte(ini), "anim.hlsl": []byte(packedObjectAnimShader24),
		"Washroom.buf": position, "WashroomA.ib": modelViewerUint32Bytes([]uint32{0, 1, 2}),
		"blend.buf": make([]byte, 3*32), "pose.buf": make([]byte, 4*2*48),
		"WashroomADiffuse.png": encodeTinyPNG(), "WashroomANormalMap.png": encodeTinyPNG(),
		"WashroomALightMap.png": encodeTinyPNG(),
	} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	sections := parseModINI(ini)
	meshes, err := buildModelViewerDirectScannedMeshes(filepath.Join(dir, "mod.ini"), sections,
		collectModelViewerDefaultVariables(sections))
	if err != nil {
		t.Fatal(err)
	}
	if len(meshes) != 1 || meshes[0].geometry == nil {
		t.Fatalf("meshes = %+v", meshes)
	}
	if uv := meshes[0].geometry.Texcoord0; !slices.Equal(uv, []float32{0.25, 0.25, 0.25, 0.25, 0.25, 0.25}) {
		t.Fatalf("diffuse UVs = %v; want the first UV set at byte 16 with V flipped", uv)
	}

	service := New()
	payload, err := service.LoadModViewer(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = service.CleanupModelViewer(context.Background(), payload.MemorySessionID) })
	if len(payload.ComputeDeformers) != 1 || len(payload.Animations) != 1 {
		t.Fatalf("deformers=%d animations=%d", len(payload.ComputeDeformers), len(payload.Animations))
	}
	d, clip := payload.ComputeDeformers[0], payload.Animations[0]
	if d.Kind != modelViewerPackedObjectKind || d.VertexCount != 3 || d.Pose == nil ||
		d.Pose.BoneCount != 2 || d.Pose.FrameCount != 4 || clip.DeformerID != d.ID ||
		clip.FrameStart != 1 || clip.FrameEnd != 3 || clip.FPS != 24 {
		t.Fatalf("deformer=%+v clip=%+v", d, clip)
	}
	if d.Base.Encoding != modelViewerPackedFloatEncoding || d.Base.Stride != 28 {
		t.Fatalf("base was not decoded for the renderer: %+v", d.Base)
	}
	data := readModelViewerProtocolBytes(t, service.protocol, d.Base.URL)
	if len(data) != 3*28 {
		t.Fatalf("decoded base bytes=%d", len(data))
	}
	for vertex := range 3 {
		if x := math.Float32frombits(binary.LittleEndian.Uint32(data[vertex*28:])); x != float32(vertex+1) {
			t.Fatalf("decoded vertex %d x=%g", vertex, x)
		}
	}
	for _, light := range []float64{0, 1} {
		state := evaluateViewerTransport(payload, map[string]any{"light_on": light})
		if len(state.Meshes) != 1 || !state.Meshes[0].Visible {
			t.Fatalf("light=%g meshes=%+v", light, state.Meshes)
		}
		mesh := state.Meshes[0]
		if mesh.TexKey != "diffuse::WashroomADiffuse.png" ||
			mesh.NormalMapKey != "normal_map::WashroomANormalMap.png" ||
			mesh.LightMapKey != "light_map::WashroomALightMap.png" {
			t.Fatalf("light=%g material=%+v", light, mesh)
		}
		if payload.Textures[mesh.TexKey].Role != "diffuse" {
			t.Fatalf("diffuse payload=%+v", payload.Textures[mesh.TexKey])
		}
	}
}

func TestModelViewerCyclicPacked24RejectsMismatchedStride(t *testing.T) {
	dir := t.TempDir()
	for name, data := range map[string][]byte{
		"anim.hlsl": []byte(packedObjectAnimShader24),
		"base.buf":  make([]byte, 3*20), "blend.buf": make([]byte, 3*32), "pose.buf": make([]byte, 2*2*48),
	} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	sections := parseModINI(`[Constants]
global $bones = 2
global $freq = 0
post ResourcePosition = copy_desc ResourceBase
post run = CustomShaderAnim
[CustomShaderAnim]
x88 = $freq
x89 = $bones
cs-t50 = ResourceBase
cs-t51 = ResourceBlend
cs-t52 = ResourcePose
cs = anim.hlsl
cs-u5 = copy ResourceBase
ResourcePosition = ref cs-u5
Dispatch = 3, 1, 1
[ResourcePosition]
[ResourceBase]
stride = 20
filename = base.buf
[ResourceBlend]
stride = 32
filename = blend.buf
[ResourcePose]
stride = 48
filename = pose.buf
`)
	resources := resolveModelViewerEffectiveResources(sections, collectModelViewerResources(sections))
	meshes := []modelViewerDirectMesh{
		{id: "mesh", positionFile: "base.buf", geometry: &modelViewerGeometry{VertexCount: 3}},
	}
	if deformer, clips := detectModelViewerComputeAnimation(
		dir,
		dir,
		"",
		sections,
		resources,
		meshes,
		nil,
	); deformer != nil ||
		len(clips) != 0 {
		t.Fatal("24-byte shader accepted a 20-byte resource")
	}
}
