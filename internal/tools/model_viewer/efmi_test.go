package modelviewer

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"nahida.live/desktop/internal/infra"
)

func writeEFMIComponentBuffers(t *testing.T, dir, component string) {
	t.Helper()
	meshes := filepath.Join(dir, "Meshes")
	if err := os.MkdirAll(meshes, 0o700); err != nil {
		t.Fatal(err)
	}
	pos := make([]byte, 3*16)
	for vertex := range 3 {
		binary.LittleEndian.PutUint32(pos[vertex*16:], math.Float32bits(float32(vertex)))
	}
	tc := make([]byte, 3*12)
	for vertex, uv := range [][2]float32{{0, .25}, {1, .75}, {.5, .5}} {
		binary.LittleEndian.PutUint32(tc[vertex*12:], math.Float32bits(uv[0]))
		binary.LittleEndian.PutUint32(tc[vertex*12+4:], math.Float32bits(uv[1]))
	}
	files := map[string][]byte{
		component + "_VB0.buf": pos,
		component + "_VB1.buf": tc,
		component + "_VB2.buf": make([]byte, 3*12),
		component + "_IB.buf":  {0, 0, 1, 0, 2, 0},
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(meshes, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func writeEFMIDumpBuffers(t *testing.T, dir string) {
	t.Helper()
	bufferDir := filepath.Join(dir, "Buffer")
	if err := os.MkdirAll(bufferDir, 0o700); err != nil {
		t.Fatal(err)
	}
	pos := make([]byte, 3*16)
	for vertex := range 3 {
		binary.LittleEndian.PutUint32(pos[vertex*16:], math.Float32bits(float32(vertex)))
	}
	tc := make([]byte, 3*12)
	for vertex, uv := range [][2]float32{{0, .25}, {1, .75}, {.5, .5}} {
		binary.LittleEndian.PutUint32(tc[vertex*12:], math.Float32bits(uv[0]))
		binary.LittleEndian.PutUint32(tc[vertex*12+4:], math.Float32bits(uv[1]))
	}
	files := map[string][]byte{
		"809f7872-114840-0-Position.buf": pos,
		"809f7872-114840-0-Texcoord.buf": tc,
		"809f7872-114840-0-Blend.buf":    make([]byte, 3*12),
		"809f7872-114840-0-Index.buf":    modelViewerUint32Bytes([]uint32{0, 1, 2}),
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(bufferDir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

const efmiArmatureShader = `
struct PoseAttributes { float3 S; float3 T; float4 QR; float4 QD; };
RWBuffer<float4> rw_buffer : register(u5);
Buffer<float> base : register(t110);
Buffer<uint4> blend : register(t111);
StructuredBuffer<PoseAttributes> pose : register(t112);
#define TIME IniParams[88].x
#define VG_COUNT IniParams[89].x
void main() {
    float4 pos = float4(in_position.x, in_position.z, -in_position.y, 1.0f);
    norm = float3(norm.x, -norm.z, norm.y);
    int4 idx_prev = frame * vg_count + indices;
    int4 idx_next = (frame + 1) * vg_count + indices;
    pos.xyz = pos.xyz * scale + bias;
    float sign1 = sign(dot(p0_prev.QR, p1_prev.QR));
    qr /= qr_len;
    qd /= qr_len;
    float t0 = 2.0f*(-qdw*qx + qdx*qw - qdy*qz + qdz*qy);
    rw_buffer[i] = float4(pos_result.x, -pos_result.z, pos_result.y,
        asfloat(norm2packed(float3(norm_result.x, norm_result.z, -norm_result.y), in_normal)));
}`

const efmiArmatureINI = `
[Constants]
global $required_efmi_version = 1.46
global $mod_enabled = 0
global $lod_level = 0
global $dt
global $Freq_shape = 0
global $Freq_armature = 0
global $VG_count = 2
global persist $anime_state = 1
global $start_frame = 0
global $end_frame = 2
run = CommandListInitShapes
[Present]
$Freq_shape = $Freq_shape + 3* $dt
$Freq_armature = $Freq_armature +20* $dt
if $Freq_armature > $end_frame - 1
    $Freq_armature = $start_frame
endif
if $anime_state == 0
    $start_frame = 0
    $end_frame = 2
elif $anime_state == 1
    $start_frame = 1
    $end_frame = 3
endif
run = CommandListShapeAnim
[CommandListShapeAnim]
if $Freq_shape > 31.4
    $Freq_shape = 0
endif
$component_id = 0
$shapekey_id = 1
$shapekey_value = (sin($Freq_shape+1.57)+1)/2
run = CommandListSetShapeKey
[CommandListInitShapes]
$component_id = 0
Pool_ShapeKeyBatchConfigs[$component_id] = ref Resource_Component0_Configs
Pool_ShapeKeyVertexIds[$component_id] = ref Resource_Component0_Ids
Pool_ShapeKeyVertexOffsets[$component_id] = ref Resource_Component0_Deltas
Pool_VertexPositions[$component_id] = copy Resource_Component0_VB0
Pool_ShapeKeyedPositionsRW[$component_id] = copy Resource_Component0_VB0
$component_id = 9
Pool_ShapeKeyedPositionsRW[$component_id] = copy Resource_Component9_VB0
[CommandList_Component_DrawInstances]
handling = skip
run = CommandList\EFMIv1\Component_DrawInstances
[CommandList_Draw_Component0]
ib = ref Resource_Component0_IB
run = CustomShaderArmature_C0
vb0 = ref Resource_Component0_VB0_anim
vb1 = ref Resource_Component0_VB1
drawindexed = 6, 0, 0
[CommandList_Draw_Component1]
ib = ref Resource_Component1_IB
run = CustomShaderArmature_C1
vb0 = ref Resource_Component1_VB0_anim
vb1 = ref Resource_Component1_VB1
drawindexed = 3, 0, 0
[CommandList_Draw_Component2]
ib = ref Resource_Component2_IB
vb0 = ref Resource_Component2_VB0
vb1 = ref Resource_Component2_VB1
;drawindexed = 3, 0, 0
[TextureOverride_EntryPoint_Component0]
hash = 11111111
if $mod_enabled && DRAW_TYPE == 4
    CommandList\EFMIv1\Callback_Component_DrawCustom = ref CommandList_Draw_Component0
    run = CommandList_Component_DrawInstances
endif
[TextureOverride_EntryPoint_Component1]
hash = 22222222
if $mod_enabled && DRAW_TYPE == 4
    CommandList\EFMIv1\Callback_Component_DrawCustom = ref CommandList_Draw_Component1
    run = CommandList_Component_DrawInstances
endif
[TextureOverride_EntryPoint_Component2]
hash = 33333333
if $mod_enabled && DRAW_TYPE == 4
    CommandList\EFMIv1\Callback_Component_DrawCustom = ref CommandList_Draw_Component2
    run = CommandList_Component_DrawInstances
endif
[Resource_Component0_VB0_anim]
type = RWBuffer
format = R32G32B32A32_FLOAT
stride = 16
array = 3
[CustomShaderArmature_C0]
cs = ./anim.hlsl
    x88 = $Freq_armature
    x89 = $VG_count
    cs-t110 = copy Pool_ShapeKeyedPositionsRW[$uid]
    cs-t111 = ref Resource_Component0_VB2
    cs-t112 = ref Resource_pose
    cs-u5 = ref Resource_Component0_VB0_anim
    Dispatch = 1, 1, 1
    cs-u5 = null
    cs-t110 = null
    cs-t111 = null
    cs-t112 = null
[Resource_Component1_VB0_anim]
type = RWBuffer
format = R32G32B32A32_FLOAT
stride = 16
array = 3
[CustomShaderArmature_C1]
cs = ./anim.hlsl
    x88 = $Freq_armature
    x89 = $VG_count
    cs-t110 = copy Resource_Component1_VB0
    cs-t111 = ref Resource_Component1_VB2
    cs-t112 = ref Resource_pose
    cs-u5 = ref Resource_Component1_VB0_anim
    Dispatch = 1, 1, 1
    cs-u5 = null
[Resource_pose]
type = StructuredBuffer
stride = 56
filename = pose.buf
[Resource_Component0_Configs]
type = Buffer
stride = 16
filename = Meshes/Component0_Configs.buf
[Resource_Component0_Ids]
type = Buffer
stride = 4
filename = Meshes/Component0_Ids.buf
[Resource_Component0_Deltas]
type = Buffer
format = DXGI_FORMAT_R16_FLOAT
stride = 2
filename = Meshes/Component0_Deltas.buf
`

func writeEFMIArmatureFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	var ini strings.Builder
	ini.WriteString(efmiArmatureINI)
	for _, component := range []string{"Component0", "Component1", "Component2", "Component9"} {
		writeEFMIComponentBuffers(t, dir, component)
		// The blend record is four 16-bit weights and four 16-bit bones.
		blend := make([]byte, 3*16)
		for vertex := range 3 {
			binary.LittleEndian.PutUint16(blend[vertex*16:], 65535)
			binary.LittleEndian.PutUint16(blend[vertex*16+8:], 1)
		}
		if err := os.WriteFile(filepath.Join(dir, "Meshes", component+"_VB2.buf"), blend, 0o600); err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&ini, `[Resource_%[1]s_IB]
type = Buffer
format = DXGI_FORMAT_R16_UINT
filename = Meshes/%[1]s_IB.buf
[Resource_%[1]s_VB0]
type = Buffer
stride = 16
filename = Meshes/%[1]s_VB0.buf
[Resource_%[1]s_VB1]
type = Buffer
stride = 12
filename = Meshes/%[1]s_VB1.buf
[Resource_%[1]s_VB2]
type = Buffer
stride = 16
filename = Meshes/%[1]s_VB2.buf
`, component)
	}

	// Shape key 1 moves vertex 2 by +1 on X; key 0 holds no entries.
	config := make([]byte, modelViewerEFMIShapeConfigBytes)
	for key := 2; key < 128; key++ {
		binary.LittleEndian.PutUint32(config[3*16+key*4:], 1)
	}
	deltas := make([]byte, 6)
	binary.LittleEndian.PutUint16(deltas, 0x3c00)
	ids := make([]byte, 4)
	binary.LittleEndian.PutUint32(ids, 2)
	pose := make([]byte, 4*2*56)
	for name, data := range map[string][]byte{
		"mod.ini":                       []byte(ini.String()),
		"anim.hlsl":                     []byte(efmiArmatureShader),
		"pose.buf":                      pose,
		"Meshes/Component0_Configs.buf": config,
		"Meshes/Component0_Ids.buf":     ids,
		"Meshes/Component0_Deltas.buf":  deltas,
	} {
		if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(name)), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// Components skinned by one pose buffer must form one deformer: a clip drives a
// single deformer, so a split or misplaced vertex range would tear them apart.
func TestLoadModViewerMergesEFMIDrawTimeArmatureComponents(t *testing.T) {
	fixture := loadViewerDir(t, writeEFMIArmatureFixture(t))
	result := fixture.result

	if len(result.Meshes) != 2 {
		t.Fatalf("meshes = %#v, want the two drawn components", result.Meshes)
	}
	if indices := readViewerMesh(t, fixture.protocol, result.Meshes[0].GeometryURL).Indices; len(indices) != 3 {
		t.Fatalf("overflowing draw indices = %v, want the 3 the buffer holds", indices)
	}
	if len(result.ComputeDeformers) != 1 {
		t.Fatalf("deformers = %#v", result.ComputeDeformers)
	}
	deformer := result.ComputeDeformers[0]
	if deformer.VertexCount != 6 || len(deformer.MeshIDs) != 2 || deformer.Pose == nil ||
		deformer.Pose.DualQuaternionVariant != modelViewerEFMIArmatureVariant ||
		deformer.Pose.BoneCount != 2 || deformer.Pose.FrameCount != 4 {
		t.Fatalf("deformer = %+v", deformer)
	}

	second := readModelViewerProtocolBytes(t, fixture.protocol, deformer.MeshSourceIndices[deformer.MeshIDs[1]])
	if got := binary.LittleEndian.Uint32(second); got != 3 {
		t.Fatalf("second component starts at record %d, want 3", got)
	}
	base := readModelViewerProtocolBytes(t, fixture.protocol, deformer.Base.URL)
	if len(base) != 6*28 || math.Float32frombits(binary.LittleEndian.Uint32(base[12:])) != 1 {
		t.Fatalf("base records = %d bytes", len(base))
	}
	blend := readModelViewerProtocolBytes(t, fixture.protocol, deformer.Pose.Blend.URL)
	if len(blend) != 6*32 || math.Float32frombits(binary.LittleEndian.Uint32(blend)) != 1 ||
		binary.LittleEndian.Uint32(blend[16:]) != 1 {
		t.Fatalf("blend records = %v", blend[:32])
	}

	if len(deformer.ShapePasses) != 1 {
		t.Fatalf("shape passes = %+v", deformer.ShapePasses)
	}
	pass := deformer.ShapePasses[0]
	if pass.PhaseRate != 3 || pass.WrapAt != 31.4 || pass.PhaseOffset != 1.57 || pass.Amplitude != 0.5 {
		t.Fatalf("shape pass = %+v", pass)
	}
	target := readModelViewerProtocolBytes(t, fixture.protocol, pass.Target.URL)
	for vertex := range 6 {
		delta := math.Float32frombits(binary.LittleEndian.Uint32(target[vertex*28:])) -
			math.Float32frombits(binary.LittleEndian.Uint32(base[vertex*28:]))
		if want := float32(map[bool]int{true: 1, false: 0}[vertex == 2]); delta != want {
			t.Fatalf("vertex %d shape delta = %v, want %v", vertex, delta, want)
		}
	}

	if len(result.Animations) != 2 || result.Animations[0].FrameStart != 0 || result.Animations[0].FrameEnd != 1 ||
		result.Animations[1].FrameStart != 1 || result.Animations[1].FrameEnd != 2 || result.Animations[1].FPS != 20 {
		t.Fatalf("clips = %+v", result.Animations)
	}
}

func TestModelViewerScannerParsesDrawIndexedInstanced(t *testing.T) {
	sections := parseModINI(`[TextureOverrideBody]
ib = ResourceBodyIB
vb0 = ResourcePos
vb1 = ResourceTc
vb2 = ResourceBlend
handling = skip
drawindexedinstanced = 3, 0, 0
drawindexedinstanced = 6, INSTANCE_COUNT, 3, 2, FIRST_INSTANCE`)
	records, err := collectModelViewerDirectDrawRecords(sections, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].auto || records[0].draw.IndexCount != 6 || records[0].draw.StartIndex != 3 ||
		records[0].draw.BaseVertex != 2 {
		t.Fatalf("records = %#v", records)
	}
}

func TestModelViewerScannerSkipsLODOverrides(t *testing.T) {
	sections := parseModINI(`[TextureOverride_Component0]
ib = Resource_Component0_IB
vb0 = Resource_Component0_VB0
vb1 = Resource_Component0_VB1
run = CommandList_Draw_Component0
[TextureOverride_Component0_LOD0]
ib = Resource_Component0_IB
vb0 = Resource_Component0_VB0
vb1 = Resource_Component0_VB1
run = CommandList_Draw_Component0
[CommandList_Draw_Component0]
drawindexedinstanced = 3, INSTANCE_COUNT, 0, 0, FIRST_INSTANCE`)
	records, err := collectModelViewerDirectDrawRecords(sections, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].sectionName != "_Component0" || records[0].draw.IndexCount != 3 {
		t.Fatalf("records = %#v", records)
	}
}

func TestLoadModViewerLoadsEFMIDumpStyleInstancedDraw(t *testing.T) {
	dir := t.TempDir()
	writeEFMIDumpBuffers(t, dir)
	writeTextureFile(t, dir, "Texture/809f7872-114840-0-DiffuseMap.png", encodeTinyPNG())
	if err := os.WriteFile(filepath.Join(dir, "mod.ini"), []byte(`[TextureOverride_809f7872_114840_0]
hash = 809f7872
handling = skip
ps-t16 = Resource-809f7872-114840-0-DiffuseMap
vb2 = Resource_809f7872_114840_0_Blend
vb0 = Resource_809f7872_114840_0_Position
vb1 = Resource_809f7872_114840_0_Texcoord
ib = Resource_809f7872_114840_0_Index
drawindexedinstanced = 3, INSTANCE_COUNT, 0, 0, FIRST_INSTANCE
[Resource_809f7872_114840_0_Position]
type = Buffer
stride = 16
filename = Buffer/809f7872-114840-0-Position.buf
[Resource_809f7872_114840_0_Texcoord]
type = Buffer
stride = 12
filename = Buffer/809f7872-114840-0-Texcoord.buf
[Resource_809f7872_114840_0_Blend]
type = Buffer
stride = 12
filename = Buffer/809f7872-114840-0-Blend.buf
[Resource_809f7872_114840_0_Index]
type = Buffer
format = DXGI_FORMAT_R32_UINT
filename = Buffer/809f7872-114840-0-Index.buf
[Resource-809f7872-114840-0-DiffuseMap]
filename = Texture/809f7872-114840-0-DiffuseMap.png
`), 0o600); err != nil {
		t.Fatal(err)
	}
	fixture := loadViewerDir(t, dir)
	if len(fixture.result.Meshes) != 1 {
		t.Fatalf("meshes = %#v", fixture.result.Meshes)
	}
	mesh := fixture.result.Meshes[0]
	if texKey(mesh) == "" || fixture.result.Textures[texKey(mesh)].Role != "diffuse" {
		t.Fatalf("texKey=%q textures=%#v", texKey(mesh), fixture.result.Textures)
	}
	uvs := readViewerMesh(t, fixture.protocol, mesh.GeometryURL).UVs
	if len(uvs) < 2 || math.Abs(float64(uvs[1]-.75)) > 1e-5 {
		t.Fatalf("uvs = %v", uvs)
	}
}

func TestModelViewerScannerFollowsEFMICallbackDraw(t *testing.T) {
	sections := parseModINI(`[CommandList_Draw_Component0]
ib = ref Resource_Component0_IB
vb0 = ref Resource_Component0_VB0
vb1 = ref Resource_Component0_VB1
drawindexed = 3, 0, 0
[CommandList_Component_DrawInstances]
handling = skip
run = CommandList\EFMIv1\Component_DrawInstances
[TextureOverride_EntryPoint_Component0]
hash = deffba8c
if $mod_enabled && DRAW_TYPE == 4
    CommandList\EFMIv1\Callback_Component_DrawCustom = ref CommandList_Draw_Component0
    run = CommandList_Component_DrawInstances
endif
[TextureOverride_EntryPoint_Component0_LOD1]
hash = aabbccdd
if $mod_enabled && DRAW_TYPE == 4
    CommandList\EFMIv1\Callback_Component_DrawCustom = ref CommandList_Draw_Component0
    run = CommandList_Component_DrawInstances
endif`)
	records, err := collectModelViewerDirectDrawRecords(sections, map[string]any{"modenabled": float64(0)})
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("records = %#v", records)
	}
	record := records[0]
	if record.sectionName != "_EntryPoint_Component0" || record.state.ib != "_Component0_IB" ||
		record.state.vb0 != "_Component0_VB0" ||
		record.state.vb1 != "_Component0_VB1" ||
		record.draw.IndexCount != 3 {
		t.Fatalf("record = %#v", record)
	}
}

func TestModelViewerScannerScopesEFMINestedSkipToItsBranch(t *testing.T) {
	ini := func(body string) []modINISection {
		return parseModINI(`[Constants]
global $required_efmi_version = 1.46
global $hide = 0
[CommandList_Hide]
handling = skip
[TextureOverride_EntryPoint_Component0]
hash = deffba8c
ib = ref Resource_Component0_IB
vb0 = ref Resource_Component0_VB0
vb1 = ref Resource_Component0_VB1
` + body)
	}
	load := func(sections []modINISection) []modelViewerDirectDrawRecord {
		records, err := collectModelViewerDirectDrawRecords(sections, collectModelViewerDefaultVariables(sections))
		if err != nil {
			t.Fatal(err)
		}
		return records
	}

	if records := load(ini("run = CommandList_Hide\n")); len(records) != 0 {
		t.Fatalf("unconditional skip kept records = %#v", records)
	}

	records := load(ini("if $hide == 1\n    run = CommandList_Hide\nendif\n"))
	if len(records) != 1 || !records[0].implicit {
		t.Fatalf("conditional skip records = %#v", records)
	}
	if !modelViewerDNFSatisfied(records[0].conditions, map[string]string{"hide": "0"}) ||
		modelViewerDNFSatisfied(records[0].conditions, map[string]string{"hide": "1"}) {
		t.Fatalf("conditions = %#v", records[0].conditions)
	}
}

func TestModelViewerScannerKeepsLOD0BuffersFromEFMIDrawList(t *testing.T) {
	sections := parseModINI(`[Constants]
global $lod_level = 0
[CommandList_Draw_Component0]
ib = ref Resource_Component0_IB
if $lod_level == 0
    vb0 = ref Resource_Component0_VB0
    vb1 = ref Resource_Component0_VB1
elif $lod_level == 1
    vb0 = ref Resource_Component0_VB0
    vb1 = ref Resource_Component0_VB1_LOD1
endif
drawindexed = 3, 0, 0
[TextureOverride_EntryPoint_Component0]
hash = deffba8c
if $mod_enabled && DRAW_TYPE == 4
    CommandList\EFMIv1\Callback_Component_DrawCustom = ref CommandList_Draw_Component0
    run = CommandList_Component_DrawInstances
endif`)
	records, err := collectModelViewerDirectDrawRecords(sections, collectModelViewerDefaultVariables(sections))
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].state.vb1 != "_Component0_VB1" {
		t.Fatalf("records = %#v", records)
	}
}

func TestLoadModViewerIgnoresEFMIDispatcherWithoutCallback(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "mod.ini"), []byte(`[TextureOverride_EntryPoint_Component0]
hash = deffba8c
if $mod_enabled && DRAW_TYPE == 4
    run = CommandList_Component_DrawInstances
endif
[CommandList_Component_DrawInstances]
handling = skip
`), 0o600); err != nil {
		t.Fatal(err)
	}
	service := NewWithOptions(Options{Protocol: infra.NewProtocol()})
	_, err := service.LoadModViewer(context.Background(), dir)
	if err == nil || err.Error() != "No mesh geometry found across 1 ini file(s)." {
		t.Fatalf("err = %v", err)
	}
}

func TestLoadModViewerPicksEFMIDumpDiffuseBoundAtNonZeroSlot(t *testing.T) {
	dir := t.TempDir()
	writeEFMIComponentBuffers(t, dir, "Component3")
	writeTextureFile(t, dir, "Textures/Components-3 t=beef001f.png", encodeColorPNG(32, 32))
	writeTextureFile(t, dir, "Textures/Components-3 t=beef0020.png", encodeFlatPNG(32, 32))
	if err := os.WriteFile(filepath.Join(dir, "mod.ini"), []byte(`[Constants]
global $required_efmi_version = 1.46
global $mod_enabled = 0
global $object_detected = 0
global $lod_level = 0
[CommandList_Draw_Component3]
run = CommandList\EFMIv1\OverrideTextures
ib = ref Resource_Component3_IB
vb0 = ref Resource_Component3_VB0
vb1 = ref Resource_Component3_VB1
vb2 = ref Resource_Component3_VB2
ps-t13 = Resource_Texture19
ps-t14 = Resource_Texture18
drawindexed = 3, 0, 0
[CommandList_Component_DrawInstances]
handling = skip
run = CommandList\EFMIv1\Component_DrawInstances
[TextureOverride_EntryPoint_Component3]
hash = 52ebe5f7
if $mod_enabled && DRAW_TYPE == 4
    CommandList\EFMIv1\Callback_Component_DrawCustom = ref CommandList_Draw_Component3
    run = CommandList_Component_DrawInstances
endif
[Resource_Texture18]
filename = Textures/Components-3 t=beef0020.png
[TextureOverride_Texture18]
hash = beef0020
if $object_detected
    this = Resource_Texture18
endif
[Resource_Texture19]
filename = Textures/Components-3 t=beef001f.png
[TextureOverride_Texture19]
hash = beef001f
if $object_detected
    this = Resource_Texture19
endif
[Resource_Component3_IB]
type = Buffer
format = DXGI_FORMAT_R16_UINT
filename = Meshes/Component3_IB.buf
[Resource_Component3_VB0]
type = Buffer
stride = 16
filename = Meshes/Component3_VB0.buf
[Resource_Component3_VB1]
type = Buffer
stride = 12
filename = Meshes/Component3_VB1.buf
[Resource_Component3_VB2]
type = Buffer
stride = 12
filename = Meshes/Component3_VB2.buf
`), 0o600); err != nil {
		t.Fatal(err)
	}
	result := loadViewerDir(t, dir).result
	if len(result.Meshes) != 1 {
		t.Fatalf("meshes = %#v", result.Meshes)
	}
	if !strings.Contains(texKey(result.Meshes[0]), "beef001f.png") {
		t.Fatalf("texKey = %q", texKey(result.Meshes[0]))
	}
}

func TestLoadModViewerBindsRenamedEFMIDiffuseToSharedAtlasGroup(t *testing.T) {
	dir := t.TempDir()
	for _, file := range []string{
		"Components-0-1-2 t=beef0001.png",
		"Components-0-1 t=beef0002.png",
		"Components-2 t=beef0003.png",
		"BodyDiffuseTexture.png",
		"BodyDiffuseTextureT.png",
	} {
		writeTextureFile(t, dir, "Textures/"+file, encodeColorPNG(32, 32))
	}
	var ini strings.Builder
	ini.WriteString(`[Constants]
global $required_efmi_version = 1.00
global $mod_enabled = 0
global $object_detected = 0
global persist $Body = 1
global persist $Suit = 1
global persist $Hair = 1
global persist $Tattoo = 0
[KeySwapTattoo]
key = o
type = cycle
$Tattoo = 0,1
[Resource_Atlas]
filename = Textures/Components-0-1-2 t=beef0001.png
[TextureOverride_Atlas]
hash = beef0001
this = Resource_Atlas
[Resource_Mask]
filename = Textures/Components-0-1 t=beef0002.png
[TextureOverride_Mask]
hash = beef0002
this = Resource_Mask
[Resource_Hair]
filename = Textures/Components-2 t=beef0003.png
[TextureOverride_Hair]
hash = beef0003
this = Resource_Hair
[Resource_BodyDiffuse]
filename = Textures/BodyDiffuseTexture.png
[Resource_BodyDiffuseT]
filename = Textures/BodyDiffuseTextureT.png
[TextureOverride_Body]
hash = beef0004
if $object_detected
    if $Tattoo == 1
        this = Resource_BodyDiffuseT
    else
        this = Resource_BodyDiffuse
    endif
endif
`)
	for index, part := range []string{"Body", "Suit", "Hair"} {
		component := "Component" + strconv.Itoa(index)
		writeEFMIComponentBuffers(t, dir, component)
		fmt.Fprintf(&ini, `[CommandList_Draw_%[1]s]
ib = ref Resource_%[1]s_IB
vb0 = ref Resource_%[1]s_VB0
vb1 = ref Resource_%[1]s_VB1
vb2 = ref Resource_%[1]s_VB2
if $%[2]s
    drawindexedinstanced = 3, INSTANCE_COUNT, 0, 0, FIRST_INSTANCE
endif
[TextureOverride_%[1]s]
hash = %08[3]x
if $mod_enabled && DRAW_TYPE == 4
    handling = skip
    run = CommandList_Draw_%[1]s
endif
[Resource_%[1]s_IB]
type = Buffer
format = DXGI_FORMAT_R16_UINT
filename = Meshes/%[1]s_IB.buf
[Resource_%[1]s_VB0]
type = Buffer
stride = 16
filename = Meshes/%[1]s_VB0.buf
[Resource_%[1]s_VB1]
type = Buffer
stride = 12
filename = Meshes/%[1]s_VB1.buf
[Resource_%[1]s_VB2]
type = Buffer
stride = 12
filename = Meshes/%[1]s_VB2.buf
`, component, part, index+1)
	}
	if err := os.WriteFile(filepath.Join(dir, "mod.ini"), []byte(ini.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	result := loadViewerDir(t, dir).result

	for component, want := range map[string][2]string{
		"_Component0": {"BodyDiffuseTexture.png", "BodyDiffuseTextureT.png"},
		"_Component1": {"BodyDiffuseTexture.png", "BodyDiffuseTextureT.png"},
		"_Component2": {"beef0003.png", "beef0003.png"},
	} {
		meshes := meshesNamed(result, component)
		if len(meshes) != 1 {
			t.Fatalf("%s meshes = %#v", component, result.Meshes)
		}
		for value, file := range want {
			got := viewerEvalResolveTexture(
				meshes[0].TextureVariants,
				meshes[0].TexKey,
				map[string]any{"Tattoo": strconv.Itoa(value)},
			)
			if !strings.Contains(got, file) {
				t.Fatalf("%s Tattoo=%d texture = %q, want %s", component, value, got, file)
			}
		}
	}
}

func TestLoadModViewerPicksEFMIDiffuseFromUnnamedSlotBinding(t *testing.T) {
	dir := t.TempDir()
	writeEFMIComponentBuffers(t, dir, "Component3")
	writeTextureFile(t, dir, "Textures/Mask.png", encodeFlatPNG(32, 32))
	writeTextureFile(t, dir, "Textures/A1.png", encodeColorPNG(32, 32))
	writeTextureFile(t, dir, "Textures/A2.png", encodeFlatPNG(16, 16))
	if err := os.WriteFile(filepath.Join(dir, "mod.ini"), []byte(`[Constants]
global $mod_enabled = 0
global persist $T = 1
[KeySwapT]
key = 9
type = cycle
$T = 0,1
[CommandList_Draw_Component3]
run = CommandList\EFMIv1\OverrideTextures
ib = ref Resource_Component3_IB
vb0 = ref Resource_Component3_VB0
vb1 = ref Resource_Component3_VB1
vb2 = ref Resource_Component3_VB2
ps-t12 = Resource_Data
ps-t13 = Resource_Mask
ps-t14 = Resource_A1
if $T == 1
ps-t14 = Resource_A2
endif
drawindexed = 3, 0, 0
[TextureOverride_Component3]
hash = 52ebe5f7
if $mod_enabled && DRAW_TYPE == 4
    handling = skip
    run = CommandList_Draw_Component3
endif
[Resource_Data]
type = Buffer
stride = 12
filename = Meshes/Component3_VB2.buf
[Resource_Mask]
filename = Textures/Mask.png
[Resource_A1]
filename = Textures/A1.png
[Resource_A2]
filename = Textures/A2.png
[Resource_Component3_IB]
type = Buffer
format = DXGI_FORMAT_R16_UINT
filename = Meshes/Component3_IB.buf
[Resource_Component3_VB0]
type = Buffer
stride = 16
filename = Meshes/Component3_VB0.buf
[Resource_Component3_VB1]
type = Buffer
stride = 12
filename = Meshes/Component3_VB1.buf
[Resource_Component3_VB2]
type = Buffer
stride = 12
filename = Meshes/Component3_VB2.buf
`), 0o600); err != nil {
		t.Fatal(err)
	}
	result := loadViewerDir(t, dir).result
	if len(result.Meshes) != 1 {
		t.Fatalf("meshes = %#v", result.Meshes)
	}

	// The buffer and the flat map at lower slots are not diffuse; the slot's toggle variants survive.
	mesh := result.Meshes[0]
	for value, want := range map[string]string{"0": "A1.png", "1": "A2.png"} {
		got := viewerEvalResolveTexture(mesh.TextureVariants, mesh.TexKey, map[string]any{"T": value})
		if !strings.Contains(got, want) {
			t.Fatalf("T=%s texture = %q, want %s", value, got, want)
		}
	}
}

func TestLoadModViewerLoadsEFMIAlpha10CallbackDraw(t *testing.T) {
	dir := t.TempDir()
	writeEFMIComponentBuffers(t, dir, "Component0")
	writeTextureFile(t, dir, "Textures/body.png", encodeTinyPNG())
	if err := os.WriteFile(filepath.Join(dir, "mod.ini"), []byte(`[Constants]
global $required_efmi_version = 1.41
global $mod_enabled = 0
global $object_detected = 0
global $lod_level = 0
[CommandList_Draw_Component0]
ib = ref Resource_Component0_IB
if $lod_level == 0
    vb0 = ref Resource_Component0_VB0
    vb1 = ref Resource_Component0_VB1
    vb2 = ref Resource_Component0_VB2
elif $lod_level == 1
    vb0 = ref Resource_Component0_VB0
    vb1 = ref Resource_Component0_VB1_LOD1
    vb2 = ref Resource_Component0_VB2
endif
ps-t20 = res Resource_SwimsuitDiffuse
drawindexed = 3, 0, 0
[CommandList_Component_DrawInstances]
handling = skip
run = CommandList\EFMIv1\Component_DrawInstances
[TextureOverride_EntryPoint_Component0]
hash = deffba8c
if $mod_enabled && DRAW_TYPE == 4
    CommandList\EFMIv1\Callback_Component_DrawCustom = ref CommandList_Draw_Component0
    run = CommandList_Component_DrawInstances
endif
[TextureOverride_EntryPoint_Component0_LOD1]
hash = aabbccdd
if $mod_enabled && DRAW_TYPE == 4
    CommandList\EFMIv1\Callback_Component_DrawCustom = ref CommandList_Draw_Component0
    run = CommandList_Component_DrawInstances
endif
[Resource_Component0_IB]
type = Buffer
format = DXGI_FORMAT_R16_UINT
filename = Meshes/Component0_IB.buf
[Resource_Component0_VB0]
type = Buffer
stride = 16
filename = Meshes/Component0_VB0.buf
[Resource_Component0_VB1]
type = Buffer
stride = 12
filename = Meshes/Component0_VB1.buf
[Resource_Component0_VB1_LOD1]
type = Buffer
stride = 12
filename = Meshes/Component0_VB1.buf
[Resource_Component0_VB2]
type = Buffer
stride = 12
filename = Meshes/Component0_VB2.buf
[Resource_SwimsuitDiffuse]
filename = Textures/body.png
`), 0o600); err != nil {
		t.Fatal(err)
	}
	result := loadViewerDir(t, dir).result
	if len(result.Meshes) != 1 {
		t.Fatalf("meshes = %#v", result.Meshes)
	}
	if evaluated := evaluateViewerTransport(result, nil); !evaluated.Meshes[0].Visible {
		t.Fatalf("default mesh is hidden: %#v", evaluated)
	}
	if texKey(result.Meshes[0]) == "" {
		t.Fatalf("missing diffuse: %#v", result.Meshes[0])
	}
}

func TestLoadModViewerLoadsEFMIComponentCommandList(t *testing.T) {
	dir := t.TempDir()
	writeEFMIComponentBuffers(t, dir, "Component0")
	writeTextureFile(t, dir, "Textures/body.png", encodeTinyPNG())
	if err := os.WriteFile(filepath.Join(dir, "mod.ini"), []byte(`[Constants]
global $required_efmi_version = 1.10
global $mod_enabled = 0
global $object_detected = 0
global persist $swapvar_toggle_0 = -1
global $draw_component_0_body = 1
[KeySwap]
type = cycle
$swapvar_toggle_0 = -1, 0
[CommandList_Draw_Component0]
ib = ref Resource_Component0_IB
vb0 = ref Resource_Component0_VB0
vb1 = ref Resource_Component0_VB1
vb2 = ref Resource_Component0_VB2
Resource\RabbitFX\Diffuse = ref Resource_Texture0
if $draw_component_0_body
    drawindexedinstanced = 3, INSTANCE_COUNT, 0, 0, FIRST_INSTANCE
endif
[TextureOverride_Component0]
hash = 737f4c35
if $mod_enabled && DRAW_TYPE == 4
    handling = skip
    run = CommandList_Draw_Component0
endif
[TextureOverride_Component0_LOD0]
hash = 86f40ec1
if $mod_enabled && DRAW_TYPE == 4
    handling = skip
    run = CommandList_Draw_Component0
endif
[Resource_Component0_IB]
type = Buffer
format = DXGI_FORMAT_R16_UINT
filename = Meshes/Component0_IB.buf
[Resource_Component0_VB0]
type = Buffer
stride = 16
filename = Meshes/Component0_VB0.buf
[Resource_Component0_VB1]
type = Buffer
stride = 12
filename = Meshes/Component0_VB1.buf
[Resource_Component0_VB2]
type = Buffer
stride = 12
filename = Meshes/Component0_VB2.buf
[Resource_Texture0]
filename = Textures/body.png
`), 0o600); err != nil {
		t.Fatal(err)
	}
	result := loadViewerDir(t, dir).result
	if len(result.Meshes) != 1 {
		t.Fatalf("meshes = %#v", result.Meshes)
	}
	if evaluated := evaluateViewerTransport(result, nil); !evaluated.Meshes[0].Visible {
		t.Fatalf("default mesh is hidden: %#v", evaluated)
	}
	if texKey(result.Meshes[0]) == "" {
		t.Fatalf("missing diffuse: %#v", result.Meshes[0])
	}
}
