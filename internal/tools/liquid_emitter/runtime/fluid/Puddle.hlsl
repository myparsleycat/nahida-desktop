// Shared CPU-independent limits for liquid resting on a surface: slow floor
// deposition and wet patches, drops clinging to the body and their trails.
#ifndef LIQUID_FLOOR_PUDDLE
#define LIQUID_FLOOR_PUDDLE
static const float PUDDLE_ENTRY_SPEED=.25;
static const float PUDDLE_LIFETIME=2.4;
// Upward-facing ground up to 45 degrees from horizontal.
static const float PUDDLE_MIN_UP=.70710678;
bool floorPuddle(float marker){return marker>4.5 && marker<5.5;}
float puddleWidth(float spread){return 1+2*saturate(spread);}
float puddleHalfHeight(float radius,float spread){
 float width=puddleWidth(spread);
 return radius/(width*width);
}
bool slowFloorContact(float3 velocity,float3 normal){
 return normal.y>=PUDDLE_MIN_UP && dot(velocity,velocity)<=PUDDLE_ENTRY_SPEED*PUDDLE_ENTRY_SPEED;
}
// A clinging drop shrinks away over the last ADHESION_FADE of its life.
static const float ADHESION_LIFETIME=1.6,ADHESION_FADE=.3;
// A trail segment a running drop leaves on the body (Splash.hlsl), faded out
// over its last TRAIL_FADE. It lies TRAIL_LIFT off the surface, as a puddle
// does, so the scene depth does not take it for something behind the body.
static const float TRAIL_MARKER=6;
static const float TRAIL_LIFETIME=1.5,TRAIL_FADE=.4,TRAIL_LIFT=.0002;
bool trailSegment(float marker){return marker>5.5 && marker<6.5;}
#endif

