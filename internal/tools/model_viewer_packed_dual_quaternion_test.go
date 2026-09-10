package tools

import (
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A small recognition fixture; pose arithmetic is verified independently in the
// renderer tests rather than storing a third-party model or its large buffers.
const packedDualQuaternionShader = `
struct VertexAttributes { uint2 position; uint normal; uint tangent; uint texcoord; };
struct BlendAttributes { float4 weights; int4 indicies; };
struct PoseAttributes { float3 S; float3 T; float4 QR; float4 QD; };
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
 pos.z = f16tof32(base[i].position.x >> 16);
 pos.y = f16tof32(base[i].position.y & 0xffff)*-1.0f;
 pos.w = 1.0f;
 int frame = (int)floor(TIME);
 float inter = TIME - floor(TIME);
 int4 blend_indicy = frame*((int)VG_COUNT)+blend[i].indicies;
 int4 blend_indicy_next = (frame+1)*((int)VG_COUNT)+blend[i].indicies;
 float3 scale = pose[blend_indicy.x].S * blend[i].weights.x *(1.0f-inter);
 float3 bias = pose[blend_indicy.x].T * blend[i].weights.x *(1.0f-inter);
 pos.xyz = pos.xyz*scale + bias;
 float4 qr = pose[blend_indicy.x].QR * blend[i].weights.x *(1.0f-inter);
 float4 qd = pose[blend_indicy.x].QD * blend[i].weights.x *(1.0f-inter);
 if (dot(pose[blend_indicy.x].QR, pose[blend_indicy.y].QR) < 0) {
   qr -= pose[blend_indicy.y].QR * blend[i].weights.y;
 }
 if (dot(pose[blend_indicy.x].QR, pose[blend_indicy_next.x].QR) < 0) {
   qr -= pose[blend_indicy_next.x].QR * blend[i].weights.x * inter;
 }
 qd = qd / length(qr);
 qr = qr / length(qr);
 float4 trans = float4(1, 0, 0, 2*(-qd.w*qr.x+qd.x*qr.w-qd.y*qr.z+qd.z*qr.y));
 float4 pos_result, normal_result, normal;
 pos_result.x = dot(trans, pos);
 normal_result.x = dot(trans, normal);
 rw_buffer[i].position.x = (uint)f32tof16(pos_result.z)<<16 | (uint)f32tof16(pos_result.x);
 rw_buffer[i].position.y = (uint)f32tof16(pos_result.w)<<16 | (uint)f32tof16(pos_result.y*-1.0f);
 rw_buffer[i].normal = i32toi8(int(normal_result.y*-1.0f))<<16;
}`

const packedDualQuaternionINI = `
[Constants]
global $freq = 0
global $bones = 1
global $start = 6
global $end = 5159
global $dt
post ResourcePosition = copy_desc ResourcePosition.1
[Present]
run = CustomShaderPose
[CustomShaderPose]
$freq = $freq + 24 * $dt
if $freq > $end-2
  $freq = $start
endif
x88 = $freq
x89 = $bones
cs-t50 = copy ResourcePosition.1
cs-t51 = copy ResourceBlend
cs-t52 = copy ResourcePose
cs = anim.hlsl
cs-u5 = copy ResourcePosition.1
ResourcePosition = ref cs-u5
Dispatch = 3, 1, 1
[TextureOverridePosition]
hash = 12345678
vb0 = ResourcePosition
[TextureOverrideHead]
hash = 23456789
handling = skip
ib = ResourceIB
drawindexed = auto
[ResourcePosition]
[ResourcePosition.1]
type = Buffer
stride = 20
filename = base.buf
[ResourceBlend]
type = Buffer
stride = 32
filename = blend.buf
[ResourcePose]
type = Buffer
stride = 52
filename = pose.buf
[ResourceIB]
type = Buffer
format = DXGI_FORMAT_R32_UINT
filename = head.ib
`

func writePackedDualQuaternionFixture(t *testing.T, dir, ini string) {
	t.Helper()
	base := make([]byte, 3*20)
	for vertex := range 3 {
		binary.LittleEndian.PutUint16(base[vertex*20:], modelViewerFloatToHalfBits(float32(vertex)))
		binary.LittleEndian.PutUint16(base[vertex*20+6:], modelViewerFloatToHalfBits(1))
		base[vertex*20+10] = 127
		binary.LittleEndian.PutUint16(base[vertex*20+12:], modelViewerFloatToHalfBits(0.75))
		binary.LittleEndian.PutUint16(base[vertex*20+16:], modelViewerFloatToHalfBits(0.25))
		binary.LittleEndian.PutUint16(base[vertex*20+18:], modelViewerFloatToHalfBits(0.75))
	}
	for name, data := range map[string][]byte{
		"mod.ini": []byte(ini), "anim.hlsl": []byte(packedDualQuaternionShader),
		"base.buf": base, "blend.buf": make([]byte, 3*32), "pose.buf": make([]byte, 5160*56),
		"head.ib": {0, 0, 0, 0, 1, 0, 0, 0, 2, 0, 0, 0},
	} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// A ZZMI object variant keeps the 24-byte packed base and moves the frame
// accumulator and state ranges into [Present], leaving the compute pass with
// only the frame variable.
const packedDualQuaternion24Shader = `
struct VertexAttributes { uint2 position; uint normal; uint tangent; uint texcoord0; uint texcoord1; };
struct BlendAttributes { float4 weights; int4 indicies; };
struct PoseAttributes { float3 S; float3 T; float4 QR; float4 QD; };
RWStructuredBuffer<VertexAttributes> rw_buffer : register(u5);
StructuredBuffer<VertexAttributes> base : register(t50);
StructuredBuffer<BlendAttributes> blend : register(t51);
StructuredBuffer<PoseAttributes> pose : register(t52);
#define TIME IniParams[88].x
#define VG_COUNT IniParams[89].x
void main(uint3 threadID : SV_DispatchThreadID) {
 uint i = threadID.x;
 BlendAttributes b = blend[i];
 float4 pos;
 pos.x = f16tof32(base[i].position.x & 0xffff);
 pos.z = f16tof32(base[i].position.x >> 16);
 pos.y = f16tof32(base[i].position.y & 0xffff) * -1.0f;
 pos.w = 1.0f;
 float4 normal = float4(0, 0, 0, 0);
 normal.x = float(i8toi32(base[i].normal.x & 0xff));
 int frame = (int)TIME;
 float inter = frac(TIME);
 float inter_prev = 1.0f - inter;
 int vg_count = (int)VG_COUNT;
 int4 idx_prev = frame * vg_count + b.indicies;
 int4 idx_next = (frame + 1) * vg_count + b.indicies;
 PoseAttributes p0_prev = pose[idx_prev.x];
 PoseAttributes p1_prev = pose[idx_prev.y];
 float4 weights = b.weights;
 float3 scale = (p0_prev.S * weights.x + p1_prev.S * weights.y) * inter_prev;
 float3 bias = (p0_prev.T * weights.x + p1_prev.T * weights.y) * inter_prev;
 pos.xyz = pos.xyz * scale + bias;
 float4 qr = p0_prev.QR * weights.x;
 float4 qd = p0_prev.QD * weights.x;
 float sign1 = sign(dot(p0_prev.QR, p1_prev.QR));
 qr += p1_prev.QR * weights.y * sign1;
 qd += p1_prev.QD * weights.y * sign1;
 float qr_len = length(qr);
 qr /= qr_len;
 qd /= qr_len;
 float qx = qr.x, qy = qr.y, qz = qr.z, qw = qr.w;
 float qdx = qd.x, qdy = qd.y, qdz = qd.z, qdw = qd.w;
 float m00 = 1.0f - 2.0f*qy*qy - 2.0f*qz*qz;
 float m01 = 2.0f*(qx*qy - qw*qz);
 float m02 = 2.0f*(qx*qz + qw*qy);
 float m10 = 2.0f*(qx*qy + qw*qz);
 float m11 = 1.0f - 2.0f*qx*qx - 2.0f*qz*qz;
 float m12 = 2.0f*(qy*qz - qw*qx);
 float m20 = 2.0f*(qx*qz - qw*qy);
 float m21 = 2.0f*(qy*qz + qw*qx);
 float m22 = 1.0f - 2.0f*qx*qx - 2.0f*qy*qy;
 float t0 = 2.0f*(-qdw*qx + qdx*qw - qdy*qz + qdz*qy);
 float4 pos_result;
 pos_result.x = m00*pos.x + m01*pos.y + m02*pos.z + t0*pos.w;
 float4 normal_result;
 normal_result.x = m00*normal.x + m01*normal.y + m02*normal.z;
 rw_buffer[i].position.x = (uint)f32tof16(pos_result.z)<<16 | (uint)f32tof16(pos_result.x);
 rw_buffer[i].normal.x = i32toi8(int(normal_result.x));
}`

const packedDualQuaternion24INI = `
[Constants]
global $freq = 0
global $bones = 2
global $start = 0
global $end = 0
global $dt
global persist $state = 0
post ResourcePosition = copy_desc ResourcePosition.1
[Present]
$freq = $freq + 24 * $dt
if $freq > $end
  $freq = $start
endif
if $state == 0
  $start = 1
  $end = 3
elif $state == 1
  $start = 3
  $end = 5
endif
run = CustomShaderPose
[CustomShaderPose]
x88 = $freq
x89 = $bones
cs-t50 = copy ResourcePosition.1
cs-t51 = copy ResourceBlend
cs-t52 = copy ResourcePose
cs = anim.hlsl
cs-u5 = copy ResourcePosition.1
ResourcePosition = ref cs-u5
Dispatch = 3, 1, 1
[TextureOverridePosition]
hash = 12345678
vb0 = ResourcePosition
[TextureOverrideHead]
hash = 23456789
handling = skip
ib = ResourceIB
drawindexed = auto
[ResourcePosition]
[ResourcePosition.1]
type = Buffer
stride = 24
filename = base.buf
[ResourceBlend]
type = Buffer
stride = 32
filename = blend.buf
[ResourcePose]
type = Buffer
stride = 56
filename = pose.buf
[ResourceIB]
type = Buffer
format = DXGI_FORMAT_R32_UINT
filename = head.ib
`

func TestPackedDualQuaternion24PresentStateClips(t *testing.T) {
	dir := t.TempDir()
	for name, data := range map[string][]byte{
		"mod.ini": []byte(packedDualQuaternion24INI), "anim.hlsl": []byte(packedDualQuaternion24Shader),
		"base.buf": make([]byte, 3*24), "blend.buf": make([]byte, 3*32),
		"pose.buf": make([]byte, 8*2*56), "head.ib": make([]byte, 12),
	} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if stride, known := modelViewerPackedDualQuaternionBaseStride(packedDualQuaternion24Shader); !known ||
		stride != modelViewerPackedObjectStride24 {
		t.Fatalf("24-byte variant not recognized: stride=%d known=%t", stride, known)
	}
	sections := parseModINI(packedDualQuaternion24INI)
	resources := resolveModelViewerEffectiveResources(sections, collectModelViewerResources(sections))
	deformer, clips := detectModelViewerComputeAnimation(
		dir,
		dir,
		"",
		sections,
		resources,
		[]modelViewerDirectMesh{
			{id: "mesh", positionFile: "base.buf", geometry: &modelViewerGeometry{VertexCount: 3}},
		},
		nil,
	)
	if deformer == nil || deformer.Kind != modelViewerPackedDualQuaternionKind ||
		deformer.Base.Stride != modelViewerPackedObjectStride24 ||
		deformer.VertexCount != 3 || deformer.Pose == nil ||
		deformer.Pose.BoneCount != 2 || deformer.Pose.FrameCount != 8 {
		t.Fatalf("unexpected deformer: %+v", deformer)
	}
	if len(clips) != 2 ||
		clips[0].FrameStart != 1 || clips[0].FrameEnd != 3 || clips[0].FPS != 24 ||
		clips[1].FrameStart != 3 || clips[1].FrameEnd != 5 || clips[1].FPS != 24 {
		t.Fatalf("unexpected present-derived clips: %+v", clips)
	}
	packed := collectModelViewerPackedObjectResources(dir, dir, sections)
	if packed[modelViewerNormalizeKey("position")] != 16 {
		t.Fatalf("packed UV layout: %v", packed)
	}
}

// Some 24-byte object ports append a second UV word without changing the pose
// math, so the legacy interpolation body must be recognized with either record.
var packedDualQuaternion24LegacyShader = strings.Replace(
	packedDualQuaternionShader,
	"uint2 position; uint normal; uint tangent; uint texcoord;",
	"uint2 position; uint normal; uint tangent; uint texcoord; uint texcoord1;",
	1,
)

func TestPackedDualQuaternion24LegacyBody(t *testing.T) {
	if stride, known := modelViewerPackedDualQuaternionBaseStride(packedDualQuaternion24LegacyShader); !known ||
		stride != modelViewerPackedObjectStride24 {
		t.Fatalf("24-byte legacy variant not recognized: stride=%d known=%t", stride, known)
	}
	if stride, known := modelViewerPackedDualQuaternionBaseStride(packedDualQuaternionShader); !known ||
		stride != modelViewerPackedObjectStride {
		t.Fatalf("20-byte legacy variant misread: stride=%d known=%t", stride, known)
	}
	dir := t.TempDir()
	ini := strings.Replace(packedDualQuaternionINI, "stride = 20", "stride = 24", 1)
	ini = strings.Replace(ini, "stride = 52", "stride = 56", 1)
	for name, data := range map[string][]byte{
		"mod.ini":   []byte(ini),
		"anim.hlsl": []byte(packedDualQuaternion24LegacyShader),
		"base.buf":  make([]byte, 3*24),
		"blend.buf": make([]byte, 3*32),
		"pose.buf":  make([]byte, 5160*56),
		"head.ib":   make([]byte, 12),
	} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	sections := parseModINI(ini)
	resources := resolveModelViewerEffectiveResources(sections, collectModelViewerResources(sections))
	deformer, clips := detectModelViewerComputeAnimation(
		dir,
		dir,
		"",
		sections,
		resources,
		[]modelViewerDirectMesh{
			{id: "mesh", positionFile: "base.buf", geometry: &modelViewerGeometry{VertexCount: 3}},
		},
		nil,
	)
	if deformer == nil || deformer.Kind != modelViewerPackedDualQuaternionKind ||
		deformer.Base.Stride != modelViewerPackedObjectStride24 ||
		deformer.VertexCount != 3 || deformer.Pose == nil ||
		deformer.Pose.BoneCount != 1 || deformer.Pose.FrameCount != 5160 {
		t.Fatalf("unexpected deformer: %+v", deformer)
	}
	if len(clips) != 1 || clips[0].FrameStart != 6 || clips[0].FrameEnd != 5157 || clips[0].FPS != 24 {
		t.Fatalf("unexpected clips: %+v", clips)
	}
}

func TestPackedDualQuaternion24CompoundGuardClips(t *testing.T) {
	for _, tc := range []struct {
		name  string
		guard string
	}{
		{"or guard", "if $freq > $end || $freq < $start"},
		{"and guard", "if $freq > $end && $pause == 0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			ini := strings.Replace(packedDualQuaternion24INI, "if $freq > $end", tc.guard, 1)
			for name, data := range map[string][]byte{
				"mod.ini": []byte(ini), "anim.hlsl": []byte(packedDualQuaternion24Shader),
				"base.buf": make([]byte, 3*24), "blend.buf": make([]byte, 3*32),
				"pose.buf": make([]byte, 8*2*56), "head.ib": make([]byte, 12),
			} {
				if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			sections := parseModINI(ini)
			resources := resolveModelViewerEffectiveResources(sections, collectModelViewerResources(sections))
			deformer, clips := detectModelViewerComputeAnimation(
				dir,
				dir,
				"",
				sections,
				resources,
				[]modelViewerDirectMesh{
					{id: "mesh", positionFile: "base.buf", geometry: &modelViewerGeometry{VertexCount: 3}},
				},
				nil,
			)
			if deformer == nil || deformer.Kind != modelViewerPackedDualQuaternionKind ||
				deformer.Pose == nil || deformer.Pose.FrameCount != 8 {
				t.Fatalf("unexpected deformer: %+v", deformer)
			}
			if len(clips) != 2 ||
				clips[0].FrameStart != 1 || clips[0].FrameEnd != 3 || clips[0].FPS != 24 ||
				clips[1].FrameStart != 3 || clips[1].FrameEnd != 5 || clips[1].FPS != 24 {
				t.Fatalf("unexpected compound-guard clips: %+v", clips)
			}
		})
	}
}

func TestPackedDualQuaternionDetection(t *testing.T) {
	for _, tc := range []struct {
		name, from, to string
		valid          bool
	}{
		{"legacy stride", "", "", true},
		{"correct stride", "stride = 52", "stride = 56", true},
		{"wrong stride", "stride = 52", "stride = 48", false},
		{"invalid bones", "$bones = 1", "$bones = 1.5", false},
		{"invalid end", "$end = 5159", "$end = 6000", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			ini := strings.Replace(packedDualQuaternionINI, tc.from, tc.to, 1)
			writePackedDualQuaternionFixture(t, dir, ini)
			sections := parseModINI(ini)
			resources := resolveModelViewerEffectiveResources(sections, collectModelViewerResources(sections))
			var diagnostics []string
			deformer, clips := detectModelViewerComputeAnimation(
				dir,
				dir,
				"",
				sections,
				resources,
				[]modelViewerDirectMesh{
					{id: "mesh", positionFile: "base.buf", geometry: &modelViewerGeometry{VertexCount: 3}},
				},
				nil,
				func(message string) { diagnostics = append(diagnostics, message) },
			)
			if !tc.valid {
				if deformer != nil || len(clips) != 0 {
					t.Fatal("invalid animation accepted")
				}
				return
			}
			if deformer == nil || deformer.Kind != modelViewerPackedDualQuaternionKind ||
				deformer.Pose.Frames.Stride != 56 ||
				deformer.Pose.FrameCount != 5160 ||
				deformer.VertexCount != 3 {
				t.Fatalf("unexpected deformer: %+v", deformer)
			}
			if len(clips) != 1 || clips[0].FrameStart != 6 || clips[0].FrameEnd != 5157 || clips[0].FPS != 24 ||
				len(clips[0].Frames) != 5152 ||
				clips[0].Frames[5151].Index != 5157 {
				t.Fatalf("unexpected clips: %d", len(clips))
			}
			if tc.name == "legacy stride" &&
				(len(diagnostics) != 1 || !strings.Contains(diagnostics[0], "declaredStride=52")) {
				t.Fatalf("diagnostics: %v", diagnostics)
			}
			for _, resource := range resources {
				if modelViewerNormalizeKey(resource.Name) == "pose" && tc.name == "legacy stride" &&
					resource.Stride != 52 {
					t.Fatal("original resource was mutated")
				}
			}
			packed := collectModelViewerPackedObjectResources(dir, dir, sections)
			if packed["position"] != 16 || packed["position1"] != 16 {
				t.Fatalf("alias layouts: %v", packed)
			}
			cache := newModelViewerBufferCache()
			defer cache.releaseAll()
			source := resolveModelViewerDrawVertexSource(
				dir,
				"mihoyo",
				modelViewerDirectBufferState{vb0: "Position", ib: "IB"},
				modelViewerResourceMap(resources),
				resources,
				cache,
				packed,
			)
			buffers, _, err := loadModelViewerDrawVertexBuffers(dir, source, cache)
			if err != nil || len(buffers.layout.Elements) < 3 || buffers.layout.Elements[2].AlignedByteOffset != 16 {
				t.Fatalf("UV layout: %+v %v source=%+v resources=%+v", buffers.layout, err, source, resources)
			}
			geometry, err := extractModelViewerGeometry(
				buffers.combined,
				buffers.stride,
				buffers.layout,
				[]uint32{0, 1, 2},
				true,
				false,
				true,
				nil,
			)
			if err != nil || geometry == nil || len(geometry.Texcoord0) != 6 || geometry.Texcoord0[0] != 0.25 ||
				geometry.Texcoord0[1] != 0.75 {
				t.Fatalf("decoded UV: %+v error=%v", geometry, err)
			}

		})
	}
}

func TestPackedDualQuaternionRejectsChangedShaderAndBuffers(t *testing.T) {
	for _, replacement := range []string{"float4 x; float4 y; float4 z;", "float3 S; float3 T; float4 QR;"} {
		if isKnownModelViewerPackedDualQuaternionShader(
			strings.ReplaceAll(packedDualQuaternionShader, "float3 S; float3 T; float4 QR; float4 QD;", replacement),
		) {
			t.Fatal("wrong pose accepted")
		}
	}
	for _, filename := range []string{"pose.buf", "blend.buf", "base.buf", "anim.hlsl"} {
		t.Run(filename, func(t *testing.T) {
			dir := t.TempDir()
			writePackedDualQuaternionFixture(t, dir, packedDualQuaternionINI)
			if err := os.WriteFile(filepath.Join(dir, filename), []byte{0}, 0o600); err != nil {
				t.Fatal(err)
			}
			sections := parseModINI(packedDualQuaternionINI)
			resources := resolveModelViewerEffectiveResources(sections, collectModelViewerResources(sections))
			deformer, _ := detectModelViewerComputeAnimation(
				dir,
				dir,
				"",
				sections,
				resources,
				[]modelViewerDirectMesh{
					{id: "mesh", positionFile: "base.buf", geometry: &modelViewerGeometry{VertexCount: 3}},
				},
				nil,
			)
			if deformer != nil {
				t.Fatal("corrupt buffer or unknown shader accepted")
			}
		})
	}
}

func TestPackedDualQuaternionUVStreamEvidence(t *testing.T) {
	for _, tc := range []struct {
		name               string
		offset, size, want int
		corrupt            bool
	}{
		{"actual UV at 12", 12, 12, 12, false},
		{"actual UV at 16", 16, 12, 16, false},
		{"one mismatched vertex", 12, 12, 16, true},
		{"truncated stream", 12, 8, 16, false},
		{"missing stream", 12, 0, 16, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writePackedDualQuaternionFixture(t, dir, packedDualQuaternionINI)
			base, err := os.ReadFile(filepath.Join(dir, "base.buf"))
			if err != nil {
				t.Fatal(err)
			}
			uv := make([]byte, 12)
			for vertex := range 3 {
				copy(uv[vertex*4:], base[vertex*20+tc.offset:vertex*20+tc.offset+4])
			}
			if tc.corrupt {
				uv[11] ^= 1
			}
			if tc.size > 0 {
				if err := os.WriteFile(filepath.Join(dir, "baseTexcoord.buf"), uv[:tc.size], 0o600); err != nil {
					t.Fatal(err)
				}
			}
			sections := parseModINI(packedDualQuaternionINI)
			resources := resolveModelViewerEffectiveResources(sections, collectModelViewerResources(sections))
			cache := newModelViewerBufferCache()
			defer cache.releaseAll()
			source := resolveModelViewerDrawVertexSource(
				dir,
				"mihoyo",
				modelViewerDirectBufferState{vb0: "Position", ib: "IB"},
				modelViewerResourceMap(resources),
				resources,
				cache,
				collectModelViewerPackedObjectResources(dir, dir, sections),
			)
			buffers, ok, err := loadModelViewerDrawVertexBuffers(dir, source, cache)
			if err != nil || !ok {
				t.Fatalf("load: %v", err)
			}
			if got := buffers.layout.Elements[2].AlignedByteOffset; got != tc.want {
				t.Fatalf("UV offset=%d want=%d", got, tc.want)
			}
			geometry, err := extractModelViewerGeometry(
				buffers.combined,
				buffers.stride,
				buffers.layout,
				[]uint32{2, 0, 1},
				false,
				false,
				true,
				nil,
			)
			if err != nil {
				t.Fatal(err)
			}
			for vertex, index := range geometry.SourceIndices {
				for axis := range 2 {
					want := modelViewerHalfToFloat(binary.LittleEndian.Uint16(base[int(index)*20+tc.want+axis*2:]))
					if geometry.Texcoord0[vertex*2+axis] != want {
						t.Fatal("UV source-index mapping changed")
					}
				}
			}
		})
	}
}

func TestComputePoseClipLimits(t *testing.T) {
	for _, tc := range []struct {
		start, end string
		count      int
		valid      bool
	}{
		{"0", "65535", 65537, true}, {"0", "65536", 65538, false},
		{"-1", "5", 10, false}, {"2", "1", 10, false}, {"0", "10", 10, false},
		{"0.5", "5", 10, false},
	} {
		sections := parseModINI(
			"[Constants]\nglobal $first=" + tc.start + "\nglobal $last=" + tc.end + "\n[CustomShaderPose]\n$freq=$freq+24*$dt\nif $freq > $last\n$freq=$first\nendif",
		)
		clips, explicit := detectModelViewerGIMIShapePoseClips(
			sections,
			sections[1],
			"$freq",
			collectModelViewerDefaultVariables(sections),
			nil,
			"test",
			tc.count,
		)
		if !explicit || (len(clips) == 1) != tc.valid {
			t.Fatalf("range %s..%s: clips=%d explicit=%t", tc.start, tc.end, len(clips), explicit)
		}
	}
}

func TestComputePoseInvalidStatePreservesValidClips(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		firstStart, firstEnd   string
		secondStart, secondEnd string
		validState             string
	}{
		{"invalid last", "0", "2", "10", "12", "0"},
		{"invalid first", "10", "12", "0", "2", "1"},
		{"all invalid", "10", "12", "20", "22", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			ini := strings.Replace(
				packedDualQuaternionINI,
				"[Present]\n",
				"[Present]\nif $mode == 0\n$start = "+tc.firstStart+"\n$end = "+tc.firstEnd+"\nelse if $mode == 1\n$start = "+tc.secondStart+"\n$end = "+tc.secondEnd+"\nendif\n",
				1,
			)
			ini = strings.Replace(ini, "$end-2", "$end", 1)
			ini = strings.Replace(ini, "stride = 52", "stride = 56", 1)
			writePackedDualQuaternionFixture(t, dir, ini)
			if err := os.Truncate(filepath.Join(dir, "pose.buf"), 3*56); err != nil {
				t.Fatal(err)
			}
			sections := parseModINI(ini)
			resources := resolveModelViewerEffectiveResources(sections, collectModelViewerResources(sections))
			var diagnostics []string
			deformer, clips := detectModelViewerComputeAnimation(
				dir,
				dir,
				"",
				sections,
				resources,
				[]modelViewerDirectMesh{
					{id: "mesh", positionFile: "base.buf", geometry: &modelViewerGeometry{VertexCount: 3}},
				},
				nil,
				func(message string) {
					diagnostics = append(diagnostics, message)
				},
			)
			if tc.validState == "" {
				if deformer != nil || len(clips) != 0 {
					t.Fatal("all-invalid state ranges must not create a full-pose fallback")
				}
				return
			}
			if deformer == nil || len(clips) != 1 {
				t.Fatalf("valid state %s was lost: deformer=%v clips=%d", tc.validState, deformer, len(clips))
			}
			clip := clips[0]
			if !strings.HasSuffix(clip.ID, ":"+tc.validState) || clip.FrameStart != 0 || clip.FrameEnd != 2 ||
				len(clip.Frames) != 3 ||
				clip.FPS != 24 {
				t.Fatalf("unexpected surviving clip: %+v", clip)
			}
			if len(clip.VariableIDs) != 1 || clip.VariableIDs[0] != "mode" ||
				modelViewerString(clip.Frames[0].Values["mode"]) != tc.validState {
				t.Fatalf("surviving clip lost its state binding: %+v", clip)
			}
			if len(diagnostics) != 1 || !strings.Contains(diagnostics[0], "variable=\"mode\"") ||
				!strings.Contains(diagnostics[0], "frameStart=10 frameEnd=12 frameCount=3") ||
				!strings.Contains(diagnostics[0], "pose.buf") ||
				!strings.Contains(diagnostics[0], "anim.hlsl") {
				t.Fatalf("missing invalid-state diagnostic: %v", diagnostics)
			}
		})
	}
}

func TestComputePoseLiteralRangeAndFallback(t *testing.T) {
	sections := parseModINI("[CustomShaderPose]\n$freq=$freq+24*$dt\nif $freq > 9\n$freq=2\nendif")
	clips, explicit := detectModelViewerGIMIShapePoseClips(sections, sections[0], "$freq", nil, nil, "test", 11)
	if !explicit || len(clips) != 1 || clips[0].FrameStart != 2 || clips[0].FrameEnd != 9 || clips[0].FPS != 24 {
		t.Fatal("literal range was lost")
	}
	sections = parseModINI("[CustomShaderPose]\n$freq=$freq+24*$dt")
	clips, explicit = detectModelViewerGIMIShapePoseClips(sections, sections[0], "$freq", nil, nil, "test", 11)
	if explicit || len(clips) != 0 {
		t.Fatal("missing range must allow full-pose fallback")
	}
	for _, stride := range []int{20, 24} {
		if modelViewerPackedObjectLayout("", stride).Elements[2].AlignedByteOffset != 12 {
			t.Fatal("legacy packed UV offset changed")
		}
	}
}

// readLocalSiblingTexcoord finds the separate UV stream a packed base may ship
// next to its position buffer, using the same candidates the loader probes.
func readLocalSiblingTexcoord(t *testing.T, dir, positionPath string, vertexCount int) []byte {
	t.Helper()
	relative, err := filepath.Rel(dir, positionPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, filename := range modelViewerSiblingTexcoordFilenames(modelViewerResource{Filename: relative}) {
		path := filepath.Join(dir, filepath.FromSlash(filename))
		info, statErr := os.Stat(path)
		if statErr != nil || !info.Mode().IsRegular() || info.Size() != int64(vertexCount*4) {
			continue
		}
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatal(readErr)
		}
		return raw
	}
	t.Fatalf("sibling texcoord stream not found next to %s", relative)
	return nil
}

// Opt-in integration test; no local mod assets or machine-specific paths enter CI.
func TestPackedDualQuaternionLocalMod(t *testing.T) {
	dir := os.Getenv("MODEL_VIEWER_PACKED_DQ_MOD")
	if dir == "" {
		t.Skip("set MODEL_VIEWER_PACKED_DQ_MOD to the sample mod folder")
	}
	paths, err := filepath.Glob(filepath.Join(dir, "*.ini"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("INI discovery: %v", err)
	}
	budget, err := newModelViewerLoadBudget(dir)
	if err != nil {
		t.Fatal(err)
	}
	service := &Tools{}
	prepared, err := service.prepareModelViewerGeometry(context.Background(), dir, paths, budget)
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.cache.releaseAll()
	if len(prepared.computeDeformers) != 1 || len(prepared.computeAnimations) != 1 {
		t.Fatalf("deformers=%d animations=%d", len(prepared.computeDeformers), len(prepared.computeAnimations))
	}
	d, c := prepared.computeDeformers[0], prepared.computeAnimations[0]
	if d.Kind != modelViewerPackedDualQuaternionKind || d.VertexCount != 23775 || d.Pose.BoneCount != 446 ||
		d.Pose.FrameCount != 5160 ||
		c.FrameStart != 6 ||
		c.FrameEnd != 5157 ||
		c.FPS != 24 {
		t.Fatalf("unexpected local mod: %+v clip=%d..%d fps=%g", d, c.FrameStart, c.FrameEnd, c.FPS)
	}
	t.Logf(
		"vertices=%d bones=%d poseFrames=%d clip=%d..%d fps=%g",
		d.VertexCount,
		d.Pose.BoneCount,
		d.Pose.FrameCount,
		c.FrameStart,
		c.FrameEnd,
		c.FPS,
	)
	uv := readLocalSiblingTexcoord(t, dir, d.Base.sourcePath, d.VertexCount)
	checked := 0
	for _, work := range prepared.textures {
		for _, mesh := range work.meshes {
			geometry := mesh.geometry
			if geometry == nil || len(geometry.Texcoord0) != geometry.VertexCount*2 {
				t.Fatal("missing local mesh UVs")
			}
			for vertex := range geometry.VertexCount {
				index := vertex
				if len(geometry.SourceIndices) > 0 {
					index = int(geometry.SourceIndices[vertex])
				}
				for axis := range 2 {
					want := modelViewerHalfToFloat(binary.LittleEndian.Uint16(uv[index*4+axis*2:]))
					if axis == 1 {
						want = 1 - want // Mesh construction flips V for renderer textures.
					}
					if geometry.Texcoord0[vertex*2+axis] != want {
						t.Fatalf("local UV mismatch at source vertex %d axis %d", index, axis)
					}
				}
				checked++
			}
		}
	}
	if checked == 0 {
		t.Fatal("no local UVs checked")
	}
	t.Logf("verified UVs against original stream for %d mesh vertices", checked)
}

// Opt-in integration test for the 24-byte packed base variant, whose frame
// accumulator and state ranges may live in [Present]; no local mod assets or
// machine-specific paths enter CI.
func TestPackedDualQuaternion24LocalMod(t *testing.T) {
	dir := os.Getenv("MODEL_VIEWER_PACKED_DQ24_MOD")
	if dir == "" {
		t.Skip("set MODEL_VIEWER_PACKED_DQ24_MOD to the sample mod folder")
	}
	paths, err := filepath.Glob(filepath.Join(dir, "*.ini"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("INI discovery: %v", err)
	}
	budget, err := newModelViewerLoadBudget(dir)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := (&Tools{}).prepareModelViewerGeometry(context.Background(), dir, paths, budget)
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.cache.releaseAll()
	if len(prepared.computeDeformers) != 1 || len(prepared.computeAnimations) != 6 {
		t.Fatalf(
			"deformers=%d animations=%d",
			len(prepared.computeDeformers),
			len(prepared.computeAnimations),
		)
	}
	d := prepared.computeDeformers[0]
	if d.Kind != modelViewerPackedDualQuaternionKind || d.Base.Stride != modelViewerPackedObjectStride24 ||
		d.VertexCount != 107249 || d.Pose == nil || d.Pose.BoneCount != 821 || d.Pose.FrameCount != 1606 {
		t.Fatalf("unexpected local deformer: %+v", d)
	}
	first, last := prepared.computeAnimations[0], prepared.computeAnimations[len(prepared.computeAnimations)-1]
	if first.FrameStart != 85 || first.FrameEnd != 1005 || first.FPS != 24 ||
		last.FrameStart != 1545 || last.FrameEnd != 1605 || last.FPS != 24 {
		t.Fatalf("unexpected local clips: %+v .. %+v", first, last)
	}
	base, err := os.ReadFile(d.Base.sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, work := range prepared.textures {
		for _, mesh := range work.meshes {
			geometry := mesh.geometry
			if geometry == nil || len(geometry.Texcoord0) != geometry.VertexCount*2 {
				t.Fatal("missing local mesh UVs")
			}
			for vertex := range geometry.VertexCount {
				index := vertex
				if len(geometry.SourceIndices) > 0 {
					index = int(geometry.SourceIndices[vertex])
				}
				for axis := range 2 {
					want := modelViewerHalfToFloat(
						binary.LittleEndian.Uint16(base[index*d.Base.Stride+16+axis*2:]),
					)
					if axis == 1 {
						want = 1 - want // Mesh construction flips V for renderer textures.
					}
					if geometry.Texcoord0[vertex*2+axis] != want {
						t.Fatalf("local UV mismatch at source vertex %d axis %d", index, axis)
					}
				}
				checked++
			}
		}
	}
	if checked == 0 {
		t.Fatal("no local UVs checked")
	}
	t.Logf(
		"vertices=%d bones=%d poseFrames=%d clips=%d",
		d.VertexCount,
		d.Pose.BoneCount,
		d.Pose.FrameCount,
		len(prepared.computeAnimations),
	)
}
