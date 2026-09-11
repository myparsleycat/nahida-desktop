package tools

import (
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestDetectModelViewerGIMIShapePoseComputeAnimation(t *testing.T) {
	dir := t.TempDir()
	writeSizedFile := func(name string, size int) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), make([]byte, size), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeSizedFile("base.buf", 3*40)
	writeSizedFile("key1.buf", 3*40)
	writeSizedFile("key2.buf", 3*40)
	writeSizedFile("blend.buf", 3*32)
	writeSizedFile("pose.buf", 3*2*56)
	shapeShader := `
struct VertexAttributes { float3 position; float3 normal; float4 tangent; };
RWStructuredBuffer<VertexAttributes> rw_buffer : register(u5);
StructuredBuffer<VertexAttributes> base : register(t50);
StructuredBuffer<VertexAttributes> shapekey : register(t51);
	#define FREQ IniParams[88].x
	void main(uint3 id) {
	  uint i=id.x;
	  VertexAttributes diff;
	  diff.position = shapekey[i].position - base[i].position;
	  diff.normal = shapekey[i].normal - base[i].normal;
	  diff.tangent = shapekey[i].tangent - base[i].tangent;
	  rw_buffer[i].position += diff.position * (0.5*(sin(FREQ*30)+1));
	  rw_buffer[i].normal += diff.normal * (0.5*(sin(FREQ*30)+1));
	  rw_buffer[i].tangent += diff.tangent * (0.5*(sin(FREQ*30)+1));
	}`
	boneShader := `
	struct VertexAttributes { float3 position; float3 normal; float4 tangent; };
	struct BlendAttributes { float4 weights; int4 indicies; };
	struct PoseAttributes { float3 S; float3 T; float4 QR; float4 QD; };
RWStructuredBuffer<VertexAttributes> outbuf : register(u5);
StructuredBuffer<VertexAttributes> base : register(t50);
	StructuredBuffer<BlendAttributes> blend : register(t51);
	StructuredBuffer<PoseAttributes> pose : register(t52);
	void main(uint3 id) {
	 int frame=0, vg_count=2; BlendAttributes b=blend[id.x]; float4 weights=b.weights;
	 int4 idx_prev=frame*vg_count+b.indicies;
	 int4 idx_next=(frame+1)*vg_count+b.indicies;
	 PoseAttributes p0_prev=pose[idx_prev.x], p1_prev=pose[idx_prev.y], p0_next=pose[idx_next.x];
	 float3 scale=p0_prev.S*weights.x; float3 bias=p0_prev.T*weights.x;
	 float4 pos=float4(base[id.x].position,1); pos.xyz=pos.xyz*scale+bias;
	 float4 qr=p0_prev.QR*weights.x; float4 qd=p0_prev.QD*weights.x;
	 qr+=p1_prev.QR*weights.y*sign(dot(p0_prev.QR,p1_prev.QR));
	 float qr_len=length(qr); qr/=qr_len; qd/=qr_len;
	 float qx=qr.x,qy=qr.y,qz=qr.z,qw=qr.w,qdx=qd.x,qdy=qd.y,qdz=qd.z,qdw=qd.w;
	 float m00=1-2*qy*qy-2*qz*qz,m01=2*(qx*qy-qw*qz),m02=2*(qx*qz+qw*qy);
	 float t0=2*(-qdw*qx+qdx*qw-qdy*qz+qdz*qy);
	 float4 normal=float4(base[id.x].normal,0),pos_result,normal_result;
	 pos_result.x=m00*pos.x+m01*pos.y+m02*pos.z+t0*pos.w;
	 normal_result.x=m00*normal.x+m01*normal.y+m02*normal.z;
	 outbuf[id.x].position=float3(pos_result.x,pos_result.y,pos_result.z);
	 outbuf[id.x].normal=normalize(float3(normal_result.x,normal_result.y,normal_result.z));
	}`
	if err := os.WriteFile(filepath.Join(dir, "shape.hlsl"), []byte(shapeShader), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bone.hlsl"), []byte(boneShader), 0o600); err != nil {
		t.Fatal(err)
	}
	parsed := parseModelViewerINI(`
[Constants]
global $freq_key = 0
global $freq_pose = 0
global $speed = 0.1
global $dt
global $bones = 2
global $start = 0
global $end = 2
global persist $anime_state = 0

[Present]
if $anime_state == 0
  $start = 0
  $end = 2
endif
run = CustomShaderShape
run = CustomShaderPose

[CustomShaderShape]
$freq_key = $freq_key + $speed * $dt
if $freq_key > 5.236
  $freq_key = 0
endif
x88 = $freq_key
cs-t50 = copy ResourcePosition.2
cs-t51 = copy ResourcePosition_key1
cs = shape.hlsl
cs-u5 = copy ResourcePosition.2
Dispatch = 1,1,1
x88 = $freq_key - 0.05236
cs-t51 = copy ResourcePosition_key2
ResourcePosition.1 = ref cs-u5
Dispatch = 1,1,1

[CustomShaderPose]
$freq_pose = $freq_pose + 30 * $dt
if $freq_pose > $end
  $freq_pose = $start
endif
x88 = $freq_pose
x89 = $bones
cs-t50 = copy ResourcePosition.1
cs-t51 = copy ResourcePoseBlend
cs-t52 = copy ResourcePose
cs = bone.hlsl
cs-u5 = copy ResourcePosition.1
ResourcePosition = ref cs-u5
Dispatch = 1,1,1

[ResourcePosition]
[ResourcePosition.1]
[ResourcePosition.2]
stride = 40
filename = base.buf
[ResourcePosition_key1]
stride = 40
filename = key1.buf
[ResourcePosition_key2]
stride = 40
filename = key2.buf
[ResourcePoseBlend]
stride = 32
filename = blend.buf
[ResourcePose]
stride = 56
filename = pose.buf
`, filepath.Join(dir, "mod.ini"))
	sections, names := scopeModelViewerSections(parsed.Sections, 0, "")
	resources := resolveModelViewerEffectiveResources(sections, collectModelViewerResources(sections))
	meshes := []modelViewerDirectMesh{
		{id: "mesh", positionFile: "base.buf", geometry: &modelViewerGeometry{VertexCount: 3}},
	}
	deformer, clips := detectModelViewerComputeAnimation(dir, dir, "", sections, resources, meshes, names)
	if deformer == nil {
		t.Fatal("expected known GIMI shape/pose deformer")
	}
	if deformer.VertexCount != 3 || deformer.Pose == nil || deformer.Pose.BoneCount != 2 ||
		deformer.Pose.FrameCount != 3 {
		t.Fatalf("unexpected pose descriptor: %+v", deformer)
	}
	if len(deformer.ShapePasses) != 2 || deformer.ShapePasses[0].AngularScale != 30 ||
		deformer.ShapePasses[1].PhaseOffset != -0.05236 {
		t.Fatalf("unexpected shape passes: %+v", deformer.ShapePasses)
	}
	if len(clips) != 1 || clips[0].FrameStart != 0 || clips[0].FrameEnd != 2 || clips[0].FPS != 30 ||
		clips[0].Label != "Anime State 0" {
		t.Fatalf("unexpected clips: %+v", clips)
	}

	shaderDir := filepath.Join(dir, "nested")
	if err := os.MkdirAll(shaderDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(shaderDir, "shape.hlsl"), []byte(shapeShader), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(shaderDir, "bone.hlsl"), []byte(boneShader), 0o600); err != nil {
		t.Fatal(err)
	}
	scoped, _ := detectModelViewerComputeAnimation(dir, shaderDir, "ini0", sections, resources, meshes, names)
	otherScoped, _ := detectModelViewerComputeAnimation(dir, shaderDir, "ini1", sections, resources, meshes, names)
	if scoped == nil || otherScoped == nil || scoped.ID == deformer.ID || scoped.ID == otherScoped.ID {
		t.Fatalf("compute scope IDs are not unique: base=%v first=%v second=%v", deformer, scoped, otherScoped)
	}

	if err := os.Remove(filepath.Join(dir, "pose.buf")); err != nil {
		t.Fatal(err)
	}
	shapeOnly, shapeClips := detectModelViewerComputeAnimation(dir, dir, "", sections, resources, meshes, names)
	if shapeOnly == nil || shapeOnly.Pose != nil || len(shapeOnly.ShapePasses) != 2 || len(shapeClips) != 1 {
		t.Fatalf(
			"invalid bone data did not preserve shape-only animation: deformer=%+v clips=%+v",
			shapeOnly,
			shapeClips,
		)
	}
}

func TestDetectModelViewerComputeRejectsInvalidBoneCount(t *testing.T) {
	dir := t.TempDir()
	for name, size := range map[string]int{"base.buf": 40, "blend.buf": 32, "pose.buf": 56} {
		if err := os.WriteFile(filepath.Join(dir, name), make([]byte, size), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	boneShader := `
	struct VertexAttributes { float3 position; float3 normal; float4 tangent; };
	struct BlendAttributes { float4 weights; int4 indicies; };
	struct PoseAttributes { float3 S; float3 T; float4 QR; float4 QD; };
RWStructuredBuffer<VertexAttributes> outbuf : register(u5);
StructuredBuffer<VertexAttributes> base : register(t50);
	StructuredBuffer<BlendAttributes> blend : register(t51);
	StructuredBuffer<PoseAttributes> pose : register(t52);
	void main(uint3 id) {
	 int frame=0, vg_count=2; BlendAttributes b=blend[id.x]; float4 weights=b.weights;
	 int4 idx_prev=frame*vg_count+b.indicies;
	 int4 idx_next=(frame+1)*vg_count+b.indicies;
	 PoseAttributes p0_prev=pose[idx_prev.x], p1_prev=pose[idx_prev.y], p0_next=pose[idx_next.x];
	 float3 scale=p0_prev.S*weights.x; float3 bias=p0_prev.T*weights.x;
	 float4 pos=float4(base[id.x].position,1); pos.xyz=pos.xyz*scale+bias;
	 float4 qr=p0_prev.QR*weights.x; float4 qd=p0_prev.QD*weights.x;
	 qr+=p1_prev.QR*weights.y*sign(dot(p0_prev.QR,p1_prev.QR));
	 float qr_len=length(qr); qr/=qr_len; qd/=qr_len;
	 float qx=qr.x,qy=qr.y,qz=qr.z,qw=qr.w,qdx=qd.x,qdy=qd.y,qdz=qd.z,qdw=qd.w;
	 float m00=1-2*qy*qy-2*qz*qz,m01=2*(qx*qy-qw*qz),m02=2*(qx*qz+qw*qy);
	 float t0=2*(-qdw*qx+qdx*qw-qdy*qz+qdz*qy);
	 float4 normal=float4(base[id.x].normal,0),pos_result,normal_result;
	 pos_result.x=m00*pos.x+m01*pos.y+m02*pos.z+t0*pos.w;
	 normal_result.x=m00*normal.x+m01*normal.y+m02*normal.z;
	 outbuf[id.x].position=float3(pos_result.x,pos_result.y,pos_result.z);
	 outbuf[id.x].normal=normalize(float3(normal_result.x,normal_result.y,normal_result.z));
	}`
	if err := os.WriteFile(filepath.Join(dir, "bone.hlsl"), []byte(boneShader), 0o600); err != nil {
		t.Fatal(err)
	}
	parsed := parseModelViewerINI(`[Present]
run = CustomShaderPose
[CustomShaderPose]
x88 = $frame
x89 = 1.5
cs-t50 = ResourcePosition
cs-t51 = ResourceBlend
cs-t52 = ResourcePose
cs = bone.hlsl
cs-u5 = ResourcePosition
ResourcePositionOut = ref cs-u5
Dispatch = 1,1,1
[ResourcePosition]
stride = 40
filename = base.buf
[ResourceBlend]
stride = 32
filename = blend.buf
[ResourcePose]
stride = 56
filename = pose.buf`, filepath.Join(dir, "mod.ini"))
	if deformer, _ := detectModelViewerComputeAnimation(
		dir,
		dir,
		"",
		parsed.Sections,
		collectModelViewerResources(parsed.Sections),
		[]modelViewerDirectMesh{{id: "mesh", positionFile: "base.buf", geometry: &modelViewerGeometry{VertexCount: 1}}},
		nil,
	); deformer != nil &&
		deformer.Pose != nil {
		t.Fatal("non-integral bone count must not enable pose animation")
	}
}

func TestReadModelViewerComputeShaderRejectsOversizedFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "huge.hlsl")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	if err := file.Truncate(maxModelViewerComputeShaderBytes + 1); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, ok := readModelViewerComputeShader(dir, dir, "huge.hlsl"); ok {
		t.Fatal("oversized compute shader was accepted")
	}
}

func TestDetectModelViewerComputeRejectsShaderNameWithoutSignature(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "anim_bone.hlsl"), []byte("void main() {}"), 0o600); err != nil {
		t.Fatal(err)
	}
	parsed := parseModelViewerINI(`[CustomShaderPose]
x88 = $frame
x89 = 2
cs-t50 = ResourcePosition
cs-t51 = ResourceBlend
cs-t52 = ResourcePose
cs = anim_bone.hlsl
cs-u5 = ResourcePosition
ResourcePositionOut = ref cs-u5
Dispatch = 1,1,1`, filepath.Join(dir, "mod.ini"))
	if deformer, _ := detectModelViewerComputeAnimation(
		dir,
		dir,
		"",
		parsed.Sections,
		collectModelViewerResources(parsed.Sections),
		nil,
		nil,
	); deformer != nil {
		t.Fatal("shader filename alone must not enable compute animation")
	}
}

func TestKnownModelViewerBoneShaderRejectsMarkerOnlyImplementation(t *testing.T) {
	shader := `
struct VertexAttributes { float3 position; float3 normal; float4 tangent; };
struct BlendAttributes { float4 weights; int4 indicies; };
struct PoseAttributes { float3 S; float3 T; float4 QR; float4 QD; };
StructuredBuffer<VertexAttributes> base : register(t50);
StructuredBuffer<BlendAttributes> blend : register(t51);
StructuredBuffer<PoseAttributes> pose : register(t52);
void main() {
  int frame=0, vg_count=2; int i=frame*vg_count; int j=(frame+1)*vg_count;
  PoseAttributes p0_prev=pose[i]; float4 qr=p0_prev.QR; float4 qd=p0_prev.QD;
  float s=sign(dot(p0_prev.QR,qr)); float qr_len=length(qr);
  qr/=qr_len; qd/=qr_len; float4 normal_result=qr;
}`
	if isKnownModelViewerGIMIShapePoseBoneShader(shader) {
		t.Fatal("marker-only shader was accepted as a GIMI shape/pose bone shader")
	}
	if isKnownModelViewerGIMIShapePoseBoneShader("// " + strings.Join([]string{
		"StructuredBuffer<VertexAttributes> register(t50)",
		"StructuredBuffer<BlendAttributes> register(t51)",
		"StructuredBuffer<PoseAttributes> register(t52)",
		"frame*vg_count sign(dot( p0_prev.QR p0_prev.QD qr/=qr_len qd/=qr_len",
		"normalize(float3(normal_result",
	}, " ")) {
		t.Fatal("comment-only shader signature was accepted")
	}
}

func TestDetectModelViewerCyclicPackedComputeAnimation(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "base.buf"), make([]byte, 3*20), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "blend.buf"), make([]byte, 3*32), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pose.buf"), make([]byte, 2*2*48), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "anim.hlsl"), []byte(packedObjectAnimShader), 0o600); err != nil {
		t.Fatal(err)
	}
	parsed := parseModelViewerINI(`[Constants]
global $VG_count = 2
global $Freq = 0
global $dt
post ResourceClosetPosition = copy_desc ResourceClosetPosition.1
post run = CustomShaderComputeAnim
[CustomShaderComputeAnim]
$Freq = $Freq + 24 * $dt
x88 = $Freq
x89 = $VG_count
cs-t50 = copy ResourceClosetPosition.1
cs-t51 = copy ResourceClosetBlend
cs-t52 = copy ResourceClosetPose
cs = anim.hlsl
cs-u5 = copy ResourceClosetPosition.1
ResourceClosetPosition = ref cs-u5
Dispatch = 3, 1, 1
[ResourceClosetPosition]
[ResourceClosetPosition.1]
stride = 20
filename = base.buf
[ResourceClosetBlend]
stride = 32
filename = blend.buf
[ResourceClosetPose]
stride = 48
filename = pose.buf
`, filepath.Join(dir, "mod.ini"))
	sections, names := scopeModelViewerSections(parsed.Sections, 0, "")
	resources := resolveModelViewerEffectiveResources(sections, collectModelViewerResources(sections))
	meshes := []modelViewerDirectMesh{
		{id: "mesh", positionFile: "base.buf", geometry: &modelViewerGeometry{VertexCount: 3}},
	}
	deformer, clips := detectModelViewerComputeAnimation(dir, dir, "", sections, resources, meshes, names)
	if deformer == nil || deformer.Kind != modelViewerPackedObjectKind {
		t.Fatalf("deformer = %+v", deformer)
	}
	if deformer.VertexCount != 3 || deformer.Pose == nil || deformer.Pose.BoneCount != 2 ||
		deformer.Pose.FrameCount != 2 ||
		len(deformer.ShapePasses) != 0 {
		t.Fatalf("unexpected cyclic descriptor: %+v", deformer)
	}
	if len(clips) != 1 || clips[0].DeformerID != deformer.ID || clips[0].FPS != 24 {
		t.Fatalf("clips = %+v", clips)
	}
}

func TestDetectModelViewerCyclicPackedSelectsMatchingBranch(t *testing.T) {
	dir := t.TempDir()
	for name, size := range map[string]int{"first.buf": 4 * 20, "base.buf": 3 * 20, "first-blend.buf": 4 * 32, "blend.buf": 3 * 32, "pose.buf": 4 * 2 * 48} {
		if err := os.WriteFile(filepath.Join(dir, name), make([]byte, size), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "anim.hlsl"), []byte(packedObjectAnimShader), 0o600); err != nil {
		t.Fatal(err)
	}
	ini := `[Constants]
global $bones = 2
global $freq = 0
global $dt
global $swap = 0
global $start = 1
global $end = 5
post ResourcePosition = copy_desc ResourcePosition.1
post run = CustomShaderAnim
[CustomShaderAnim]
$freq = $freq + 24 * $dt
if $freq > $end-2
    $freq = $start
endif
x88 = $freq
x89 = $bones
cs = anim.hlsl
cs-t52 = ResourcePose
if $swap == 0
    cs-t50 = ResourcePosition.0
    cs-t51 = ResourceBlend.0
    cs-u5 = copy ResourcePosition.0
    ResourcePosition = ref cs-u5
    Dispatch = 4, 1, 1
else if $swap == 1
    cs-t50 = ResourcePosition.1
    cs-t51 = ResourceBlend.1
    cs-u5 = copy ResourcePosition.1
    ResourcePosition = ref cs-u5
    Dispatch = 3, 1, 1
endif
[ResourcePosition]
[ResourcePosition.0]
stride = 20
filename = first.buf
[ResourcePosition.1]
stride = 20
filename = base.buf
[ResourceBlend.0]
stride = 32
filename = first-blend.buf
[ResourceBlend.1]
stride = 32
filename = blend.buf
[ResourcePose]
stride = 48
filename = pose.buf
`
	for _, test := range []struct {
		name, output string
		want         bool
	}{{"later matching branch", "Position.1", true}, {"unmatched output", "Unmatched", false}} {
		t.Run(test.name, func(t *testing.T) {
			text := strings.Replace(ini, "copy_desc ResourcePosition.1", "copy_desc Resource"+test.output, 1)
			text += "\n[ResourceUnmatched]\nstride = 20\nfilename = unmatched.buf\n"
			sections, names := scopeModelViewerSections(parseModINI(text), 0, "")
			resources := resolveModelViewerEffectiveResources(sections, collectModelViewerResources(sections))
			meshes := []modelViewerDirectMesh{
				{id: "mesh", positionFile: "base.buf", geometry: &modelViewerGeometry{VertexCount: 3}},
			}
			deformer, clips := detectModelViewerComputeAnimation(dir, dir, "", sections, resources, meshes, names)
			if !test.want {
				if deformer != nil || len(clips) != 0 {
					t.Fatal("unmatched geometry enabled animation")
				}
				return
			}
			if deformer == nil || deformer.Kind != modelViewerPackedObjectKind || deformer.VertexCount != 3 ||
				filepath.Base(deformer.Base.sourcePath) != "base.buf" ||
				filepath.Base(deformer.Pose.Blend.sourcePath) != "blend.buf" {
				t.Fatalf("matching branch was not selected: %+v", deformer)
			}
			if len(clips) != 1 || clips[0].FrameStart != 1 || clips[0].FrameEnd != 3 || clips[0].FPS != 24 {
				t.Fatalf("matching branch clip: %+v", clips)
			}
			if !slices.Equal(clips[0].VariableIDs, []string{"mvscope0swap"}) {
				t.Fatalf("branch selector is not owned by the clip: %v", clips[0].VariableIDs)
			}
			for _, frame := range clips[0].Frames {
				if frame.Values["mvscope0swap"] != float64(1) {
					t.Fatalf("frame does not activate its geometry branch: %+v", frame)
				}
			}
		})
	}
}

func TestBindModelViewerComputeBranchRejectsConflictingClips(t *testing.T) {
	clips := []modelViewerPreparedAnimationClip{
		{
			ID:          "matching",
			VariableIDs: []string{"mode"},
			Frames:      []modelViewerPreparedAnimationFrame{{Values: map[string]any{"mode": float64(1)}}},
		},
		{
			ID:          "conflicting",
			VariableIDs: []string{"mode"},
			Frames:      []modelViewerPreparedAnimationFrame{{Values: map[string]any{"mode": float64(0)}}},
		},
	}
	bound := bindModelViewerComputeBranch(clips, []modelViewerStateEquality{{variable: "mode", value: "1"}})
	if len(bound) != 1 || bound[0].ID != "matching" || !slices.Equal(bound[0].VariableIDs, []string{"mode"}) {
		t.Fatalf("conflicting clip was rebound to another branch: %+v", bound)
	}
}

const packedShapeAnimShader = `
struct VertexAttributes {
    uint2 position;
    uint normal;
    uint texcoord;
    uint tangent;
};
RWStructuredBuffer<VertexAttributes> rw_buffer : register(u5);
StructuredBuffer<VertexAttributes> base : register(t50);
StructuredBuffer<VertexAttributes> shapekey : register(t51);
#define FREQ IniParams[88].x
void main(uint3 threadID : SV_DispatchThreadID) {
    uint i = threadID.x;
    float4 t1, t2;
    t1.x = f16tof32(shapekey[i].position.x >> 16) - f16tof32(base[i].position.x >> 16);
    t2 += t1 * (0.5*(sin(FREQ*30)+1));
    rw_buffer[i].position.x = (uint)f32tof16(t2.x)<<16 | (uint)f32tof16(t2.y);
}
`

func writePackedShapeBuffer(t *testing.T, dir, name string) {
	t.Helper()
	buf := make([]byte, 3*modelViewerPackedObjectStride)
	writePackedObjectVertex(buf, 0, 1, 2, 3, 0, 0, 0, 0, 127)
	writePackedObjectVertex(buf, 20, 4, 5, 6, 0, 0, 0, 0, 127)
	writePackedObjectVertex(buf, 40, 7, 8, 9, 0, 0, 0, 0, 127)
	if err := os.WriteFile(filepath.Join(dir, name), buf, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestDetectModelViewerPackedShapeComputeAnimation(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"Kimono1.buf", "Kimono2.buf", "Kimono3.buf", "Kimono4.buf"} {
		writePackedShapeBuffer(t, dir, name)
	}
	if err := os.WriteFile(filepath.Join(dir, "anim.hlsl"), []byte(packedShapeAnimShader), 0o600); err != nil {
		t.Fatal(err)
	}
	parsed := parseModelViewerINI(`[Constants]
global $active
global persist $Speed = 0.5
global $Freq = 0
global $dt
global $anime_state = 0
post ResourceKimono = copy_desc ResourceKimono.1
post run = CustomShaderComputeAnim
[CustomShaderComputeAnim]
$dt = time - $ts
if $anime_state == 0
    if $pause == 0
        $Freq = $Freq + $Speed * $dt
    endif
    if $Freq > 10
        $Freq = -0.05236
        $anime_state = 1
    endif
    x88 = $Freq
    cs-t50 = copy ResourceKimono.1
    cs-t51 = copy ResourceKimono.2
    cs = ./anim.hlsl
    cs-u5 = copy ResourceKimono.1
    ResourceKimono = ref cs-u5
    Dispatch = 3, 1, 1
else if $anime_state == 1
    if $pause == 0
        $Freq = $Freq + $Speed * $dt
    endif
    if $Freq > 0.05236
        $Freq = -0.05236
        $anime_state = 2
    endif
    x88 = $Freq
    cs-t50 = copy ResourceKimono.1
    cs-t51 = copy ResourceKimono.3
    cs = ./anim.hlsl
    cs-u5 = copy ResourceKimono.1
    ResourceKimono = ref cs-u5
    Dispatch = 3, 1, 1
else if $anime_state == 2
    if $pause == 0
        $Freq = $Freq + 5 * $dt
    endif
    if $Freq > 12
        $Freq = -0.05236
        $anime_state = 3
    endif
    x88 = $Freq
    cs-t50 = copy ResourceKimono.3
    cs-t51 = copy ResourceKimono.4
    cs = ./anim.hlsl
    cs-u5 = copy ResourceKimono.3
    ResourceKimono = ref cs-u5
    Dispatch = 3, 1, 1
else if $anime_state == 3
    if $pause == 0
        $Freq = $Freq + 0.1 * $dt
    endif
    x88 = $Freq
    cs-t50 = copy ResourceKimono.3
    cs-t51 = copy ResourceKimono.1
    cs = ./anim.hlsl
    cs-u5 = copy ResourceKimono.3
    ResourceKimono = ref cs-u5
    Dispatch = 3, 1, 1
    if $Freq > 0.05236
        $Freq = -0.05236
        $anime_state = 0
    endif
endif
[ResourceKimono]
[ResourceKimono.1]
stride = 20
filename = Kimono1.buf
[ResourceKimono.2]
stride = 20
filename = Kimono2.buf
[ResourceKimono.3]
stride = 20
filename = Kimono3.buf
[ResourceKimono.4]
stride = 20
filename = Kimono4.buf
`, filepath.Join(dir, "mod.ini"))
	sections, names := scopeModelViewerSections(parsed.Sections, 0, "")
	resources := resolveModelViewerEffectiveResources(sections, collectModelViewerResources(sections))
	meshes := []modelViewerDirectMesh{
		{id: "mesh", positionFile: "Kimono1.buf", geometry: &modelViewerGeometry{VertexCount: 3}},
	}
	deformer, clips := detectModelViewerComputeAnimation(dir, dir, "", sections, resources, meshes, names)
	if deformer == nil || deformer.Kind != modelViewerPackedShapeKind {
		t.Fatalf("deformer = %+v", deformer)
	}
	if deformer.VertexCount != 3 || deformer.Pose != nil || len(deformer.ShapePasses) != 0 ||
		len(deformer.ShapeStages) != 4 {
		t.Fatalf("unexpected packed shape descriptor: %+v", deformer)
	}
	if deformer.ShapeStages[0].PhaseRate != 0.5 || deformer.ShapeStages[0].WrapAt != 10 ||
		deformer.ShapeStages[0].PhaseStart != -0.05236 {
		t.Fatalf("stage0 = %+v", deformer.ShapeStages[0])
	}
	if deformer.ShapeStages[1].PhaseStart != -0.05236 || deformer.ShapeStages[2].PhaseStart != -0.05236 ||
		deformer.ShapeStages[3].PhaseStart != -0.05236 {
		t.Fatalf("incoming phase starts = %+v", deformer.ShapeStages)
	}
	if !samePathFold(deformer.ShapeStages[0].Base.sourcePath, deformer.Base.sourcePath) {
		t.Fatalf("stage0 base = %+v deformer base = %+v", deformer.ShapeStages[0].Base, deformer.Base)
	}
	if deformer.ShapeStages[2].PhaseRate != 5 || deformer.ShapeStages[2].WrapAt != 12 ||
		samePathFold(deformer.ShapeStages[2].Base.sourcePath, deformer.Base.sourcePath) {
		t.Fatalf("stage2 = %+v", deformer.ShapeStages[2])
	}
	if deformer.ShapeStages[3].PhaseRate != 0.1 || deformer.ShapeStages[3].WrapAt != 0.05236 ||
		deformer.ShapeStages[0].Duration <= 0 {
		t.Fatalf("stage3 = %+v", deformer.ShapeStages[3])
	}
	if len(clips) != 1 || clips[0].DeformerID != deformer.ID || clips[0].FrameEnd < 2 {
		t.Fatalf("clips = %+v", clips)
	}
}

func TestDetectModelViewerPackedShapeIncomingPhaseStart(t *testing.T) {
	dir := t.TempDir()
	writePackedShapeBuffer(t, dir, "base.buf")
	writePackedShapeBuffer(t, dir, "key.buf")
	if err := os.WriteFile(filepath.Join(dir, "anim.hlsl"), []byte(packedShapeAnimShader), 0o600); err != nil {
		t.Fatal(err)
	}
	parsed := parseModelViewerINI(`[Constants]
global $Freq = 0
global $dt
global $anime_state = 0
post run = CustomShaderComputeAnim
[CustomShaderComputeAnim]
if $anime_state == 0
    $Freq = $Freq + 1 * $dt
    if $Freq > 10
        $Freq = 3
        $anime_state = 1
    endif
    x88 = $Freq
    cs-t50 = copy ResourceKimono.1
    cs-t51 = copy ResourceKimono.2
    cs = anim.hlsl
    cs-u5 = copy ResourceKimono.1
    ResourceKimono = ref cs-u5
    Dispatch = 3, 1, 1
else if $anime_state == 1
    $Freq = $Freq + 1 * $dt
    if $Freq > 9
        $Freq = 1
        $anime_state = 0
    endif
    x88 = $Freq
    cs-t50 = copy ResourceKimono.2
    cs-t51 = copy ResourceKimono.1
    cs = anim.hlsl
    cs-u5 = copy ResourceKimono.2
    ResourceKimono = ref cs-u5
    Dispatch = 3, 1, 1
endif
[ResourceKimono.1]
stride = 20
filename = base.buf
[ResourceKimono.2]
stride = 20
filename = key.buf
`, filepath.Join(dir, "mod.ini"))
	sections, names := scopeModelViewerSections(parsed.Sections, 0, "")
	resources := resolveModelViewerEffectiveResources(sections, collectModelViewerResources(sections))
	meshes := []modelViewerDirectMesh{
		{id: "mesh", positionFile: "base.buf", geometry: &modelViewerGeometry{VertexCount: 3}},
	}
	deformer, _ := detectModelViewerComputeAnimation(dir, dir, "", sections, resources, meshes, names)
	if deformer == nil || len(deformer.ShapeStages) != 2 {
		t.Fatalf("deformer = %+v", deformer)
	}
	stage0, stage1 := deformer.ShapeStages[0], deformer.ShapeStages[1]
	if stage0.PhaseStart != 1 || stage0.WrapAt != 10 || stage0.Duration != 9 {
		t.Fatalf("stage0 should start from the previous transition reset: %+v", stage0)
	}
	if stage1.PhaseStart != 3 || stage1.WrapAt != 9 || stage1.Duration != 6 {
		t.Fatalf("stage1 should start from the previous transition reset: %+v", stage1)
	}
}

func TestDetectModelViewerPackedShapeIncomingVariableReset(t *testing.T) {
	dir := t.TempDir()
	writePackedShapeBuffer(t, dir, "base.buf")
	writePackedShapeBuffer(t, dir, "key.buf")
	if err := os.WriteFile(filepath.Join(dir, "anim.hlsl"), []byte(packedShapeAnimShader), 0o600); err != nil {
		t.Fatal(err)
	}
	parsed := parseModelViewerINI(`[Constants]
global $start = 1
global $next = 3
global $Freq = 0
global $dt
global $anime_state = 0
post run = CustomShaderComputeAnim
[CustomShaderComputeAnim]
if $anime_state == 0
    $Freq = $Freq + 1 * $dt
    if $Freq > 10
        $Freq = $next
        $anime_state = 1
    endif
    x88 = $Freq
    cs-t50 = copy ResourceKimono.1
    cs-t51 = copy ResourceKimono.2
    cs = anim.hlsl
    cs-u5 = copy ResourceKimono.1
    ResourceKimono = ref cs-u5
    Dispatch = 3, 1, 1
else if $anime_state == 1
    $Freq = $Freq + 1 * $dt
    if $Freq > 9
        $Freq = $start
        $anime_state = 0
    endif
    x88 = $Freq
    cs-t50 = copy ResourceKimono.2
    cs-t51 = copy ResourceKimono.1
    cs = anim.hlsl
    cs-u5 = copy ResourceKimono.2
    ResourceKimono = ref cs-u5
    Dispatch = 3, 1, 1
endif
[ResourceKimono.1]
stride = 20
filename = base.buf
[ResourceKimono.2]
stride = 20
filename = key.buf
`, filepath.Join(dir, "mod.ini"))
	sections, names := scopeModelViewerSections(parsed.Sections, 0, "")
	resources := resolveModelViewerEffectiveResources(sections, collectModelViewerResources(sections))
	meshes := []modelViewerDirectMesh{
		{id: "mesh", positionFile: "base.buf", geometry: &modelViewerGeometry{VertexCount: 3}},
	}
	deformer, _ := detectModelViewerComputeAnimation(dir, dir, "", sections, resources, meshes, names)
	if deformer == nil || len(deformer.ShapeStages) != 2 {
		t.Fatalf("deformer = %+v", deformer)
	}
	stage0, stage1 := deformer.ShapeStages[0], deformer.ShapeStages[1]
	if stage0.PhaseStart != 1 || stage0.WrapAt != 10 || stage0.Duration != 9 {
		t.Fatalf("stage0 = %+v; want the $start-backed incoming reset", stage0)
	}
	if stage1.PhaseStart != 3 || stage1.WrapAt != 9 || stage1.Duration != 6 {
		t.Fatalf("stage1 = %+v; want the $next-backed incoming reset", stage1)
	}
}

func TestDetectModelViewerPackedShapeSinglePass(t *testing.T) {
	dir := t.TempDir()
	writePackedShapeBuffer(t, dir, "base.buf")
	writePackedShapeBuffer(t, dir, "key.buf")
	if err := os.WriteFile(filepath.Join(dir, "anim.hlsl"), []byte(packedShapeAnimShader), 0o600); err != nil {
		t.Fatal(err)
	}
	parsed := parseModelViewerINI(`[Constants]
global $Freq = 0
global $dt
global $speed = 0.5
post run = CustomShaderComputeAnim
[CustomShaderComputeAnim]
$Freq = $Freq + $speed * $dt
if $Freq > 6.283
    $Freq = 0
endif
x88 = $Freq
cs-t50 = copy ResourceKimono.1
cs-t51 = copy ResourceKimono.2
cs = anim.hlsl
cs-u5 = copy ResourceKimono.1
ResourceKimono = ref cs-u5
Dispatch = 3, 1, 1
[ResourceKimono.1]
stride = 20
filename = base.buf
[ResourceKimono.2]
stride = 20
filename = key.buf
`, filepath.Join(dir, "mod.ini"))
	sections, names := scopeModelViewerSections(parsed.Sections, 0, "")
	resources := resolveModelViewerEffectiveResources(sections, collectModelViewerResources(sections))
	meshes := []modelViewerDirectMesh{
		{id: "mesh", positionFile: "base.buf", geometry: &modelViewerGeometry{VertexCount: 3}},
	}
	deformer, clips := detectModelViewerComputeAnimation(dir, dir, "", sections, resources, meshes, names)
	if deformer == nil || deformer.Kind != modelViewerPackedShapeKind || len(deformer.ShapePasses) != 0 ||
		len(deformer.ShapeStages) != 1 {
		t.Fatalf("deformer = %+v", deformer)
	}
	if deformer.ShapeStages[0].PhaseRate != 0.5 || deformer.ShapeStages[0].WrapAt != 6.283 ||
		deformer.ShapeStages[0].Duration <= 0 ||
		len(clips) != 1 {
		t.Fatalf("stage=%+v clips=%+v", deformer.ShapeStages[0], clips)
	}
}

func TestDetectModelViewerPackedShapeVariableBackedReset(t *testing.T) {
	dir := t.TempDir()
	writePackedShapeBuffer(t, dir, "base.buf")
	writePackedShapeBuffer(t, dir, "key.buf")
	if err := os.WriteFile(filepath.Join(dir, "anim.hlsl"), []byte(packedShapeAnimShader), 0o600); err != nil {
		t.Fatal(err)
	}
	parsed := parseModelViewerINI(`[Constants]
global $start = -0.05236
global $Freq = 0
global $dt
global $speed = 0.5
post run = CustomShaderComputeAnim
[CustomShaderComputeAnim]
$Freq = $Freq + $speed * $dt
if $Freq > 10
    $Freq = $start
endif
x88 = $Freq
cs-t50 = copy ResourceKimono.1
cs-t51 = copy ResourceKimono.2
cs = anim.hlsl
cs-u5 = copy ResourceKimono.1
ResourceKimono = ref cs-u5
Dispatch = 3, 1, 1
[ResourceKimono.1]
stride = 20
filename = base.buf
[ResourceKimono.2]
stride = 20
filename = key.buf
`, filepath.Join(dir, "mod.ini"))
	sections, names := scopeModelViewerSections(parsed.Sections, 0, "")
	resources := resolveModelViewerEffectiveResources(sections, collectModelViewerResources(sections))
	meshes := []modelViewerDirectMesh{
		{id: "mesh", positionFile: "base.buf", geometry: &modelViewerGeometry{VertexCount: 3}},
	}
	deformer, clips := detectModelViewerComputeAnimation(dir, dir, "", sections, resources, meshes, names)
	if deformer == nil || len(deformer.ShapeStages) != 1 {
		t.Fatalf("deformer = %+v", deformer)
	}
	stage := deformer.ShapeStages[0]
	if stage.PhaseStart != -0.05236 {
		t.Fatalf("phase start = %v; want the $start default", stage.PhaseStart)
	}
	if want := (10 + 0.05236) / 0.5; math.Abs(stage.Duration-want) > 1e-9 {
		t.Fatalf("duration = %v; want %v", stage.Duration, want)
	}
	if len(clips) != 1 {
		t.Fatalf("clips = %+v", clips)
	}
}

func TestFindModelViewerAccumulatorLoopResolvesDefaultVariables(t *testing.T) {
	sections := parseModINI(`[CustomShaderComputeAnim]
$Freq = $Freq + 0.2 * $dt
if $Freq > 5.18364
    $Freq = $start
endif`)
	wrap, reset := findModelViewerAccumulatorLoop(sections, "Freq", map[string]any{"start": -0.05236})
	if wrap != 5.18364 || reset != -0.05236 {
		t.Fatalf("wrap = %v reset = %v; want the $start default", wrap, reset)
	}
}

func TestFindModelViewerAccumulatorLoopKeepsResetInsideSelectedGuard(t *testing.T) {
	for _, test := range []struct {
		name  string
		ini   string
		wrap  float64
		reset float64
	}{
		{
			name: "ignore key and state entry assignments",
			ini: `[KeySeek]
$Freq = 1
[CustomShaderComputeAnim]
$Freq = 2
if $Freq > 5 ; loop boundary
    $Freq = 0 ; loop reset
endif`,
			wrap: 5,
		},
		{
			name: "ignore nested assignments",
			ini: `[CustomShaderComputeAnim]
if $enabled
    if $Freq > 5
        if $mode
            $Freq = 2
        endif
        $Freq = -0.5
    endif
endif`,
			wrap:  5,
			reset: -0.5,
		},
		{
			name: "ignore else branch",
			ini: `[CustomShaderComputeAnim]
if $Freq > 5
    $unrelated = 1
else
    $Freq = 2
endif`,
			wrap: 5,
		},
		{
			name: "do not borrow reset from another loop",
			ini: `[CustomShaderComputeAnim]
if $Freq > 5
    $unrelated = 1
endif
[CustomShaderOther]
if $Freq > 10
    $Freq = 3
endif`,
			wrap: 5,
		},
		{
			name: "no loop guard",
			ini: `[KeySeek]
$Freq = -0.5`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			wrap, reset := findModelViewerAccumulatorLoop(parseModINI(test.ini), "Freq", nil)
			if wrap != test.wrap || reset != test.reset {
				t.Fatalf("loop = (%v, %v); want (%v, %v)", wrap, reset, test.wrap, test.reset)
			}
		})
	}
}

func TestModelViewerPackedShapeDurationWithoutWrap(t *testing.T) {
	stage := ModelViewerComputeShapeStage{PhaseStart: -0.5, PhaseRate: 1, AngularScale: 1}
	if duration := modelViewerPackedShapeStageDuration(stage); math.Abs(duration-2*math.Pi) > 1e-9 {
		t.Fatalf("duration = %v; want a complete sine period without a wrap boundary", duration)
	}
}

func TestDetectModelViewerPackedShapeRejectsUnsequencedPasses(t *testing.T) {
	dir := t.TempDir()
	writePackedShapeBuffer(t, dir, "base.buf")
	writePackedShapeBuffer(t, dir, "key.buf")
	if err := os.WriteFile(filepath.Join(dir, "anim.hlsl"), []byte(packedShapeAnimShader), 0o600); err != nil {
		t.Fatal(err)
	}
	parsed := parseModelViewerINI(`[Constants]
global $Freq = 0
global $dt
global $speed = 0.5
post run = CustomShaderComputeAnim
[CustomShaderComputeAnim]
$Freq = $Freq + $speed * $dt
x88 = $Freq
cs-t50 = copy ResourceKimono.1
cs-t51 = copy ResourceKimono.2
cs = anim.hlsl
cs-u5 = copy ResourceKimono.1
ResourceKimono = ref cs-u5
Dispatch = 3, 1, 1
x88 = $Freq
cs-t50 = copy ResourceKimono.1
cs-t51 = copy ResourceKimono.2
cs = anim.hlsl
cs-u5 = copy ResourceKimono.1
ResourceKimono = ref cs-u5
Dispatch = 3, 1, 1
[ResourceKimono.1]
stride = 20
filename = base.buf
[ResourceKimono.2]
stride = 20
filename = key.buf
`, filepath.Join(dir, "mod.ini"))
	sections, names := scopeModelViewerSections(parsed.Sections, 0, "")
	resources := resolveModelViewerEffectiveResources(sections, collectModelViewerResources(sections))
	meshes := []modelViewerDirectMesh{
		{id: "mesh", positionFile: "base.buf", geometry: &modelViewerGeometry{VertexCount: 3}},
	}
	deformer, clips := detectModelViewerComputeAnimation(dir, dir, "", sections, resources, meshes, names)
	if deformer != nil || clips != nil {
		t.Fatalf("unsequenced packed shape passes should fail closed: deformer=%+v clips=%+v", deformer, clips)
	}
}

func TestPackedShapeShaderRejectsBoneKernel(t *testing.T) {
	if isKnownModelViewerPackedShapeShader(packedObjectAnimShader) {
		t.Fatal("packed bone shader must not be classified as packed shapekey")
	}
	if !isKnownModelViewerPackedShapeShader(packedShapeAnimShader) {
		t.Fatal("packed shapekey shader was rejected")
	}
}
