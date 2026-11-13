#include "../bodymesh/Dimensions.hlsl"
StructuredBuffer<float4> collisionBodyAtlas:register(t114);
Buffer<uint> collisionBodyModels:register(t115);
float3 bodyClosestPoint(float3 p,float3 a,float3 b,float3 c){
 float3 u=b-a,v=c-a,q=p-a;
 float aa=dot(u,u),ab=dot(u,v),bb=dot(v,v),det=aa*bb-ab*ab;
 if(det<=1e-18)return a;
 float y=(dot(q,u)*bb-dot(q,v)*ab)/det,z=(dot(q,v)*aa-dot(q,u)*ab)/det;
 if(y>=0&&z>=0&&y+z<=1)return a+u*y+v*z;
 float3 best=a;float distance=1e30;float3 vertices[3]={a,b,c};
 [unroll]for(uint k=0;k<3;k++){
  float3 edge=vertices[(k+1)%3]-vertices[k];
  float3 at=vertices[k]+edge*saturate(dot(p-vertices[k],edge)/max(dot(edge,edge),1e-18));
  float d=dot(at-p,at-p);if(d<distance){best=at;distance=d;}
 }
 return best;
}
bool bodySurfaceContact(uint owner,float3 from,float3 to,float radius,bool swept,out float3 contactPoint,out float3 normal){
 contactPoint=to;normal=float3(0,1,0);
 uint size,stride;collisionBodyAtlas.GetDimensions(size,stride);
 if(size<8*BODY_STRIDE)return false;
 uint model=collisionBodyModels[(uint)slotAnchor(owner,4).y];if(model==0)return false;
 float now=IniParams[239].y,best=swept?2:.04;
 bool found=false;
 [loop]for(uint mesh=0;mesh<8;mesh++){
  uint base=mesh*BODY_STRIDE;float4 head=collisionBodyAtlas[base],meta=collisionBodyAtlas[base+1];
  if(meta.w<.5||(uint)meta.x!=model||abs(now-head.w)>.05||length(head.xyz-slotRoot(owner))>=.01)continue;
  uint node=1;
  [loop]for(uint visit=0;visit<BODY_LEAVES*2&&node>0;visit++){
   uint bounds=base+2+BODY_FACES*4+node*2;
   float3 lo=collisionBodyAtlas[bounds].xyz,hi=collisionBodyAtlas[bounds+1].xyz;
   float reach=swept?radius:.04;
   bool overlap=all(lo<=hi)&&all(max(from,to)>=lo-reach)&&all(min(from,to)<=hi+reach);
   if(overlap&&node<BODY_LEAVES){node*=2;continue;}
   if(overlap){
    uint first=(node-BODY_LEAVES)*32,last=min(first+32,(uint)meta.y);
    [loop]for(uint face=first;face<last;face++){
     uint at=base+2+face*4;
     float4 av=collisionBodyAtlas[at],bv=collisionBodyAtlas[at+1],cv=collisionBodyAtlas[at+2];
     if(min(av.w,min(bv.w,cv.w))<.5)continue;
     float3 a=av.xyz,b=bv.xyz,c=cv.xyz,n=cross(b-a,c-a);float lengthN=length(n);
     if(lengthN<1e-9||!all(isfinite(n)))continue;n/=lengthN;
     float start=dot(from-a,n);if(start<0){n=-n;start=-start;}
     float score;float3 sample;
     if(swept){
      float end=dot(to-a,n),travel=start-end;
      if(travel<=1e-8||start<radius-.0005||end>radius)continue;
      score=saturate((start-radius)/travel);sample=lerp(from,to,score);
     }else{sample=to;score=length(sample-bodyClosestPoint(sample,a,b,c));}
     if(score>=best)continue;
     float3 support=bodyClosestPoint(sample,a,b,c);
     if(swept&&length(sample-support)>radius+.0001)continue;
     best=score;contactPoint=swept?sample+n*.0001:support;normal=n;found=true;
    }
   }
   [loop]while(node>0&&(node&1u)!=0)node>>=1;
   if(node>0)node++;
  }
 }
 return found;
}
