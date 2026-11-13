#include "SSFRCommon.hlsl"
#include "Yogurt.hlsl"
Texture2D<float2> yogurtField:register(t100);
Texture2D<float4> yogurtShape:register(t101);
Texture2D<float3> yogurtColor:register(t102);
StructuredBuffer<float4> fluidAmbient:register(t105);
static const float3 YOG_SHADE=float3(.94,.93,.96);
struct ScreenVarying{float4 position:SV_Position;};
#ifdef VERTEX_SHADER
ScreenVarying main(uint id:SV_VertexID){ScreenVarying o;o.position=screenTriangle(id);return o;}
#endif
#ifdef PIXEL_SHADER
float fieldAt(int2 pixel,int2 last){return yogurtField.Load(int3(clamp(pixel,int2(0,0),last),0)).y;}
float4 main(ScreenVarying i):SV_Target{
 uint width,height;yogurtField.GetDimensions(width,height);
 int2 pixel=int2(i.position.xy),last=int2(width-1,height-1);
 float2 field=yogurtField.Load(int3(pixel,0));
 clip(field.y-YOG_ISO*.5);
 float2 slope=float2(fieldAt(pixel+int2(1,0),last)-fieldAt(pixel-int2(1,0),last),fieldAt(pixel+int2(0,1),last)-fieldAt(pixel-int2(0,1),last))*.5;
 float inside=(field.y-YOG_ISO)/max(length(slope),1e-4);
 float edge=saturate(inside+.5);
 clip(edge-.004);
 float4 shape=yogurtShape.Load(int3(pixel,0));
 float3 base=yogurtColor.Load(int3(pixel,0))/field.y;
 float thickness=max(shape.z,0);
 float2 lean=shape.xy/field.y*smoothstep(0,.003,thickness);
 float3 normal=normalize(float3(lean,sqrt(max(1-dot(lean,lean),.0004))));
 const float3 light=normalize(float3(-.25,.25,.93));
 float lit=smoothstep(-.5,-.05,dot(normal,light));
 float3 surround=fluidAmbient[1].rgb;
 float luminance=all(isfinite(surround))?dot(surround,float3(.2126,.7152,.0722)):.6;
 float exposure=clamp(pow(saturate(luminance/.55),.6),.96,1);
 float3 color=base*lerp(YOG_SHADE,1,lit);
 color*=lerp(1,YOG_SHADE*.95,.5*(1-smoothstep(.4,2.0,inside)));
 float3 halfway=normalize(light+float3(0,0,1));
 float shine=dot(normal,halfway);
 float highlight=(smoothstep(.962,.986,shine)*.95+pow(saturate(shine),20)*.14)*smoothstep(.0008,.002,thickness);
 color=(color+highlight*lerp(1-base,1,.35))*exposure;
 float code=shape.w/field.y;
 float body=saturate(thickness/.0045);
 float alpha=lerp(.74,.95,body*body);
 alpha=code<=1?saturate(alpha*code):lerp(alpha,1,saturate(code-1));
 return float4(color,alpha*edge);
}
#endif
