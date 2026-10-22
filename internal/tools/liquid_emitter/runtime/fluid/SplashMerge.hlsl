// Picks the drop each clinging drop would merge with, and the puddle each new
// floor puddle pours into (Merge.hlsl), between SplashUpdate and SplashSpawn.
// It lists the new puddles for SplashSpawn: those that take a reserved slot
// apart from those that pour, so only the first count toward the slots.
#include "ParticleConfig.hlsl"
Texture1D<float4> IniParams:register(t120);
#include "Puddle.hlsl"
#include "Slots.hlsl"
#include "Contact.hlsl"
struct FluidParticle{float4 positionAge;float4 velocityAlive;float4 predictedLambda;float4 densityBirth;};
StructuredBuffer<FluidParticle> source:register(t100);
StructuredBuffer<float4> contactHistory:register(t102);
RWBuffer<uint> mergeInto:register(u1);
// ParticleConfig.hlsl ResourcePuddleTransferCounter and ResourcePuddleTransferList.
RWBuffer<uint> puddleTransferCounter:register(u6);
RWBuffer<uint> puddleTransferList:register(u7);
#include "Merge.hlsl"
[numthreads(64,1,1)]
void main(uint3 id:SV_DispatchThreadID){
 if(id.x>=AIRBORNE_SPLASH_COUNT)return;
 FluidParticle p=source[id.x];
 if(p.velocityAlive.w<.5||!floorPuddle(p.densityBirth.w)){mergeInto[id.x]=mergeCandidate(id.x);return;}
 uint host=puddleHost(id.x),rank;
 mergeInto[id.x]=host;
 if(host==NO_MERGE){
  InterlockedAdd(puddleTransferCounter[0],1,rank);
  puddleTransferList[rank]=id.x;
 }else{
  InterlockedAdd(puddleTransferCounter[TRANSFER_PUDDLE_POURED],1,rank);
  puddleTransferList[TRANSFER_POURED_LIST+rank]=id.x;
 }
}
