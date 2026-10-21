// Takes the volume of the drops SplashSpawn released off each oozing outlet
// (EmitterState.hlsl commitEmitter), after SplashSpawn.
#include "ParticleConfig.hlsl"
Texture1D<float4> IniParams:register(t120);
#include "Slots.hlsl"
#include "EmitterState.hlsl"
#include "Diagnostics.hlsl"
// Per slot and request, 1 when SplashSpawn gave the request a particle.
Buffer<uint> oozeDelivered:register(t101);
RWStructuredBuffer<float4> emitters:register(u0);
RWBuffer<uint> diagnostics:register(u1);
[numthreads(SLOT_COUNT,1,1)]
void main(uint3 id:SV_DispatchThreadID){
 uint s=id.x,base=s*EMITTER_ROWS;
 EmitterRows st;
 [unroll]for(uint k=0;k<EMITTER_ROWS;k++)st.r[k]=emitters[base+k];
 uint delivered;bool waiting;
 commitEmitter(st,oozeDelivered[s*OOZE_REQUESTS]!=0,oozeDelivered[s*OOZE_REQUESTS+1]!=0,delivered,waiting);
 [unroll]for(uint row=0;row<EMITTER_ROWS;row++)emitters[base+row]=st.r[row];
 if(delivered>0)InterlockedAdd(diagnostics[DIAG_OUTLET_DELIVERED],delivered);
 if(waiting)InterlockedAdd(diagnostics[DIAG_OUTLET_WAITING],1);
}
