// Pass 1 of the screen-space fluid: every live particle is an ellipsoid whose
// nearest view depth lands in o0 through min blending and whose ray chord
// adds up in o1 as thickness.
#include "SSFRCommon.hlsl"
#include "JetGuide.hlsl"
#include "SheetProfile.hlsl"
StructuredBuffer<FluidParticle> particles:register(t100);
StructuredBuffer<FluidParticle> splashes:register(t101);
StructuredBuffer<float4> actor:register(t102);
StructuredBuffer<JetGuide> renderGuides:register(t112);
struct Varying{
 float4 position:SV_Position;
 float3 world:TEXCOORD0;
 nointerpolation float3 center:TEXCOORD1;
 // xyz: one anisotropy axis, w: scale along it (<1 flattens, >1 stretches).
 nointerpolation float4 axisScale:TEXCOORD2;
 nointerpolation float2 radiusFade:TEXCOORD3;
 nointerpolation float valid:TEXCOORD4;
};
float birthRadiusScale(float value){return value>=48?.70:(value>=32?.85:1);}
#ifdef VERTEX_SHADER
Varying main(uint id:SV_VertexID){
 uint i=id/6;bool primary=i<PRIMARY_COUNT;uint poolIndex=primary?i:i-PRIMARY_COUNT;
 FluidParticle p;if(primary)p=particles[poolIndex];else p=splashes[poolIndex];
 float visualScale=lerp(.75,1,smoothstep(.28,1.69,IniParams[237].z));
 float radius=SURFACE_RADIUS*(primary&&p.densityBirth.w>=16?birthRadiusScale(p.densityBirth.w):visualScale*(primary?1:max(p.densityBirth.z,.1)));
 bool puddle=!primary&&floorPuddle(p.densityBirth.w);
 if(puddle)radius=SURFACE_RADIUS*max(p.densityBirth.z,.1)*puddleWidth(p.densityBirth.x);
 float3 center=actor[3].xyz+p.positionAge.xyz;
 float4 projected=project(center);
 bool valid=p.velocityAlive.w>.5&&all(isfinite(p.positionAge))&&all(isfinite(p.velocityAlive))&&length(p.positionAge.xyz)<2.5&&p.positionAge.y>=-1&&actor[4].w>.5&&all(isfinite(projected))&&projected.w>.05;

 float4 axisScale=float4(1,0,0,1);
 float fade=1;
 if(puddle){
  float width=puddleWidth(p.densityBirth.x);
  axisScale=float4(p.predictedLambda.xyz,max(1/(width*width*width),.03));
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
   if(amount>.001&&lateralLength>.001){
    float focus=sheetFocusDistance(amount),distance=max(p.densityBirth.z,0);
    float compression=smoothstep(0,focus*.2,distance)*(1-smoothstep(focus,focus+.02,distance));
    axisScale=float4(lateral/lateralLength,1-.6*amount*compression);
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
 o.world=world;o.center=center;o.axisScale=axisScale;o.radiusFade=float2(radius,fade);o.valid=valid?1:0;
 return o;
}
#endif
#ifdef PIXEL_SHADER
struct Surface{float depth:SV_Target0;float thickness:SV_Target1;};
Surface main(Varying i){
 clip(i.valid-.5);
 float3 eye=eyePosition();
 float3 direction=normalize(i.world-eye);
 float3 origin=eye-i.center;
 // Scale the ray along the anisotropy axis and intersect the plain sphere.
 float stretch=1/i.axisScale.w-1;
 float3 scaledOrigin=origin+i.axisScale.xyz*(dot(origin,i.axisScale.xyz)*stretch);
 float3 scaledDirection=direction+i.axisScale.xyz*(dot(direction,i.axisScale.xyz)*stretch);
 float radius=i.radiusFade.x;
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
 Surface o;o.depth=projected.w;o.thickness=(exit-entry)*i.radiusFade.y;return o;
}
#endif
