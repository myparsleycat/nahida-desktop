#include "ParticleConfig.hlsl"
Texture1D<float4> IniParams:register(t120);
#include "Slots.hlsl"
#include "Puddle.hlsl"
StructuredBuffer<float4> frameState:register(t105);
struct FluidParticle{float4 positionAge;float4 velocityAlive;float4 predictedLambda;float4 densityBirth;};
StructuredBuffer<FluidParticle> source:register(t100);
#define BODY_SEARCH
#include "../bodymesh/Attachment.hlsl"
[numthreads(64,1,1)]
void main(uint3 id:SV_DispatchThreadID){
 uint i=id.x;if(i>=SPLASH_COUNT)return;
 FluidParticle p=source[i];
 if(p.velocityAlive.w<.5||floorPuddle(p.densityBirth.w))return;
 uint owner=splashSlot(p.velocityAlive.w);float now=IniParams[239].y;
 if(!slotLive(owner)){bodyBindings[i*3]=0;return;}
 float dt=IniParams[239].x*2;
 float3 shift=frameState[owner*4].w>.5?frameState[owner*4].xyz-slotRoot(owner):0;
 if(length(shift)>1.5){bodyBindings[i*3]=0;return;}
 float3 from=p.positionAge.xyz+shift,normal=p.predictedLambda.xyz;
 uint mesh;float radius=trailSegment(p.densityBirth.w)?TRAIL_LIFT:FLUID_RADIUS*p.densityBirth.z;
 float4 binding=bodyBindings[i*3],saved=bodyBindings[i*3+1];
 // Validate against the current capture before renewing the consumer's timestamp.
 if(binding.x>0&&binding.y==slotAnchor(owner,4).y&&binding.z==p.densityBirth.y&&bodyBindings[i*3+2].x==slotGeneration(owner)&&now>=saved.z){
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
 bool swept=p.predictedLambda.w<.5;
 float3 endPoint=swept?from+(p.velocityAlive.xyz+float3(0,-9.81,0)*dt)*dt:from;
 bodyBegin(i,owner,p.densityBirth.y,radius,endPoint,swept,from,normal,mesh);
}
