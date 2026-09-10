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

	"nahida.live/desktop/internal/infra"
)

const packedObjectAnimShader = `
struct VertexAttributes {
    uint2 position;
    uint normal;
    uint texcoord;
    uint tangent;
};
struct BlendAttributes { float4 weights; int4 indicies; };
struct PoseAttributes { float4 x; float4 y; float4 z; };
RWStructuredBuffer<VertexAttributes> rw_buffer : register(u5);
StructuredBuffer<VertexAttributes> base : register(t50);
StructuredBuffer<BlendAttributes> blend : register(t51);
StructuredBuffer<PoseAttributes> pose : register(t52);
#define TIME IniParams[88].x
#define VG_COUNT IniParams[89].x
void main(uint3 threadID : SV_DispatchThreadID) {
    uint i = threadID.x;
    float4 pos;
    pos.x = f16tof32(base[i].position.x & 0xffff);
    int frame = (int)floor(TIME);
    float4 trans = pose[frame*((int)VG_COUNT)+blend[i].indicies.x].x;
    float4 pos_result;
    pos_result.x = dot(trans, pos);
    trans = pose[(frame+1)*((int)VG_COUNT)+blend[i].indicies.x].x;
    rw_buffer[i].position.x = (uint)f32tof16(pos_result.x);
}
`

func modelViewerFloatToHalfBits(value float32) uint16 {
	bits := math.Float32bits(value)
	sign := uint16((bits >> 16) & 0x8000)
	exp := int((bits>>23)&0xff) - 127 + 15
	frac := uint16((bits >> 13) & 0x3ff)
	switch {
	case exp <= 0:
		return sign
	case exp >= 31:
		return sign | 0x7c00
	default:
		return sign | uint16(exp)<<10 | frac
	}
}

func writePackedObjectVertex(buf []byte, offset int, x, y, z, u, v float32, nx, ny, nz int8) {
	binary.LittleEndian.PutUint16(buf[offset:], modelViewerFloatToHalfBits(x))
	binary.LittleEndian.PutUint16(buf[offset+2:], modelViewerFloatToHalfBits(y))
	binary.LittleEndian.PutUint16(buf[offset+4:], modelViewerFloatToHalfBits(z))
	binary.LittleEndian.PutUint16(buf[offset+6:], modelViewerFloatToHalfBits(1))
	buf[offset+8] = byte(nx)
	buf[offset+9] = byte(ny)
	buf[offset+10] = byte(nz)
	buf[offset+11] = 0
	binary.LittleEndian.PutUint16(buf[offset+12:], modelViewerFloatToHalfBits(u))
	binary.LittleEndian.PutUint16(buf[offset+14:], modelViewerFloatToHalfBits(v))
}

func TestModelViewerPackedObjectDrawExtractsWithoutVB1(t *testing.T) {
	dir := t.TempDir()
	position := make([]byte, 3*20)
	writePackedObjectVertex(position, 0, 1, 2, 3, 0.25, 0.75, 0, 0, 127)
	writePackedObjectVertex(position, 20, 4, 5, 6, 0.5, 0.5, 0, 0, 127)
	writePackedObjectVertex(position, 40, 7, 8, 9, 0, 1, 0, 0, 127)
	if err := os.WriteFile(filepath.Join(dir, "InazumaCloset.buf"), position, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(dir, "InazumaClosetHead.ib"),
		modelViewerUint32Bytes([]uint32{0, 1, 2}),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	leftover := make([]byte, 3*4)
	for i := range leftover {
		leftover[i] = 0xff
	}
	if err := os.WriteFile(filepath.Join(dir, "InazumaClosetTexcoord.buf"), leftover, 0o600); err != nil {
		t.Fatal(err)
	}
	iniText := `[Constants]
post ResourceClosetPosition = copy_desc ResourceClosetPosition.1
post run = CustomShaderComputeAnim
[CustomShaderComputeAnim]
cs-t50 = copy ResourceClosetPosition.1
cs-t51 = copy ResourceClosetBlend
cs-t52 = copy ResourceClosetPose
cs = anim.hlsl
cs-u5 = copy ResourceClosetPosition.1
ResourceClosetPosition = ref cs-u5
Dispatch = 3, 1, 1
[TextureOverrideClosetPosition]
hash = 11111111
vb0 = ResourceClosetPosition
[TextureOverrideClosetHead]
hash = 22222222
ib = ResourceClosetHeadIB
ps-t0 = ResourceClosetHeadDiffuse
drawindexedinstanced = auto
[ResourceClosetPosition]
[ResourceClosetPosition.1]
type = Buffer
stride = 20
filename = InazumaCloset.buf
[ResourceClosetBlend]
stride = 32
filename = InazumaClosetBlend.buf
[ResourceClosetPose]
stride = 48
filename = pose.buf
[ResourceClosetHeadIB]
format = DXGI_FORMAT_R32_UINT
filename = InazumaClosetHead.ib
[ResourceClosetHeadDiffuse]
filename = head.png
`
	if err := os.WriteFile(filepath.Join(dir, "anim.hlsl"), []byte(packedObjectAnimShader), 0o600); err != nil {
		t.Fatal(err)
	}
	writeTextureFile(t, dir, "head.png", encodeTinyPNG())
	iniPath := filepath.Join(dir, "mod.ini")
	if err := os.WriteFile(iniPath, []byte(iniText), 0o600); err != nil {
		t.Fatal(err)
	}
	sections := parseModINI(iniText)
	meshes, err := buildModelViewerDirectScannedMeshes(iniPath, sections, collectModelViewerDefaultVariables(sections))
	if err != nil {
		t.Fatal(err)
	}
	if len(meshes) != 1 || meshes[0].geometry == nil {
		t.Fatalf("meshes = %#v", meshes)
	}
	geo := meshes[0].geometry
	if geo.Position[0] != 1 || geo.Position[1] != 2 || geo.Position[2] != 3 {
		t.Fatalf("position = %v", geo.Position)
	}
	if len(geo.Normal) < 3 || geo.Normal[0] != 0 || geo.Normal[1] != 0 || geo.Normal[2] != 1 {
		t.Fatalf("packed rest-pose normal should match identity frame: %v", geo.Normal)
	}
	if len(geo.Texcoord0) < 2 || geo.Texcoord0[0] != 0.25 || geo.Texcoord0[1] != 0.25 {
		t.Fatalf("texcoord after V-flip = %v", geo.Texcoord0)
	}
	if len(meshes[0].textureAssignments) == 0 {
		t.Fatal("expected Head diffuse assignment")
	}

	service := NewWithOptions(Options{Protocol: infra.NewProtocol()})
	result, loadErr := service.LoadModViewer(context.Background(), dir)
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if len(result.Meshes) != 1 {
		t.Fatalf("loaded meshes = %#v", result.Meshes)
	}
}

func TestModelViewerPackedObjectHeuristicWinsWithoutShader(t *testing.T) {
	dir := t.TempDir()
	position := make([]byte, 3*20)
	writePackedObjectVertex(position, 0, 1, 0, 0, 0, 0, 0, 0, 127)
	writePackedObjectVertex(position, 20, 0, 1, 0, 1, 0, 0, 0, 127)
	writePackedObjectVertex(position, 40, 0, 0, 1, 0, 1, 0, 0, 127)
	if err := os.WriteFile(filepath.Join(dir, "Object.buf"), position, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(dir, "ObjectHead.ib"),
		modelViewerUint32Bytes([]uint32{0, 1, 2}),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	iniText := `[TextureOverrideObjectPosition]
vb0 = ResourceObjectPosition
[TextureOverrideObjectHead]
ib = ResourceObjectHeadIB
drawindexed = 3, 0, 0
[ResourceObjectPosition]
stride = 20
filename = Object.buf
[ResourceObjectHeadIB]
format = DXGI_FORMAT_R32_UINT
filename = ObjectHead.ib`
	iniPath := filepath.Join(dir, "mod.ini")
	if err := os.WriteFile(iniPath, []byte(iniText), 0o600); err != nil {
		t.Fatal(err)
	}
	sections := parseModINI(iniText)
	meshes, err := buildModelViewerDirectScannedMeshes(iniPath, sections, collectModelViewerDefaultVariables(sections))
	if err != nil {
		t.Fatal(err)
	}
	if len(meshes) != 1 || meshes[0].geometry.Position[0] != 1 {
		t.Fatalf("meshes = %#v", meshes)
	}
}

func TestModelViewerStaticStride24ObjectKeepsTrailingUV0(t *testing.T) {
	dir := t.TempDir()
	position := make([]byte, 3*modelViewerPackedObjectStride24)
	uvs := [][2]float32{{0.25, 0.75}, {0.5, 0.5}, {0, 1}}
	for vertex := range 3 {
		offset := vertex * modelViewerPackedObjectStride24
		writePackedObjectVertex(position, offset, float32(vertex+1), 2, 3, 0, 0, 0, 0, 127)
		binary.LittleEndian.PutUint32(position[offset+12:], 0x7f007f00)
		binary.LittleEndian.PutUint16(position[offset+16:], modelViewerFloatToHalfBits(uvs[vertex][0]))
		binary.LittleEndian.PutUint16(position[offset+18:], modelViewerFloatToHalfBits(uvs[vertex][1]))
		binary.LittleEndian.PutUint16(position[offset+20:], modelViewerFloatToHalfBits(0.5))
		binary.LittleEndian.PutUint16(position[offset+22:], modelViewerFloatToHalfBits(0.5))
	}
	if err := os.WriteFile(filepath.Join(dir, "Cafe.buf"), position, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(dir, "CafeA.ib"),
		modelViewerUint32Bytes([]uint32{0, 1, 2}),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	iniText := `[TextureOverrideCafePosition]
vb0 = ResourceCafePosition
[TextureOverrideCafeA]
ib = ResourceCafeAIB
drawindexed = 3, 0, 0
[ResourceCafePosition]
stride = 24
filename = Cafe.buf
[ResourceCafeAIB]
format = DXGI_FORMAT_R32_UINT
filename = CafeA.ib`
	iniPath := filepath.Join(dir, "mod.ini")
	if err := os.WriteFile(iniPath, []byte(iniText), 0o600); err != nil {
		t.Fatal(err)
	}
	sections := parseModINI(iniText)
	meshes, err := buildModelViewerDirectScannedMeshes(iniPath, sections, collectModelViewerDefaultVariables(sections))
	if err != nil {
		t.Fatal(err)
	}
	if len(meshes) != 1 || meshes[0].geometry == nil {
		t.Fatalf("meshes = %#v", meshes)
	}
	uv := meshes[0].geometry.Texcoord0
	want := []float32{0.25, 0.25, 0.5, 0.5, 0, 0}
	if !slices.Equal(uv, want) {
		t.Fatalf("diffuse UVs = %v; want the UV set after the tangent at byte 16", uv)
	}
}

func TestModelViewerStaticStride28ObjectKeepsTrailingUV0(t *testing.T) {
	dir := t.TempDir()
	position := make([]byte, 3*modelViewerPackedObjectStride28)
	uvs := [][2]float32{{0.25, 0.75}, {0.5, 0.5}, {0, 1}}
	for vertex := range 3 {
		offset := vertex * modelViewerPackedObjectStride28
		writePackedObjectVertex(position, offset, float32(vertex+1), 2, 3, 0, 0, 0, 0, 127)
		binary.LittleEndian.PutUint32(position[offset+12:], 0x7f007f00)
		binary.LittleEndian.PutUint32(position[offset+16:], 0xff000000)
		binary.LittleEndian.PutUint16(position[offset+20:], modelViewerFloatToHalfBits(uvs[vertex][0]))
		binary.LittleEndian.PutUint16(position[offset+22:], modelViewerFloatToHalfBits(uvs[vertex][1]))
		binary.LittleEndian.PutUint16(position[offset+24:], modelViewerFloatToHalfBits(0.5))
		binary.LittleEndian.PutUint16(position[offset+26:], modelViewerFloatToHalfBits(0.5))
	}
	if err := os.WriteFile(filepath.Join(dir, "Cafe.buf"), position, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(dir, "CafeA.ib"),
		modelViewerUint32Bytes([]uint32{0, 1, 2}),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	iniText := `[TextureOverrideCafePosition]
vb0 = ResourceCafePosition
[TextureOverrideCafeA]
ib = ResourceCafeAIB
drawindexed = 3, 0, 0
[ResourceCafePosition]
stride = 28
filename = Cafe.buf
[ResourceCafeAIB]
format = DXGI_FORMAT_R32_UINT
filename = CafeA.ib`
	iniPath := filepath.Join(dir, "mod.ini")
	if err := os.WriteFile(iniPath, []byte(iniText), 0o600); err != nil {
		t.Fatal(err)
	}
	sections := parseModINI(iniText)
	meshes, err := buildModelViewerDirectScannedMeshes(iniPath, sections, collectModelViewerDefaultVariables(sections))
	if err != nil {
		t.Fatal(err)
	}
	if len(meshes) != 1 || meshes[0].geometry == nil {
		t.Fatalf("meshes = %#v", meshes)
	}
	uv := meshes[0].geometry.Texcoord0
	want := []float32{0.25, 0.25, 0.5, 0.5, 0, 0}
	if !slices.Equal(uv, want) {
		t.Fatalf("diffuse UVs = %v; want the UV set after tangent and color at byte 20", uv)
	}
}

// makeStride24BufferWithTrailingUV builds 24-byte packed vertices whose tangent
// word at byte 12 is non-finite, matching dumps from the cyclic animation
// layout where UV0 sits at byte 16.
func makeStride24BufferWithTrailingUV(vertexCount int) []byte {
	buf := make([]byte, vertexCount*modelViewerPackedObjectStride24)
	for vertex := range vertexCount {
		offset := vertex * modelViewerPackedObjectStride24
		writePackedObjectVertex(buf, offset, float32(vertex+1), 2, 3, 0, 0, 0, 0, 127)
		binary.LittleEndian.PutUint32(buf[offset+12:], 0x7f007f00)
		binary.LittleEndian.PutUint16(buf[offset+16:], modelViewerFloatToHalfBits(0.1+float32(vertex)*0.1))
		binary.LittleEndian.PutUint16(buf[offset+18:], modelViewerFloatToHalfBits(0.9-float32(vertex)*0.1))
	}
	return buf
}

func TestModelViewerPackedUVScoreCountsNonFiniteWordsAgainstTheScore(t *testing.T) {
	buf := makeStride24BufferWithTrailingUV(4)
	for vertex := range 4 {
		if vertex%2 == 1 {
			continue
		}
		offset := vertex*modelViewerPackedObjectStride24 + 12
		binary.LittleEndian.PutUint16(buf[offset:], modelViewerFloatToHalfBits(0.4+float32(vertex)*0.1))
		binary.LittleEndian.PutUint16(buf[offset+2:], modelViewerFloatToHalfBits(0.6-float32(vertex)*0.1))
	}

	score := modelViewerPackedUVScore(buf, modelViewerPackedObjectStride24, 12)
	if score <= 0 || score >= 1 {
		t.Fatalf("half-non-finite tangent word score = %v; want a partial score", score)
	}
	if live := modelViewerPackedUVScore(buf, modelViewerPackedObjectStride24, 16); live != 1 {
		t.Fatalf("UV0 word score = %v; want 1", live)
	}
	if got := detectModelViewerPackedTexcoordOffset(buf, modelViewerPackedObjectStride24); got != 16 {
		t.Fatalf("detected offset = %d; want the UV0 word at byte 16", got)
	}
}

func TestModelViewerDeclaredTexcoordOffsetSurvivesStride24Guess(t *testing.T) {
	dir := t.TempDir()
	position := makeStride24BufferWithTrailingUV(3)
	if err := os.WriteFile(filepath.Join(dir, "Cafe.buf"), position, 0o600); err != nil {
		t.Fatal(err)
	}
	cache := newModelViewerBufferCache()
	declared := modelViewerDrawVertexSource{
		kind: modelViewerDrawVertexPacked,
		position: modelViewerResource{
			Name:     "ResourceCafePosition",
			Filename: "Cafe.buf",
			Stride:   modelViewerPackedObjectStride24,
		},
		packedTexcoordOffset:   12,
		packedTexcoordDeclared: true,
		packed:                 position,
		packedStride:           modelViewerPackedObjectStride24,
	}

	buffers, loaded, err := loadModelViewerDrawVertexBuffers(dir, declared, cache)
	if err != nil || !loaded {
		t.Fatalf("declared layout: loaded=%t err=%v", loaded, err)
	}
	if element := findModelViewerElement(buffers.layout, "TEXCOORD", -1); element == nil ||
		element.AlignedByteOffset != 12 {
		t.Fatalf("declared TEXCOORD element = %#v; want the declared byte 12", element)
	}

	undeclared := declared
	undeclared.packedTexcoordDeclared = false
	buffers, loaded, err = loadModelViewerDrawVertexBuffers(dir, undeclared, cache)
	if err != nil || !loaded {
		t.Fatalf("undeclared layout: loaded=%t err=%v", loaded, err)
	}
	if element := findModelViewerElement(buffers.layout, "TEXCOORD", -1); element == nil ||
		element.AlignedByteOffset != 16 {
		t.Fatalf("undeclared TEXCOORD element = %#v; want the data-derived byte 16", element)
	}
}

func writeModelViewerHalfTexcoord(t *testing.T, path string, vertexCount int, u, v float32) {
	t.Helper()
	data := make([]byte, vertexCount*20)
	for vertex := range vertexCount {
		binary.LittleEndian.PutUint16(data[vertex*20:], modelViewerFloatToHalfBits(u))
		binary.LittleEndian.PutUint16(data[vertex*20+2:], modelViewerFloatToHalfBits(v))
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestModelViewerFamilyTexcoordFallbackForObjectDump(t *testing.T) {
	dir := t.TempDir()
	writeViewerGeometryN(t, dir, 3)
	if err := os.Rename(filepath.Join(dir, "pos.buf"), filepath.Join(dir, "BodyPosition.buf")); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(dir, "tc.buf"), filepath.Join(dir, "BodyTexcoord.buf")); err != nil {
		t.Fatal(err)
	}
	writeModelViewerHalfTexcoord(t, filepath.Join(dir, "BodyTexcoord.buf"), 3, 0.25, 0.75)
	iniText := `[TextureOverrideBodyPosition]
vb0 = ResourceBodyPosition
[TextureOverrideBody]
ib = ResourceBodyIB
drawindexed = 3, 0, 0
[ResourceBodyPosition]
stride = 40
filename = BodyPosition.buf
[ResourceBodyIB]
format = DXGI_FORMAT_R32_UINT
filename = body.ib`
	iniPath := filepath.Join(dir, "mod.ini")
	if err := os.WriteFile(iniPath, []byte(iniText), 0o600); err != nil {
		t.Fatal(err)
	}
	sections := parseModINI(iniText)
	meshes, err := buildModelViewerDirectScannedMeshes(iniPath, sections, collectModelViewerDefaultVariables(sections))
	if err != nil {
		t.Fatal(err)
	}
	if len(meshes) != 1 || meshes[0].geometry == nil {
		t.Fatalf("meshes = %#v", meshes)
	}
	geo := meshes[0].geometry
	if geo.Position[0] != 0 || geo.Position[1] != 0 || geo.Position[2] != 0 {
		t.Fatalf("position = %v", geo.Position)
	}
	if len(geo.Texcoord0) < 2 || geo.Texcoord0[0] != 0.25 || geo.Texcoord0[1] != 0.25 {
		t.Fatalf("sibling texcoord after V-flip = %v", geo.Texcoord0)
	}
}

func TestLoadModViewerReportsMissingTexcoordInsteadOfMissingBuffers(t *testing.T) {
	dir := t.TempDir()
	writeViewerGeometry(t, dir)
	ini := `[TextureOverrideBody]
ib = ResourceBodyIB
vb0 = ResourcePos
drawindexed = 3, 0, 0
[ResourcePos]
filename = pos.buf
stride = 40
[ResourceBodyIB]
filename = body.ib
format = DXGI_FORMAT_R32_UINT
`
	if err := os.WriteFile(filepath.Join(dir, "mod.ini"), []byte(ini), 0o600); err != nil {
		t.Fatal(err)
	}
	service := NewWithOptions(Options{Protocol: infra.NewProtocol()})
	_, err := service.LoadModViewer(context.Background(), dir)
	if err == nil || !strings.Contains(err.Error(), "Draw sections are missing a texcoord buffer (vb1).") {
		t.Fatalf("err = %v", err)
	}
}

func TestModelViewerPackedShaderDoesNotReclassifyUnrelatedVB1(t *testing.T) {
	dir := t.TempDir()
	closet := make([]byte, 3*20)
	writePackedObjectVertex(closet, 0, 1, 2, 3, 0.25, 0.75, 0, 0, 127)
	writePackedObjectVertex(closet, 20, 4, 5, 6, 0.5, 0.5, 0, 0, 127)
	writePackedObjectVertex(closet, 40, 7, 8, 9, 0, 1, 0, 0, 127)
	body := make([]byte, 3*24)
	for vertex, position := range [][3]float32{{9, 8, 7}, {6, 5, 4}, {3, 2, 1}} {
		for component, value := range position {
			binary.LittleEndian.PutUint32(body[vertex*24+component*4:], math.Float32bits(value))
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "InazumaCloset.buf"), closet, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "BodyFloat.buf"), body, 0o600); err != nil {
		t.Fatal(err)
	}
	writeModelViewerHalfTexcoord(t, filepath.Join(dir, "BodyTexcoord.buf"), 3, 0.5, 0)
	if err := os.WriteFile(
		filepath.Join(dir, "InazumaClosetHead.ib"),
		modelViewerUint32Bytes([]uint32{0, 1, 2}),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(dir, "body.ib"),
		modelViewerUint32Bytes([]uint32{0, 1, 2}),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "anim.hlsl"), []byte(packedObjectAnimShader), 0o600); err != nil {
		t.Fatal(err)
	}
	iniText := `[Constants]
post ResourceClosetPosition = copy_desc ResourceClosetPosition.1
post run = CustomShaderComputeAnim
[CustomShaderComputeAnim]
cs-t50 = copy ResourceClosetPosition.1
cs-t51 = copy ResourceClosetBlend
cs-t52 = copy ResourceClosetPose
cs = anim.hlsl
cs-u5 = copy ResourceClosetPosition.1
ResourceClosetPosition = ref cs-u5
Dispatch = 3, 1, 1
[TextureOverrideClosetPosition]
vb0 = ResourceClosetPosition
[TextureOverrideClosetHead]
ib = ResourceClosetHeadIB
drawindexed = 3, 0, 0
[TextureOverrideBody]
ib = ResourceBodyIB
vb0 = ResourceBodyPos
vb1 = ResourceBodyTc
drawindexed = 3, 0, 0
[ResourceClosetPosition]
[ResourceClosetPosition.1]
type = Buffer
stride = 20
filename = InazumaCloset.buf
[ResourceClosetBlend]
stride = 32
filename = InazumaClosetBlend.buf
[ResourceClosetPose]
stride = 48
filename = pose.buf
[ResourceClosetHeadIB]
format = DXGI_FORMAT_R32_UINT
filename = InazumaClosetHead.ib
[ResourceBodyPos]
stride = 24
filename = BodyFloat.buf
[ResourceBodyTc]
stride = 20
filename = BodyTexcoord.buf
[ResourceBodyIB]
format = DXGI_FORMAT_R32_UINT
filename = body.ib
`
	iniPath := filepath.Join(dir, "mod.ini")
	if err := os.WriteFile(iniPath, []byte(iniText), 0o600); err != nil {
		t.Fatal(err)
	}
	sections := parseModINI(iniText)
	meshes, err := buildModelViewerDirectScannedMeshes(iniPath, sections, collectModelViewerDefaultVariables(sections))
	if err != nil {
		t.Fatal(err)
	}
	if len(meshes) != 2 {
		t.Fatalf("meshes = %#v", meshes)
	}
	byFile := map[string]*modelViewerGeometry{}
	for index := range meshes {
		byFile[meshes[index].positionFile] = meshes[index].geometry
	}
	closetGeo := byFile["InazumaCloset.buf"]
	bodyGeo := byFile["BodyFloat.buf"]
	if closetGeo == nil || closetGeo.Position[0] != 1 || closetGeo.Texcoord0[0] != 0.25 ||
		closetGeo.Texcoord0[1] != 0.25 {
		t.Fatalf("packed closet = %#v", closetGeo)
	}
	if bodyGeo == nil || bodyGeo.Position[0] != 9 || bodyGeo.Position[1] != 8 || bodyGeo.Position[2] != 7 {
		t.Fatalf("body position = %#v", bodyGeo)
	}
	if len(bodyGeo.Texcoord0) < 2 || bodyGeo.Texcoord0[0] != 0.5 || bodyGeo.Texcoord0[1] != 1 {
		t.Fatalf("unrelated vb1 must win over file-wide packed hint: %v", bodyGeo.Texcoord0)
	}
}

func TestModelViewerExplicitVB1StillLoadsCharacterGeometry(t *testing.T) {
	dir := t.TempDir()
	writeViewerGeometry(t, dir)
	iniPath := filepath.Join(dir, "mod.ini")
	ini := `[TextureOverrideBody]
ib = ResourceBodyIB
vb0 = ResourcePos
vb1 = ResourceTc
drawindexed = 3, 0, 0
` + viewerBodyResources
	if err := os.WriteFile(iniPath, []byte(ini), 0o600); err != nil {
		t.Fatal(err)
	}
	sections := parseModINI(ini)
	meshes, err := buildModelViewerDirectScannedMeshes(iniPath, sections, collectModelViewerDefaultVariables(sections))
	if err != nil {
		t.Fatal(err)
	}
	if len(meshes) != 1 {
		t.Fatalf("meshes = %#v", meshes)
	}
}

func TestModelViewerStride24ObjectFrameSwapWithoutPositionSuffix(t *testing.T) {
	dir := t.TempDir()
	frame0 := make([]byte, 3*modelViewerPackedObjectStride24)
	frame1 := make([]byte, 3*modelViewerPackedObjectStride24)
	writePackedObjectVertex(frame0, 0, 1, 2, 3, 0.25, 0.75, 0, 0, 127)
	writePackedObjectVertex(frame0, 24, 4, 5, 6, 0.5, 0.5, 0, 0, 127)
	writePackedObjectVertex(frame0, 48, 7, 8, 9, 0, 1, 0, 0, 127)
	writePackedObjectVertex(frame1, 0, 10, 11, 12, 0.25, 0.75, 0, 0, 127)
	writePackedObjectVertex(frame1, 24, 13, 14, 15, 0.5, 0.5, 0, 0, 127)
	writePackedObjectVertex(frame1, 48, 16, 17, 18, 0, 1, 0, 0, 127)
	if err := os.WriteFile(filepath.Join(dir, "Fire.0.buf"), frame0, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Fire.1.buf"), frame1, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(dir, "FireBody.ib"),
		modelViewerUint32Bytes([]uint32{0, 1, 2}),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	writeTextureFile(t, dir, "body.png", encodeTinyPNG())
	iniText := `[Constants]
global $speed = 4
global $frameStart = 0
global $frameEnd = 1
global $swapvar = 0
global $auxTime = 0
[Present]
if $auxTime % $speed == 0
    if $swapvar < $frameEnd
        $swapvar = $swapvar + 1
    else
        $swapvar = $frameStart
    endif
endif
post $auxTime=$auxTime+1
[TextureOverrideFire]
hash = 3cb1fdfe
run = CommandListFire
[TextureOverrideFireIB]
hash = cc3e40e2
run = CommandListFireIB
[TextureOverrideFireHead]
hash = cc3e40e2
match_first_index = 0
run = CommandListFireHead
[TextureOverrideFireBody]
hash = cc3e40e2
match_first_index = 864
run = CommandListFireBody
[CommandListFire]
if $swapvar == 0
	vb0 = ResourceFire.0
else if $swapvar == 1
	vb0 = ResourceFire.1
endif
[CommandListFireIB]
if $swapvar == 0
	handling = skip
	drawindexed = auto
else if $swapvar == 1
	handling = skip
	drawindexed = auto
endif
[CommandListFireHead]
if $swapvar == 0
	ib = null
	ps-t0 = ResourceFireHeadDiffuse.0
else if $swapvar == 1
	ib = null
	ps-t0 = ResourceFireHeadDiffuse.1
endif
[CommandListFireBody]
if $swapvar == 0
	ib = ResourceFireBodyIB.0
	ps-t0 = ResourceFireBodyDiffuse.0
else if $swapvar == 1
	ib = ResourceFireBodyIB.1
	ps-t0 = ResourceFireBodyDiffuse.1
endif
[ResourceFire.0]
type = Buffer
stride = 24
filename = Fire.0.buf
[ResourceFire.1]
type = Buffer
stride = 24
filename = Fire.1.buf
[ResourceFireBodyIB.0]
type = Buffer
format = DXGI_FORMAT_R32_UINT
filename = FireBody.ib
[ResourceFireBodyIB.1]
type = Buffer
format = DXGI_FORMAT_R32_UINT
filename = FireBody.ib
[ResourceFireBodyDiffuse.0]
filename = body.png
[ResourceFireBodyDiffuse.1]
filename = body.png
[ResourceFireHeadDiffuse.0]
filename = body.png
[ResourceFireHeadDiffuse.1]
filename = body.png
`
	iniPath := filepath.Join(dir, "ZJH.ini")
	if err := os.WriteFile(iniPath, []byte(iniText), 0o600); err != nil {
		t.Fatal(err)
	}
	sections := parseModINI(iniText)
	meshes, err := buildModelViewerDirectScannedMeshes(iniPath, sections, collectModelViewerDefaultVariables(sections))
	if err != nil {
		t.Fatal(err)
	}
	if len(meshes) != 2 {
		t.Fatalf("scanned meshes = %d %#v", len(meshes), meshes)
	}
	if meshes[0].geometry == nil || meshes[0].geometry.Position[0] != 1 || meshes[0].geometry.Position[1] != 2 ||
		meshes[0].geometry.Position[2] != 3 {
		t.Fatalf("frame 0 position = %v", meshes[0].geometry.Position)
	}
	if meshes[1].geometry == nil || meshes[1].geometry.Position[0] != 10 {
		t.Fatalf("frame 1 position = %v", meshes[1].geometry.Position)
	}
	if len(meshes[0].geometry.Texcoord0) < 2 || meshes[0].geometry.Texcoord0[0] != 0.25 ||
		meshes[0].geometry.Texcoord0[1] != 0.25 {
		t.Fatalf("texcoord after V-flip = %v", meshes[0].geometry.Texcoord0)
	}

	service := NewWithOptions(Options{Protocol: infra.NewProtocol()})
	result, loadErr := service.LoadModViewer(context.Background(), dir)
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if len(result.Meshes) != 2 {
		t.Fatalf("loaded meshes = %#v", result.Meshes)
	}
	if len(result.Animations) != 1 || result.Animations[0].ID != "swapvar" || result.Animations[0].FrameEnd != 1 {
		t.Fatalf("animations = %#v", result.Animations)
	}
}

func TestModelViewerStride24PackedHeuristicRejectsFloat3(t *testing.T) {
	data := make([]byte, 3*24)
	positions := [][3]float32{{0, 1, 0}, {1, 1, 0}, {0, 1, 1}}
	for vertex, position := range positions {
		for component, value := range position {
			binary.LittleEndian.PutUint32(data[vertex*24+component*4:], math.Float32bits(value))
		}
	}
	if modelViewerPositionLooksPackedObject(data, 24) {
		t.Fatal("float3 stride 24 should not look packed")
	}
	layout, err := inferModelViewerFmtLayout(
		modelViewerBufferGroup{Key: "Fire", VB: data, Stride: 24},
		nil,
		"mihoyo",
		"DXGI_FORMAT_R32_UINT",
	)
	if err != nil {
		t.Fatal(err)
	}
	position := findModelViewerElement(layout, "POSITION", -1)
	if position == nil || position.Format != "DXGI_FORMAT_R32G32B32_FLOAT" {
		t.Fatalf("layout = %#v", layout)
	}
}

func TestModelViewerStride24PackedHeuristicAcceptsHalf4(t *testing.T) {
	data := make([]byte, 3*24)
	writePackedObjectVertex(data, 0, 1, 0, 0, 0, 0, 0, 0, 127)
	writePackedObjectVertex(data, 24, 0, 1, 0, 1, 0, 0, 0, 127)
	writePackedObjectVertex(data, 48, 0, 0, 1, 0, 1, 0, 0, 127)
	if !modelViewerPositionLooksPackedObject(data, 24) {
		t.Fatal("half4 stride 24 should look packed")
	}
	layout, err := inferModelViewerFmtLayout(
		modelViewerBufferGroup{Key: "Fire", VB: data, Stride: 24},
		nil,
		"mihoyo",
		"DXGI_FORMAT_R32_UINT",
	)
	if err != nil {
		t.Fatal(err)
	}
	if layout.Stride != 24 ||
		findModelViewerElement(layout, "POSITION", -1).Format != "DXGI_FORMAT_R16G16B16A16_FLOAT" {
		t.Fatalf("layout = %#v", layout)
	}
}
