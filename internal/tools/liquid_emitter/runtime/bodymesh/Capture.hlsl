#include "../camera/Camera.hlsl"
#include "Dimensions.hlsl"
#ifndef CAPTURE_VERTEX_DEFINED
struct OriginalVertex {float4 clip:SV_Position;};
#endif
struct V {float4 clip:SV_Position;nointerpolation float4 world:TEXCOORD0;};
#ifdef GEOMETRY_SHADER
[maxvertexcount(3)]
void main(triangle OriginalVertex input[3],uint face:SV_PrimitiveID,inout PointStream<V> stream){
 for(uint i=0;i<3;i++){
  float4 p=input[i].clip;
  float4 v=p.x*camera[12]+p.y*camera[13]+p.z*camera[14]+p.w*camera[15];
  float3 rel=(v.x*camera[4].xyz+v.y*camera[5].xyz+v.z*camera[6].xyz)/v.w;
  V o;o.world=float4(rel,abs(v.w)>1e-8&&all(isfinite(rel))?1:0);
  o.clip=float4(-1+2*(3*(face%128)+i+.5)/BODY_TEX_WIDTH,1-2*(face/128+.5)/BODY_TEX_HEIGHT,0,1);
  stream.Append(o);
 }
}
#endif
#ifdef PIXEL_SHADER
float4 main(V input):SV_Target{return input.world;}
#endif
