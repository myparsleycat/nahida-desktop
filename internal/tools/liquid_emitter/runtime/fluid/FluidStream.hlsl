// Pass 1 of the screen-space fluid, after FluidSurface.hlsl into the same
// targets with the same blending: the stream particles StreamLink.hlsl
// joined, drawn as one tube along their links (StreamTube.hlsl), and the
// liquid gathering at each oozing outlet (EmitterState.hlsl), a drop on a
// neck that thins until it breaks. Vertices of PRIMARY_COUNT links, one per
// stream particle toward the one ahead of it, then of SLOT_COUNT outlets.
#include "SSFRCommon.hlsl"
#include "JetGuide.hlsl"
#include "StreamLinks.hlsl"
#include "StreamTube.hlsl"
StructuredBuffer<FluidParticle> particles:register(t100);
#include "Slots.hlsl"
#include "EmitterState.hlsl"
StructuredBuffer<float4> links:register(t104);
StructuredBuffer<float4> emitters:register(t105);
static const uint SHAPE_NONE=0,SHAPE_LINK=1,SHAPE_OUTLET=2;
// A tube thinner than this many pixels in radius is traced this wide, with
// its thickness thinned by the same area, so a far stream covers whole
// pixels instead of breaking into dashes, and absorbs no more for it.
static const float TUBE_MIN_PIXELS=.6;
static const float COLOR_WEIGHT_FLOOR=1e-5;
struct Varying{
 float4 position:SV_Position;
 float2 ndc:TEXCOORD0;
 // Link: xyz its particle, w its radius, then the particle ahead. Outlet:
 // the drop's center and radius, then the outlet and the neck's radius.
 nointerpolation float4 start:TEXCOORD1;
 nointerpolation float4 end:TEXCOORD2;
 // Link: the Hermite tangents at both particles. Outlet: the axis the drop
 // stretches along and the normal it flattens across, each with its scale.
 nointerpolation float4 startAxis:TEXCOORD3;
 nointerpolation float4 endAxis:TEXCOORD4;
 nointerpolation float4 liquid:TEXCOORD5;
 nointerpolation uint tag:TEXCOORD6;
 nointerpolation float3 materialFade:TEXCOORD7;
 // The shape (SHAPE_*), a link's frusta, and 1 for a cap at its start and
 // 2 at its end.
 nointerpolation uint3 shape:TEXCOORD8;
};
#ifdef VERTEX_SHADER
// Any target of the pass, for its size.
Texture2D<float> targetSize:register(t107);
Varying hidden(){
 Varying o=(Varying)0;
 o.position=float4(2,2,0,1);
 return o;
}
Varying main(uint id:SV_VertexID){
 uint i=id/6;
 Varying o=(Varying)0;
 float3 center;float bound;uint owner;
 uint view=debugView();
 if(i<PRIMARY_COUNT){
  float4 record=links[i*LINK_ROWS+1];
  uint ahead=asuint(record.y);
  owner=primarySlot(i);
  if(ahead>=PRIMARY_COUNT||!slotLive(owner))return hidden();
  float3 root=slotRoot(owner);
  float4 here=links[i*LINK_ROWS],there=links[ahead*LINK_ROWS];
  float3 p0=root+particles[i].positionAge.xyz,p1=root+particles[ahead].positionAge.xyz;
  if(!all(isfinite(p0))||!all(isfinite(p1))||here.w<=0||there.w<=0)return hidden();
  o.start=float4(p0,here.w);o.end=float4(p1,there.w);
  o.startAxis=float4(here.xyz,0);o.endAxis=float4(there.xyz,0);
  o.shape=uint3(SHAPE_LINK,tubePieces(p0,here.xyz,p1,there.xyz),
   (asuint(record.x)==LINK_NONE?1u:0u)|(asuint(links[ahead*LINK_ROWS+1].y)==LINK_NONE?2u:0u));
  // The curve stays inside the hull of its Bezier control points.
  float3 c0=p0+here.xyz/3,c1=p1-there.xyz/3;
  center=(p0+p1+c0+c1)*.25;
  bound=max(max(distance(center,p0),distance(center,p1)),max(distance(center,c0),distance(center,c1)))+max(here.w,there.w);
  o.tag=owner;
  o.materialFade.z=1;
  if(view==DEBUG_FLOW)o.liquid.rgb=debugHue(owner*31+(uint)emitters[owner*EMITTER_ROWS+EMITTER_FLOW].y);
 }else{
  owner=i-PRIMARY_COUNT;
  if(owner>=SLOT_COUNT||!slotLive(owner))return hidden();
  uint base=owner*EMITTER_ROWS;
  float4 ooze=emitters[base+EMITTER_OOZE],front=emitters[base+EMITTER_FRONT],shape=emitters[base+EMITTER_SHAPE];
  float4 normal=emitters[base+EMITTER_NORMAL],outlet=emitters[base+EMITTER_OUTLET];
  if(!(ooze.x>0)||ooze.w<=0)return hidden();
  float3 root=slotRoot(owner);
  // Stretching keeps the drop's volume: it lengthens along its fall and,
  // gathered on a surface, flattens across it and spreads.
  bool attached=normal.w>.5;
  float stretch=1+.6*shape.w,flatten=attached?OOZE_FLATTEN:1;
  float radius=oozeRadius(ooze.x)/pow(stretch*flatten,1.0/3);
  o.start=float4(root+front.xyz,radius);o.end=float4(root+outlet.xyz,front.w);
  o.startAxis=float4(shape.xyz,stretch);
  o.endAxis=attached?float4(normal.xyz,flatten):float4(0,0,0,1);
  o.shape=uint3(SHAPE_OUTLET,1,0);
  center=o.start.xyz;
  bound=max(radius*max(stretch,1/flatten),distance(o.start.xyz,o.end.xyz)+front.w)*1.1;
  o.tag=owner|(attached?TAG_FILM:0);
  o.materialFade.z=ooze.w;
  if(view==DEBUG_OOZE)o.liquid.rgb=(uint)ooze.y==OOZE_GROW?float3(.1,.9,.1):((uint)ooze.y==OOZE_STRETCH?float3(1,.9,0):float3(1,.1,.1));
  else if(view==DEBUG_CONTACT)o.liquid.rgb=attached?float3(.1,.9,.1):.35;
 }

 float4 rect;
 if(!screenRect(center,bound,rect))return hidden();
 // Room for a thin tube traced wider than it is.
 uint width,height;targetSize.GetDimensions(width,height);
 float2 margin=4/max(float2(width,height),1);
 rect=float4(max(rect.xy-margin,-1),min(rect.zw+margin,1));
 o.position=rectCorner(rect,id%6);
 o.ndc=o.position.xy;
 float4 liquid=slotLiquid(owner);
 o.liquid=float4(debugColors()?(any(o.liquid.rgb>0)?o.liquid.rgb:.35):liquid.rgb,liquid.w);
 o.materialFade.xy=float2(slotScattering(owner),slotRoughness(owner));
 return o;
}
#endif
#ifdef PIXEL_SHADER
struct Surface{float depth:SV_Target0;float thickness:SV_Target1;float4 color:SV_Target2;float absorptionDepth:SV_Target3;float2 material:SV_Target4;};
Surface main(Varying i){
 // Screen derivatives only hold before any pixel is clipped.
 float2 dims=2/max(abs(float2(ddx(i.ndc.x),ddy(i.ndc.y))),1e-9);
 clip(float(i.shape.x)-.5);
 float3 eye=eyePosition(),direction=pixelRay(i.ndc);
 float3 origin=eye-i.start.xyz;
 float2 spans[TUBE_SPANS];
 float scale=1;
 if(i.shape.x==SHAPE_LINK){
  float perMeter=pixelsPerMeter(max(project(i.start.xyz).w,NEAR_W),dims);
  float widen=max(1,TUBE_MIN_PIXELS/max(min(i.start.w,i.end.w)*perMeter,1e-6));
  scale=1/(widen*widen);
  linkSpans(origin,direction,0,i.start.w*widen,i.startAxis.xyz,i.end.xyz-i.start.xyz,i.end.w*widen,i.endAxis.xyz,
   i.shape.y,(i.shape.z&1)!=0,(i.shape.z&2)!=0,spans);
 }else{
  [unroll]for(uint k=0;k<TUBE_SPANS;k++)spans[k]=NO_SPAN;
  spans[0]=ellipsoidSpan(origin,direction,0,i.start.w,i.startAxis,i.endAxis);
  // The neck runs from the outlet into the drop.
  if(i.end.w>0){
   float3 toDrop=normalize(-(i.end.xyz-i.start.xyz)+i.startAxis.xyz*1e-6);
   spans[1]=frustumSpan(origin,direction,i.end.xyz-i.start.xyz,i.end.w,toDrop,0,i.end.w,toDrop);
  }
 }
 float2 first;
 float total=spanUnion(spans,nearDistance(direction),first);
 clip(total-1e-7);

 // With the eye inside the liquid, the surface is where the ray leaves it.
 float near=nearDistance(direction);
 float3 hit=eye+direction*(first.x>near?first.x:first.y);
 float4 projected=project(hit);
 if(sceneInFront(screenUV(projected),projected))clip(-1);
 Surface o;
 o.depth=encodeFluidDepth(projected.w,i.tag);
 o.thickness=total*scale*i.materialFade.z;
 float weight=o.thickness+COLOR_WEIGHT_FLOOR;
 o.color=float4(i.liquid.rgb*weight,weight);
 o.absorptionDepth=i.liquid.w*1000*weight;
 o.material=i.materialFade.xy*weight;
 return o;
}
#endif
