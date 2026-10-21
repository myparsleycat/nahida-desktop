// Ray intervals through the tube FluidStream.hlsl draws along a stream's
// links: each link is a Hermite curve between two particles, cut into at
// most TUBE_PIECES cone frusta, and each frustum ends on a plane across the
// curve's own tangent there. Consecutive frusta, also those of the next
// link, which shares the tangent at their particle, then meet on one plane
// and neither overlap nor leave a gap. Only the open ends of a chain get a
// round cap, and the union of the intervals counts what a cap shares with
// its frustum once. Nothing here reads a resource.
#ifndef LIQUID_STREAM_TUBE
#define LIQUID_STREAM_TUBE
static const uint TUBE_PIECES=4,TUBE_SPANS=TUBE_PIECES+2;
static const float2 NO_SPAN=float2(1e30,-1e30);
float3 hermite(float3 p0,float3 t0,float3 p1,float3 t1,float s){
 float s2=s*s,s3=s2*s;
 return (2*s3-3*s2+1)*p0+(s3-2*s2+s)*t0+(-2*s3+3*s2)*p1+(s3-s2)*t1;
}
float3 hermiteTangent(float3 p0,float3 t0,float3 p1,float3 t1,float s){
 float s2=s*s;
 return (6*s2-6*s)*p0+(3*s2-4*s+1)*t0+(-6*s2+6*s)*p1+(3*s2-2*s)*t1;
}
// A straight link takes one frustum, a bending one TUBE_PIECES.
uint tubePieces(float3 p0,float3 t0,float3 p1,float3 t1){
 float3 chord=p1-p0;float len=length(chord);
 if(len<1e-6)return 1;
 float3 u=chord/len;
 float bend=min(dot(normalize(t0+u*1e-6),u),dot(normalize(t1+u*1e-6),u));
 return bend>.995?1:TUBE_PIECES;
}
// Keeps the part of span where f0+t*f1 >= 0, or > 0 when open; false once
// nothing is left. A ray lying in the plane two frusta share belongs to one.
bool keepHalfSpace(inout float2 span,float f0,float f1,bool open){
 if(abs(f1)<1e-9)return open?f0>0:f0>=0;
 float t=-f0/f1;
 if(f1>0)span.x=max(span.x,t);else span.y=min(span.y,t);
 return span.x<span.y;
}
float coneValue(float a,float b,float c,float t){return (a*t+2*b)*t+c;}
// The span of the ray o+t*d inside the frustum from a, radius ra, to b,
// radius rb, between the planes through a and b across na and nb, which
// point along the curve.
float2 frustumSpanNear(float3 o,float3 d,float3 a,float ra,float3 na,float3 b,float rb,float3 nb){
 float3 axis=b-a;float len=length(axis);
 if(len<1e-7)return NO_SPAN;
 float3 u=axis/len;
 // A plane tipped more than the curve can bend stands across the chord.
 if(dot(na,u)<.2)na=u;
 if(dot(nb,u)<.2)nb=u;
 float2 span=float2(-1e30,1e30);
 if(!keepHalfSpace(span,dot(o-a,na),dot(d,na),false)||!keepHalfSpace(span,dot(b-o,nb),-dot(d,nb),true))return NO_SPAN;

 // Inside the cone where (a t^2 + 2 b t + c) <= 0.
 float k=(rb-ra)/len;
 float3 w=o-a;
 float s0=dot(w,u),ds=dot(d,u);
 float3 q0=w-u*s0,q1=d-u*ds;
 float r0=ra+k*s0,dr=k*ds;
 float qa=dot(q1,q1)-dr*dr,qb=dot(q0,q1)-r0*dr,qc=dot(q0,q0)-r0*r0;
 float disc=qb*qb-qa*qc;
 if(qa>1e-9){
  if(disc<0)return NO_SPAN;
  float root=sqrt(disc);
  return float2(max(span.x,(-qb-root)/qa),min(span.y,(-qb+root)/qa));
 }
 // A ray along the axis: inside from whichever end of the slab is inside to
 // where it crosses the wall.
 if(span.x<-1e29||span.y>1e29)return NO_SPAN;
 bool first=coneValue(qa,qb,qc,span.x)<=0,last=coneValue(qa,qb,qc,span.y)<=0;
 if(first==last)return first?span:NO_SPAN;
 float crossing=-qc/(2*qb);
 if(abs(qa)>1e-12&&disc>=0){
  float root=sqrt(disc),t1=(-qb-root)/qa,t2=(-qb+root)/qa;
  crossing=t1>=span.x&&t1<=span.y?t1:t2;
 }
 return first?float2(span.x,crossing):float2(crossing,span.y);
}
// The quadratics lose the few millimeters a tube spans to cancellation when
// the ray starts meters away, so each is solved from the ray's point nearest
// the shape.
float2 frustumSpan(float3 o,float3 d,float3 a,float ra,float3 na,float3 b,float rb,float3 nb){
 float shift=dot(a-o,d);
 float2 span=frustumSpanNear(o+d*shift,d,a,ra,na,b,rb,nb);
 return span.x<span.y?span+shift:NO_SPAN;
}
float2 sphereSpan(float3 o,float3 d,float3 center,float radius){
 float shift=dot(center-o,d);
 float3 w=o+d*shift-center;
 float b=dot(w,d),c=dot(w,w)-radius*radius,disc=b*b-c;
 if(disc<0)return NO_SPAN;
 float root=sqrt(disc);
 return float2(-b-root,-b+root)+shift;
}
// Length of the union of spans beyond from, and the first run of spans that
// overlap, which holds the surface the ray meets first.
float spanUnion(float2 spans[TUBE_SPANS],float from,out float2 firstRun){
 [unroll]for(uint k=0;k<TUBE_SPANS;k++)spans[k].x=max(spans[k].x,from);
 [unroll]for(uint i=1;i<TUBE_SPANS;i++){
  [unroll]for(uint j=i;j>0;j--){
   if(spans[j].x<spans[j-1].x){float2 swap=spans[j];spans[j]=spans[j-1];spans[j-1]=swap;}
  }
 }
 firstRun=NO_SPAN;
 float total=0,start=-1e30,end=-1e30;
 [unroll]for(uint n=0;n<TUBE_SPANS;n++){
  float2 span=spans[n];
  if(span.y<=span.x)continue;
  if(span.x>end){
   total+=max(end-start,0);
   if(end>start&&firstRun.y<firstRun.x)firstRun=float2(start,end);
   start=span.x;end=span.y;
  }else end=max(end,span.y);
 }
 if(end>start&&firstRun.y<firstRun.x)firstRun=float2(start,end);
 return total+max(end-start,0);
}
// Scales v along a unit axis, or leaves it for a zero axis. The two axes of
// one ellipsoid are orthogonal, so the scalings commute.
float3 scaleAlong(float3 v,float4 axisScale){return v+axisScale.xyz*(dot(v,axisScale.xyz)*(1/axisScale.w-1));}
// The span of the ray o+t*d, d of unit length, inside the sphere of radius
// at center stretched by w along each of two orthogonal axes.
float2 ellipsoidSpan(float3 o,float3 d,float3 center,float radius,float4 axisScale,float4 normalScale){
 float shift=dot(center-o,d);
 float3 so=scaleAlong(scaleAlong(o+d*shift-center,axisScale),normalScale);
 float3 sd=scaleAlong(scaleAlong(d,axisScale),normalScale);
 float a=dot(sd,sd),b=dot(so,sd),c=dot(so,so)-radius*radius,disc=b*b-a*c;
 if(disc<0||a<1e-12)return NO_SPAN;
 float root=sqrt(disc);
 return float2((-b-root)/a,(-b+root)/a)+shift;
}
// The spans of a ray through one link: its frusta, and caps where it ends a
// chain. Positions are relative to the ray's origin o.
void linkSpans(float3 o,float3 d,float3 p0,float r0,float3 t0,float3 p1,float r1,float3 t1,uint pieces,bool capStart,bool capEnd,out float2 spans[TUBE_SPANS]){
 [unroll]for(uint k=0;k<TUBE_SPANS;k++)spans[k]=NO_SPAN;
 float3 a=p0,na=normalize(hermiteTangent(p0,t0,p1,t1,0)+(p1-p0)*1e-6);
 [unroll]for(uint piece=0;piece<TUBE_PIECES;piece++){
  if(piece<pieces){
   float s=float(piece+1)/pieces;
   float3 b=hermite(p0,t0,p1,t1,s),nb=normalize(hermiteTangent(p0,t0,p1,t1,s)+(p1-p0)*1e-6);
   spans[piece]=frustumSpan(o,d,a,lerp(r0,r1,float(piece)/pieces),na,b,lerp(r0,r1,s),nb);
   a=b;na=nb;
  }
 }
 if(capStart)spans[TUBE_PIECES]=sphereSpan(o,d,p0,r0);
 if(capEnd)spans[TUBE_PIECES+1]=sphereSpan(o,d,p1,r1);
}
#endif
