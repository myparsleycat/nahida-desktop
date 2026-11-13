#ifndef YOGURT_COMMON
#define YOGURT_COMMON
static const uint YOG_COUNT=2048;
static const uint YOG_REQUESTS=128;
struct YParcel{float4 pos;float4 vel;float4 nrm;float4 aux;float4 ext;float4 sway;float4 bend;};
static const float YOG_AIR=0,YOG_DEPTH=1,YOG_MESH=2;
static const float YOG_KIND_EMITTED=0,YOG_KIND_SMEAR=1,YOG_KIND_DROP=2;
static const float YOG_LIFT=.0002;
static const float YOG_RHO=1030,YOG_YIELD=25,YOG_ETA=10;
static const float YOG_MAX_CREEP=.022;
static const float YOG_BASE_RADIUS=.00325;
static const float YOG_FLOW_SCALE=2;
static const float YOG_STREAM_RADIUS=1.0,YOG_MAX_RADIUS=1.12,YOG_DRIP_RADIUS=.97,YOG_DEAD_RADIUS=.14;
static const float YOG_EMIT_RATE=1.4;
static const float YOG_LIFE=42,YOG_SMEAR_LIFE=15,YOG_FADE=6;
static const float YOG_HANG_TIME=4.6;
static const float YOG_BULB_SHARE=.36;
static const uint YOG_HANG_SHAPES=4;
static const float YOG_REACH=2.4;
static const float YOG_ISO=.5625;
float yogVolume(float r){return 4.18879*r*r*r;}
float yogRadius(float v){return pow(max(v,0)/4.18879,1.0/3);}
float yogHangLength(float r){return clamp(9*r,.022,.1);}
float yogHangProgress(float ell,float r){return pow(saturate(ell/yogHangLength(r)),1/2.2);}
float yogBulbShare(float u){return lerp(.08,YOG_BULB_SHARE,smoothstep(0,1,u));}
float3 yogHangVector(float ell,float2 sway){float3 v=float3(sway.x,-ell,sway.y);return v*(ell/max(length(v),1e-6));}
float yogAspect(float kind,float stateTime,float normalY){
 if(kind==YOG_KIND_SMEAR)return .3;
 float rest=lerp(.5,.22,smoothstep(.6,.95,normalY));
 rest=lerp(rest,.9,saturate(-normalY*1.5));
 return lerp(rest,.95,exp(-max(stateTime,0)/(kind==YOG_KIND_DROP?.6:.22)));
}
void yogDome(float v,float aspect,out float a,out float h){a=pow(max(v,0)/(2.0944*aspect),1.0/3);h=aspect*a;}
float yogHash(uint x){x^=x>>16;x*=0x7feb352du;x^=x>>15;x*=0x846ca68bu;x^=x>>16;return float(x>>8)*(1.0/16777216.0);}
float yogRandom(uint index,float birth,uint channel){return yogHash(asuint(birth)^(index*747796405u+2891336453u)^(channel*0x9e3779b9u));}
static const float YOG_FLOW_MIN=.6;
void yogParcelDome(uint index,float base,float4 pos,float4 nrm,float4 aux,float4 ext,float volume,out float a,out float h){
 if(aux.z==YOG_KIND_SMEAR&&ext.y>0){a=ext.y;h=min(.12*a,.0003);return;}
 float worn=saturate((yogRadius(volume)/max(base,1e-5)-YOG_FLOW_MIN)/(1-YOG_FLOW_MIN));
 yogDome(volume,yogAspect(aux.z,aux.w,nrm.y)*lerp(.78,1.28,yogRandom(index,aux.y,5))*lerp(.42,1,worn),a,h);
}
#ifdef LIQUID_SLOTS
float3 yogEmitterNormal(uint s){
 float4 av=slotAnchor(s,6),bv=slotAnchor(s,7),cv=slotAnchor(s,8);
 if(min(av.w,min(bv.w,cv.w))<.5)return 0;
 float3 normal=cross(bv.xyz-av.xyz,cv.xyz-av.xyz);float len=length(normal);
 if(len<1e-9||!all(isfinite(normal)))return 0;
 normal/=len;
 float along=dot(normal,-slotAnchor(s,1).xyz);
 if(abs(along)>.15)return along<0?-normal:normal;
 float3 center=(av.xyz+bv.xyz+cv.xyz)/3-slotRoot(s);
 return dot(normal.xz,center.xz)<0?-normal:normal;
}
#endif
#endif
