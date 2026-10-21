// Picks the drop each clinging drop would merge with (Merge.hlsl), between
// SplashUpdate and SplashSpawn.
#include "ParticleConfig.hlsl"
#include "Puddle.hlsl"
Texture1D<float4> IniParams:register(t120);
#include "Slots.hlsl"
#include "Contact.hlsl"
struct FluidParticle{float4 positionAge;float4 velocityAlive;float4 predictedLambda;float4 densityBirth;};
StructuredBuffer<FluidParticle> source:register(t100);
StructuredBuffer<float4> contactHistory:register(t102);
RWBuffer<uint> mergeInto:register(u1);
#include "Merge.hlsl"
[numthreads(64,1,1)]
void main(uint3 id:SV_DispatchThreadID){
 if(id.x<AIRBORNE_SPLASH_COUNT)mergeInto[id.x]=mergeCandidate(id.x);
}
