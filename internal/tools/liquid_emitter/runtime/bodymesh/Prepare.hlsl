#include "Dimensions.hlsl"
Texture1D<float4> IniParams:register(t120);
StructuredBuffer<float4> atlas:register(t0);
StructuredBuffer<float4> topology:register(t1);
StructuredBuffer<float4> stats:register(t2);
StructuredBuffer<float4> candidates:register(t3);
Buffer<uint> modelMap:register(t4);
RWBuffer<uint> args:register(u0);
RWStructuredBuffer<float4> registry:register(u1);
[numthreads(1,1,1)]
void main(uint3 id:SV_DispatchThreadID){
 for(uint k=0;k<16;k++)args[k]=0;
 if(stats[9].z!=1||stats[9].x<0||stats[9].x>=16)return;
 uint profile=(uint)stats[9].y,model=modelMap[profile];if(model==0)return;
 uint rows,stride;topology.GetDimensions(rows,stride);if(rows<2)return;
 uint faces=(uint)topology[0].x;if(faces==0||faces>BODY_FACES)return;
 float frame=IniParams[225].y,version=topology[0].y;
 float3 root=candidates[1+(uint)stats[9].x*9+5].xyz;
 float4 counter=registry[8];if(counter.x!=frame)counter=float4(frame,0,0,0);
 counter.z+=1;
 for(uint s=0;s<(uint)counter.y;s++){
  float4 key=registry[s];
  if(key.x==frame&&key.y==model&&key.z==version&&length(atlas[s*BODY_STRIDE].xyz-root)<.01){
   counter.w+=1;registry[8]=counter;return;
  }
 }
 uint slot=(uint)counter.y;
 if(slot>=8){registry[8]=counter;return;}
 registry[slot]=float4(frame,model,version,0);counter.y+=1;registry[8]=counter;
 args[0]=faces*3;args[1]=1;args[3]=asuint((int)IniParams[243].z);args[4]=(uint)IniParams[225].z;
 args[5]=(faces+63)/64;args[6]=1;args[7]=1;
 args[8]=max(1,BODY_LEAVES/64);args[9]=1;args[10]=1;
 args[11]=1;args[12]=1;args[13]=1;args[14]=slot;args[15]=1;
}
