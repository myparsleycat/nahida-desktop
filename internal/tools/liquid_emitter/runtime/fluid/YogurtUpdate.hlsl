#include "ParticleConfig.hlsl"
Texture1D<float4> IniParams:register(t120);
#include "Puddle.hlsl"
#include "Slots.hlsl"
StructuredBuffer<float4> frameState:register(t105);
#include "Collision.hlsl"
#include "../bodymesh/Dimensions.hlsl"
#include "../bodymesh/Attachment.hlsl"
#include "Yogurt.hlsl"
StructuredBuffer<YParcel> source:register(t100);
StructuredBuffer<float4> exchange:register(t102);
RWStructuredBuffer<YParcel> target:register(u0);
RWStructuredBuffer<YParcel> requests:register(u2);
RWBuffer<uint> counter:register(u3);
bool ask(YParcel q){uint rank;InterlockedAdd(counter[0],1,rank);if(rank>=min(YOG_REQUESTS,counter[2]))return false;requests[rank]=q;return true;}
[numthreads(64,1,1)]
void main(uint3 id:SV_DispatchThreadID){
 uint i=id.x;if(i>=YOG_COUNT)return;
 YParcel p=source[i];
 YParcel dead=(YParcel)0;
 if(p.pos.w<=0){target[i]=dead;return;}
 float dt=IniParams[239].x*2,now=IniParams[239].y;
 uint owner=splashSlot(p.vel.w),fs=owner*4;
 float3 shift=frameState[fs].w>.5?frameState[fs].xyz-slotRoot(owner):0;
 if((uint)p.bend.w!=slotGeneration(owner)||!slotLive(owner)||slotRebound(owner)||!slotYogurtMode(owner)||length(shift)>1.5||now>=p.ext.x||!all(isfinite(p.pos))){target[i]=dead;return;}
 float base=YOG_BASE_RADIUS*slotWidth(owner);
 float state=p.nrm.w,kind=p.aux.z;
 bool attached=state>.5,smear=kind==YOG_KIND_SMEAR,hanging=p.aux.x>0;
 float r=p.pos.w,volume=yogVolume(r);
 float3 n=p.nrm.xyz;
 float3 steady=n,normalSum=0;
 float a,h;yogParcelDome(i,base,p.pos,p.nrm,p.aux,p.ext,volume,a,h);
 float crowd=0,weightSum=0,gain=0,loss=exchange[i].y;float3 velocitySum=0,push=0,pull=0,centerSum=0;bool hangBlocked=false,followed=false,yields=false;float covered=0;
 if(attached){
  float age=now-p.aux.y;
  bool gathers=hanging||n.y<-.2||n.y>PUDDLE_MIN_UP;
  float3 fall=float3(0,-1,0)-n*(-n.y);
  [loop]for(uint j=0;j<YOG_COUNT;j++){
   float4 qp=source[j].pos;
   if(qp.w<=0||j==i)continue;
   float3 d=qp.xyz-p.pos.xyz;float reach=3*(r+qp.w);
   if(dot(d,d)>=reach*reach)continue;
   YParcel q=source[j];
   if(q.vel.w!=p.vel.w)continue;
   if(q.nrm.w<.5)continue;
   float dist=length(d);
   bool qSmear=q.aux.z==YOG_KIND_SMEAR,qHanging=q.aux.x>0;
   if(hanging&&qHanging&&dist<2.5*(r+qp.w)&&(q.aux.x>p.aux.x||(q.aux.x==p.aux.x&&j<i)))yields=true;
   if(dist<2.5*(r+qp.w)&&(qHanging||(!qSmear&&qp.w>=YOG_DRIP_RADIUS*base&&(qp.w>r||(qp.w==r&&j<i)))))hangBlocked=true;
   if(dot(n,q.nrm.xyz)<.2)continue;
   float qv=yogVolume(qp.w),qa,qh;yogParcelDome(j,base,q.pos,q.nrm,q.aux,q.ext,qv,qa,qh);
   float overlap=1-dist/(a+qa);
   if(overlap<=0)continue;
   float w=saturate(overlap*2);
   if(smear&&!qSmear)covered=max(covered,saturate((1-dist/(a+1.05*qa))/.2));
   if(!qSmear){
    crowd+=w*min(1,qv/volume);velocitySum+=q.vel.xyz*w;weightSum+=w;centerSum+=d*w;normalSum+=q.nrm.xyz*w;
    if(dot(d,fall)<-.2*dist)followed=true;
   }
   if((uint)exchange[j].x==i){gain+=exchange[j].y;pull+=d*exchange[j].y;}
   if(!smear&&!qSmear&&overlap>.62)push-=d/max(dist,1e-6)*((overlap-.62)*min(a,qa)*6);
  }
 }
 float3 gathered=0;
 if(gain>0){
  gathered=pull/max(volume+gain,1e-12);
  if(gain>volume*.002&&IniParams[240].z>.5)p.ext.x=max(p.ext.x,now+YOG_LIFE*.6);
 }
 if(IniParams[240].z<.5)p.ext.x=min(p.ext.x,now+8);
 volume=max(volume+gain-loss,0);r=yogRadius(volume);
 if(r<YOG_DEAD_RADIUS*base){target[i]=dead;return;}
 float3 from=p.pos.xyz+shift,to=from,velocity=p.vel.xyz;
 float stateTime=p.aux.w+dt;
 uint mesh=8;float3 at=from,boundNormal=n;
 bool onMesh=bodyReuse(i,owner,p.aux.y,YOG_LIFT,at,boundNormal,mesh);
 if(!attached){
  velocity+=float3(0,-9.81,0)*dt;velocity*=exp(-.6*dt);
  to=from+velocity*dt;
  if(onMesh){to=at;n=boundNormal;state=YOG_MESH;}
  else{
   float3 contact,normal;float reach=max(r*.6,.002);
   bool touched=collideFluid(owner,from,to,velocity,reach,contact,normal);
   if(!touched)touched=sweptBySurface(owner,to,velocity,reach,contact,normal);
   if(touched&&normal.y<PUDDLE_MIN_UP){
    float3 surface,surfaceNormal;
    if(!screenSurface(owner,to,surface,surfaceNormal)||dot(to-surface,surfaceNormal)<-.008)touched=false;
   }
   if(touched){to=contact+normal*(YOG_LIFT-reach);n=normal;state=YOG_DEPTH;}
  }
  if(state>.5){velocity=0;stateTime=0;p.ext.z=0;p.ext.w=0;p.ext.y=0;}
  else if(length(to)>3||to.y<-2){target[i]=dead;return;}
 }else{
  if(onMesh){n=boundNormal;state=YOG_MESH;}else at=from;
  if(state==YOG_MESH&&!onMesh){p.pos=float4(from,r);p.nrm.w=YOG_AIR;p.vel.xyz=0;p.aux.x=0;p.ext.w=0;target[i]=p;return;}
  p.ext.w=0;
  velocity-=n*dot(velocity,n);
  const float3 gravity=float3(0,-9.81,0);
  float3 downhill=gravity-steady*dot(gravity,steady);downhill-=n*dot(downhill,n);float slope=length(downhill);
  float thick=h*(1+.4*min(crowd,2))*YOG_FLOW_SCALE;
  float stop=YOG_YIELD/(YOG_RHO*max(slope,.05));
  float speed=(smear||thick<=stop)?0:min(YOG_RHO*slope*(thick-stop)*(thick-stop)/(2*YOG_ETA*slotViscosity(owner)),YOG_MAX_CREEP);
  if(hanging)speed*=1-.85*yogHangProgress(p.aux.x,r);
  float3 wanted=slope>1e-4?downhill/slope*speed:0;
  if(weightSum>0){float3 around=velocitySum/weightSum;around-=n*dot(around,n);wanted=lerp(wanted,around,.85*weightSum/(weightSum+.3));}
  velocity=smear?0:lerp(wanted,velocity,exp(-dt/.22));
  float3 cohesion=0;
  if(weightSum>0&&!smear){
   float3 toward=centerSum/weightSum;toward-=n*dot(toward,n);
   float3 along=slope>1e-4?downhill/slope:0;
   float3 lengthwise=along*dot(toward,along);
   cohesion=(toward-lengthwise)*1.6+lengthwise*.9;
  }
  float3 settle=(push-n*dot(push,n)+cohesion)*dt+(gathered-n*dot(gathered,n));
  float settleLength=length(settle),settleLimit=.012*dt;
  if(settleLength>settleLimit)settle*=settleLimit/settleLength;
  float3 move=velocity*dt+settle;
  if(smear)move=0;
  bool blocked=false;float3 before=at;
  if(onMesh){
   float4 savedA=bodyBindings[i*3],savedB=bodyBindings[i*3+1];
   to=at;
   if(dot(move,move)<1e-12)bodyBindings[i*3+1]=float4(savedB.xy,now,savedB.w);
   else{
    float3 contact,normal,edge;bool boundary;
    if(bodyFollowDetailed(i,mesh,at+move,YOG_LIFT,contact,normal,boundary,edge,true)){to=contact;n=normal;blocked=dot(edge,edge)>.5;}
    else{
     bodyBindings[i*3]=savedA;bodyBindings[i*3+1]=float4(savedB.xy,now,savedB.w);velocity=0;blocked=true;
    }
   }
  }else{
   before=from;to=from+move;
   float3 surface,normal;bool found=screenSurface(owner,to,surface,normal);
   float gap=found?dot(to-surface,normal):0;
   if(found&&dot(n,normal)>.35&&gap>-.05&&gap<.03&&length(to-surface)<.06){
    to+=normal*(YOG_LIFT-gap);n=normal;
   }
   else if(found&&gap>=.03){
    state=YOG_AIR;stateTime=0;velocity=0;p.aux.x=0;to=from;
   }else to=from;
  }
  if(state>.5){
   if(!smear&&!hanging)p.ext.y+=length(to-before);
   if(!smear&&!hanging&&!followed&&p.ext.y>.45*a){
    float part=clamp(volume-yogVolume(YOG_FLOW_MIN*base),0,volume*.04);
    YParcel trace;
    trace.pos=float4(before,yogRadius(part));trace.vel=float4(0,0,0,p.vel.w);
    trace.nrm=float4(n,YOG_DEPTH);trace.aux=float4(0,now,YOG_KIND_SMEAR,1);
    trace.ext=float4(min(now+YOG_SMEAR_LIFE,p.ext.x),a,0,0);trace.sway=0;trace.bend=float4(0,0,0,slotGeneration(owner));
    if(part>0&&ask(trace)){p.ext.y=0;volume-=part;r=yogRadius(volume);}
   }
   float weight=saturate(-steady.y);bool under=weight>.2||blocked;
   if(blocked)weight=max(weight,.6);
   float ell=p.aux.x;
   bool begins=ell<=0&&under&&!smear&&!hangBlocked&&r>=YOG_DRIP_RADIUS*base&&stateTime>.4&&length(velocity)<.006;
   if(ell>0||begins){
    if(begins)p.ext.y=floor(yogRandom(i,now,21)*YOG_HANG_SHAPES)+.5;
    float full=yogHangLength(r);
    if(yields)ell=max(ell-.12*dt,0);
    else if(!under)ell=max(ell-.04*dt,0);
    else ell+=full/(YOG_HANG_TIME*sqrt(slotViscosity(owner)))*2.2*pow(max(yogHangProgress(ell,r),.05),1.2)*max(weight,.35)*dt;
    float2 swing=p.sway.xy,swingSpeed=p.sway.zw,strandBend=p.bend.xy;
    if(ell>0){
     float2 carried=(to-from).xz;
     if(dot(carried,carried)>.0025)carried=0;
     float stiffness=9.81/max(ell,.012),damping=.85*sqrt(stiffness);
     swingSpeed+=(-stiffness*swing-damping*(swingSpeed-carried/dt))*dt;
     swing+=swingSpeed*dt-carried;
     float limit=.8*ell,off=length(swing);if(off>limit)swing*=limit/off;
     float2 middleSpeed=.5*(carried/dt+swingSpeed);
     float2 blown=-.07*middleSpeed;
     float bowLimit=.3*ell,bow=length(blown);if(bow>bowLimit)blown*=bowLimit/bow;
     strandBend=lerp(blown,strandBend,exp(-dt/.1));
    }else{swing=0;swingSpeed=0;strandBend=0;}
    float3 anchor=to-n*YOG_LIFT,tip=anchor+yogHangVector(ell,swing);
    float share=yogBulbShare(yogHangProgress(ell,r));
    if(ell>.012){
     float bulb=yogRadius(volume*share);
     [unroll]for(uint k=0;k<2&&ell>0;k++){
      float along=k==0?1:.55,size=k==0?bulb:.5*bulb;
      float3 probe=lerp(anchor,tip,along),surface,surfaceNormal;
      if(!screenSurface(owner,probe,surface,surfaceNormal))continue;
      float gap=dot(probe-surface,surfaceNormal);
      if(gap>=.8*size||gap<-.03||length(surface-anchor)<1.6*bulb+a)continue;
      float part=volume*share*along;
      YParcel landed;
      landed.pos=float4(surface+surfaceNormal*YOG_LIFT,yogRadius(part));landed.vel=float4(0,0,0,p.vel.w);
      landed.nrm=float4(surfaceNormal,YOG_DEPTH);landed.aux=float4(0,now,YOG_KIND_DROP,0);
      landed.ext=float4(p.ext.x,0,0,0);landed.sway=0;landed.bend=float4(0,0,0,slotGeneration(owner));
      if(!ask(landed))continue;
      volume-=part;r=yogRadius(volume);p.ext.z=ell*along*.7;ell=0;swing=0;swingSpeed=0;
     }
    }
    if(ell>=full){
     float part=volume*YOG_BULB_SHARE;
     YParcel drop;
     drop.pos=float4(tip,yogRadius(part));
     drop.vel=float4(swingSpeed.x,-full/YOG_HANG_TIME*2.2,swingSpeed.y,p.vel.w);
     drop.nrm=float4(0,0,0,YOG_AIR);drop.aux=float4(0,now,YOG_KIND_DROP,0);
     drop.ext=float4(p.ext.x,0,-ell*.4,0);drop.sway=0;drop.bend=float4(0,0,0,slotGeneration(owner));
     if(ask(drop)){
     volume-=part;r=yogRadius(volume);p.ext.z=ell*.5;ell=0;swing=0;swingSpeed=0;}
    }
    p.aux.x=ell;p.sway=float4(swing,swingSpeed);p.bend=float4(ell>0?strandBend:float2(0,0),0,slotGeneration(owner));
    if(ell<=0)p.ext.y=0;
   }
  }
 }
 p.ext.z*=exp(-dt/.75);if(abs(p.ext.z)<.0005)p.ext.z=0;
 if(smear)p.ext.z=covered;
 if(!all(isfinite(to))||!all(isfinite(velocity))){target[i]=dead;return;}
 if(state>.5&&attached&&dot(steady,steady)>.5){
  float3 aim=n;
  if(weightSum>0)aim=normalize(n+normalSum/weightSum*.8);
  float apart=dot(steady,n);
  steady=normalize(lerp(aim,steady,exp(-dt/lerp(.2,.07,saturate((.82-apart)/.6))))+n*1e-4);
  n=steady;
 }
 p.pos=float4(to,r);p.vel.xyz=velocity;p.nrm=float4(n,state);p.aux.w=stateTime;
 target[i]=p;
}
