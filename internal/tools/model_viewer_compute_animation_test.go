package tools

import (
	"os"
	"path/filepath"
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
	meshes := []modelViewerDirectMesh{{id: "mesh", positionFile: "base.buf", geometry: &modelViewerGeometry{VertexCount: 3}}}
	deformer, clips := detectModelViewerComputeAnimation(dir, dir, "", sections, resources, meshes, names)
	if deformer == nil {
		t.Fatal("expected known GIMI shape/pose deformer")
	}
	if deformer.VertexCount != 3 || deformer.Pose == nil || deformer.Pose.BoneCount != 2 || deformer.Pose.FrameCount != 3 {
		t.Fatalf("unexpected pose descriptor: %+v", deformer)
	}
	if len(deformer.ShapePasses) != 2 || deformer.ShapePasses[0].AngularScale != 30 || deformer.ShapePasses[1].PhaseOffset != -0.05236 {
		t.Fatalf("unexpected shape passes: %+v", deformer.ShapePasses)
	}
	if len(clips) != 1 || clips[0].FrameStart != 0 || clips[0].FrameEnd != 2 || clips[0].FPS != 30 || clips[0].Label != "Anime State 0" {
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
		t.Fatalf("invalid bone data did not preserve shape-only animation: deformer=%+v clips=%+v", shapeOnly, shapeClips)
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
	if deformer, _ := detectModelViewerComputeAnimation(dir, dir, "", parsed.Sections, collectModelViewerResources(parsed.Sections), []modelViewerDirectMesh{{id: "mesh", positionFile: "base.buf", geometry: &modelViewerGeometry{VertexCount: 1}}}, nil); deformer != nil && deformer.Pose != nil {
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
	if deformer, _ := detectModelViewerComputeAnimation(dir, dir, "", parsed.Sections, collectModelViewerResources(parsed.Sections), nil, nil); deformer != nil {
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
