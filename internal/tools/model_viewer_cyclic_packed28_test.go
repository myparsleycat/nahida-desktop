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

var packedObjectAnimShader28 = strings.Replace(packedObjectAnimShader,
	"uint normal;\n    uint texcoord;\n    uint tangent;",
	"uint normal;\n    uint tangent;\n    uint color;\n    uint texcoord;\n    uint texcoord1;", 1)

func TestModelViewerCyclicPacked28LoadsAnimationAndDiffuseUVs(t *testing.T) {
	dir := t.TempDir()
	position := make([]byte, 3*28)
	for vertex := range 3 {
		offset := vertex * 28
		binary.LittleEndian.PutUint16(position[offset:], modelViewerFloatToHalfBits(float32(vertex+1)))
		binary.LittleEndian.PutUint16(position[offset+2:], modelViewerFloatToHalfBits(2))
		binary.LittleEndian.PutUint16(position[offset+4:], modelViewerFloatToHalfBits(3))
		binary.LittleEndian.PutUint16(position[offset+6:], modelViewerFloatToHalfBits(1))
		position[offset+8] = 0
		position[offset+9] = 0
		position[offset+10] = 127
		position[offset+11] = 0

		// Tangent, color, and second UV must never become the diffuse UVs.
		binary.LittleEndian.PutUint32(position[offset+12:], 0x7f007f00)
		binary.LittleEndian.PutUint32(position[offset+16:], 0xff000000)
		binary.LittleEndian.PutUint16(position[offset+20:], modelViewerFloatToHalfBits(0.25))
		binary.LittleEndian.PutUint16(position[offset+22:], modelViewerFloatToHalfBits(0.75))
		binary.LittleEndian.PutUint16(position[offset+24:], modelViewerFloatToHalfBits(0.5))
		binary.LittleEndian.PutUint16(position[offset+26:], modelViewerFloatToHalfBits(0.5))
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
if $light_on == 1
    drawindexed = auto
endif
[ResourceWashroomPosition]
[ResourceWashroomPosition.1]
stride = 28
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
`
	for name, data := range map[string][]byte{
		"mod.ini": []byte(ini), "anim.hlsl": []byte(packedObjectAnimShader28),
		"Washroom.buf": position, "WashroomA.ib": modelViewerUint32Bytes([]uint32{0, 1, 2}),
		"blend.buf": make([]byte, 3*32), "pose.buf": make([]byte, 4*2*48),
		"WashroomADiffuse.png": encodeTinyPNG(),
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
	geometry := meshes[0].geometry
	if !slices.Equal(geometry.Position, []float32{1, 2, 3, 2, 2, 3, 3, 2, 3}) {
		t.Fatalf("positions = %v; want half4 XYZ", geometry.Position)
	}
	if uv := geometry.Texcoord0; !slices.Equal(uv, []float32{0.25, 0.25, 0.25, 0.25, 0.25, 0.25}) {
		t.Fatalf("diffuse UVs = %v; want the first UV set at byte 20 with V flipped", uv)
	}

	service := New()
	payload, err := service.LoadModViewer(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = service.CleanupModelViewer(context.Background(), payload.MemorySessionID) })
	if len(payload.Meshes) != 1 || payload.Meshes[0].Bounds == nil {
		t.Fatalf("meshes = %+v", payload.Meshes)
	}
	if bounds := payload.Meshes[0].Bounds; math.IsNaN(bounds.Radius) || bounds.Radius <= 0 {
		t.Fatalf("bounds = %+v", bounds)
	}
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
	state := evaluateViewerTransport(payload, map[string]any{"light_on": 1})
	if len(state.Meshes) != 1 || !state.Meshes[0].Visible {
		t.Fatalf("meshes=%+v", state.Meshes)
	}
	if state.Meshes[0].TexKey != "diffuse::WashroomADiffuse.png" {
		t.Fatalf("material=%+v", state.Meshes[0])
	}
}
