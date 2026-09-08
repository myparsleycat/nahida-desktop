package tools

import (
	"context"
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
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
	if err := os.WriteFile(filepath.Join(dir, "InazumaClosetHead.ib"), modelViewerUint32Bytes([]uint32{0, 1, 2}), 0o600); err != nil {
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
	if err := os.WriteFile(filepath.Join(dir, "ObjectHead.ib"), modelViewerUint32Bytes([]uint32{0, 1, 2}), 0o600); err != nil {
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

func TestModelViewerFamilyTexcoordFallbackForObjectDump(t *testing.T) {
	dir := t.TempDir()
	writeViewerGeometryN(t, dir, 3)
	if err := os.Rename(filepath.Join(dir, "pos.buf"), filepath.Join(dir, "BodyPosition.buf")); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(dir, "tc.buf"), filepath.Join(dir, "BodyTexcoord.buf")); err != nil {
		t.Fatal(err)
	}
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
	if len(meshes) != 1 {
		t.Fatalf("meshes = %#v", meshes)
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
