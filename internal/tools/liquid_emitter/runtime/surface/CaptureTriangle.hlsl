// Reuse the game's active vertex shader: capture a chosen, already-skinned triangle.
#include "../camera/Camera.hlsl"
struct OriginalVertex { float4 clip : SV_Position; };
struct CapturedVertex { float4 clip : SV_Position; nointerpolation float4 world : TEXCOORD0; };
#ifdef GEOMETRY_SHADER
[maxvertexcount(12)]
void main(triangle OriginalVertex input[3], inout TriangleStream<CapturedVertex> stream) {
 for(uint i=0;i<3;i++) {
  float4 p=input[i].clip;
  // Unproject through inverse projection and inverse view rotation separately.
  // Keep the translation out until AFTER triangle differences/normal evaluation.
  float4 view=p.x*camera[12]+p.y*camera[13]+p.z*camera[14]+p.w*camera[15];
  float3 relative=(view.x*camera[4].xyz+view.y*camera[5].xyz+view.z*camera[6].xyz)/view.w;
  float4 world=float4(relative,abs(view.w)>1e-8?1:0);
  const float2 corners[4]={float2(0,-1),float2(0,1),float2(1,-1),float2(1,1)};
  for(uint j=0;j<4;j++) {
   CapturedVertex o;o.world=world;
   o.clip=float4(-1+(i+corners[j].x)*(2.0/3.0),corners[j].y,0,1);
   stream.Append(o);
  }
  stream.RestartStrip();
 }
}
#endif
#ifdef PIXEL_SHADER
float4 main(CapturedVertex input):SV_Target { return input.world; }
#endif
