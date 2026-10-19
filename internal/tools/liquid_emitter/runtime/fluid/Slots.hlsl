// Per-character emitter slots that AssignSlots.hlsl fills each frame.
// Slot rows 0-8 are the anchor in ResolveSurface's layout: right, up, forward,
// emitter point, (priority, profile id, area, seen this frame), character
// root, captured triangle a, b, c. The anchor survives frames the character
// is not drawn in, with the seen flag cleared.
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
// Splash particles come from every slot, so their live flag carries the
// owner: velocityAlive.w is 1+slot while alive and 0 once dead.
float splashAliveTag(uint s){return 1+s;}
uint splashSlot(float aliveTag){return (uint)clamp(round(aliveTag)-1,0,SLOT_COUNT-1);}
#endif
