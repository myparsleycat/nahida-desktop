Texture1D<float4> IniParams : register(t120);
cbuffer Instances : register(b1) { float4 rows[4096]; }
cbuffer Scene : register(b2) { float4 scene[208]; }
StructuredBuffer<float4> bones : register(t0);
RWStructuredBuffer<float4> result : register(u0);
[numthreads(1,1,1)]
void main(uint3 tid:SV_DispatchThreadID) {
 float3 reference=scene[69].xyz, player=reference; float best=1e20;
 for(uint k=34;k<38;k++) { float4 r=scene[k]; float2 d=r.xz-reference.xz; float ds=dot(d,d); if(r.w>.01&&r.w<10&&ds<best){best=ds;player=r.xyz;} }
 if(best>4) return;
 uint n,stride; bones.GetDimensions(n,stride);
 uint count=min((uint)IniParams[245].x,256), first=(uint)IniParams[245].y;
 for(uint i=0;i<count;i++) {
  uint r=(i+first)*16; if(r+15>=4096) break;
  float3 d=rows[r+3].xyz-player; float ds=dot(d,d);
  if(abs(rows[r+3].w-1)>.01 || ds>.04) continue;
  uint offset=asuint(rows[r+5]).x;
  if(offset<1536 || offset+3>=n) continue;
  if(result[0].w>.5 && ds>=result[0].x) continue;
  uint entries=min(768u,n-offset-3);
  result[0]=float4(ds,offset,entries,1);
  result[1]=float4(player,1);
  for(uint j=0;j<16;j++) result[2+j]=rows[r+j];
  for(uint b=0;b<768;b++) result[18+b]=b<entries?bones[offset+3+b]:0;
 }
}

