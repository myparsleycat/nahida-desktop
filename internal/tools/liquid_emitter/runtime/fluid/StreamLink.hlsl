// Joins each slot's stream particles into chains (StreamLinks.hlsl) once the
// solver is done with a frame. One group sorts a slot's particles by birth
// time, then by emission sequence, so both banks interleave in the order
// they left the nozzle. A particle joins the one born just before it when
// both belong to the slot's current character and flow and lie where one
// flow would put them; a join is made at birth or never, and once broken it
// stays broken, so drops that drift back together are not bridged again.
#include "ParticleConfig.hlsl"
Texture1D<float4> IniParams:register(t120);
#include "Slots.hlsl"
#include "JetGuide.hlsl"
#include "StreamLinks.hlsl"
struct FluidParticle{float4 positionAge;float4 velocityAlive;float4 predictedLambda;float4 densityBirth;};
StructuredBuffer<FluidParticle> particles:register(t100);
// FluidSolver.hlsl streamMeta.
StructuredBuffer<uint4> streamMeta:register(t101);
StructuredBuffer<JetGuide> guides:register(t112);
RWStructuredBuffer<float4> links:register(u0);
static const uint NO_KEY=0xffffffffu;
groupshared uint sortTime[PRIMARY_PER_SLOT],sortSequence[PRIMARY_PER_SLOT],sortIndex[PRIMARY_PER_SLOT];
groupshared uint joinedBehind[PRIMARY_PER_SLOT];
// Orders floats of either sign as uints.
uint orderedFloat(float value){uint bits=asuint(value);return (bits&0x80000000u)!=0?~bits:bits|0x80000000u;}
// Whether newer continues the flow of older, born just before it.
bool continues(uint older,uint newer){
 uint4 a=streamMeta[older],b=streamMeta[newer];
 if(a.x!=b.x||a.y!=b.y)return false;
 FluidParticle p=particles[older],q=particles[newer];
 float gap=q.densityBirth.y-p.densityBirth.y;
 if(gap<0||gap>STREAM_GAP)return false;
 float speed=max(length(p.velocityAlive.xyz),length(q.velocityAlive.xyz));
 float width=SURFACE_RADIUS*max(asfloat(a.w),asfloat(b.w));
 float reach=min(STREAM_BRIDGE_MAX,(speed*gap+2.5*width)*1.5);
 if(distance(p.positionAge.xyz,q.positionAge.xyz)>reach)return false;
 float3 u=p.velocityAlive.xyz,v=q.velocityAlive.xyz;
 return dot(u,u)<.04||dot(v,v)<.04||dot(normalize(u),normalize(v))>.7;
}
[numthreads(PRIMARY_PER_SLOT,1,1)]
void main(uint3 group:SV_GroupID,uint lane:SV_GroupIndex){
 uint slot=group.x,segment=slot*PRIMARY_PER_SLOT,i=segment+lane;
 FluidParticle p=particles[i];
 bool eligible=slotLive(slot)&&p.velocityAlive.w>.5&&all(isfinite(p.positionAge))&&all(isfinite(p.velocityAlive))
  &&streamMeta[i].x==slotGeneration(slot)&&streamGuide(guides[i]);
 sortTime[lane]=eligible?orderedFloat(p.densityBirth.y):NO_KEY;
 sortSequence[lane]=eligible?streamMeta[i].z:NO_KEY;
 sortIndex[lane]=lane;
 GroupMemoryBarrierWithGroupSync();
 [loop]for(uint size=2;size<=PRIMARY_PER_SLOT;size<<=1){
  [loop]for(uint stride=size>>1;stride>0;stride>>=1){
   uint other=lane^stride;
   if(other>lane){
    bool later=sortTime[lane]>sortTime[other]||(sortTime[lane]==sortTime[other]&&sortSequence[lane]>sortSequence[other]);
    if(later==((lane&size)==0)){
     uint time=sortTime[lane],sequence=sortSequence[lane],index=sortIndex[lane];
     sortTime[lane]=sortTime[other];sortSequence[lane]=sortSequence[other];sortIndex[lane]=sortIndex[other];
     sortTime[other]=time;sortSequence[other]=sequence;sortIndex[other]=index;
    }
   }
   GroupMemoryBarrierWithGroupSync();
  }
 }

 // From here each thread owns the particle at its place in birth order.
 bool valid=sortTime[lane]!=NO_KEY;
 uint self=segment+sortIndex[lane];
 uint behind=lane>0&&sortTime[lane-1]!=NO_KEY?segment+sortIndex[lane-1]:LINK_NONE;
 bool joins=valid&&behind!=LINK_NONE&&continues(behind,self);
 float4 record=links[self*LINK_ROWS+1];
 uint sequence=streamMeta[self].z,joined=asuint(record.z);
 uint behindSequence=behind!=LINK_NONE?streamMeta[behind].z:LINK_NONE;
 if(asuint(record.w)!=sequence)joined=joins?behindSequence:LINK_BROKEN;
 else if(!joins||joined!=behindSequence)joined=LINK_BROKEN;
 bool linked=valid&&joined!=LINK_BROKEN;
 joinedBehind[lane]=linked?1:0;
 GroupMemoryBarrierWithGroupSync();
 bool linkedAhead=valid&&lane+1<PRIMARY_PER_SLOT&&joinedBehind[lane+1]!=0;
 uint ahead=linkedAhead?segment+sortIndex[lane+1]:LINK_NONE;
 // A particle that cannot join, such as a spray's sheet, keeps a record that
 // joins nothing, which FluidSurface.hlsl draws as its own drop.
 if(!valid){
  links[self*LINK_ROWS]=0;
  links[self*LINK_ROWS+1]=float4(asfloat(LINK_NONE),asfloat(LINK_NONE),asfloat(LINK_BROKEN),asfloat(sequence));
  return;
 }

 // A falling stream thins as it speeds up, keeping its flow.
 FluidParticle q=particles[self];
 float launch=max(storedLaunchSpeed(q.densityBirth.w),.2),speed=max(length(q.velocityAlive.xyz),.2);
 float radius=SURFACE_RADIUS*asfloat(streamMeta[self].w)*clamp(sqrt(launch/speed),.55,1.1);
 float3 position=q.positionAge.xyz,tangent=0;
 if(linked&&linkedAhead)tangent=(particles[ahead].positionAge.xyz-particles[behind].positionAge.xyz)*.5;
 else if(linkedAhead)tangent=particles[ahead].positionAge.xyz-position;
 else if(linked)tangent=position-particles[behind].positionAge.xyz;
 links[self*LINK_ROWS]=float4(tangent,radius);
 links[self*LINK_ROWS+1]=float4(asfloat(linked?behind:LINK_NONE),asfloat(ahead),asfloat(joined),asfloat(sequence));
}
