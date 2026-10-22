// Pass 2 of the screen-space fluid: one separable step of a narrow-range
// depth filter (Truong and Yuksel 2018) with a plain Gaussian on thickness.
// Pixels without fluid never pull the surface, so silhouettes keep their
// size; farther samples clamp to the center so a surface behind another
// does not bleed into it; nearer ones are skipped as a separate body.
// SMOOTH_STEP selects the direction.
#include "SSFRCommon.hlsl"
#include "Slots.hlsl"
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
// Depth change per pixel along SMOOTH_STEP of the puddle at `pixel`, from the
// neighbors on its body on either side, or zero where neither is.
float puddleSlope(int2 pixel,int2 last,float center,uint tag,float threshold){
 float change=0,count=0;
 [unroll]for(int side=-1;side<=1;side+=2){
  float raw=fluidDepth.Load(int3(clamp(pixel+SMOOTH_STEP*side,int2(0,0),last),0));
  if(noFluid(raw)||!sameFluidBody(tag,fluidDepthTag(raw)))continue;
  float step=(fluidDepthValue(raw)-center)*side;
  // A neighbor farther off than the floor recedes is another surface.
  if(abs(step)>4*threshold)continue;
  change+=step;count++;
 }
 return count>0?change/count:0;
}
Smoothed main(ScreenVarying i){
 uint width,height;fluidDepth.GetDimensions(width,height);
 int2 pixel=int2(i.position.xy);
 float raw=fluidDepth.Load(int3(pixel,0));
 Smoothed o;o.depth=raw;o.thickness=fluidThickness.Load(int3(pixel,0));
 if(noFluid(raw))return o;
 float center=fluidDepthValue(raw);
 uint tag=fluidDepthTag(raw);

 // Puddles of several characters share one film, and clinging drops and
 // their trails are drawn at full size whatever the stream's, so only a
 // stream's own thickness narrows its filter.
 float worldRadius=SURFACE_RADIUS*2*((tag&(TAG_PUDDLE|TAG_FILM))!=0?1:slotThread(tag&TAG_SLOT_MASK));
 float maxRadius=(tag&TAG_SPRAY)!=0?SPRAY_SMOOTH_RADIUS:SMOOTH_MAX_RADIUS;
 float radiusPixels=clamp(worldRadius*pixelsPerMeter(center,float2(width,height)),1,maxRadius);
 int taps=(int)ceil(radiusPixels);
 float sigma=radiusPixels*.5;
 float threshold=worldRadius*1.5;
 // A puddle lies on the floor, which a low view sees recede by more than the
 // threshold within a few pixels: measured against the center alone, the
 // nearer samples drop out and the farther ones clamp, pushing it behind the
 // floor by an amount that changes from row to row. Its samples are measured
 // against its own slope instead, so a plane stays where it is.
 float slope=(tag&TAG_PUDDLE)!=0?puddleSlope(pixel,int2(width-1,height-1),center,tag,threshold):0;
 float depthWeight=0,depthSum=0,thicknessWeight=0,thicknessSum=0;
 [loop]for(int k=-taps;k<=taps;k++){
  int2 sample=clamp(pixel+SMOOTH_STEP*k,int2(0,0),int2(width-1,height-1));
  float gaussian=exp(-float(k*k)/(2*sigma*sigma));
  float sampleRaw=fluidDepth.Load(int3(sample,0));
  bool fluid=!noFluid(sampleRaw);
  if(fluid&&!sameFluidBody(tag,fluidDepthTag(sampleRaw)))continue;
  thicknessSum+=fluidThickness.Load(int3(sample,0))*gaussian;thicknessWeight+=gaussian;
  float depth=fluidDepthValue(sampleRaw)-slope*k;
  if(!fluid||depth<center-threshold)continue;
  depthSum+=min(depth,center+threshold)*gaussian;depthWeight+=gaussian;
 }
 o.depth=encodeFluidDepth(depthWeight>0?depthSum/depthWeight:center,tag);
 o.thickness=thicknessWeight>0?thicknessSum/thicknessWeight:o.thickness;
 return o;
}
#endif
