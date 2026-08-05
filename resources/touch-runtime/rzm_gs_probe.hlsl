// rzm_gs_probe.hlsl
// Made by: Rayvich
// Native VS -> custom GS/PS calibration probe.
//
// Each draw captures one indexed vertex after the native VS:
//   row 0: clip.xyzw
//   row 1: vertexIndex, sampleSlot, 0, valid
//
// The compute detector reconstructs a component-level local->clip transform
// from 4 non-coplanar samples, then projects all mesh vertices itself.

#define RZM_CALIB_SAMPLES 8u
#define RZM_CALIB_ROWS    2u

struct VS_OUT
{
    float4 pos       : SV_Position;
};

struct GS_OUT
{
    float4 pos  : SV_Position;
    float4 data : TEXCOORD0;
};

Buffer<uint> RZMIndexBuffer : register(t1);
Texture1D<float4> IniParams : register(t120);

#define CALIB_PARAMS IniParams[26]

#ifdef GEOMETRY_SHADER

void EmitTexel(uint col, uint row, float4 data, inout TriangleStream<GS_OUT> stream)
{
    if (col >= RZM_CALIB_SAMPLES || row >= RZM_CALIB_ROWS)
        return;

    float xMin = -1.0 + 2.0 * ((float)col / (float)RZM_CALIB_SAMPLES);
    float xMax = -1.0 + 2.0 * ((float)(col + 1u) / (float)RZM_CALIB_SAMPLES);
    float yMax =  1.0 - 2.0 * ((float)row / (float)RZM_CALIB_ROWS);
    float yMin =  1.0 - 2.0 * ((float)(row + 1u) / (float)RZM_CALIB_ROWS);

    GS_OUT o;
    o.data = data;

    o.pos = float4(xMin, yMax, 0.0, 1.0); stream.Append(o);
    o.pos = float4(xMax, yMax, 0.0, 1.0); stream.Append(o);
    o.pos = float4(xMin, yMin, 0.0, 1.0); stream.Append(o);
    o.pos = float4(xMax, yMin, 0.0, 1.0); stream.Append(o);
    stream.RestartStrip();
}

[maxvertexcount(8)]
void main(
    point VS_OUT input[1],
    uint primID : SV_PrimitiveID,
    inout TriangleStream<GS_OUT> stream)
{
    uint sampleSlot = (uint)max(CALIB_PARAMS.x, 0.0);
    uint indexOffset = (uint)max(CALIB_PARAMS.y, 0.0);

    uint indexCount;
    RZMIndexBuffer.GetDimensions(indexCount);
    if (indexOffset >= indexCount)
        return;

    uint vertexIndex = RZMIndexBuffer[indexOffset];

    EmitTexel(sampleSlot, 0u, input[0].pos, stream);
    EmitTexel(sampleSlot, 1u, float4((float)vertexIndex, (float)sampleSlot, 0.0, 1.0), stream);
}

#endif

#ifdef PIXEL_SHADER

void main(GS_OUT input, out float4 o0 : SV_Target0)
{
    o0 = input.data;
}

#endif
