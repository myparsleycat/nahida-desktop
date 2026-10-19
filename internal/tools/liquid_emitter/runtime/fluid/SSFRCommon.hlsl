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
// Whether scene geometry at `uv` hides a fluid point with clip `projected`.
// Upscaled postprocessing targets need a normalized depth lookup instead of
// a differently sized native DSV on the final color target.
bool sceneInFront(float2 uv,float4 projected){
 if(IniParams[237].y<=.5)return false;
 uint depthWidth,depthHeight;sceneDepth.GetDimensions(depthWidth,depthHeight);
 if(depthWidth==0||depthHeight==0)return false;
 int2 last=int2(depthWidth-1,depthHeight-1);
 int2 depthPixel=clamp(int2(saturate(uv)*float2(depthWidth,depthHeight)),int2(0,0),last);
 float4 base=(uv.x*2-1)*camera[12]+(1-uv.y*2)*camera[13]+camera[15];
 float scene=sceneViewDistance(depthPixel,last,base);
 if(scene<0)return false;
 float4 fluidView=base+(projected.z/projected.w)*camera[14];
 if(abs(fluidView.w)<=1e-8)return false;
 // The nearest texel of a lower resolution, jittered depth buffer is off by
 // up to one texel of the surface's slope. On a floor seen at a grazing angle
 // that is millimeters, enough to cut a puddle lying on it into rows. The
 // smoother side's slope is used so occluder silhouettes stay tight.
 float slope=max(
  smootherSlope(scene,sceneViewDistance(depthPixel+int2(1,0),last,base),sceneViewDistance(depthPixel-int2(1,0),last,base)),
  smootherSlope(scene,sceneViewDistance(depthPixel+int2(0,1),last,base),sceneViewDistance(depthPixel-int2(0,1),last,base)));
 // An occluder one texel wide, such as a railing, has the background on both
 // sides, and that depth gap is no slope. A floor seen from standing height
 // changes by under 2 percent of its distance per texel out to about 15 m,
 // so the cap keeps puddles whole while thin occluders still hide liquid.
 slope=min(slope,scene*.02);
 // A 0.7 mm view-space bias, rather than a depth-buffer bias whose physical
 // size grows rapidly with camera distance.
 return scene+.0007+slope<abs(fluidView.z/fluidView.w);
}
#endif
