#include "SSFRCommon.hlsl"
#include "Slots.hlsl"
#include "Yogurt.hlsl"
StructuredBuffer<YParcel> parcels:register(t100);
Texture2D<float> fluidDepth:register(t104);
struct Varying{
 float4 position:SV_Position;
 float3 ray:TEXCOORD0;
 nointerpolation float4 s0:TEXCOORD1;
 nointerpolation float4 s1:TEXCOORD2;
 nointerpolation float4 s2:TEXCOORD3;
 nointerpolation float4 s3:TEXCOORD4;
 nointerpolation float4 axis:TEXCOORD5;
 nointerpolation float4 color:TEXCOORD6;
 nointerpolation float bias:TEXCOORD7;
 nointerpolation float2 look:TEXCOORD8;
 nointerpolation float4 shapeA:TEXCOORD9;
 nointerpolation float4 shapeB:TEXCOORD10;
};
#ifdef VERTEX_SHADER
Varying main(uint id:SV_VertexID){
 Varying o=(Varying)0;o.position=float4(2,2,2,1);
 uint index=id/12,quad=(id/6)%2,corner=id%6;
 if(index>=YOG_COUNT)return o;
 YParcel p=parcels[index];
 if(p.pos.w<=0)return o;
 uint owner=splashSlot(p.vel.w);
 if(!slotLive(owner)||(uint)p.bend.w!=slotGeneration(owner))return o;
 float now=IniParams[239].y,kind=p.aux.z;
 float fade=smoothstep(p.ext.x-YOG_FADE,p.ext.x,now);
 float scale=kind==YOG_KIND_SMEAR?1:1-.35*fade;
 if(kind==YOG_KIND_EMITTED)scale*=lerp(.4,1,smoothstep(0,.35,now-p.aux.y));
 float r=p.pos.w*scale;
 if(r<=1e-5)return o;
 float3 eye=eyePosition(),root=slotRoot(owner),world=p.pos.xyz+root;
 bool attached=p.nrm.w>.5;
 float3 n=attached?normalize(p.nrm.xyz):float3(0,1,0);
 float volume=yogVolume(r),ell=attached?p.aux.x:0,progress=ell>0?yogHangProgress(ell,p.pos.w):0;
 float bulbVolume=ell>0?volume*yogBulbShare(progress):0;
 float a=r,h=r;float3 center=world,axis=float3(0,1,0);
 if(attached){
  yogParcelDome(index,YOG_BASE_RADIUS*slotWidth(owner),p.pos,p.nrm,p.aux,p.ext,volume-bulbVolume,a,h);
  center=world-n*YOG_LIFT;axis=n;
 }else{
  float speed=length(p.vel.xyz),stretch=saturate(speed/2);
  if(speed>1e-4)axis=p.vel.xyz/speed;
  h=r*(1+.3*stretch);a=r/sqrt(1+.3*stretch);
 }
 float4 tint=float4(IniParams[251].rgb,1);
 float4 look=slots[owner*SLOT_ROWS+SLOT_LOOK];
 if(((uint)round(look.y)&1u)!=0)tint.rgb=saturate(slots[owner*SLOT_ROWS+SLOT_TINT].rgb);
 float opacity=2-slotTransparency(owner);
 float chance=yogRandom(index,p.aux.y,7);
 float clearest=(chance<.5||kind==YOG_KIND_SMEAR)?1:lerp(.1,.45,(chance-.5)/.5);
 float3 across=normalize(float3(yogRandom(index,p.aux.y,8),yogRandom(index,p.aux.y,9),yogRandom(index,p.aux.y,10))-.5+1e-4);
 o.color=float4(tint.rgb,1);o.look=float2(opacity,1-fade);
 float3 boundCenter;float boundRadius;
 if(quad==0){
  o.s0=float4(center-eye,a);o.axis=float4(axis,h);
  o.s1=float4(attached&&kind==YOG_KIND_SMEAR?1-saturate(p.ext.z):1,0,0,0);
  o.s2=float4(across,clearest);
  o.bias=attached?max(.002,.6*h+.002):.0007;
  if(attached&&kind==YOG_KIND_SMEAR)o.bias=-o.bias;
  boundCenter=center;boundRadius=YOG_REACH*max(a,h);
 }else{
  const float3 down=float3(0,-1,0);
  float3 p0,p1,p2,p3;float r0,r1,r2,r3;float segments;
  if(ell>0){
   uint variant=(uint)p.ext.y%YOG_HANG_SHAPES;
   const float neckScale[4]={1,1.4,.85,.78},bulbScale[4]={1,.72,.9,1.08},taperScale[4]={1,.75,.9,1.7},lumpSize[4]={.28,.12,.6,.18};
   float bulb=yogRadius(bulbVolume)*bulbScale[variant];
   float neck=max(r*lerp(.6,.34,progress)*neckScale[variant]*(1-.75*smoothstep(.975,1,progress)),.0005);
   p0=center;p3=center+yogHangVector(ell,p.sway.xy);p1=p3;p2=p3;
   r0=min(a*.8,r)*(1-.25*progress);r1=bulb;
   r2=neck;r3=saturate(2.6*taperScale[variant]*bulb/max(ell,1e-4));
   p2=float3(yogRandom(index,p.aux.y,11),lerp(1.4,2.8,yogRandom(index,p.aux.y,12)),lumpSize[variant]);
   if(variant==2)p2.xy=float2(.25+.5*yogRandom(index,p.aux.y,11),1);
   segments=3;
  }else if(attached&&kind!=YOG_KIND_SMEAR&&p.ext.z>0){
   p0=center;p1=center+down*p.ext.z;p2=p1;p3=p1;
   r0=min(a*.6,r*.8);r1=max(.3*r*saturate(p.ext.z/.02),.0005);r2=r1;r3=r1;segments=1;
  }else if(!attached&&p.ext.z<0){
   p0=center;p1=center-down*(-p.ext.z);p2=p1;p3=p1;
   r0=r*.75;r1=max(.25*r*saturate(-p.ext.z/.02),.0005);r2=r1;r3=r1;segments=1;
  }else if(attached&&kind!=YOG_KIND_SMEAR){
   float3 uphill=float3(0,1,0)-n*n.y;float steep=length(uphill);
   if(steep<.3)return o;
   p0=center;p1=center+uphill/steep*(a*lerp(.9,1.5,yogRandom(index,p.aux.y,15))*saturate((steep-.3)/.4));p2=p1;p3=p1;
   r0=min(1.1*h,.62*a);r1=.6*r0;r2=r1;r3=r1;segments=1;
  }else return o;
  o.s0=float4(p0-eye,r0);o.s1=float4(p1-eye,r1);o.s2=float4(p2-eye,r2);o.s3=float4(p3-eye,r3);
  if(segments>2){
   float3 control=(p0+p3)*.5+2*float3(p.bend.x,0,p.bend.y);
   float3 q1=p0*(4.0/9)+control*(4.0/9)+p3*(1.0/9),q2=p0*(1.0/9)+control*(4.0/9)+p3*(4.0/9);
   o.shapeA=float4(r2,r3,p2.x,p2.y);o.shapeB=float4(clearest,lerp(.3,.75,yogRandom(index,p.aux.y,13)),p2.z,0);
   o.s1=float4(q1-eye,r1);o.s2=float4(q2-eye,0);o.s3=float4(p3-eye,0);
  }
  o.axis=float4(0,0,0,-segments);
  o.bias=attached?.004:.0007;
  boundCenter=(p0+p3)*.5;boundRadius=length(p3-p0)*.5+length(p.bend.xy)+YOG_REACH*1.35*max(r0,r1);
 }
 float4 middle=project(boundCenter);
 if(middle.w<.05+boundRadius)return o;
 const float2 corners[6]={float2(-1,-1),float2(1,-1),float2(-1,1),float2(-1,1),float2(1,-1),float2(1,1)};
 float3 right=normalize(camera[4].xyz),up=normalize(camera[5].xyz);
 float cover=boundRadius*1.02/sqrt(max(1-boundRadius*boundRadius/(middle.w*middle.w),.25));
 float3 cornerPoint=boundCenter+(right*corners[corner].x+up*corners[corner].y)*cover;
 o.position=project(cornerPoint);o.ray=cornerPoint-eye;
 return o;
}
#endif
#ifdef PIXEL_SHADER
struct Targets{float2 field:SV_Target0;float4 shape:SV_Target1;float3 color:SV_Target2;};
void segmentPass(float3 dir,float4 a,float4 b,inout float best,inout float along,inout float radius,inout float3 radial){
 float3 e=b.xyz-a.xyz;float ee=dot(e,e),de=dot(dir,e),da=dot(dir,a.xyz),ea=dot(e,a.xyz);
 float denominator=ee-de*de;
 float s=denominator>1e-12?saturate((de*da-ea)/denominator):0;
 float3 nearest=a.xyz+e*s;float t=dot(dir,nearest);
 float3 offset=dir*t-nearest;float span=length(offset),size=lerp(a.w,b.w,s);
 float m=span/max(size,1e-6);
 if(m<best){best=m;along=t;radius=size;radial=span>1e-7?offset/span:float3(0,0,0);}
}
float pendantRadius(float s,float root,float bulb,float4 shape,float lumpSize){
 float neck=shape.x,taper=clamp(shape.y,.2,.85);
 float flare=1-saturate(s/.4);
 float rope=smoothstep(0,.25,s)*(1-smoothstep(1-taper-.1,1-taper+.15,s));
 float lumps=1+lumpSize*rope*sin(6.2831853*(s*shape.w+shape.z));
 float swell=smoothstep(1-taper,1,s);
 return neck*lumps+(root-neck)*flare*flare+(bulb-neck)*swell*swell*(3-2*swell);
}
Targets main(Varying i){
 float3 dir=normalize(i.ray);
 float m,t,chord;float3 normal;
 float dense=1;
 if(i.axis.w>=0){
  float3 n=i.axis.xyz;float a=i.s0.w,h=i.axis.w;
  float3 q0=-i.s0.xyz;q0=(q0-n*dot(q0,n))/a+n*(dot(q0,n)/h);
  float3 qd=(dir-n*dot(dir,n))/a+n*(dot(dir,n)/h);
  float dd=dot(qd,qd),tq=-dot(q0,qd)/dd;
  float3 qc=q0+qd*tq;float m2=dot(qc,qc);
  m=sqrt(m2);t=tq;chord=0;
  if(m2<1){float s=sqrt(1-m2)/sqrt(dd);t=tq-s;chord=2*s;qc=q0+qd*t;}
  float3 gradient=(qc-n*dot(qc,n))/a+n*(dot(qc,n)/h);
  normal=dot(gradient,gradient)>1e-12?normalize(gradient):-dir;
  dense=lerp(i.s2.w,1,smoothstep(-.55,.75,dot(qc,i.s2.xyz)));
 }else{
  float best=1e9,radius=0;float3 radial=0;t=0;
  if(i.axis.w<-1.5){
   const float3 points[4]={i.s0.xyz,i.s1.xyz,i.s2.xyz,i.s3.xyz};
   float where=0;
   [unroll]for(uint k=0;k<3;k++){
    float3 from=points[k],e=points[k+1]-from;float ee=dot(e,e),de=dot(dir,e),da=dot(dir,from),ea=dot(e,from);
    float denominator=ee-de*de;
    float local=denominator>1e-12?saturate((de*da-ea)/denominator):0;
    float3 nearest=from+e*local;float along=dot(dir,nearest);
    float3 offset=dir*along-nearest;float span=length(offset);
    float s=(k+local)/3,size=pendantRadius(s,i.s0.w,i.s1.w,i.shapeA,i.shapeB.z);
    float nearness=span/max(size,1e-6);
    if(nearness<best){best=nearness;t=along;radius=size;where=s;radial=span>1e-7?offset/span:float3(0,0,0);}
   }
   dense=lerp(i.shapeB.x,1,smoothstep(.1,.42,abs(where-i.shapeB.y)));
  }else segmentPass(dir,i.s0,i.s1,best,t,radius,radial);
  m=best;chord=0;normal=radial;
  if(m<1){float s=sqrt(1-m*m);t-=radius*s;chord=2*radius*s;normal=normalize(radial*m-dir*s);}
 }
 clip(YOG_REACH-m);
 clip(t-.02);
 float weight=1-m*m/(YOG_REACH*YOG_REACH);weight*=weight;
 if(i.axis.w>=0)weight*=i.s1.x;
 float3 world=eyePosition()+dir*t;
 float4 projected=project(world);
 float raw=fluidDepth.Load(int3(int2(i.position.xy),0));
 if(!noFluid(raw)&&fluidDepthValue(raw)<projected.w-.003)clip(-1);
 if(IniParams[237].y>.5){
  float2 uv=screenUV(projected);
  float allowance=abs(i.bias);
  if(i.axis.w>=0&&allowance>.0015)allowance=lerp(i.s0.w,.6*i.axis.w,abs(dot(i.axis.xyz,dir)))+.002;
  float behind=behindScene(uv,projected);
  float gap=behind-allowance-sceneSlopeAllowanceAt(uv);
  weight*=1-smoothstep(0,.004,gap);
  clip(weight-1e-5);
 }
 Targets o;
 o.field=float2(projected.w,1)*weight;
 float baseline=lerp(.6,1,dense);
 float code=(i.look.x<=1?i.look.x*baseline:lerp(baseline,2,i.look.x-1))*i.look.y;
 o.shape=float4(dot(normal,normalize(camera[4].xyz))*weight,dot(normal,normalize(camera[5].xyz))*weight,chord,code*weight);
 o.color=i.color.rgb*weight;
 return o;
}
#endif
