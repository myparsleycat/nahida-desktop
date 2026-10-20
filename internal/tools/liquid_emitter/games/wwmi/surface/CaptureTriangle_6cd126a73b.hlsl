// Vertex outputs of the main-view vertex shaders with filter_index 611000.
#define CAPTURE_VERTEX_DEFINED
struct OriginalVertex {
 float4 field0 : TEXCOORD10;
 float4 field1 : TEXCOORD11;
 float4 field2 : COLOR0;
 float2 field3 : TEXCOORD0;
 float2 field4 : TEXCOORD1;
 float2 field5 : TEXCOORD2;
 float2 field6 : TEXCOORD3;
 float2 field7 : TEXCOORD4;
 float2 field8 : TEXCOORD5;
 float4 field9 : VELOCITY_PREV_POS0;
 float4 clip : SV_POSITION;
};
#include "CaptureTriangle.hlsl"
