#include "ParticleConfig.hlsl"
#include "Puddle.hlsl"
#include "JetGuide.hlsl"
#include "SheetProfile.hlsl"
StructuredBuffer<JetGuide> renderGuides:register(t112);
// Local implicit surface reconstruction; no analytic contour clipping.
Texture1D<float4> IniParams:register(t120);
cbuffer Camera:register(b13){float4 camera[96];}
Texture2D<float4> sceneLighting:register(t104);
Texture2D<float> sceneDepth:register(t106);
struct FluidParticle{float4 positionAge;float4 velocityAlive;float4 predictedLambda;float4 densityBirth;};
StructuredBuffer<FluidParticle> particles:register(t100);
StructuredBuffer<FluidParticle> splashes:register(t101);
StructuredBuffer<float4> actor:register(t102);
struct Varying{
 float4 position:SV_Position;float2 uv:TEXCOORD0;
 nointerpolation float3 center:TEXCOORD1;nointerpolation float valid:TEXCOORD2;
 nointerpolation float radius:TEXCOORD5;
 nointerpolation float coreRadius:TEXCOORD18;
 nointerpolation float4 thinAxis:TEXCOORD17;
 nointerpolation float impact:TEXCOORD8;
 nointerpolation float puddle:TEXCOORD19;
 nointerpolation float bound:TEXCOORD7;
 nointerpolation float2 phaseAge:TEXCOORD4;
 nointerpolation float4 n0:TEXCOORD9;nointerpolation float4 n1:TEXCOORD10;
 nointerpolation float4 n2:TEXCOORD11;nointerpolation float4 n3:TEXCOORD12;
 nointerpolation float4 n4:TEXCOORD13;nointerpolation float4 n5:TEXCOORD14;
 nointerpolation float4 n6:TEXCOORD15;nointerpolation float4 n7:TEXCOORD16;
};
float4 project(float3 p){return p.x*camera[16]+p.y*camera[17]+p.z*camera[18]+camera[19];}
static const float RADIUS=FLUID_RADIUS;
static const float BOUND=3.2;
float birthRadiusScale(float value){return value>=48?.70:(value>=32?.85:1);}
// Approximate half-connector volume, normalized by the original sphere volume.
float connectorShare(float separation,float core){
 if(separation<=.001 || separation>=5)return 0;
 float waist=min(core,lerp(1.10,.16,smoothstep(1.5,5,separation)));
 float dx=separation/16,volume=0;
 [unroll]for(uint sampleIndex=0;sampleIndex<8;sampleIndex++){
  float x=(sampleIndex+.5)*dx,t=x/separation;
  float neck=lerp(waist,core,pow(abs(2*t-1),3));
  neck=max(0,neck-smoothstep(4.5,5,separation)*4);
  float sphereSection=max(0,core*core-x*x);
  volume+=max(0,neck*neck-sphereSection)*dx;
 }
 return .75*volume;
}
#ifdef VERTEX_SHADER
Varying main(uint id:SV_VertexID){
 uint i=id/6;bool primary=i<PRIMARY_COUNT;uint poolIndex=primary?i:i-PRIMARY_COUNT;
 FluidParticle p;if(primary)p=particles[poolIndex];else p=splashes[poolIndex];
 float visualScale=lerp(.75,1,smoothstep(.28,1.69,IniParams[237].z));
 float radius=RADIUS*(primary && p.densityBirth.w>=16?birthRadiusScale(p.densityBirth.w):visualScale*(primary?1:max(p.densityBirth.z,.1)));
 bool puddle=!primary && floorPuddle(p.densityBirth.w);
 if(puddle)radius=RADIUS*max(p.densityBirth.z,.1)*puddleWidth(p.densityBirth.x);
 float3 center=actor[3].xyz+p.positionAge.xyz;
 float4 projected=project(center);
 bool valid=p.velocityAlive.w>.5&&all(isfinite(p.positionAge))&&all(isfinite(p.velocityAlive))&&length(p.positionAge.xyz)<2.5&&p.positionAge.y>=-1&&actor[4].w>.5&&all(isfinite(projected))&&projected.w>.05;
 valid=valid&&abs(camera[8].x*radius*BOUND/max(projected.w,.05))<.06;
 float breakup=primary?saturate(-renderGuides[poolIndex].carrierAmount.w):0;
 float branchTag=primary?renderGuides[poolIndex].longLane.w:0;
 bool branch=primary && breakup>.95 && branchTag>=2;
 bool fullScatter=primary && branchTag< -2;
 float distances[8];float4 nearby[8];
 [unroll]for(uint k=0;k<8;k++){distances[k]=1e20;nearby[k]=0;}
 if(valid && !fullScatter){
  uint begin=(!primary && puddle)?AIRBORNE_SPLASH_COUNT:0;
  uint count=primary?PRIMARY_COUNT:(puddle?SPLASH_COUNT:AIRBORNE_SPLASH_COUNT);
  [loop]for(uint j=begin;j<count;j++){
   if(j==poolIndex)continue;
   FluidParticle q;if(primary)q=particles[j];else q=splashes[j];
   if(q.velocityAlive.w<.5)continue;
   bool otherPuddle=!primary && floorPuddle(q.densityBirth.w);
   if(puddle!=otherPuddle)continue;
   if(puddle && (dot(p.predictedLambda.xyz,q.predictedLambda.xyz)<.985
      || abs(dot(q.positionAge.xyz-p.positionAge.xyz,p.predictedLambda.xyz))>radius*.2))continue;
   if(primary && renderGuides[j].longLane.w< -2)continue;
   float otherRadius=RADIUS*(primary && q.densityBirth.w>=16?birthRadiusScale(q.densityBirth.w):visualScale*(primary?1:max(q.densityBirth.z,.1)));
   if(otherPuddle)otherRadius=RADIUS*max(q.densityBirth.z,.1)*puddleWidth(q.densityBirth.x);
   if(abs(otherRadius-radius)>radius*.05)continue;
   float3 delta=(q.positionAge.xyz-p.positionAge.xyz)/radius;
   float distance=dot(delta,delta);
   if(distance>=(branch?16:lerp(25,4.0,breakup)) || !all(isfinite(delta)))continue;
   // Overlapping/touching droplets merge locally regardless of trajectory.
   // Six local neighbors are separate from the two long-range stream links.
   if(distance<=4.41){
    float4 candidate=float4(delta,float(j+(primary?0:PRIMARY_COUNT))+1);
    [unroll]for(uint closeSlot=2;closeSlot<8;closeSlot++){
     if(distance<distances[closeSlot]){
      float oldDistance=distances[closeSlot];float4 oldNeighbor=nearby[closeSlot];
      distances[closeSlot]=distance;nearby[closeSlot]=candidate;
      distance=oldDistance;candidate=oldNeighbor;
     }
    }
    continue;
   }
   if(puddle)continue; // Wet patches connect by overlap, without velocity/stream links.
   if(breakup>=.999 && !branch)continue;
   if(branch){
    JetGuide ownGuide=renderGuides[poolIndex],otherGuide=renderGuides[j];
    if(abs(otherGuide.longLane.w-branchTag)>.1 || otherGuide.carrierAmount.w>-.95 || floor(ownGuide.originBirth.w/4.8)!=floor(otherGuide.originBirth.w/4.8))continue;
   }
   float speedP=length(p.velocityAlive.xyz),speedQ=length(q.velocityAlive.xyz);
   if(min(speedP,speedQ)<.02)continue;
   float3 directionP=p.velocityAlive.xyz/speedP,directionQ=q.velocityAlive.xyz/speedQ;
   if(dot(directionP,directionQ)<.90)continue;
   float3 tangent=normalize(directionP+directionQ);
   float along=dot(delta,tangent);
   float separation=sqrt(distance);
   float lateral=length(delta-tangent*along);
   if(abs(along)<.85*separation || lateral>.8+.1*separation)continue;
   // Only one predecessor and one successor: neighboring parallel paths do
   // not acquire a web of sideways bridges.
   uint slot=along>=0?0:1;
   if(distance<distances[slot]){
    distances[slot]=distance;
    nearby[slot]=float4(delta,float(j+(primary?0:PRIMARY_COUNT))+1);
   }
  }
 }
 const float2 corners[6]={float2(-1,-1),float2(1,-1),float2(-1,1),float2(-1,1),float2(1,-1),float2(1,1)};
 float2 uv=corners[id%6];float3 world=center+radius*BOUND*(normalize(camera[4].xyz)*uv.x+normalize(camera[5].xyz)*uv.y);
 Varying o;o.position=valid?project(world):float4(2,2,0,1);o.uv=uv;o.center=center;o.valid=valid?1:0;
 // Debit both half-connectors from this particle's visible volume. Solve
 // core^3 + allocated connector volume = original unit-sphere volume.
 float lower=.20,upper=1;
 [unroll]for(uint iteration=0;iteration<8;iteration++){
  float core=(lower+upper)*.5,used=core*core*core;
  [unroll]for(uint link=0;link<2;link++)if(nearby[link].w>0)used+=connectorShare(length(nearby[link].xyz),core);
  if(used>1)upper=core;else lower=core;
 }
 o.coreRadius=(nearby[0].w>0 || nearby[1].w>0)?(lower+upper)*.5:1;
 o.radius=radius;o.bound=BOUND;o.impact=(!primary&&p.densityBirth.w>1.5)?1:0;
 o.thinAxis=float4(1,0,0,1);o.puddle=puddle?1:0;
 if(puddle){
  float width=puddleWidth(p.densityBirth.x);
  o.thinAxis=float4(p.predictedLambda.xyz,1/(width*width*width));
 }
 if(primary){
  JetGuide guide=renderGuides[poolIndex];
  float amount=saturate(guide.carrierAmount.w);
  float3 lateral=cross(guide.axisSpeed.xyz,guide.longLane.xyz);
  float lateralLength=length(lateral);
  if(amount>.001 && lateralLength>.001){
   float focus=sheetFocusDistance(amount),distance=max(p.densityBirth.z,0);
   float compression=smoothstep(0,focus*.2,distance)*(1-smoothstep(focus,focus+.02,distance));
   o.thinAxis=float4(lateral/lateralLength,1-.6*amount*compression);
  }
 }
 o.phaseAge=float2(float(i),p.positionAge.w);
 o.n0=nearby[0];o.n1=nearby[1];o.n2=nearby[2];o.n3=nearby[3];
 o.n4=nearby[4];o.n5=nearby[5];o.n6=nearby[6];o.n7=nearby[7];return o;
}
#endif
#ifdef PIXEL_SHADER
struct Pixel{float4 color:SV_Target;float depth:SV_Depth;};
float dropletSurface(float3 p,float id,float core){
 float phase=frac(id*.61803398875)*6.2831853,t=IniParams[239].y;
 float r=length(p);float3 n=p/max(r,1e-6);
 float ripple=.025*sin(n.x*3+phase+t*5)*sin(n.z*3-phase*.7)+.018*cos(n.y*4+phase-t*7);
 return r-core-ripple*core;
}
float surfaceShape(float3 p,Varying i){
 // Order-independent soft union: each equal-size sample contributes to one
 // scalar field, rather than stretching geometry or folding capsules together.
 float3 thinNormal=i.thinAxis.xyz;float thinScale=max(i.thinAxis.w,i.puddle>.5?.03:.4);
 p+=thinNormal*dot(p,thinNormal)*(1/thinScale-1);
 const float softness=.36;
 float field=exp(-dropletSurface(p,i.phaseAge.x,i.coreRadius)/softness);
 float4 nearby[8]={i.n0,i.n1,i.n2,i.n3,i.n4,i.n5,i.n6,i.n7};
 [unroll]for(uint a=0;a<8;a++)nearby[a].xyz+=thinNormal*dot(nearby[a].xyz,thinNormal)*(1/thinScale-1);
 [unroll]for(uint k=0;k<8;k++){
  if(nearby[k].w>0){
   float weight=1-smoothstep(4.5,5.0,length(nearby[k].xyz));
   field+=weight*exp(-dropletSurface(p-nearby[k].xyz,nearby[k].w-1,i.coreRadius)/softness);
  }
 }
 float distance=-softness*log(max(field,1e-12));
 // Reconstruct thin necks only for the two trajectory-qualified neighbors.
 [unroll]for(uint k=0;k<2;k++){
  if(nearby[k].w>0){
   float3 axis=nearby[k].xyz;float separation=length(axis);
   if(separation>.001 && separation<5){
    float t=saturate(dot(p,axis)/dot(axis,axis));
    float gap=smoothstep(1.5,5.0,separation);
    float waist=min(i.coreRadius,lerp(1.10,.16,gap));
    float neck=lerp(waist,i.coreRadius,pow(abs(2*t-1),3));
    float bridge=length(p-axis*t)-neck;
    float edgeFade=smoothstep(4.5,5,separation);
    bridge+=edgeFade*4;
    float blend=.20,h=saturate(.5+.5*(bridge-distance)/blend);
    distance=lerp(bridge,distance,h)-blend*h*(1-h);
   }
  }
 }
 return distance*thinScale;
}
float3 hdrAt(float2 uv){
 uint w,h;sceneLighting.GetDimensions(w,h);if(w==0||h==0)return 0;
 int2 pixel=int2(saturate(uv)*float2(w-1,h-1));
 float3 color=sceneLighting.Load(int3(pixel,0)).rgb;
 return all(isfinite(color))?clamp(color,0,64):0;
}
float2 screenUV(float4 p){return float2(p.x/p.w*.5+.5,.5-p.y/p.w*.5);}
Pixel main(Varying i){
 clip(i.valid-.5);
 float3 right=normalize(camera[4].xyz),up=normalize(camera[5].xyz),toward=normalize(camera[7].xyz-i.center);
 float3 rayOrigin=(right*i.uv.x+up*i.uv.y+toward)*i.bound;
 float travel=0,endTravel=i.bound*2,marchScale=1;float3 hit=rayOrigin;bool found=false;
 if(i.puddle>.5){
  // Intersect a conservative floor slab before marching the thin surface.
  // This avoids spending all steps in empty space above a flattened patch.
  float thin=max(i.thinAxis.w,.03);
  float slab=.22+1.9*thin;
  float3 stretchedRay=toward+i.thinAxis.xyz*dot(toward,i.thinAxis.xyz)*(1/thin-1);
  marchScale=1/max(thin*length(stretchedRay),1e-6);
  float height=dot(rayOrigin,i.thinAxis.xyz),slope=dot(-toward,i.thinAxis.xyz);
  if(abs(slope)>1e-5){
   float a=(-slab-height)/slope,b=(slab-height)/slope;
   travel=max(0,min(a,b));endTravel=min(endTravel,max(a,b));
   clip(endTravel-travel);
  }else{clip(slab-abs(height));}
 }
 [loop]for(uint step=0;step<44;step++){
  hit=rayOrigin-toward*travel;
  float distance=surfaceShape(hit,i);
  if(distance<.008){found=true;break;}
  travel+=max(distance*.65*marchScale,.004);
  if(travel>endTravel)break;
 }
 clip(found?1:-1);
 // Each patch owns its nearest-center region, avoiding stacked alpha from
 // rendering the same merged surface once for every neighboring particle.
 float ownDistance=dot(hit,hit);
 float4 nearby[8]={i.n0,i.n1,i.n2,i.n3,i.n4,i.n5,i.n6,i.n7};
 [unroll]for(uint k=0;k<8;k++){
  if(nearby[k].w>0){
   float3 delta=hit-nearby[k].xyz;float otherDistance=dot(delta,delta);
   if(otherDistance<ownDistance-.00001 || (abs(otherDistance-ownDistance)<=.00001 && nearby[k].w-1<i.phaseAge.x))clip(-1);
  }
 }
 const float e=.012;
 float3 n=float3(
 surfaceShape(hit+float3(e,0,0),i)-surfaceShape(hit-float3(e,0,0),i),
 surfaceShape(hit+float3(0,e,0),i)-surfaceShape(hit-float3(0,e,0),i),
 surfaceShape(hit+float3(0,0,e),i)-surfaceShape(hit-float3(0,0,e),i));
 n/=max(length(n),1e-6);float3 world=i.center+hit*i.radius;float4 projected=project(world);clip(projected.w-.01);
 float2 uv=screenUV(projected);
 // Upscaled postprocessing targets need normalized depth lookup instead of
 // attaching a differently sized native DSV to the final color target.
 if(IniParams[237].y>.5){
  uint depthWidth,depthHeight;sceneDepth.GetDimensions(depthWidth,depthHeight);
  if(depthWidth>0 && depthHeight>0){
   int2 depthPixel=clamp(int2(saturate(uv)*float2(depthWidth,depthHeight)),int2(0,0),int2(depthWidth-1,depthHeight-1));
   float obstacle=sceneDepth.Load(int3(depthPixel,0));
   if(isfinite(obstacle) && obstacle>0 && obstacle<1){
    float4 base=(uv.x*2-1)*camera[12]+(1-uv.y*2)*camera[13]+camera[15];
    float4 sceneView=base+obstacle*camera[14];
    float4 fluidView=base+(projected.z/projected.w)*camera[14];
    if(abs(sceneView.w)>1e-8 && abs(fluidView.w)>1e-8){
     float sceneDistance=abs(sceneView.z/sceneView.w);
     float fluidDistance=abs(fluidView.z/fluidView.w);
     // A fixed 0.7 mm view-space bias, rather than a depth-buffer bias
     // whose physical size grows rapidly with camera distance.
     clip(sceneDistance+.0007-fluidDistance);
    }
   }
  }
 }
 // Read a separate snapshot, never the scene render target currently being written.
 float3 reflected=reflect(-toward,n);
 float2 reflectionUV=saturate(uv+float2(dot(reflected,right),-dot(reflected,up))*.19);
 float3 reflection=hdrAt(reflectionUV)*.52;
 reflection+=hdrAt(reflectionUV+float2(.002,0))*.12+hdrAt(reflectionUV-float2(.002,0))*.12;
 reflection+=hdrAt(reflectionUV+float2(0,.002))*.12+hdrAt(reflectionUV-float2(0,.002))*.12;
 float3 ambient=(hdrAt(uv+float2(.04,0))+hdrAt(uv-float2(.04,0))+hdrAt(uv+float2(0,.04))+hdrAt(uv-float2(0,.04)))*.25;
 // Broad scene probes avoid treating dark clothing immediately behind the jet as the entire environment.
 float3 broad=(hdrAt(float2(.15,.15))+hdrAt(float2(.85,.15))+hdrAt(float2(.2,.55))+hdrAt(float2(.8,.55)))*.25;
 ambient=ambient*.6+broad*.4;
 float luminance=dot(ambient,float3(.2126,.7152,.0722));
 float illumination=saturate(luminance/(luminance+.18));
 float fresnel=.0204+.9796*pow(1-saturate(dot(n,toward)),5);
 // Approximate a compact key-light lobe; its intensity follows the captured scene, not a constant glow.
 float3 light=normalize(float3(-.4,.8,.2));
 float spec=pow(saturate(dot(n,normalize(light+toward))),96);
 float shoulder=pow(saturate(dot(n,normalize(light+toward))),24);
 float3 body=float3(.78,.51,.055)*(.78+.30*illumination);
 float3 glint=float3(1,.97,.84)*(spec*4.3+shoulder*.30)*illumination;
 float3 color=body+reflection*(.45+1.65*fresnel)*(1+.40*i.impact)+glint*(1+.65*i.impact);
 // Increase coverage only at highlights, so thin transparent droplets retain visible sparkle.
 float distanceVisibility=saturate((projected.w-2)/8);
 float alpha=clamp(.52+.16*fresnel+spec*.36*illumination+.08*distanceVisibility,.52,.96);
 alpha=lerp(alpha,clamp(alpha*1.18+.04,.64,.98),i.impact);
 if(i.puddle>.5)alpha*=1-smoothstep(PUDDLE_LIFETIME-.4,PUDDLE_LIFETIME,i.phaseAge.y);
 Pixel o;o.depth=saturate(projected.z/projected.w);o.color=float4(color,alpha);return o;
}
#endif

