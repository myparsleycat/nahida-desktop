#include "../fluid/ParticleConfig.hlsl"
Texture1D<float4> IniParams:register(t120);
Texture2D<float4> captured:register(t0);
StructuredBuffer<float4> profiles:register(t1);
StructuredBuffer<float4> modelOffsets:register(t2);
cbuffer Camera:register(b13){float4 camera[96];}
RWStructuredBuffer<float4> candidates:register(u0);
RWStructuredBuffer<float4> stats:register(u1);
#include "CharacterRoot.hlsl"
[numthreads(1,1,1)]
void main(uint3 tid:SV_DispatchThreadID){
 stats[0].x+=1;
 float4 rootRow=characterRoot();
 stats[1]=rootRow;
 // The drawn character's own root, whoever it is: the controlled character,
 // a party member or the character screen model.
 if(abs(rootRow.w-1)>.01)return;
 float3 root=rootRow.xyz;
 stats[0].y+=1;stats[2]=float4(root,1);
 float4 a=captured.Load(int3(0,0,0)),b=captured.Load(int3(1,0,0)),c=captured.Load(int3(2,0,0));
 stats[3]=a;stats[4]=b;stats[5]=c;
 if(min(a.w,min(b.w,c.w))<.5)return;
 stats[0].z+=1;
 uint id=(uint)IniParams[243].x;
 uint profileCount,profileStride;profiles.GetDimensions(profileCount,profileStride);if((id+1)*PROFILE_ROWS>profileCount)return;
 float4 profile=profiles[id*PROFILE_ROWS];
 // Cleared slots mean no JSON profile. Never use an old/root fallback.
 if(profile.x>100||abs(profile.y+profile.z+profile.w-1)>.001||min(profile.y,min(profile.z,profile.w))<-.00001)return;
 float3 p=a.xyz*profile.y+b.xyz*profile.z+c.xyz*profile.w+camera[7].xyz;
 float3 delta=p-root;stats[6]=float4(delta,1);
 // Reject a bad projection or an unrelated render pass instead of moving the emitter.
 if(dot(delta.xz,delta.xz)>1.0 || delta.y<-.5 || delta.y>1.7)return;
 float3 n=cross(b.xyz-a.xyz,c.xyz-a.xyz);
 float area=length(n);stats[7]=float4(area,id,profile.x,1);if(area<1e-9||area>.03)return;
 stats[0].w+=1;

 // Every part of one character is drawn with the same root, so the root
 // groups the hooks of a frame by character. Within one, the emitter with the
 // lowest priority value wins, as the single-emitter runtime chose before.
 uint count=min((uint)candidates[0].x,CANDIDATE_CAPACITY),slot=count;
 [loop]for(uint k=0;k<count;k++){
  float3 other=candidates[1+k*CANDIDATE_ROWS+5].xyz-root;
  if(dot(other,other)<.0001){slot=k;break;}
 }
 if(slot<count && profile.x>=candidates[1+slot*CANDIDATE_ROWS+4].x)return;
 if(slot==count){
  if(count>=CANDIDATE_CAPACITY)return;
  candidates[0]=float4(count+1,0,0,0);
 }

 n/=area;
 // Carry the model's forward/up frame through the live triangle deformation.
 float3 tangent=normalize(b.xyz-a.xyz);
 float3 bitangent=cross(n,tangent);
 float3 f=profiles[id*PROFILE_ROWS+1].xyz,u=profiles[id*PROFILE_ROWS+2].xyz;
 float3 forward=normalize(tangent*f.x+bitangent*f.y+n*f.z);
 float3 upHint=normalize(tangent*u.x+bitangent*u.y+n*u.z);
 float3 right=normalize(cross(upHint,forward));
 float3 up=normalize(cross(forward,right));
 // Per-model offsets are independent of the captured surface triangle.
 float3 localOffset=modelOffsets[id].xyz;
 p+=right*localOffset.x+up*localOffset.y+forward*localOffset.z;
 stats[8]=float4(p,1);
 uint base=1+slot*CANDIDATE_ROWS;
 candidates[base]=float4(right,0);candidates[base+1]=float4(up,0);candidates[base+2]=float4(forward,0);
 candidates[base+3]=float4(p,1);candidates[base+4]=float4(profile.x,id,area,1);
 candidates[base+5]=float4(root,1);candidates[base+6]=float4(a.xyz+camera[7].xyz,a.w);candidates[base+7]=float4(b.xyz+camera[7].xyz,b.w);candidates[base+8]=float4(c.xyz+camera[7].xyz,c.w);
}
