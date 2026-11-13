#ifndef BODY_FACES
#include "../bodymesh/Dimensions.hlsl"
#endif
Buffer<uint> bodyModelMap:register(t111);
StructuredBuffer<float4> bodyAtlas:register(t110);
#ifndef BODY_BINDING_REGISTER
#define BODY_BINDING_REGISTER u1
#endif
RWStructuredBuffer<float4> bodyBindings:register(BODY_BINDING_REGISTER);
bool bodyFace(uint mesh,uint face,out float3 a,out float3 b,out float3 c,out float3 n){
 uint base=mesh*BODY_STRIDE; a=b=c=n=0;
 if(face>=(uint)bodyAtlas[base+1].y)return false;
 float4 av=bodyAtlas[base+2+face*4],bv=bodyAtlas[base+3+face*4],cv=bodyAtlas[base+4+face*4];
 a=av.xyz;b=bv.xyz;c=cv.xyz;n=cross(b-a,c-a);
 float len=length(n);if(min(av.w,min(bv.w,cv.w))<.5||len<1e-9||!all(isfinite(n)))return false;
 n/=len;return true;
}
float3 bodyBary(float3 p,float3 a,float3 b,float3 c){
 float3 u=b-a,v=c-a,q=p-a;float aa=dot(u,u),ab=dot(u,v),bb=dot(v,v),det=aa*bb-ab*ab;
 if(det<1e-18)return -1;
 float y=(dot(q,u)*bb-dot(q,v)*ab)/det,z=(dot(q,v)*aa-dot(q,u)*ab)/det;
 return float3(1-y-z,y,z);
}
bool bodyReuseSlot(uint existing,uint owner,uint model,float4 binding,float4 saved,float radius,float3 from,out float3 next,out float3 normal){
 next=from;normal=0;
 float now=IniParams[239].y;
 float4 h=bodyAtlas[existing*BODY_STRIDE],m=bodyAtlas[existing*BODY_STRIDE+1];
 if(m.w!=1||(uint)m.x!=model||saved.w!=m.z||now-h.w<-.02||now-h.w>=.05||length(h.xyz-slotRoot(owner))>=.01)return false;
 float3 a,b,c,n;
 if(!bodyFace(existing,(uint)binding.x-1,a,b,c,n))return false;
 normal=n*(binding.w<0?-1:1);
 next=a*saved.x+b*saved.y+c*(1-saved.x-saved.y)+normal*(radius+.0001);
 return length(next-from)<.5;
}
bool bodyReuse(uint particle,uint owner,float birth,float radius,inout float3 from,inout float3 normal,out uint mesh){
 mesh=8;
 uint count,stride;bodyAtlas.GetDimensions(count,stride);if(count<8*BODY_STRIDE)return false;
 uint profile=(uint)slotAnchor(owner,4).y;
 uint model=bodyModelMap[profile];if(model==0)return false;
 float now=IniParams[239].y;
 float4 binding=bodyBindings[particle*3],saved=bodyBindings[particle*3+1];
 if(slotRebound(owner)||binding.y!=profile||bodyBindings[particle*3+2].x!=slotGeneration(owner)||binding.z!=birth||now-saved.z>.08||now<saved.z)binding=0;
 float3 followedFrom,followedNormal;
 if(binding.x>0){
  uint cached=(uint)abs(binding.w)-1;
  if(cached<8&&bodyReuseSlot(cached,owner,model,binding,saved,radius,from,followedFrom,followedNormal)){
   from=followedFrom;normal=followedNormal;mesh=cached;return true;
  }
  [loop]for(uint existing=0;existing<8;existing++){
   if(existing==cached)continue;
   if(bodyReuseSlot(existing,owner,model,binding,saved,radius,from,followedFrom,followedNormal)){
    from=followedFrom;normal=followedNormal;mesh=existing;
    binding.w=(binding.w<0?-1.0:1.0)*float(existing+1);bodyBindings[particle*3]=binding;return true;
   }
  }
 }
 return false;
}
#ifdef BODY_SEARCH
bool bodyBegin(uint particle,uint owner,float birth,float radius,float3 endPoint,bool swept,inout float3 from,inout float3 normal,out uint mesh){
 mesh=8;
 uint size,stride;bodyAtlas.GetDimensions(size,stride);
 if(size<8*BODY_STRIDE)return false;
 uint profile=(uint)slotAnchor(owner,4).y,model=bodyModelMap[profile];
 if(model==0)return false;
 float now=IniParams[239].y,best=swept?2:.008;
 uint selected=0;float3 weights=0;float side=1;
 [loop]for(uint m=0;m<8;m++){
  uint base=m*BODY_STRIDE;float4 header=bodyAtlas[base],meta=bodyAtlas[base+1];
  if(meta.w<.5||(uint)meta.x!=model||abs(now-header.w)>.05||length(header.xyz-slotRoot(owner))>=.01)continue;
  uint node=1;
  [loop]for(uint visit=0;visit<BODY_LEAVES*2&&node>0;visit++){
   uint bounds=base+2+BODY_FACES*4+node*2;
   float3 lo=bodyAtlas[bounds].xyz,hi=bodyAtlas[bounds+1].xyz;
   float reach=radius+.0081;
   bool overlap=all(lo<=hi)&&all(max(from,endPoint)>=lo-reach)&&all(min(from,endPoint)<=hi+reach);
   if(overlap&&node<BODY_LEAVES){node*=2;continue;}
   if(overlap){
    uint first=(node-BODY_LEAVES)*32,last=min(first+32,(uint)meta.y);
    [loop]for(uint f=first;f<last;f++){
     float3 a,b,c,n;
     if(!bodyFace(m,f,a,b,c,n))continue;
     float sign=dot(n,swept?from-a:normal)>=0?1:-1;n*=sign;
     if(!swept&&dot(n,normal)<.7)continue;
     float gap=dot(from-a,n),error=abs(gap-radius-.0001);float3 at=from;
     if(swept){float finish=dot(endPoint-a,n),travel=gap-finish;
      if(travel<=1e-8||gap<radius-.0005||finish>radius+.0001)continue;
      error=saturate((gap-radius-.0001)/travel);at=lerp(from,endPoint,error);gap=dot(at-a,n);
     }
     float3 bary=bodyBary(at-n*gap,a,b,c);
     if(error<best&&all(bary>=0)){best=error;selected=f+1;weights=bary;side=sign;mesh=m;}
    }
   }
   [loop]while(node>0&&(node&1u)!=0)node>>=1;
   if(node>0)node++;
  }
 }
 bodyBindings[particle*3]=0;
 if(selected==0)return false;
 float3 a,b,c,n;bodyFace(mesh,selected-1,a,b,c,n);
 normal=n*side;from=a*weights.x+b*weights.y+c*weights.z+normal*(radius+.0001);
 bodyBindings[particle*3]=float4(selected,profile,birth,side*(mesh+1));
 bodyBindings[particle*3+1]=float4(weights.xy,now,bodyAtlas[mesh*BODY_STRIDE+1].z);
 bodyBindings[particle*3+2]=float4(slotGeneration(owner),0,0,0);
 return true;
}
#endif
bool bodyFollowDetailed(uint particle,uint mesh,float3 desired,float radius,out float3 contact,out float3 normal,out bool reachedBoundary,out float3 heldEdge,bool holdBalancedEdge){
 heldEdge=0;
 reachedBoundary=false;
 float4 binding=bodyBindings[particle*3];uint face=(uint)binding.x-1;
 contact=desired;normal=0;float3 a,b,c,n,weights;
 uint faceCount=(uint)bodyAtlas[mesh*BODY_STRIDE+1].y;
 if(!bodyFace(mesh,face,a,b,c,n)){bodyBindings[particle*3]=0;return false;}
 n*=binding.w<0?-1:1;
 float4 saved=bodyBindings[particle*3+1];
 float3 start=a*saved.x+b*saved.y+c*(1-saved.x-saved.y);
 float3 remaining=desired-(start+n*(radius+.0001));
 remaining-=n*dot(remaining,n);
 uint previous=faceCount;
 float3 pinnedFloor=-1e9;
 [loop]for(uint step=0;step<faceCount;step++){
  weights=max(bodyBary(start+remaining,a,b,c),pinnedFloor);
  if(min(weights.x,min(weights.y,weights.z))>=-1e-6){
   weights=max(weights,0);weights/=max(dot(weights,1),1e-8);
   contact=a*weights.x+b*weights.y+c*weights.z+n*(radius+.0001);normal=n;
   binding.x=face+1;bodyBindings[particle*3]=binding;
   bodyBindings[particle*3+1]=float4(weights.xy,IniParams[239].y,bodyAtlas[mesh*BODY_STRIDE+1].z);
   return true;
  }
  float3 fromWeights=max(bodyBary(start,a,b,c),0);
  fromWeights/=max(dot(fromWeights,1),1e-8);
  float fraction=1;uint opposite=0;
  [unroll]for(uint k=0;k<3;k++)if(weights[k]<-1e-6){
   float t=fromWeights[k]/max(fromWeights[k]-weights[k],1e-12);
   if(t<fraction){fraction=t;opposite=k;}
  }
  float edge=bodyAtlas[mesh*BODY_STRIDE+2+face*4+3][opposite];
  if(edge<0){reachedBoundary=true;break;}
  if(uint(edge)>=faceCount)break;
  uint next=(uint)edge;
  if(next==previous&&fraction<1e-6)break;
  float3 hitWeights=lerp(fromWeights,weights,fraction);
  hitWeights*=float3(opposite!=0,opposite!=1,opposite!=2);hitWeights=max(hitWeights,0);hitWeights/=max(dot(hitWeights,1),1e-8);
  start=a*hitWeights.x+b*hitWeights.y+c*hitWeights.z;
  remaining*=1-fraction;
  float3 edgeA=opposite==0?b:(opposite==1?c:a);
  float3 edgeB=opposite==0?c:(opposite==1?a:b);
  float3 nextNormal,nextA,nextB,nextC;
  if(!bodyFace(mesh,next,nextA,nextB,nextC,nextNormal))break;
  const float seamTolerance2=2.5e-9;
  bool edgeMatches=(dot(edgeA-nextB,edgeA-nextB)<=seamTolerance2&&dot(edgeB-nextA,edgeB-nextA)<=seamTolerance2)
   ||(dot(edgeA-nextC,edgeA-nextC)<=seamTolerance2&&dot(edgeB-nextB,edgeB-nextB)<=seamTolerance2)
   ||(dot(edgeA-nextA,edgeA-nextA)<=seamTolerance2&&dot(edgeB-nextC,edgeB-nextC)<=seamTolerance2);
  if(!edgeMatches)break;
  nextNormal*=binding.w<0?-1:1;
  [branch]if(holdBalancedEdge){
   const float3 down=float3(0,-1,0);
   float3 edgeDirection=normalize(edgeB-edgeA);
   float3 intoNext=cross(edgeDirection,nextNormal),outOfFace=cross(edgeDirection,n);
   if(dot(intoNext,(nextA+nextB+nextC)/3-edgeA)<0)intoNext=-intoNext;
   if(dot(outOfFace,(a+b+c)/3-edgeA)>0)outOfFace=-outOfFace;
   if(dot(down,outOfFace)>.02&&dot(down,intoNext)<-.02){
    heldEdge=edgeDirection;
    remaining=edgeDirection*dot(remaining,edgeDirection);
    pinnedFloor=float3(opposite==0?0:-1e9,opposite==1?0:-1e9,opposite==2?0:-1e9);
    continue;
   }
  }
  a=nextA;b=nextB;c=nextC;pinnedFloor=-1e9;
  float cosine=clamp(dot(n,nextNormal),-1,1);
  float3 axis=cross(n,nextNormal);float sine=length(axis);
  if(cosine<-.9999)break;
  if(sine>1e-6){axis/=sine;remaining=remaining*cosine+cross(axis,remaining)*sine+axis*dot(axis,remaining)*(1-cosine);}
  remaining-=nextNormal*dot(remaining,nextNormal);
  previous=face;face=next;n=nextNormal;
 }
 bodyBindings[particle*3]=0;return false;
}
bool bodyFollowDetailed(uint particle,uint mesh,float3 desired,float radius,out float3 contact,out float3 normal,out bool reachedBoundary){
 float3 heldEdge;return bodyFollowDetailed(particle,mesh,desired,radius,contact,normal,reachedBoundary,heldEdge,false);
}
bool bodyFollow(uint particle,uint mesh,float3 desired,float radius,out float3 contact,out float3 normal){
 bool reachedBoundary;return bodyFollowDetailed(particle,mesh,desired,radius,contact,normal,reachedBoundary);
}
