// Pass 1 of the screen-space fluid: every live particle is an ellipsoid whose
// nearest view depth lands in o0 through min blending and whose ray chord
// adds up in o1 as thickness. A drop clinging to the body is a lens drawn
// out along its flow and pressed flat against the surface, so a trail of
// them smooths into one rivulet, and the segments a running drop leaves
// behind it are a thin film on the body that joins them. A floor puddle
// particle is a soft disc on its ground plane instead: the discs of
// neighbors add up into one flat film.
// o2 adds up each particle's absorption weighted by its thickness, with the
// weight in alpha, o3 its absorption depth (mm) and o4 its scattering and
// roughness under the same weight, so the composite colors overlapping
// liquids of different characters by how much of each the ray crosses.
// Stream particles StreamLink.hlsl joined are left to FluidStream.hlsl.
#include "SSFRCommon.hlsl"
#include "JetGuide.hlsl"
#include "SheetProfile.hlsl"
#include "StreamLinks.hlsl"
#include "Contact.hlsl"
StructuredBuffer<FluidParticle> particles:register(t100);
StructuredBuffer<FluidParticle> splashes:register(t101);
StructuredBuffer<float4> contactHistory:register(t102);
#include "Slots.hlsl"
StructuredBuffer<float4> links:register(t104);
StructuredBuffer<JetGuide> renderGuides:register(t112);
// Disc radius over the puddle particle's own, so neighboring discs overlap.
static const float PUDDLE_SPLAT_SCALE=2;
// Film one disc adds at its center; the composite thresholds the sum.
static const float PUDDLE_SPLAT_FILM=.0012;
static const float MODE_NONE=0,MODE_ELLIPSOID=1,MODE_DISC=2,MODE_CAPSULE=3;
// A trail segment's half width over the lens of the drop that left it, and
// the film it adds on its center line: close to the drop's own, so the drop
// and its trail read as one rivulet rather than a bead on a thin line.
static const float TRAIL_WIDTH=.85,TRAIL_FILM=.0016;
// A clinging drop's stretch along its flow, from standing to sliding, so its
// tail overlaps its trail, and its half thickness across the surface as a
// fraction of its radius, at most FILM_HALF_MAX: a large merged drop spreads
// instead of rising into a dome above its trail.
static const float FILM_STRETCH_MIN=2.5,FILM_STRETCH_MAX=4,FILM_FLATTEN=.25,FILM_HALF_MAX=.0011;
struct Varying{
 float4 position:SV_Position;
 float2 ndc:TEXCOORD0;
 nointerpolation float3 center:TEXCOORD1;
 // Ellipsoid: xyz one anisotropy axis, w the scale along it (<1 flattens,
 // >1 stretches). Disc and capsule: xyz the surface normal.
 nointerpolation float4 axisScale:TEXCOORD2;
 nointerpolation float2 radiusFade:TEXCOORD3;
 nointerpolation float mode:TEXCOORD4;
 // The owner's liquid (Slots.hlsl slotLiquid): absorption per meter and
 // absorption depth in meters.
 nointerpolation float4 liquid:TEXCOORD5;
 // The depth tag (SSFRCommon.hlsl encodeFluidDepth).
 nointerpolation uint tag:TEXCOORD6;
 // Ellipsoid: a second anisotropy axis orthogonal to axisScale.xyz, or zero
 // for none, with its scale in w. The third axis of the frame keeps scale 1.
 // Capsule: xyz the segment direction, w its half length; center is its
 // midpoint.
 nointerpolation float4 normalScale:TEXCOORD7;
 // The owner's scattering and roughness (Slots.hlsl).
 nointerpolation float2 material:TEXCOORD8;
};
// Keeps the color weight of a covered pixel above what half floats flush to
// zero, however thin the film there.
static const float COLOR_WEIGHT_FLOOR=1e-5;
float birthRadiusScale(float value){return value>=48?.70:(value>=32?.85:1);}
#ifdef VERTEX_SHADER
// The debug color of a splash particle under DEBUG_CONTACT: a drop holding on
// to a surface it sees green, one held without seeing it yellow, a trail
// blue or, held, orange, a puddle cyan and a falling drop gray.
float3 contactColor(uint index,bool attached,bool trail,bool puddle){
 bool held=contactHistory[index*CONTACT_ROWS+2].w>.5;
 if(puddle)return float3(0,.8,.9);
 if(trail)return held?float3(1,.5,0):float3(.15,.35,1);
 if(attached)return held?float3(1,.9,0):float3(.1,.9,.1);
 return .35;
}
Varying main(uint id:SV_VertexID){
 uint i=id/6;bool primary=i<PRIMARY_COUNT;uint poolIndex=primary?i:i-PRIMARY_COUNT;
 FluidParticle p;if(primary)p=particles[poolIndex];else p=splashes[poolIndex];
 uint owner=primary?primarySlot(poolIndex):splashSlot(p.velocityAlive.w);
 float visualScale=lerp(.75,1,smoothstep(.28,1.69,slotDrive(owner)));
 float width=primary?slotThread(owner):1;
 float radius=SURFACE_RADIUS*width*(primary&&p.densityBirth.w>=16?birthRadiusScale(p.densityBirth.w):visualScale*(primary?1:max(p.densityBirth.z,.1)));
 bool puddle=!primary&&floorPuddle(p.densityBirth.w),trail=!primary&&trailSegment(p.densityBirth.w);
 if(puddle)radius=SURFACE_RADIUS*max(p.densityBirth.z,.1)*puddleWidth(p.densityBirth.x)*PUDDLE_SPLAT_SCALE;
 float3 root=slotRoot(owner),center=root+p.positionAge.xyz;
 bool valid=p.velocityAlive.w>.5&&all(isfinite(p.positionAge))&&all(isfinite(p.velocityAlive))&&length(p.positionAge.xyz)<2.5&&p.positionAge.y>=-1&&slotLive(owner);
 valid=valid&&!(primary&&streamLinked(links[poolIndex*LINK_ROWS+1]));

 float4 axisScale=float4(1,0,0,1),normalScale=float4(0,0,0,1);
 float fade=1;
 bool attached=!primary&&!puddle&&!trail&&p.predictedLambda.w>.5;
 if(puddle){
  axisScale=float4(p.predictedLambda.xyz,1);
  valid=valid&&dot(axisScale.xyz,axisScale.xyz)>.5;
  // The solver keeps a puddle particle's center its half height above the
  // ground (Splash.hlsl). The disc goes on the contact plane itself, or at a
  // low camera angle the depth check would take the gap for an obstacle.
  center-=axisScale.xyz*puddleHalfHeight(FLUID_RADIUS*max(p.densityBirth.z,.1),p.densityBirth.x);
  fade=1-smoothstep(PUDDLE_LIFETIME-.4,PUDDLE_LIFETIME,p.positionAge.w);
 }else if(trail){
  // The solver keeps the segment's end on the surface (Splash.hlsl).
  float3 normal=p.predictedLambda.xyz,segment=p.velocityAlive.xyz;
  float halfLength=length(segment)*.5;
  valid=valid&&abs(dot(normal,normal)-1)<.05&&halfLength>1e-5;
  radius*=TRAIL_WIDTH;
  center-=segment*.5;
  axisScale=float4(normal,1);
  normalScale=float4(segment/max(halfLength*2,1e-5),halfLength);
  fade=1-smoothstep(TRAIL_LIFETIME-TRAIL_FADE,TRAIL_LIFETIME,p.positionAge.w);
 }else{
  float speed=length(p.velocityAlive.xyz);
  if(attached){
   // A clinging drop is drawn out along its flow, or downhill while it still
   // stands, and pressed against the surface. The solver keeps its center
   // its own radius off the surface (Splash.hlsl); the flattened lens is
   // lowered to touch it, and the scene depth hides whatever sits below.
   float3 normal=p.predictedLambda.xyz;
   valid=valid&&abs(dot(normal,normal)-1)<.05;
   // It shrinks away over the end of its life instead of vanishing at once.
   float ending=smoothstep(ADHESION_LIFETIME-ADHESION_FADE,ADHESION_LIFETIME,p.positionAge.w);
   radius*=1-.7*ending;
   fade=1-ending;
   float3 along=p.velocityAlive.xyz-normal*dot(p.velocityAlive.xyz,normal);
   float alongSpeed=length(along);
   float3 flow=alongSpeed>.02?along/alongSpeed:float3(0,-1,0)+normal*normal.y;
   float flowLength=length(flow);
   if(flowLength>.05)axisScale=float4(flow/flowLength,lerp(FILM_STRETCH_MIN,FILM_STRETCH_MAX,saturate(alongSpeed/.25)));
   float flatten=min(FILM_FLATTEN,FILM_HALF_MAX/max(radius,1e-5));
   normalScale=float4(normal,flatten);
   center-=normal*max(FLUID_RADIUS*max(p.densityBirth.z,.1)-radius*flatten,0);
  }else if(speed>.05){
   // Fast drops stretch along their motion, so spray reads as streaks and
   // consecutive stream particles overlap instead of beading.
   axisScale=float4(p.velocityAlive.xyz/speed,1+.8*saturate(speed/1.5));
  }
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
   if(compression>.001)axisScale=float4(lateral/lateralLength,1-.6*amount*compression);
  }
 }
 valid=valid&&fade>.01;
 float bound=trail?normalScale.w+radius:radius*max(max(axisScale.w,normalScale.w),1)*1.1;
 float4 rect;
 valid=valid&&screenRect(center,bound,rect);

 Varying o;
 o.position=valid?rectCorner(rect,id%6):float4(2,2,0,1);
 o.ndc=o.position.xy;o.center=center;o.axisScale=axisScale;o.normalScale=normalScale;o.radiusFade=float2(radius,fade);
 o.mode=valid?(trail?MODE_CAPSULE:(puddle?MODE_DISC:MODE_ELLIPSOID)):MODE_NONE;
 o.liquid=slotLiquid(owner);
 o.material=float2(slotScattering(owner),slotRoughness(owner));
 uint view=debugView();
 if(view==DEBUG_OOZE)o.liquid.rgb=.35;
 else if(view==DEBUG_FLOW)o.liquid.rgb=primary?(coherentGuide(renderGuides[poolIndex].longLane.w)?float3(1,.5,0):float3(1,0,0)):.35;
 else if(view==DEBUG_CONTACT)o.liquid.rgb=primary?.35:contactColor(poolIndex,attached,trail,puddle);
 // Airborne splashes and a stream that has broken up stay separate drops,
 // which FluidSmooth leaves nearly unfiltered; drops clinging to the body
 // and their trails are filtered at full width so they join into one rivulet.
 o.tag=owner;
 if(puddle)o.tag|=TAG_PUDDLE;
 else if(attached||trail)o.tag|=TAG_FILM;
 else if(!primary||!coherentGuide(renderGuides[poolIndex].longLane.w))o.tag|=TAG_SPRAY;
 return o;
}
#endif
#ifdef PIXEL_SHADER
struct Surface{float depth:SV_Target0;float thickness:SV_Target1;float4 color:SV_Target2;float absorptionDepth:SV_Target3;float2 material:SV_Target4;};
// Scales v along a unit axis, or leaves it for a zero axis. The two axes of
// one particle are orthogonal, so the scalings commute.
float3 scaleAlong(float3 v,float4 axisScale){return v+axisScale.xyz*(dot(v,axisScale.xyz)*(1/axisScale.w-1));}
// Millimeters keep a thin film's weighted depth clear of half-float flushing.
void weighLiquid(inout Surface o,float4 liquid,float2 material){
 float weight=o.thickness+COLOR_WEIGHT_FLOOR;
 o.color=float4(liquid.rgb*weight,weight);
 o.absorptionDepth=liquid.w*1000*weight;
 o.material=material*weight;
}
// A film lying on the scene keeps only the pixels whose scene depth agrees
// with it within a few millimeters: it then neither climbs a shoe standing in
// it nor hangs past the edge of a step.
void clipOffScene(float4 projected){
 clip(projected.w-NEAR_W);
 if(IniParams[237].y>.5){
  float2 uv=screenUV(projected);
  float gap=behindScene(uv,projected);
  if(gap<-1e29||abs(gap)>.004+sceneSlopeAllowanceAt(uv))clip(-1);
 }
}
Surface main(Varying i){
 clip(i.mode-.5);
 float3 eye=eyePosition();
 float3 direction=pixelRay(i.ndc);
 float3 origin=eye-i.center;
 float radius=i.radiusFade.x;
 Surface o;
 if(i.mode>2.5){
  // The pixel's ray meets the trail segment's surface plane, where the film
  // thins from the segment's line to nothing at its edge. Its ends are cut
  // square: consecutive segments meet end to start and add up to one even
  // film, where rounded caps would overlap into a bead at every joint.
  float3 normal=i.axisScale.xyz;
  float denominator=dot(direction,normal);
  clip(abs(denominator)-1e-4);
  float t=-dot(origin,normal)/denominator;
  clip(t);
  float3 offset=origin+direction*t;
  float along=dot(offset,i.normalScale.xyz);
  clip(i.normalScale.w-abs(along));
  float r=length(offset-i.normalScale.xyz*along);
  clip(radius-r);
  float4 projected=project(i.center+offset);
  clipOffScene(projected);
  float coverage=1-r*r/(radius*radius);
  o.depth=encodeFluidDepth(projected.w,i.tag);o.thickness=TRAIL_FILM*coverage*i.radiusFade.y;
  weighLiquid(o,i.liquid,i.material);
  return o;
 }
 if(i.mode>1.5){
  // The pixel's ray meets the puddle's ground plane.
  float3 normal=i.axisScale.xyz;
  float denominator=dot(direction,normal);
  clip(abs(denominator)-1e-4);
  float t=-dot(origin,normal)/denominator;
  clip(t);
  float3 hit=eye+direction*t;
  float r=distance(hit,i.center);
  clip(radius-r);
  float4 projected=project(hit);
  clipOffScene(projected);
  float coverage=1-r*r/(radius*radius);
  o.depth=encodeFluidDepth(projected.w,i.tag);o.thickness=PUDDLE_SPLAT_FILM*coverage*coverage*i.radiusFade.y;
  weighLiquid(o,i.liquid,i.material);
  return o;
 }
 // Scale the ray along the anisotropy axes and intersect the plain sphere.
 // The direction keeps its world length, so entry and exit stay world
 // distances along the unscaled ray.
 float3 scaledOrigin=scaleAlong(scaleAlong(origin,i.axisScale),i.normalScale);
 float3 scaledDirection=scaleAlong(scaleAlong(direction,i.axisScale),i.normalScale);
 float a=dot(scaledDirection,scaledDirection),b=dot(scaledOrigin,scaledDirection),c=dot(scaledOrigin,scaledOrigin)-radius*radius;
 float discriminant=b*b-a*c;
 clip(discriminant);
 float root=sqrt(discriminant);
 float entry=(-b-root)/a,exit=(-b+root)/a;
 // Only the part beyond the near plane counts. With the eye inside the drop,
 // or the near plane cutting it, the surface is where the ray leaves it.
 float visible=max(entry,nearDistance(direction));
 clip(exit-visible);

 float3 hit=eye+direction*(entry<visible?exit:entry);
 float4 projected=project(hit);
 if(sceneInFront(screenUV(projected),projected))clip(-1);
 o.depth=encodeFluidDepth(projected.w,i.tag);o.thickness=(exit-visible)*i.radiusFade.y;
 weighLiquid(o,i.liquid,i.material);
 return o;
}
#endif
