#include "Dimensions.hlsl"
StructuredBuffer<float4> stats:register(t2);
Buffer<uint> work:register(t5);
RWStructuredBuffer<float4> atlas:register(u0);
[numthreads(64,1,1)]
void main(uint3 id:SV_DispatchThreadID){
 if(work[15]!=1||work[14]>=8)return;
 uint base=work[14]*BODY_STRIDE,blocks=BODY_LEAVES/min(BODY_LEAVES,64);
 for(uint node=id.x+1;node<blocks;node+=64){
  uint level=firstbithigh(node),span=blocks>>level;
  uint start=(node-(1u<<level))*span;
  float3 lo=1e30,hi=-1e30;
  for(uint j=start;j<start+span;j++){
   uint row=base+2+BODY_FACES*4+(blocks+j)*2;
   lo=min(lo,atlas[row].xyz);hi=max(hi,atlas[row+1].xyz);
  }
  uint row=base+2+BODY_FACES*4+node*2;
  atlas[row]=float4(lo,0);atlas[row+1]=float4(hi,0);
 }
}
