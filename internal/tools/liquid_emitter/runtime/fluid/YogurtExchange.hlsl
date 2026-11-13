#include "ParticleConfig.hlsl"
Texture1D<float4> IniParams:register(t120);
#include "Slots.hlsl"
#include "Puddle.hlsl"
#include "Yogurt.hlsl"
StructuredBuffer<YParcel> source:register(t100);
RWStructuredBuffer<float4> exchange:register(u0);
bool transferable(YParcel p){
 uint owner=splashSlot(p.vel.w);
 return p.pos.w>0&&p.nrm.w>.5&&slotLive(owner)&&slotYogurtMode(owner)
  &&(uint)p.bend.w==slotGeneration(owner)&&IniParams[239].y<p.ext.x;
}
[numthreads(64,1,1)]
void main(uint3 id:SV_DispatchThreadID){
 uint i=id.x;if(i>=YOG_COUNT)return;
 YParcel p=source[i];exchange[i]=float4(-1,0,0,0);
 if(!transferable(p))return;
 uint owner=splashSlot(p.vel.w);float base=YOG_BASE_RADIUS*slotWidth(owner);
 float a,h;yogParcelDome(i,base,p.pos,p.nrm,p.aux,p.ext,yogVolume(p.pos.w),a,h);
 bool smear=p.aux.z==YOG_KIND_SMEAR,hanging=p.aux.x>0;
 if(hanging)return;
 float best=1e30;uint receiver=YOG_COUNT;float volume=0;
 [loop]for(uint j=0;j<YOG_COUNT;j++){
  if(j==i)continue;YParcel q=source[j];
  if(q.vel.w!=p.vel.w||!transferable(q)||q.aux.z==YOG_KIND_SMEAR||dot(p.nrm.xyz,q.nrm.xyz)<.2)continue;
  bool pendant=q.aux.x>0;
  if(!pendant&&!smear&&(q.pos.w<p.pos.w||(q.pos.w==p.pos.w&&j>i)))continue;
  if(smear&&IniParams[239].y-p.aux.y<=1.5)continue;
  float distance=length(q.pos.xyz-p.pos.xyz),qa,qh;
  yogParcelDome(j,base,q.pos,q.nrm,q.aux,q.ext,yogVolume(q.pos.w),qa,qh);
  float overlap=1-distance/(a+qa);
  bool gathers=pendant||q.nrm.y<-.2||q.nrm.y>PUDDLE_MIN_UP;
  float deep=saturate((overlap-(smear?.3:(gathers?.4:.72)))/.15);
  float limit=(gathers?YOG_MAX_RADIUS:YOG_STREAM_RADIUS)*base;
  float room=smear?1:saturate((limit-q.pos.w)/(.15*limit));
  float share=min(6*IniParams[239].x,.25)*deep*room;
  if(share>0&&distance<best){best=distance;receiver=j;volume=yogVolume(p.pos.w)*share;}
 }
 if(receiver<YOG_COUNT)exchange[i]=float4(receiver,volume,0,0);
}
