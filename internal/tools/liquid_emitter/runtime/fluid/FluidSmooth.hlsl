// Pass 2 of the screen-space fluid: one separable step of a narrow-range
// depth filter (Truong and Yuksel 2018) with a plain Gaussian on thickness.
// Pixels without fluid never pull the surface, so silhouettes keep their
// size; farther samples clamp to the center so a surface behind another
// does not bleed into it; nearer ones are skipped as a separate body.
// SMOOTH_STEP selects the direction.
#include "SSFRCommon.hlsl"
Texture2D<float> fluidDepth:register(t100);
Texture2D<float> fluidThickness:register(t101);
struct ScreenVarying{float4 position:SV_Position;};
static const int SMOOTH_MAX_RADIUS=16;
// Spray drops span a few pixels; a wider filter melts them into blobs.
static const float SPRAY_SMOOTH_RADIUS=1.5;
#ifdef VERTEX_SHADER
ScreenVarying main(uint id:SV_VertexID){ScreenVarying o;o.position=screenTriangle(id);return o;}
#endif
#ifdef PIXEL_SHADER
struct Smoothed{float depth:SV_Target0;float thickness:SV_Target1;};
Smoothed main(ScreenVarying i){
 uint width,height;fluidDepth.GetDimensions(width,height);
 int2 pixel=int2(i.position.xy);
 float raw=fluidDepth.Load(int3(pixel,0));
 Smoothed o;o.depth=raw;o.thickness=fluidThickness.Load(int3(pixel,0));
 if(noFluid(raw))return o;
 float center=fluidDepthValue(raw);
 uint tag=fluidDepthTag(raw);

 float worldRadius=SURFACE_RADIUS*2;
 float maxRadius=(tag&TAG_SPRAY)!=0?SPRAY_SMOOTH_RADIUS:SMOOTH_MAX_RADIUS;
 float radiusPixels=clamp(worldRadius*pixelsPerMeter(center,float2(width,height)),1,maxRadius);
 int taps=(int)ceil(radiusPixels);
 float sigma=radiusPixels*.5;
 float threshold=worldRadius*1.5;
 float depthWeight=0,depthSum=0,thicknessWeight=0,thicknessSum=0;
 [loop]for(int k=-taps;k<=taps;k++){
  int2 sample=clamp(pixel+SMOOTH_STEP*k,int2(0,0),int2(width-1,height-1));
  float gaussian=exp(-float(k*k)/(2*sigma*sigma));
  float sampleRaw=fluidDepth.Load(int3(sample,0));
  bool fluid=!noFluid(sampleRaw);
  if(fluid&&!sameFluidBody(tag,fluidDepthTag(sampleRaw)))continue;
  thicknessSum+=fluidThickness.Load(int3(sample,0))*gaussian;thicknessWeight+=gaussian;
  float depth=fluidDepthValue(sampleRaw);
  if(!fluid||depth<center-threshold)continue;
  depthSum+=min(depth,center+threshold)*gaussian;depthWeight+=gaussian;
 }
 o.depth=encodeFluidDepth(depthWeight>0?depthSum/depthWeight:center,tag);
 o.thickness=thicknessWeight>0?thicknessSum/thicknessWeight:o.thickness;
 return o;
}
#endif
