package tools

import (
	"context"
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"nahida.live/desktop/internal/infra"
)

// Zlevir's cyclic object animation shader keeps the vertex record unpacked and
// blends only position and normal, so it advertises a 44-byte stride.
const inlineObjectAnimShader = `
// **** CYCLIC ANIMATION SHADER ****
struct VertexAttributes {
    float3 position;
    float3 normal;
    uint texcoord0;
    float2 texcoord1;
    uint2 tangent;
};
RWStructuredBuffer<VertexAttributes> rw_buffer : register(u5);
StructuredBuffer<VertexAttributes> base : register(t50);
StructuredBuffer<VertexAttributes> shapekey : register(t51);
Texture1D<float4> IniParams : register(t120);
#define FREQ IniParams[88].x
[numthreads(1, 1, 1)]
void main(uint3 threadID : SV_DispatchThreadID)
{
    uint i = threadID.x;
    VertexAttributes diff;
    diff.position = shapekey[i].position - base[i].position;
    diff.normal = shapekey[i].normal - base[i].normal;
    //diff.tangent = shapekey[i].tangent - base[i].tangent;
    rw_buffer[i].position += diff.position*(0.5*(sin(FREQ*30)+1));
    rw_buffer[i].normal += diff.normal*(0.5*(sin(FREQ*30)+1));
}
`

func writeInlineObjectVertex(buf []byte, offset int, x, y, z, u, v, nx, ny, nz float32) {
	binary.LittleEndian.PutUint32(buf[offset:], math.Float32bits(x))
	binary.LittleEndian.PutUint32(buf[offset+4:], math.Float32bits(y))
	binary.LittleEndian.PutUint32(buf[offset+8:], math.Float32bits(z))
	binary.LittleEndian.PutUint32(buf[offset+12:], math.Float32bits(nx))
	binary.LittleEndian.PutUint32(buf[offset+16:], math.Float32bits(ny))
	binary.LittleEndian.PutUint32(buf[offset+20:], math.Float32bits(nz))
	binary.LittleEndian.PutUint16(buf[offset+24:], modelViewerFloatToHalfBits(u))
	binary.LittleEndian.PutUint16(buf[offset+26:], modelViewerFloatToHalfBits(v))
}

func TestModelViewerInlineObjectShaderLayout(t *testing.T) {
	stride, offset, known := modelViewerInlineObjectShaderLayout(inlineObjectAnimShader)
	if !known || stride != 44 || offset != 24 {
		t.Fatalf("layout=%d/%d known=%t", stride, offset, known)
	}
	if _, _, known := modelViewerInlineObjectShaderLayout(
		`struct VertexAttributes { float3 position; float3 normal; float4 tangent; };`,
	); known {
		t.Fatal("character shape layout must not be treated as an inline object record")
	}
}

func TestModelViewerInlineObjectCyclicShapeLoadsAnimationAndDiffuseUVs(t *testing.T) {
	dir := t.TempDir()
	vertices := make([]byte, 3*44)
	writeInlineObjectVertex(vertices, 0, 1, 2, 3, 0.25, 0.75, 0, 1, 0)
	writeInlineObjectVertex(vertices, 44, 4, 5, 6, 0.5, 0.5, 0, 1, 0)
	writeInlineObjectVertex(vertices, 88, 7, 8, 9, 0, 1, 0, 1, 0)
	ini := `[Constants]
global $Speed = 0.2
global $Freq = 0
global $dt
post ResourceStove = copy_desc ResourceStove.1
post run = CustomShaderComputeAnim

[KeySeek]
key = VK_F6
$Freq = 1

[Present]
run = CustomShaderComputeAnim

[CustomShaderComputeAnim]
$Freq = $Freq + $Speed * $dt
if $Freq > 5.18364
    $Freq = -0.05236
endif
x88 = $Freq
cs-t50 = copy ResourceStove.1
cs-t51 = copy ResourceStove.2
cs = ./anim.hlsl
cs-u5 = copy ResourceStove.1
ResourceStove = ref cs-u5
Dispatch = 3, 1, 1

[TextureOverrideStove]
hash = d168f116
vb0 = ResourceStove

[TextureOverrideStoveBody]
hash = 79f0c7ff
match_first_index = 0
ib = ResourceStoveBodyIB
ps-t0 = ResourceStoveBodyDiffuse
ps-t1 = ResourceStoveBodyNormalMap

[ResourceStove]
[ResourceStove.1]
type = Buffer
stride = 44
filename = Stove1.buf
[ResourceStove.2]
type = Buffer
stride = 44
filename = Stove2.buf
[ResourceStoveBodyIB]
format = DXGI_FORMAT_R32_UINT
filename = StoveBody.ib
[ResourceStoveBodyDiffuse]
filename = StoveBodyDiffuse.png
[ResourceStoveBodyNormalMap]
filename = StoveBodyNormalMap.png
`
	writeTextureFile(t, dir, "StoveBodyDiffuse.png", encodeTinyPNG())
	writeTextureFile(t, dir, "StoveBodyNormalMap.png", encodeTinyPNG())
	for name, data := range map[string][]byte{
		"mod.ini": []byte(ini), "anim.hlsl": []byte(inlineObjectAnimShader),
		"Stove1.buf": vertices, "Stove2.buf": vertices,
		"StoveBody.ib": modelViewerUint32Bytes([]uint32{0, 1, 2}),
	} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	iniPath := filepath.Join(dir, "mod.ini")
	sections := parseModINI(ini)
	inline := collectModelViewerInlineObjectResources(dir, dir, sections)
	if inline[modelViewerNormalizeKey("Stove")].texcoordOffset != 24 ||
		inline[modelViewerNormalizeKey("Stove.1")].stride != 44 {
		t.Fatalf("inline resources = %+v", inline)
	}
	meshes, err := buildModelViewerDirectScannedMeshes(
		iniPath,
		sections,
		collectModelViewerDefaultVariables(sections),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(meshes) != 1 || meshes[0].geometry == nil {
		t.Fatalf("meshes = %#v", meshes)
	}
	geometry := meshes[0].geometry
	if !slices.Equal(geometry.Position, []float32{1, 2, 3, 4, 5, 6, 7, 8, 9}) {
		t.Fatalf("positions = %v", geometry.Position)
	}
	if !slices.Equal(geometry.Normal, []float32{0, 1, 0, 0, 1, 0, 0, 1, 0}) {
		t.Fatalf("normals = %v", geometry.Normal)
	}
	if want := []float32{0.25, 0.25, 0.5, 0.5, 0, 0}; !slices.Equal(geometry.Texcoord0, want) {
		t.Fatalf("diffuse UVs = %v; want the half2 word at byte 24 with V flipped", geometry.Texcoord0)
	}

	service := NewWithOptions(Options{Protocol: infra.NewProtocol()})
	payload, err := service.LoadModViewer(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = service.CleanupModelViewer(context.Background(), payload.MemorySessionID) })
	if len(payload.ComputeDeformers) != 1 || len(payload.Animations) != 1 {
		t.Fatalf("deformers=%d animations=%d", len(payload.ComputeDeformers), len(payload.Animations))
	}
	deformer, clip := payload.ComputeDeformers[0], payload.Animations[0]
	if deformer.Kind != modelViewerGIMIShapePoseKind || deformer.VertexCount != 3 ||
		deformer.Base.Stride != 44 || deformer.Pose != nil || len(deformer.ShapePasses) != 1 ||
		clip.DeformerID != deformer.ID || len(clip.Frames) < 2 {
		t.Fatalf("deformer=%+v clip=%+v", deformer, clip)
	}
	pass := deformer.ShapePasses[0]
	if pass.PhaseRate != 0.2 || pass.WrapAt != 5.18364 || pass.PhaseStart != -0.05236 ||
		pass.AngularScale != 30 || pass.Amplitude != 0.5 || pass.Bias != 0.5 {
		t.Fatalf("shape pass = %+v", pass)
	}
	state := evaluateViewerTransport(payload, nil)
	if len(state.Meshes) != 1 || state.Meshes[0].TexKey != "diffuse::StoveBodyDiffuse.png" {
		t.Fatalf("material = %+v", state.Meshes)
	}
}
