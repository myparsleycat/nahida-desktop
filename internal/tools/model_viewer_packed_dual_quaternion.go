package tools

import "strings"

// Packed dual-quaternion armature shaders pose game vertices with Blender-space
// dual quaternions. Their base records come in two packed layouts: the legacy
// 20-byte record with the UV in the tangent slot, and the 24-byte object record
// with an explicit UV0/UV1 pair. The pose math differs between the two builds
// as well, so each variant keeps its own signature list and reports the stride
// every later stage must decode.

type modelViewerPackedDualQuaternionVariant struct {
	baseStride int
	required   []string
}

var modelViewerPackedDualQuaternionVariants = []modelViewerPackedDualQuaternionVariant{
	{
		baseStride: modelViewerPackedObjectStride,
		required: []string{
			"structvertexattributes{uint2position;uintnormal;uinttangent;uinttexcoord;}",
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
		},
	},
	{
		baseStride: modelViewerPackedObjectStride24,
		required: []string{
			"structvertexattributes{uint2position;uintnormal;uinttangent;uinttexcoord0;uinttexcoord1;}",
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
		},
	},
}

// modelViewerPackedDualQuaternionBaseStride reports the packed vertex stride a
// recognized dual-quaternion armature shader consumes.
func modelViewerPackedDualQuaternionBaseStride(shader string) (int, bool) {
	compact := compactModelViewerShader(shader)
	if !strings.Contains(compact, "structblendattributes{float4weights;int4indicies;}") {
		return 0, false
	}
	for _, variant := range modelViewerPackedDualQuaternionVariants {
		if modelViewerShaderContainsAll(compact, variant.required) {
			return variant.baseStride, true
		}
	}
	return 0, false
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
