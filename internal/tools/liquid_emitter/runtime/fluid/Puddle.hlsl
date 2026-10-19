// Shared CPU-independent limits for slow floor deposition and wet patches.
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
#endif

