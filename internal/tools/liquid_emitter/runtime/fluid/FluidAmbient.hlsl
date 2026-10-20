// Environment light of the scene for FluidComposite.hlsl: row 0 stands in for
// what lies above (the upper screen), row 1 for the broad surroundings.
// A few fixed texels flipped a whole puddle's mirror image whenever a lamp or
// an edge crossed one of them. A grid of samples whose outliers are capped at
// a multiple of their geometric mean, eased over a few frames, holds still.
Texture1D<float4> IniParams:register(t120);
Texture2D<float4> sceneLighting:register(t100);
RWStructuredBuffer<float4> ambient:register(u0);
static const uint GRID=16;
// Rows of the upper band, and of the band whose side columns give the
// surroundings without the character standing in the middle.
static const uint UPPER_ROWS=6,SIDE_LAST_ROW=10,SIDE_COLUMNS=5;
static const float OUTLIER_CAP=4;
static const float EASE_SECONDS=.15;
// rgb the clamped scene color of one grid cell, a its luminance.
groupshared float4 samples[GRID*GRID];
bool inRegion(uint index,uint region){
 uint2 cell=uint2(index%GRID,index/GRID);
 if(region==0)return cell.y<UPPER_ROWS;
 return cell.y>=UPPER_ROWS&&cell.y<=SIDE_LAST_ROW&&(cell.x<SIDE_COLUMNS||cell.x>=GRID-SIDE_COLUMNS);
}
// The cap follows the region's geometric mean, so it holds at any exposure.
float3 robustMean(uint region){
 float logSum=0,count=0;
 [loop]for(uint i=0;i<GRID*GRID;i++){
  if(!inRegion(i,region))continue;
  logSum+=log(samples[i].a+1e-4);count++;
 }
 float cap=OUTLIER_CAP*exp(logSum/max(count,1));
 float3 sum=0;
 [loop]for(uint j=0;j<GRID*GRID;j++){
  if(!inRegion(j,region))continue;
  sum+=samples[j].rgb*min(1,cap/max(samples[j].a,1e-6));
 }
 return sum/max(count,1);
}
[numthreads(GRID,GRID,1)]
void main(uint3 tid:SV_GroupThreadID){
 uint w,h;sceneLighting.GetDimensions(w,h);
 float3 color=0;
 if(w>0&&h>0){
  int2 texel=min(int2((float2(tid.xy)+.5)/GRID*float2(w,h)),int2(w-1,h-1));
  color=sceneLighting.Load(int3(texel,0)).rgb;
  color=all(isfinite(color))?clamp(color,0,64):0;
 }
 samples[tid.y*GRID+tid.x]=float4(color,dot(color,float3(.2126,.7152,.0722)));
 GroupMemoryBarrierWithGroupSync();
 if(tid.x!=0||tid.y!=0)return;

 float3 upper=robustMean(0);
 float3 broad=upper*.5+robustMean(1)*.5;
 // w marks a written row; the INI clears the buffer whenever the effect restarts.
 float4 previousUpper=ambient[0],previousBroad=ambient[1];
 if(previousUpper.w>.5&&previousBroad.w>.5&&all(isfinite(previousUpper))&&all(isfinite(previousBroad))){
  float ease=1-exp(-max(IniParams[253].w,0)/EASE_SECONDS);
  upper=lerp(previousUpper.rgb,upper,ease);
  broad=lerp(previousBroad.rgb,broad,ease);
 }
 ambient[0]=float4(upper,1);
 ambient[1]=float4(broad,1);
}
