#include "ParticleConfig.hlsl"
Texture1D<float4> IniParams:register(t120);
#include "Puddle.hlsl"
#include "Slots.hlsl"
StructuredBuffer<float4> frameState:register(t105);
#include "../bodymesh/Dimensions.hlsl"
#define BODY_SEARCH
#include "../bodymesh/Attachment.hlsl"
#include "Yogurt.hlsl"
StructuredBuffer<YParcel> source:register(t100);
[numthreads(64,1,1)]
void main(uint3 id:SV_DispatchThreadID){
 uint i=id.x;if(i>=YOG_COUNT)return;
 YParcel p=source[i];
 if(p.pos.w<=0)return;
 uint owner=splashSlot(p.vel.w);
 if((uint)p.bend.w!=slotGeneration(owner)||!slotLive(owner)||slotRebound(owner)||!slotYogurtMode(owner)){bodyBindings[i*3]=0;return;}
 float dt=IniParams[239].x*2,now=IniParams[239].y;
 float3 shift=frameState[owner*4].w>.5?frameState[owner*4].xyz-slotRoot(owner):0;
 if(length(shift)>1.5){bodyBindings[i*3]=0;return;}
 float3 from=p.pos.xyz+shift,endPoint=from,normal=p.nrm.xyz;
 uint mesh;
 float4 binding=bodyBindings[i*3],saved=bodyBindings[i*3+1];
 // Validate against the current capture before renewing the consumer's timestamp.
 if(binding.x>0&&binding.y==slotAnchor(owner,4).y&&binding.z==p.aux.y&&bodyBindings[i*3+2].x==slotGeneration(owner)&&now>=saved.z){
  uint model=bodyModelMap[(uint)binding.y],cached=(uint)abs(binding.w)-1;
  [loop]for(uint k=0;k<8&&model>0;k++){
   uint candidate=(cached+k)%8,base=candidate*BODY_STRIDE;
   float4 header=bodyAtlas[base],meta=bodyAtlas[base+1];
   if(meta.w!=1||(uint)meta.x!=model||saved.w!=meta.z||now-header.w<-.02||now-header.w>=.05||length(header.xyz-slotRoot(owner))>=.01)continue;
   if(binding.x>meta.y)continue;
   binding.w=(binding.w<0?-1.0:1.0)*float(candidate+1);bodyBindings[i*3]=binding;
   bodyBindings[i*3+1]=float4(saved.xy,now,saved.w);return;
  }
 }
 bodyBindings[i*3]=0;
 bool swept=false;
 if(p.nrm.w<.5){
  normal=yogEmitterNormal(owner);
  swept=!(p.aux.z==YOG_KIND_EMITTED&&p.aux.w<.3&&slotSeen(owner)&&dot(normal,normal)>.5);
  if(swept){float3 velocity=(p.vel.xyz+float3(0,-9.81,0)*dt)*exp(-.6*dt);endPoint=from+velocity*dt;}
 }else if(from.y<=.03&&normal.y>=PUDDLE_MIN_UP)return;
 bodyBegin(i,owner,p.aux.y,YOG_LIFT,endPoint,swept,from,normal,mesh);
}
