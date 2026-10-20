// Shared by the screen-space fluid passes: particle layout, camera rows, and
// the view-depth <-> world reconstruction every pass must agree on.
#ifndef LIQUID_SSFR_COMMON
#define LIQUID_SSFR_COMMON
#include "ParticleConfig.hlsl"
#include "Puddle.hlsl"
Texture1D<float4> IniParams:register(t120);
cbuffer Camera:register(b13){float4 camera[96];}
Texture2D<float> sceneDepth:register(t106);
struct FluidParticle{float4 positionAge;float4 velocityAlive;float4 predictedLambda;float4 densityBirth;};
// Render radius: particle spacing is about FLUID_RADIUS, so overlapping
// spheres merge into one surface once the depth is smoothed.
static const float SURFACE_RADIUS=FLUID_RADIUS*1.6;
// Depth of a pixel no particle covered. Written by the frame clear.
static const float NO_FLUID=1e30;
float4 project(float3 p){return p.x*camera[16]+p.y*camera[17]+p.z*camera[18]+camera[19];}
float2 screenUV(float4 p){return float2(p.x/p.w*.5+.5,.5-p.y/p.w*.5);}
float3 eyePosition(){return camera[7].xyz;}
bool noFluid(float depth){return depth>=NO_FLUID*.5;}
// FluidSurface keeps the nearest particle's tag in the low mantissa bits of
// its view depth: bits 0-2 the owner slot, 3 a floor puddle disc, 4 spray.
// Positive floats order like their bits, so min blending still keeps the
// nearest particle, now with its tag; the bits move the depth by under 4e-6
// of itself. A pass that writes a filtered depth must tag it again.
static const uint DEPTH_TAG_MASK=31;
static const uint TAG_SLOT_MASK=7,TAG_PUDDLE=8,TAG_SPRAY=16;
float encodeFluidDepth(float depth,uint tag){return asfloat((asuint(depth)&~DEPTH_TAG_MASK)|tag);}
float fluidDepthValue(float raw){return asfloat(asuint(raw)&~DEPTH_TAG_MASK);}
uint fluidDepthTag(float raw){return asuint(raw)&DEPTH_TAG_MASK;}
// Puddles of different characters lie in one film on the floor; any other
// liquid joins only the liquid of its own character.
bool sameFluidBody(uint a,uint b){return (a&b&TAG_PUDDLE)!=0||((a^b)&TAG_SLOT_MASK)==0;}
float4 screenTriangle(uint id){return float4(id==1?3:-1,id==2?3:-1,0,1);}
// World position whose projection has clip w `depth` at screen `uv`. Solves
// the x, y and w rows of the projection for the eye-relative offset, which
// holds under TAA jitter and either handedness.
float3 worldAt(float2 uv,float depth){
 float3 eye=camera[7].xyz;
 float3 rx=float3(camera[16].x,camera[17].x,camera[18].x);
 float3 ry=float3(camera[16].y,camera[17].y,camera[18].y);
 float3 rw=float3(camera[16].w,camera[17].w,camera[18].w);
 float4 atEye=project(eye);
 float3 b=float3((uv.x*2-1)*depth-atEye.x,(1-uv.y*2)*depth-atEye.y,depth-atEye.w);
 float det=dot(rx,cross(ry,rw));
 if(abs(det)<1e-12)return eye;
 return eye+(b.x*cross(ry,rw)+b.y*cross(rw,rx)+b.z*cross(rx,ry))/det;
}
// Screen pixels one meter spans perpendicular to the view at `depth`.
float pixelsPerMeter(float depth,float2 dims){
 float3 ry=float3(camera[16].y,camera[17].y,camera[18].y);
 float3 rw=float3(camera[16].w,camera[17].w,camera[18].w);
 float3 perpendicular=ry-rw*(dot(ry,rw)/max(dot(rw,rw),1e-12));
 return length(perpendicular)*dims.y*.5/max(depth,1e-4);
}
// View distance of the scene depth texel, or -1 where nothing was drawn.
float sceneViewDistance(int2 pixel,int2 last,float4 base){
 float obstacle=sceneDepth.Load(int3(clamp(pixel,int2(0,0),last),0));
 if(!(isfinite(obstacle)&&obstacle>0&&obstacle<1))return -1;
 float4 view=base+obstacle*camera[14];
 return abs(view.w)>1e-8?abs(view.z/view.w):-1;
}
float smootherSlope(float center,float forward,float backward){
 float slope=min(forward>=0?abs(forward-center):1e30,backward>=0?abs(center-backward):1e30);
 return slope<1e29?slope:0;
}
// Scene depth texel under `uv` and the inverse-projection base of that pixel.
// Upscaled postprocessing targets need a normalized depth lookup instead of
// a differently sized native DSV on the final color target.
bool sceneTexelAt(float2 uv,out int2 texel,out int2 last,out float4 base){
 uint w,h;sceneDepth.GetDimensions(w,h);
 last=int2(w-1,h-1);
 texel=clamp(int2(saturate(uv)*float2(w,h)),int2(0,0),last);
 base=(uv.x*2-1)*camera[12]+(1-uv.y*2)*camera[13]+camera[15];
 return w>0&&h>0;
}
// The nearest texel of a lower resolution, jittered depth buffer is off by
// up to one texel of the surface's slope. On a floor seen at a grazing angle
// that is millimeters, enough to cut a puddle lying on it into rows. The
// smoother side's slope is used so occluder silhouettes stay tight.
float sceneSlopeAllowance(int2 texel,int2 last,float4 base,float scene){
 float slope=max(
  smootherSlope(scene,sceneViewDistance(texel+int2(1,0),last,base),sceneViewDistance(texel-int2(1,0),last,base)),
  smootherSlope(scene,sceneViewDistance(texel+int2(0,1),last,base),sceneViewDistance(texel-int2(0,1),last,base)));
 // An occluder one texel wide, such as a railing, has the background on both
 // sides, and that depth gap is no slope. A floor seen from standing height
 // changes by under 2 percent of its distance per texel out to about 15 m,
 // so the cap keeps puddles whole while thin occluders still hide liquid.
 return min(slope,scene*.02);
}
float sceneSlopeAllowanceAt(float2 uv){
 int2 texel,last;float4 base;
 if(!sceneTexelAt(uv,texel,last,base))return 0;
 float scene=sceneViewDistance(texel,last,base);
 return scene<0?0:sceneSlopeAllowance(texel,last,base,scene);
}
// How far the point with clip `c` lies behind the scene at `uv`, in view
// distance; -1e30 where no geometry was drawn.
float behindScene(float2 uv,float4 c){
 int2 texel,last;float4 base;
 if(!sceneTexelAt(uv,texel,last,base))return -1e30;
 float scene=sceneViewDistance(texel,last,base);
 float4 view=base+(c.z/c.w)*camera[14];
 if(scene<0||abs(view.w)<=1e-8)return -1e30;
 return abs(view.z/view.w)-scene;
}
// Whether scene geometry at `uv` hides a fluid point with clip `projected`.
bool sceneInFront(float2 uv,float4 projected){
 if(IniParams[237].y<=.5)return false;
 float gap=behindScene(uv,projected);
 if(gap<-1e29)return false;
 // A 0.7 mm view-space bias, rather than a depth-buffer bias whose physical
 // size grows rapidly with camera distance.
 return gap>.0007+sceneSlopeAllowanceAt(uv);
}
#endif
