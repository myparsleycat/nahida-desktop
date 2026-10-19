// Pass 1 of the screen-space fluid: every live particle is an ellipsoid whose
// nearest view depth lands in o0 through min blending and whose ray chord
// adds up in o1 as thickness. A floor puddle particle is a soft disc on its
// ground plane instead: the discs of neighbors add up into one flat film.
// o2 adds up each particle's absorption weighted by its thickness, with the
// weight in alpha, and o3 its absorption depth (mm) under the same weight, so
// the composite colors overlapping liquids of different characters by how
// much of each the ray crosses.
#include "SSFRCommon.hlsl"
#include "JetGuide.hlsl"
#include "SheetProfile.hlsl"
StructuredBuffer<FluidParticle> particles:register(t100);
StructuredBuffer<FluidParticle> splashes:register(t101);
#include "Slots.hlsl"
StructuredBuffer<JetGuide> renderGuides:register(t112);
// Disc radius over the puddle particle's own, so neighboring discs overlap.
static const float PUDDLE_SPLAT_SCALE=2;
// Film one disc adds at its center; the composite thresholds the sum.
static const float PUDDLE_SPLAT_FILM=.0012;
// Stream particles farther apart than this have broken up and are not bridged.
static const float BRIDGE_MAX=SURFACE_RADIUS*8;
static const float MODE_NONE=0,MODE_ELLIPSOID=1,MODE_DISC=2;
struct Varying{
 float4 position:SV_Position;
 float3 world:TEXCOORD0;
 nointerpolation float3 center:TEXCOORD1;
 // Ellipsoid: xyz one anisotropy axis, w the scale along it (<1 flattens,
 // >1 stretches). Disc: xyz the ground normal.
 nointerpolation float4 axisScale:TEXCOORD2;
 nointerpolation float2 radiusFade:TEXCOORD3;
 nointerpolation float mode:TEXCOORD4;
 // The owner's liquid (Slots.hlsl slotLiquid): absorption per meter and
 // absorption depth in meters.
 nointerpolation float4 liquid:TEXCOORD5;
};
// Keeps the color weight of a covered pixel above what half floats flush to
// zero, however thin the film there.
static const float COLOR_WEIGHT_FLOOR=1e-5;
float birthRadiusScale(float value){return value>=48?.70:(value>=32?.85:1);}
#ifdef VERTEX_SHADER
// The slots of a bank are born in order, so slots i-1 and i+1 hold the
// particles just behind and ahead of slot i in the stream, as long as both
// are from the same emission cycle and still part of the coherent flow.
bool streamNeighbor(uint index,float3 root,float age,float3 center,out float3 position){
 FluidParticle q=particles[index];
 position=root+q.positionAge.xyz;
 return q.velocityAlive.w>.5&&all(isfinite(q.positionAge))
  &&abs(q.positionAge.w-age)<3*PRIMARY_LIFETIME/BASE_PRIMARY_COUNT
  &&renderGuides[index].longLane.w>=-2&&distance(position,center)<BRIDGE_MAX;
}
Varying main(uint id:SV_VertexID){
 uint i=id/6;bool primary=i<PRIMARY_COUNT;uint poolIndex=primary?i:i-PRIMARY_COUNT;
 FluidParticle p;if(primary)p=particles[poolIndex];else p=splashes[poolIndex];
 uint owner=primary?primarySlot(poolIndex):splashSlot(p.velocityAlive.w);
 float visualScale=lerp(.75,1,smoothstep(.28,1.69,slotDrive(owner)));
 float radius=SURFACE_RADIUS*(primary&&p.densityBirth.w>=16?birthRadiusScale(p.densityBirth.w):visualScale*(primary?1:max(p.densityBirth.z,.1)));
 bool puddle=!primary&&floorPuddle(p.densityBirth.w);
 if(puddle)radius=SURFACE_RADIUS*max(p.densityBirth.z,.1)*puddleWidth(p.densityBirth.x)*PUDDLE_SPLAT_SCALE;
 float3 root=slotRoot(owner),center=root+p.positionAge.xyz;
 float4 projected=project(center);
 bool valid=p.velocityAlive.w>.5&&all(isfinite(p.positionAge))&&all(isfinite(p.velocityAlive))&&length(p.positionAge.xyz)<2.5&&p.positionAge.y>=-1&&slotLive(owner)&&all(isfinite(projected))&&projected.w>.05;

 float4 axisScale=float4(1,0,0,1);
 float fade=1;
 if(puddle){
  axisScale=float4(p.predictedLambda.xyz,1);
  valid=valid&&dot(axisScale.xyz,axisScale.xyz)>.5;
  // The solver keeps a puddle particle's center its half height above the
  // ground (Splash.hlsl). The disc goes on the contact plane itself, or at a
  // low camera angle the depth check would take the gap for an obstacle.
  center-=axisScale.xyz*puddleHalfHeight(FLUID_RADIUS*max(p.densityBirth.z,.1),p.densityBirth.x);
  fade=1-smoothstep(PUDDLE_LIFETIME-.4,PUDDLE_LIFETIME,p.positionAge.w);
 }else{
  // Fast drops stretch along their motion, so spray reads as streaks and
  // consecutive stream particles overlap instead of beading.
  float speed=length(p.velocityAlive.xyz);
  if(speed>.05)axisScale=float4(p.velocityAlive.xyz/speed,1+.8*saturate(speed/1.5));
  if(primary){
   JetGuide guide=renderGuides[poolIndex];
   float amount=saturate(guide.carrierAmount.w);
   float3 lateral=cross(guide.axisSpeed.xyz,guide.longLane.xyz);
   float lateralLength=length(lateral);
   // Sheet particles keep their birth amount past the focus, where the
   // compression has ended, so only an active compression takes precedence.
   float compression=0;
   if(amount>.001&&lateralLength>.001){
    float focus=sheetFocusDistance(amount),distance=max(p.densityBirth.z,0);
    compression=smoothstep(0,focus*.2,distance)*(1-smoothstep(focus,focus+.02,distance));
   }
   if(compression>.001){
    axisScale=float4(lateral/lateralLength,1-.6*amount*compression);
   }else if(guide.longLane.w>=-2){
    // Nozzle flutter bunches the stream into beads with sparse stretches
    // between them. Reaching halfway to each slot neighbor keeps those
    // stretches covered until the stream has really broken up.
    uint slot=poolIndex%BASE_PRIMARY_COUNT;
    float3 behind=0,ahead=0;
    bool hasBehind=false,hasAhead=false;
    if(slot>0)hasBehind=streamNeighbor(poolIndex-1,root,p.positionAge.w,center,behind);
    if(slot<BASE_PRIMARY_COUNT-1)hasAhead=streamNeighbor(poolIndex+1,root,p.positionAge.w,center,ahead);
    float3 axis=0;float halfLength=0;
    if(hasBehind&&hasAhead){axis=ahead-behind;halfLength=max(distance(behind,center),distance(ahead,center))*.5;}
    else if(hasBehind){axis=center-behind;halfLength=length(axis)*.5;}
    else if(hasAhead){axis=ahead-center;halfLength=length(axis)*.5;}
    float axisLength=length(axis);
    if(axisLength>1e-5)axisScale=float4(axis/axisLength,clamp((halfLength+radius*.35)/radius,1,5));
   }
  }
 }
 valid=valid&&fade>.01;
 float bound=radius*max(axisScale.w,1)*1.1;
 valid=valid&&abs(camera[8].x*bound/max(projected.w,.05))<.06;

 const float2 corners[6]={float2(-1,-1),float2(1,-1),float2(-1,1),float2(-1,1),float2(1,-1),float2(1,1)};
 float2 uv=corners[id%6];
 float3 right=normalize(camera[4].xyz),up=normalize(camera[5].xyz);
 float3 world=center+bound*(right*uv.x+up*uv.y);
 Varying o;
 o.position=valid?project(world):float4(2,2,0,1);
 o.world=world;o.center=center;o.axisScale=axisScale;o.radiusFade=float2(radius,fade);
 o.mode=valid?(puddle?MODE_DISC:MODE_ELLIPSOID):MODE_NONE;
 o.liquid=slotLiquid(owner);
 return o;
}
#endif
#ifdef PIXEL_SHADER
struct Surface{float depth:SV_Target0;float thickness:SV_Target1;float4 color:SV_Target2;float absorptionDepth:SV_Target3;};
// Millimeters keep a thin film's weighted depth clear of half-float flushing.
void weighLiquid(inout Surface o,float4 liquid){
 float weight=o.thickness+COLOR_WEIGHT_FLOOR;
 o.color=float4(liquid.rgb*weight,weight);
 o.absorptionDepth=liquid.w*1000*weight;
}
Surface main(Varying i){
 clip(i.mode-.5);
 float3 eye=eyePosition();
 float3 direction=normalize(i.world-eye);
 float3 origin=eye-i.center;
 float radius=i.radiusFade.x;
 Surface o;
 if(i.mode>1.5){
  // The pixel's ray meets the puddle's ground plane, and the scene depth has
  // to agree with that point within a few millimeters: the film then neither
  // climbs a shoe standing in it nor hangs past the edge of a step.
  float3 normal=i.axisScale.xyz;
  float denominator=dot(direction,normal);
  clip(abs(denominator)-1e-4);
  float t=-dot(origin,normal)/denominator;
  clip(t);
  float3 hit=eye+direction*t;
  float r=distance(hit,i.center);
  clip(radius-r);
  float4 projected=project(hit);
  clip(projected.w-.05);
  if(IniParams[237].y>.5){
   float2 uv=screenUV(projected);
   float gap=behindScene(uv,projected);
   if(gap<-1e29||abs(gap)>.004+sceneSlopeAllowanceAt(uv))clip(-1);
  }
  float coverage=1-r*r/(radius*radius);
  o.depth=projected.w;o.thickness=PUDDLE_SPLAT_FILM*coverage*coverage*i.radiusFade.y;
  weighLiquid(o,i.liquid);
  return o;
 }
 // Scale the ray along the anisotropy axis and intersect the plain sphere.
 float stretch=1/i.axisScale.w-1;
 float3 scaledOrigin=origin+i.axisScale.xyz*(dot(origin,i.axisScale.xyz)*stretch);
 float3 scaledDirection=direction+i.axisScale.xyz*(dot(direction,i.axisScale.xyz)*stretch);
 float a=dot(scaledDirection,scaledDirection),b=dot(scaledOrigin,scaledDirection),c=dot(scaledOrigin,scaledOrigin)-radius*radius;
 float discriminant=b*b-a*c;
 clip(discriminant);
 float root=sqrt(discriminant);
 float entry=(-b-root)/a,exit=(-b+root)/a;
 clip(exit);
 entry=max(entry,0);

 float3 hit=eye+direction*entry;
 float4 projected=project(hit);
 clip(projected.w-.05);
 if(sceneInFront(screenUV(projected),projected))clip(-1);
 o.depth=projected.w;o.thickness=(exit-entry)*i.radiusFade.y;
 weighLiquid(o,i.liquid);
 return o;
}
#endif
