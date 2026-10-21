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
// The look of the emitter, copied from its profile: x the strength
// level offset, y 1 for a custom color, z its absorption depth in meters,
// w the stream width scale. All zero keeps the global settings.
static const uint SLOT_LOOK=12;
// rgb the custom tint, w the viscosity scale.
static const uint SLOT_TINT=13;
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
// The nozzle setting the slot emits with: the global strength level (253.y)
// shifted by the character's offset, from .28 at level 0 up to 3.94 at 7.
float slotDrive(uint s){
 float level=clamp(round(IniParams[253].y+slots[s*SLOT_ROWS+SLOT_LOOK].x),0,7);
 return level<.5?.28:.565+(level-1)*.5625;
}
// The character's stream width over the runtime's, scaling the nozzle
// cross-section and the rendered stream together; zero, as in a profile from
// before the setting, means 1.
float slotWidth(uint s){
 float width=slots[s*SLOT_ROWS+SLOT_LOOK].w;
 return width>0?clamp(width,.05,2):1;
}
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
