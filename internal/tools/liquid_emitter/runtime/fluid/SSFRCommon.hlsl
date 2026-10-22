// Shared by the screen-space fluid passes: particle layout, camera rows, and
// the view-depth <-> world reconstruction every pass must agree on.
#ifndef LIQUID_SSFR_COMMON
#define LIQUID_SSFR_COMMON
#include "ParticleConfig.hlsl"
Texture1D<float4> IniParams:register(t120);
#include "Puddle.hlsl"
#include "../camera/Camera.hlsl"
Texture2D<float> sceneDepth:register(t106);
struct FluidParticle{float4 positionAge;float4 velocityAlive;float4 predictedLambda;float4 densityBirth;};
// Depth of a pixel no particle covered. Written by the frame clear.
static const float NO_FLUID=1e30;
float4 project(float3 p){return p.x*camera[16]+p.y*camera[17]+p.z*camera[18]+camera[19];}
float2 screenUV(float4 p){return float2(p.x/p.w*.5+.5,.5-p.y/p.w*.5);}
float3 eyePosition(){return camera[7].xyz;}
bool noFluid(float depth){return depth>=NO_FLUID*.5;}
// FluidSurface keeps the nearest particle's tag in the low mantissa bits of
// its view depth: bits 0-2 the owner slot, 3 a floor puddle disc, 4 spray,
// 5 a film of drops clinging to the body. Positive floats order like their
// bits, so min blending still keeps the nearest particle, now with its tag;
// the six bits move the depth by at most 63 ulp, under 7.6e-6 (below 1e-5)
// of itself. A pass that writes a filtered depth must tag it again.
static const uint DEPTH_TAG_MASK=63;
static const uint TAG_SLOT_MASK=7,TAG_PUDDLE=8,TAG_SPRAY=16,TAG_FILM=32;
float encodeFluidDepth(float depth,uint tag){return asfloat((asuint(depth)&~DEPTH_TAG_MASK)|tag);}
float fluidDepthValue(float raw){return asfloat(asuint(raw)&~DEPTH_TAG_MASK);}
uint fluidDepthTag(float raw){return asuint(raw)&DEPTH_TAG_MASK;}
// Puddles of different characters lie in one film on the floor; any other
// liquid joins only the liquid of its own character.
bool sameFluidBody(uint a,uint b){return (a&b&TAG_PUDDLE)!=0||((a^b)&TAG_SLOT_MASK)==0;}
float4 screenTriangle(uint id){return float4(id==1?3:-1,id==2?3:-1,0,1);}
// Clip w at and below which the fluid passes draw nothing: their near plane.
static const float NEAR_W=.05;
// Ray distance from the eye along a unit direction to the near plane; 1e30
// for a ray that never reaches it.
float nearDistance(float3 direction){
 float rate=dot(direction,float3(camera[16].w,camera[17].w,camera[18].w));
 return rate>1e-8?max((NEAR_W-project(eyePosition()).w)/rate,0):1e30;
}
// The screen rectangle, in normalized device coordinates, that a sphere
// covers: the box around the projected corners of the cube around it, or the
// whole screen once the cube reaches the near plane, where those corners no
// longer bound it. False when the sphere lies wholly behind the near plane
// or off the screen. Its size culls nothing: a drop at the lens still draws.
bool screenRect(float3 center,float bound,out float4 rect){
 rect=float4(1e30,1e30,-1e30,-1e30);
 bool crossing=false;float farthest=-1e30;
 [unroll]for(uint k=0;k<8;k++){
  float3 corner=center+bound*float3((k&1)!=0?1:-1,(k&2)!=0?1:-1,(k&4)!=0?1:-1);
  float4 c=project(corner);
  farthest=max(farthest,c.w);
  if(c.w<=NEAR_W){crossing=true;continue;}
  rect.xy=min(rect.xy,c.xy/c.w);rect.zw=max(rect.zw,c.xy/c.w);
 }
 if(!(farthest>NEAR_W)||!all(isfinite(center))||!(bound>0))return false;
 if(crossing)rect=float4(-1,-1,1,1);
 rect=float4(max(rect.xy,-1),min(rect.zw,1));
 return all(rect.xy<rect.zw);
}
// One of the six vertices of the two triangles that cover rect.
float4 rectCorner(float4 rect,uint vertex){
 const float2 corners[6]={float2(0,0),float2(1,0),float2(0,1),float2(0,1),float2(1,0),float2(1,1)};
 return float4(lerp(rect.xy,rect.zw,corners[vertex]),0,1);
}
// What $liquid_debug_view (IniParams[240].x) shows instead of the liquid:
// the state of oozing outlets, stream flows and their links, how drops hold
// on to surfaces, or thickness and material. The particle passes then write
// a color of their own where absorption goes, which the composite shows
// unlit.
static const uint DEBUG_OOZE=1,DEBUG_FLOW=2,DEBUG_CONTACT=3,DEBUG_OPTICS=4;
uint debugView(){return (uint)IniParams[240].x;}
bool debugColors(){uint view=debugView();return view>=DEBUG_OOZE&&view<=DEBUG_CONTACT;}
float3 debugHue(uint key){
 float hue=frac(float(key)*.618034);
 return saturate(abs(frac(hue+float3(0,2.0/3,1.0/3))*6-3)-1);
}
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
// Unit ray from the eye through the screen point at normalized device xy.
float3 pixelRay(float2 ndc){
 return normalize(worldAt(float2(ndc.x*.5+.5,.5-ndc.y*.5),1)-eyePosition());
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
// How far the point with clip `c` lies behind the scene at `uv` as above,
// with the scene depth interpolated between the four texels around `uv`.
// Raw depth runs linearly across the screen on a plane, so a floor reads as
// the plane itself rather than as one step per row of a depth buffer coarser
// than the screen. -1e30 where any of the four is empty or they do not lie on
// one plane, as across the edge of a shoe.
float behindSceneSmooth(float2 uv,float4 c){
 uint w,h;sceneDepth.GetDimensions(w,h);
 if(w<2||h<2)return -1e30;
 float2 position=saturate(uv)*float2(w,h)-.5;
 int2 corner=clamp(int2(floor(position)),int2(0,0),int2(w-2,h-2));
 float2 f=saturate(position-corner);
 float d00=sceneDepth.Load(int3(corner,0)),d10=sceneDepth.Load(int3(corner+int2(1,0),0));
 float d01=sceneDepth.Load(int3(corner+int2(0,1),0)),d11=sceneDepth.Load(int3(corner+int2(1,1),0));
 int2 last=int2(w-1,h-1);
 float dl=sceneDepth.Load(int3(clamp(corner-int2(1,0),int2(0,0),last),0)),dr=sceneDepth.Load(int3(clamp(corner+int2(2,0),int2(0,0),last),0));
 float du=sceneDepth.Load(int3(clamp(corner-int2(0,1),int2(0,0),last),0)),dd=sceneDepth.Load(int3(clamp(corner+int2(0,2),int2(0,0),last),0));
 float4 d=float4(d00,d10,d01,d11),beyond=float4(dl,dr,du,dd);
 if(!all(isfinite(d)&&d>0&&d<1)||!all(isfinite(beyond)&&beyond>0&&beyond<1))return -1e30;
 // On a plane the step between texels is the same across the four and on
 // into the next texel each way; an edge parallel to the texel rows keeps
 // the first but breaks the second.
 float sx=d10-d00,sy=d01-d00,slack=1e-7;
 if(abs(d11-d01-sx)>.25*abs(sx)+slack||abs(d11-d10-sy)>.25*abs(sy)+slack)return -1e30;
 if(abs(d00-dl-sx)>.25*abs(sx)+slack||abs(dr-d10-sx)>.25*abs(sx)+slack)return -1e30;
 if(abs(d00-du-sy)>.25*abs(sy)+slack||abs(dd-d01-sy)>.25*abs(sy)+slack)return -1e30;
 float4 base=(uv.x*2-1)*camera[12]+(1-uv.y*2)*camera[13]+camera[15];
 float4 scene=base+lerp(lerp(d00,d10,f.x),lerp(d01,d11,f.x),f.y)*camera[14];
 float4 view=base+(c.z/c.w)*camera[14];
 if(abs(scene.w)<=1e-8||abs(view.w)<=1e-8)return -1e30;
 return abs(view.z/view.w)-abs(scene.z/scene.w);
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
