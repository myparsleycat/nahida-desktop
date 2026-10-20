// Vertex outputs of the main-view vertex shaders with filter_index 611005.
#define CAPTURE_VERTEX_DEFINED
struct OriginalVertex {
 float4 field0 : TEXCOORD10;
 float4 field1 : TEXCOORD11;
 float4 field2 : VELOCITY_PREV_POS0;
 float4 clip : SV_POSITION;
};
#include "CaptureTriangle.hlsl"
