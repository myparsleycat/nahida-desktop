#include "Dimensions.hlsl"
StructuredBuffer<float4> stats:register(t2);
Buffer<uint> work:register(t5);
RWStructuredBuffer<float4> atlas:register(u0);
#define BLOCK_LEAVES min(BODY_LEAVES,64)
groupshared float3 lows[64],highs[64];
[numthreads(64,1,1)]
void main(uint3 group:SV_GroupID,uint lane:SV_GroupIndex){
 if(work[15]!=1||work[14]>=8)return;
 uint base=work[14]*BODY_STRIDE;
 uint leaf=group.x*BLOCK_LEAVES+lane;
 uint start=leaf*32,end=min(start+32,(uint)atlas[base+1].y);
 float3 lo=1e30,hi=-1e30;
 if(lane<BLOCK_LEAVES){
 for(uint face=start;face<end;face++){
  float4 a=atlas[base+2+face*4],b=atlas[base+3+face*4],c=atlas[base+4+face*4];
  if(min(a.w,min(b.w,c.w))>.5&&all(isfinite(a.xyz))&&all(isfinite(b.xyz))&&all(isfinite(c.xyz))){lo=min(lo,min(a.xyz,min(b.xyz,c.xyz)));hi=max(hi,max(a.xyz,max(b.xyz,c.xyz)));}
 }
 uint row=base+2+BODY_FACES*4+(BODY_LEAVES+leaf)*2;
 atlas[row]=float4(lo,0);atlas[row+1]=float4(hi,0);
 }
 lows[lane]=lo;highs[lane]=hi;GroupMemoryBarrierWithGroupSync();
 for(uint size=BLOCK_LEAVES/2;size>0;size>>=1){
  if(lane<size){lo=min(lows[lane*2],lows[lane*2+1]);hi=max(highs[lane*2],highs[lane*2+1]);}
  GroupMemoryBarrierWithGroupSync();
  if(lane<size){
   lows[lane]=lo;highs[lane]=hi;
   uint node=(BODY_LEAVES/BLOCK_LEAVES+group.x)*size+lane;
   uint row=base+2+BODY_FACES*4+node*2;
   atlas[row]=float4(lo,0);atlas[row+1]=float4(hi,0);
  }
  GroupMemoryBarrierWithGroupSync();
 }
}
