package tools

import (
	"slices"
	"strings"
)

// Packed dual-quaternion armature shaders pose game vertices with Blender-space
// dual quaternions. Their base records come in three packed layouts: the 20-byte
// record, the 24-byte object record with an explicit UV0/UV1 pair, and the
// 28-byte object record that keeps a vertex color word before the pair. Most
// exports keep the legacy interpolation body; the newer object build replaces
// the pose math as well. Every variant reports the stride and diffuse UV offset
// later stages decode.

type modelViewerPackedDualQuaternionVariant struct {
	baseStride     int
	texcoordOffset int
	poseVariant    string
	vertexStructs  []string
	required       []string
}

// modelViewerPackedDualQuaternionLegacyBody is shared by exports that differ
// only in the vertex record they declare, including 24-byte object ports that
// keep the original per-weight math.
var modelViewerPackedDualQuaternionLegacyBody = []string{
	"structposeattributes{float3s;float3t;float4qr;float4qd;}",
	"rwstructuredbuffer<vertexattributes>", "register(u5)",
	"structuredbuffer<vertexattributes>base:register(t50)",
	"structuredbuffer<blendattributes>blend:register(t51)",
	"structuredbuffer<poseattributes>pose:register(t52)",
	"#definetimeiniparams[88].x", "#definevg_countiniparams[89].x",
	"frame=(int)floor(time)", "inter=time-floor(time)",
	"blend_indicy=frame*((int)vg_count)+blend[i].indicies",
	"blend_indicy_next=(frame+1)*((int)vg_count)+blend[i].indicies",
	"pos.x=f16tof32(base[i].position.x&0xffff)",
	"pos.z=f16tof32(base[i].position.x>>16)",
	"pos.y=f16tof32(base[i].position.y&0xffff)*-1.0f", "pos.w=1.0f",
	"scale=pose[blend_indicy.x].s*blend[i].weights.x*(1.0f-inter)",
	"bias=pose[blend_indicy.x].t*blend[i].weights.x*(1.0f-inter)",
	"pos.xyz=pos.xyz*scale+bias",
	"qr=pose[blend_indicy.x].qr*blend[i].weights.x*(1.0f-inter)",
	"qd=pose[blend_indicy.x].qd*blend[i].weights.x*(1.0f-inter)",
	"if(dot(pose[blend_indicy.x].qr,pose[blend_indicy.y].qr)<0)",
	"if(dot(pose[blend_indicy.x].qr,pose[blend_indicy_next.x].qr)<0)",
	"qd=qd/length(qr)", "qr=qr/length(qr)",
	"-qd.w*qr.x+qd.x*qr.w-qd.y*qr.z+qd.z*qr.y",
	"pos_result.x=dot(trans,pos)", "normal_result.x=dot(trans,normal)",
	"rw_buffer[i].position.x=(uint)f32tof16(pos_result.z)<<16|(uint)f32tof16(pos_result.x)",
	"rw_buffer[i].position.y=(uint)f32tof16(pos_result.w)<<16|(uint)f32tof16(pos_result.y*-1.0f)",
	"i32toi8(int(normal_result.y*-1.0f))<<16",
}

// modelViewerPackedDualQuaternionObjectBody is the newer object build of the
// S/T + QR/QD armature skinning, shared by the 24-byte and 28-byte records.
var modelViewerPackedDualQuaternionObjectBody = []string{
	"structposeattributes{float3s;float3t;float4qr;float4qd;}",
	"rwstructuredbuffer<vertexattributes>", "register(u5)",
	"structuredbuffer<vertexattributes>base:register(t50)",
	"structuredbuffer<blendattributes>blend:register(t51)",
	"structuredbuffer<poseattributes>pose:register(t52)",
	"#definetimeiniparams[88].x", "#definevg_countiniparams[89].x",
	"(int)time", "frac(time)",
	"idx_prev=frame*vg_count+b.indicies",
	"idx_next=(frame+1)*vg_count+b.indicies",
	"p0_prev.s*weights.x", "p0_prev.t*weights.x",
	"pos.xyz=pos.xyz*scale+bias",
	"sign(dot(p0_prev.qr,", "p0_prev.qd*weights.x",
	"qr/=qr_len", "qd/=qr_len",
	"-qdw*qx+qdx*qw-qdy*qz+qdz*qy",
	"m00*pos.x+m01*pos.y+m02*pos.z",
	"m00*normal.x+m01*normal.y+m02*normal.z",
	"rw_buffer[i].position.x=(uint)f32tof16(pos_result.z)<<16",
	"i32toi8(int(normal_result",
}

var modelViewerPackedDualQuaternionVariants = []modelViewerPackedDualQuaternionVariant{
	{
		baseStride:     modelViewerPackedObjectStride,
		texcoordOffset: 16,
		vertexStructs: []string{
			"structvertexattributes{uint2position;uintnormal;uinttangent;uinttexcoord;}",
		},
		required: modelViewerPackedDualQuaternionLegacyBody,
	},
	{
		baseStride:     modelViewerPackedObjectStride24,
		texcoordOffset: 16,
		vertexStructs: []string{
			"structvertexattributes{uint2position;uintnormal;uinttangent;uinttexcoord;uinttexcoord1;}",
		},
		required: modelViewerPackedDualQuaternionLegacyBody,
	},
	{
		baseStride:     modelViewerPackedObjectStride28,
		texcoordOffset: 20,
		vertexStructs: []string{
			"structvertexattributes{uint2position;uintnormal;uinttangent;uintcolor;uinttexcoord;uinttexcoord1;}",
		},
		required: modelViewerPackedDualQuaternionLegacyBody,
	},
	{
		baseStride:     modelViewerPackedObjectStride24,
		texcoordOffset: 16,
		vertexStructs: []string{
			"structvertexattributes{uint2position;uintnormal;uinttangent;uinttexcoord0;uinttexcoord1;}",
		},
		poseVariant: "object",
		required:    modelViewerPackedDualQuaternionObjectBody,
	},
	{
		baseStride:     modelViewerPackedObjectStride28,
		texcoordOffset: 20,
		vertexStructs: []string{
			"structvertexattributes{uint2position;uintnormal;uinttangent;uintcolor;uinttexcoord0;uinttexcoord1;}",
		},
		poseVariant: "object",
		required:    modelViewerPackedDualQuaternionObjectBody,
	},
}

// modelViewerPackedDualQuaternionLayout reports the packed vertex stride and
// diffuse UV offset a recognized dual-quaternion armature shader consumes.
func modelViewerPackedDualQuaternionLayout(shader string) (int, int, bool) {
	variant, known := modelViewerPackedDualQuaternionVariantForShader(shader)
	return variant.baseStride, variant.texcoordOffset, known
}

func modelViewerPackedDualQuaternionVariantForShader(shader string) (modelViewerPackedDualQuaternionVariant, bool) {
	compact := compactModelViewerShader(shader)
	if !strings.Contains(compact, "structblendattributes{float4weights;int4indicies;}") {
		return modelViewerPackedDualQuaternionVariant{}, false
	}
	for _, variant := range modelViewerPackedDualQuaternionVariants {
		if !slices.ContainsFunc(variant.vertexStructs, func(signature string) bool {
			return strings.Contains(compact, signature)
		}) {
			continue
		}
		if modelViewerShaderContainsAll(compact, variant.required) {
			return variant, true
		}
	}
	return modelViewerPackedDualQuaternionVariant{}, false
}

// modelViewerPackedDualQuaternionBaseStride reports the packed vertex stride a
// recognized dual-quaternion armature shader consumes.
func modelViewerPackedDualQuaternionBaseStride(shader string) (int, bool) {
	stride, _, known := modelViewerPackedDualQuaternionLayout(shader)
	return stride, known
}

func isKnownModelViewerPackedDualQuaternionShader(shader string) bool {
	_, known := modelViewerPackedDualQuaternionBaseStride(shader)
	return known
}

func modelViewerShaderContainsAll(compact string, required []string) bool {
	for _, signature := range required {
		if !strings.Contains(compact, signature) {
			return false
		}
	}
	return true
}
