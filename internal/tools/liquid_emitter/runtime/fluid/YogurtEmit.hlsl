#include "ParticleConfig.hlsl"
Texture1D<float4> IniParams:register(t120);
#include "Slots.hlsl"
#include "Yogurt.hlsl"
RWStructuredBuffer<YParcel> requests:register(u2);
RWBuffer<uint> counter:register(u3);
RWBuffer<uint> stat:register(u4);
RWStructuredBuffer<float4> emitState:register(u5);
RWBuffer<uint> args:register(u6);
[numthreads(SLOT_COUNT,1,1)]
void main(uint3 id:SV_DispatchThreadID){
 uint s=id.x;float dt=IniParams[239].x*2,now=IniParams[239].y;
 if(s==0){
  uint alive=stat[0];stat[1]=alive;stat[0]=0;
  counter[2]=YOG_COUNT-alive;
  bool any=alive>0;
  [loop]for(uint k=0;k<SLOT_COUNT;k++)if(slotLive(k)&&slotYogurtMode(k))any=true;
  args[0]=any?YOG_COUNT/64:0;args[1]=1;args[2]=1;args[3]=0;
  args[4]=any?YOG_COUNT*12:0;args[5]=1;args[6]=0;args[7]=0;
  args[8]=any?3:0;args[9]=1;args[10]=0;args[11]=0;
 }
 DeviceMemoryBarrierWithGroupSync();
 float4 state=emitState[s];
 bool on=slotLive(s)&&slotYogurtMode(s)&&slotSeen(s)&&slotEmitterOn(s)&&!slotRebound(s)
  &&IniParams[237].x>.5&&IniParams[240].z>.5&&IniParams[241].x>.5;
 if(!on){emitState[s]=0;return;}
 float strength=slotEmissionStrength(s);
 float credit=((uint)state.w==slotGeneration(s)?state.x:0)+YOG_EMIT_RATE*strength/slotEmissionInterval(s)*dt;
 uint births=(uint)min(floor(credit),2);credit=min(credit-births,1);
 uint serial=(uint)state.w==slotGeneration(s)?(uint)state.y:0;
 float3 origin=slotAnchor(s,3).xyz-slotRoot(s),up=slotAnchor(s,1).xyz,right=slotAnchor(s,0).xyz,forward=slotAnchor(s,2).xyz;
 float3 axis=-up;
 [loop]for(uint k=0;k<births;k++){
  uint seed=s*8191u+serial+k;
  float radius=YOG_BASE_RADIUS*slotWidth(s)*lerp(.75,1.25,yogRandom(seed,now,1));
  float angle=6.2831853*yogRandom(seed,now,2),reach=.35*radius*sqrt(yogRandom(seed,now,3));
  YParcel p;
  p.pos=float4(origin+axis*(.3*radius)+(right*cos(angle)+forward*sin(angle))*reach,radius);
  p.vel=float4(axis*(.04+.04*strength),splashAliveTag(s));
  p.nrm=float4(0,0,0,YOG_AIR);
  p.aux=float4(0,now,YOG_KIND_EMITTED,0);
  p.ext=float4(now+YOG_LIFE,0,0,0);
  p.sway=0;p.bend=float4(0,0,0,slotGeneration(s));
  uint rank;InterlockedAdd(counter[0],1,rank);
  if(rank<min(YOG_REQUESTS,counter[2]))requests[rank]=p;
 }
 emitState[s]=float4(credit,serial+births,0,slotGeneration(s));
}
