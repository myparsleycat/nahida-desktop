// Geometry shader input for a vertex shader that writes SV_Position to output
// register POSITION_REGISTER after its other outputs. The registers before it
// are declared whole whatever the vertex shader packs into them, so a driver
// linking the stages by register finds SV_Position at its register and one
// linking by semantic finds it by name. Their values are never read.
#define CAPTURE_VERTEX_DEFINED
struct OriginalVertex {
#if POSITION_REGISTER > 0
 float4 output0:TEXCOORD0;
#endif
#if POSITION_REGISTER > 1
 float4 output1:TEXCOORD1;
#endif
#if POSITION_REGISTER > 2
 float4 output2:TEXCOORD2;
#endif
#if POSITION_REGISTER > 3
 float4 output3:TEXCOORD3;
#endif
#if POSITION_REGISTER > 4
 float4 output4:TEXCOORD4;
#endif
#if POSITION_REGISTER > 5
 float4 output5:TEXCOORD5;
#endif
#if POSITION_REGISTER > 6
 float4 output6:TEXCOORD6;
#endif
#if POSITION_REGISTER > 7
 float4 output7:TEXCOORD7;
#endif
#if POSITION_REGISTER > 8
 float4 output8:TEXCOORD8;
#endif
#if POSITION_REGISTER > 9
 float4 output9:TEXCOORD9;
#endif
#if POSITION_REGISTER > 10
 float4 output10:TEXCOORD10;
#endif
#if POSITION_REGISTER > 11
 float4 output11:TEXCOORD11;
#endif
 float4 clip:SV_Position;
};
#include "CaptureTriangle.hlsl"
