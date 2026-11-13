#include "Yogurt.hlsl"
StructuredBuffer<YParcel> source:register(t100);
StructuredBuffer<YParcel> requests:register(t107);
RWStructuredBuffer<YParcel> target:register(u0);
RWBuffer<uint> counter:register(u3);
RWBuffer<uint> stat:register(u4);
[numthreads(64,1,1)]
void main(uint3 id:SV_DispatchThreadID){
 uint i=id.x;if(i>=YOG_COUNT)return;
 YParcel p=source[i];
 if(p.pos.w<=0){
  p=(YParcel)0;
  uint rank;InterlockedAdd(counter[1],1,rank);
  if(rank<min(counter[0],min(YOG_REQUESTS,counter[2])))p=requests[rank];
 }
 if(p.pos.w>0){uint ignored;InterlockedAdd(stat[0],1,ignored);}
 target[i]=p;
}
