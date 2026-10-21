// Emitter slots, one per emitter of a drawn character, that AssignSlots.hlsl
// fills each frame.
// Slot rows 0-8 are the anchor in ResolveSurface's layout: right, up, forward,
// emitter point, (priority, profile id, area, seen this frame), character
// root, captured triangle a, b, c. The anchor survives frames the character
// is not drawn in, with the seen flag cleared.
// Includers declare IniParams first.
#ifndef LIQUID_SLOTS
#define LIQUID_SLOTS
static const uint SLOT_ROWS=16;
// xyz the character root that slot particles are stored relative to; w is 1
// while the slot is assigned. A freed slot drops its particles that frame.
static const uint SLOT_ROOT=9;
// x the time the slot's character was last drawn.
static const uint SLOT_TRACK=10;
// The root one frame before SLOT_ROOT, w 1 when known; AssignSlots
// extrapolates the character's motion from it.
static const uint SLOT_PREVIOUS_ROOT=11;
// The look of the emitter, copied from its profile: x the force scale, y 1
// for a custom color, z its absorption depth in meters, w the amount scale.
// All zero keeps the global settings.
static const uint SLOT_LOOK=12;
// rgb the custom tint, w the viscosity scale.
static const uint SLOT_TINT=13;
// x the nozzle kind, one of NOZZLE_*; zero, as in a profile from before the
// setting, is the spray the runtime always had. y the scattering and z the
// roughness of the liquid, each 0 to 1; zero keeps the runtime's clear,
// mirror-like liquid.
static const uint SLOT_NOZZLE=14;
static const uint NOZZLE_SPRAY=0,NOZZLE_JET=1,NOZZLE_OOZE=2;
// x counts the characters the slot was handed to, so state kept for a slot
// across frames (EmitterState.hlsl, StreamMeta) never carries over to the
// next one. A cleared runtime starts every slot at zero again, along with
// that state.
static const uint SLOT_GENERATION=15;
#ifdef SLOT_STATE_WRITER
RWStructuredBuffer<float4> slots:register(u0);
#else
StructuredBuffer<float4> slots:register(t103);
#endif
float4 slotAnchor(uint s,uint k){return slots[s*SLOT_ROWS+k];}
bool slotSeen(uint s){return slots[s*SLOT_ROWS+4].w>.5;}
bool slotLive(uint s){return slots[s*SLOT_ROWS+SLOT_ROOT].w>.5;}
float3 slotRoot(uint s){return slots[s*SLOT_ROWS+SLOT_ROOT].xyz;}
uint primarySlot(uint i){return i/PRIMARY_PER_SLOT;}
// The emitter's force scale. Zero, as in a profile from before the setting,
// means 1.
float slotForce(uint s){
 float force=slots[s*SLOT_ROWS+SLOT_LOOK].x;
 return force>0?clamp(force,.15,2.35):1;
}
// The nozzle setting the slot emits with: the emitter's force over the former
// default level 3 (1.69), within the former levels' .28 to 3.94.
float slotDrive(uint s){return clamp(1.69*slotForce(s),.28,3.94);}
uint slotNozzle(uint s){return (uint)clamp(round(slots[s*SLOT_ROWS+SLOT_NOZZLE].x),0,2);}
float slotScattering(uint s){return saturate(slots[s*SLOT_ROWS+SLOT_NOZZLE].y);}
float slotRoughness(uint s){return saturate(slots[s*SLOT_ROWS+SLOT_NOZZLE].z);}
uint slotGeneration(uint s){return (uint)slots[s*SLOT_ROWS+SLOT_GENERATION].x;}
// The emitter's flow over the runtime's. Zero, as in a profile from before
// the setting, means 1.
float slotAmount(uint s){
 float amount=slots[s*SLOT_ROWS+SLOT_LOOK].w;
 return amount>0?clamp(amount,.02,2):1;
}
// A jet's cross-section follows its flow, so the outlet's sides scale with the
// square root of the amount. The particles cannot draw a stream thinner than
// at amount .1; less flow than that leaves the outlet there and breaks into
// drops instead, through fewer births (FluidSolver.hlsl).
float slotOutlet(uint s){return sqrt(max(slotAmount(s),.1));}
// The drawn radius of the slot's stream particles: thinner with less flow,
// while more flow adds particles rather than widening them.
float slotThread(uint s){return min(slotOutlet(s),1);}
// The character's viscosity over the runtime's water-like liquid; zero, as in
// a profile from before the setting, means 1. Every effect derived from it
// keeps its former constant at 1.
float slotViscosity(uint s){
 float viscosity=slots[s*SLOT_ROWS+SLOT_TINT].w;
 return viscosity>0?clamp(viscosity,.1,10):1;
}
// Thicker liquid splashes less on impact, thinner a little more. rsqrt is an
// approximation, so the runtime's own viscosity skips it to stay exact.
float slotSplash(uint s){
 float viscosity=slotViscosity(s);
 return viscosity==1?1:clamp(rsqrt(viscosity),.3,1.3);
}
// rgb the Beer-Lambert absorption per meter that reaches the tint at the
// absorption depth, w that depth in meters: the character's custom color or
// the global look (251).
float4 slotLiquid(uint s){
 float4 look=slots[s*SLOT_ROWS+SLOT_LOOK];
 float3 tint=IniParams[251].rgb;float depth=IniParams[251].w;
 if(look.y>.5){tint=slots[s*SLOT_ROWS+SLOT_TINT].rgb;depth=look.z;}
 depth=max(depth,.001);
 return float4(-log(max(saturate(tint),.02))/depth,depth);
}
// Splash particles come from every slot, so their live flag carries the
// owner: velocityAlive.w is 1+slot while alive and 0 once dead.
float splashAliveTag(uint s){return 1+s;}
uint splashSlot(float aliveTag){return (uint)clamp(round(aliveTag)-1,0,SLOT_COUNT-1);}
#endif
