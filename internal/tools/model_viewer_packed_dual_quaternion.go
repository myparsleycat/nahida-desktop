package tools

import "strings"

// This kernel uses packed game vertices with Blender-space dual-quaternion poses.
// In particular, its UV follows the tangent, unlike the cyclic matrix kernel.
func isKnownModelViewerPackedDualQuaternionShader(shader string) bool {
	compact := compactModelViewerShader(shader)
	required := []string{
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
	}
	for _, signature := range required {
		if !strings.Contains(compact, signature) {
			return false
		}
	}
	return strings.Contains(compact, "structblendattributes{float4weights;int4indicies;}")
}
