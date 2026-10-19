Texture1D<float4> IniParams:register(t120);
Texture2D<float4> captured:register(t0);
StructuredBuffer<float4> profiles:register(t1);
StructuredBuffer<float4> modelOffsets:register(t2);
cbuffer Camera:register(b13){float4 camera[96];}
cbuffer Scene:register(b2){float4 scene[208];}
cbuffer Instance:register(b1){float4 instance[4096];}
RWStructuredBuffer<float4> surface:register(u0);
RWStructuredBuffer<float4> stats:register(u1);
[numthreads(1,1,1)]
void main(uint3 tid:SV_DispatchThreadID){
 stats[0].x+=1;
 float3 reference=scene[69].xyz,root=reference;float best=1e20;
 for(uint k=34;k<38;k++){float4 r=scene[k];float2 d=r.xz-reference.xz;float ds=dot(d,d);if(r.w>.01&&r.w<10&&ds<best){best=ds;root=r.xyz;}}
 if(best>4)return;
 stats[0].y+=1;stats[2]=float4(root,1);
 uint r=(uint)IniParams[242].w;
 stats[1]=instance[r+3];
 float3 actorDelta=instance[r+3].xyz-root;
 if(dot(actorDelta,actorDelta)>.04 || abs(instance[r+3].w-1)>.01)return;
 stats[0].z+=1;
 float4 a=captured.Load(int3(0,0,0)),b=captured.Load(int3(1,0,0)),c=captured.Load(int3(2,0,0));
 stats[3]=a;stats[4]=b;stats[5]=c;
 if(min(a.w,min(b.w,c.w))<.5)return;
 stats[0].w+=1;
 uint id=(uint)IniParams[243].x;
 uint profileCount,profileStride;profiles.GetDimensions(profileCount,profileStride);if(id*3+2>=profileCount)return;
 float4 profile=profiles[id*3];
 // Cleared slots mean no JSON profile. Never use an old/root fallback.
 if(profile.x>100||abs(profile.y+profile.z+profile.w-1)>.001||min(profile.y,min(profile.z,profile.w))<-.00001)return;
 float3 p=a.xyz*profile.y+b.xyz*profile.z+c.xyz*profile.w+camera[7].xyz;
 float3 delta=p-root;stats[6]=float4(delta,1);
 // Reject a bad projection or an unrelated render pass instead of moving the emitter.
 if(dot(delta.xz,delta.xz)>1.0 || delta.y<-.5 || delta.y>1.7)return;
 float3 n=cross(b.xyz-a.xyz,c.xyz-a.xyz);
 float area=length(n);stats[7]=float4(area,id,profile.x,1);if(area<1e-9||area>.03)return;
 if(surface[4].w>.5 && profile.x>=surface[4].x)return;
 n/=area;
 // Carry the model's forward/up frame through the live triangle deformation.
 float3 tangent=normalize(b.xyz-a.xyz);
 float3 bitangent=cross(n,tangent);
 float3 f=profiles[id*3+1].xyz,u=profiles[id*3+2].xyz;
 float3 forward=normalize(tangent*f.x+bitangent*f.y+n*f.z);
 float3 upHint=normalize(tangent*u.x+bitangent*u.y+n*u.z);
 float3 right=normalize(cross(upHint,forward));
 float3 up=normalize(cross(forward,right));
 // Per-model offsets are independent of the captured surface triangle.
 float3 localOffset=modelOffsets[id].xyz;
 p+=right*localOffset.x+up*localOffset.y+forward*localOffset.z;
 stats[8]=float4(p,1);
 surface[0]=float4(right,0);surface[1]=float4(up,0);surface[2]=float4(forward,0);
 surface[3]=float4(p,1);surface[4]=float4(profile.x,id,area,1);
 surface[5]=float4(root,1);surface[6]=float4(a.xyz+camera[7].xyz,a.w);surface[7]=float4(b.xyz+camera[7].xyz,b.w);surface[8]=float4(c.xyz+camera[7].xyz,c.w);
}
