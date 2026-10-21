// Moves each slot's emitter state (EmitterState.hlsl) one frame on, after
// AssignSlots and Pulse and before any particle pass reads it.
#include "ParticleConfig.hlsl"
Texture1D<float4> IniParams:register(t120);
#include "Slots.hlsl"
// FluidSolver's frame state, four rows per slot: row 0 the root at the end of
// the last frame.
StructuredBuffer<float4> frameState:register(t105);
StructuredBuffer<float4> pulse:register(t109);
#include "Collision.hlsl"
#include "AdhesionFrame.hlsl"
#include "EmitterState.hlsl"
#include "Diagnostics.hlsl"
RWStructuredBuffer<float4> emitters:register(u0);
RWBuffer<uint> diagnostics:register(u1);
[numthreads(SLOT_COUNT,1,1)]
void main(uint3 id:SV_DispatchThreadID){
 uint s=id.x,base=s*EMITTER_ROWS;
 OutletInput o;
 o.dt=IniParams[239].x*2;o.now=IniParams[239].y+IniParams[239].x;
 o.live=slotLive(s);
 o.flowing=o.live&&IniParams[237].x>.5&&pulse[3].w>.5&&slotSeen(s);
 o.kind=slotNozzle(s);o.generation=slotGeneration(s);
 o.shift=frameState[s*4].w>.5?frameState[s*4].xyz-slotRoot(s):0;
 o.amount=slotAmount(s);o.force=slotForce(s);o.viscosity=slotViscosity(s);o.pulse=pulse[2].x;
 o.outlet=slotAnchor(s,3).xyz-collisionRoot(s);o.up=slotAnchor(s,1).xyz;
 float3 surfacePoint,surfaceNormal;
 o.surface=o.live&&slotSeen(s)&&screenSurface(s,o.outlet,surfacePoint,surfaceNormal)
  &&abs(dot(o.outlet-surfacePoint,surfaceNormal))<.02&&length(o.outlet-surfacePoint)<.03;
 o.surfacePoint=o.surface?surfacePoint:0;o.surfaceNormal=o.surface?surfaceNormal:float3(0,1,0);
 o.contactMode=o.surface?adhesionContactMode(s,surfacePoint,surfaceNormal):0;

 EmitterRows st;
 [unroll]for(uint k=0;k<EMITTER_ROWS;k++)st.r[k]=emitters[base+k];
 float saturated;bool expired;
 advanceEmitter(st,o,saturated,expired);
 [unroll]for(uint row=0;row<EMITTER_ROWS;row++)emitters[base+row]=st.r[row];
 if(saturated>0)InterlockedAdd(diagnostics[DIAG_OUTLET_SATURATED],1);
 if(expired)InterlockedAdd(diagnostics[DIAG_CONTACT_EXPIRED],1);
}
