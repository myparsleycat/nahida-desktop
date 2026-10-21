// The contact a clinging drop or trail segment last had with a surface,
// CONTACT_ROWS rows per splash particle in ResourceContactHistory, which
// SplashUpdate keeps and SplashSpawn starts for every particle it places:
// - xyz the last verified surface point relative to the slot root, w the time
//   it was verified, 0 for none;
// - xyz its normal, w how far the drop slid since;
// - xyz where the drop's trail last ended, w 1 while the contact is held
//   without the surface being seen.
#ifndef LIQUID_CONTACT
#define LIQUID_CONTACT
static const uint CONTACT_ROWS=3;
// A surface the depth no longer shows, hidden for a moment or at the edge of
// the screen, is kept this long, and a drop slides at most CONTACT_HOLD_SLIDE
// along its last plane meanwhile. After that a drop falls and a trail fades.
static const float CONTACT_HOLD=.1,CONTACT_HOLD_SLIDE=.005;
// A surface seen again after a hold takes the drop back only within this
// distance of the plane it was held on, and facing about the same way.
static const float CONTACT_PLANE_MATCH=.004,CONTACT_NORMAL_MATCH=.7;
struct ContactRecord{float4 plane;float4 normal;float4 trail;};
// predictedLambda.w of a clinging drop: 1 world-fixed, 2 body-following
// (AdhesionFrame.hlsl adhesionContactMode).
uint adhesionMode(float encoded){return encoded>1.5?2u:1u;}
bool contactHeld(ContactRecord h){return h.trail.w>.5;}
#endif
