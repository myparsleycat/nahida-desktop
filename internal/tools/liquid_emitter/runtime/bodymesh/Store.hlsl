Texture1D<float4> IniParams:register(t120);
#include "Dimensions.hlsl"
Buffer<uint> modelMap:register(t4);
Buffer<uint> work:register(t5);
Texture2D<float4> captured:register(t0);
StructuredBuffer<float4> topology:register(t1);
StructuredBuffer<float4> stats:register(t2);
StructuredBuffer<float4> candidates:register(t3);
#include "../camera/Camera.hlsl"
RWStructuredBuffer<float4> atlas:register(u0);
[numthreads(64,1,1)]
void main(uint3 id:SV_DispatchThreadID){
 if(stats[9].z!=1||work[15]!=1)return;uint slot=work[14];
 if(slot>=8)return;
 uint n,stride;topology.GetDimensions(n,stride);if(n<2)return;
 uint count=(uint)topology[0].x,f=id.x;
 uint base=slot*BODY_STRIDE;float3 root=candidates[1+(uint)stats[9].x*9+5].xyz;
 if(slot==0&&f>0&&f<8)atlas[f*BODY_STRIDE+1].w=0;
 if(f==0){atlas[base]=float4(root,IniParams[225].x);atlas[base+1]=float4(modelMap[(uint)stats[9].y],count,topology[0].y,1);}
 if(f>=count)return;
 for(uint k=0;k<3;k++){
  float4 p=captured.Load(int3(3*(f%128)+k,f/128,0));
  atlas[base+2+f*4+k]=float4(p.xyz+(camera[7].xyz-root),p.w);
 }
 atlas[base+2+f*4+3]=topology[f+1];
}
