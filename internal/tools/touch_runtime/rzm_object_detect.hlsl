// rzm_object_detect.hlsl
// RZMenu: component-level cursor hit detector driven by GS calibration probe.
// Made by: Rayvich
// Expected bindings:
//   cs-t0 = vertex buffer, stride 40: float3 position, float3 normal, float4 tangent
//   cs-t1 = index buffer, R32_UINT
//   cs-t2 = component ObjectMap, Buffer<float4>
//   cs-t3 = ResourceRZMBakeRT, 8x2 R32G32B32A32_FLOAT
//           row 0: clip.xyzw per calibration sample
//           row 1: vertexIndex, sampleSlot, 0, valid
//   cs-u0 = RWBuffer<float4>, closest-hit accumulator
//
// No cb slot discovery is used. A local->clip transform is reconstructed from
// 4 non-coplanar GS-captured samples and then applied to all vertices in CS.

#define THREADS_PER_GROUP 128u
#define MAX_OBJECTS       256u
#define CALIB_SAMPLES     8u
#define RZM_DETECT_LAYOUT_VERSION 5.2f

struct VertexAttributes
{
    float3 position;
    float3 normal;
    float4 tangent;
};

struct HitPayload
{
    float4 slot1;
    float4 slot2;
    float4 slot3;
    float4 slot4;
    float4 slot5;
    float4 slot6;
    float4 slot7;
};

struct Calibration
{
    float3 p0;
    float3 p1;
    float3 p2;
    float3 p3;
    float4 c0;
    float4 c1;
    float4 c2;
    float4 c3;
    float  invDet;
    float  valid;
};

// Additional object-API slots 8-9: local displacements for one screen-UV
// unit right/down.  The jiggle pass snapshots them on click and only consumes
// them; camera reconstruction remains owned by the object API.
struct ScreenBasis
{
    float3 rightLocal;
    float3 downLocal;
    float valid;
    float conditioning;
};

bool LocalToScreenUV(Calibration cal, float3 localPos, out float2 uv);

// Construct a surface-attached frame in the skinned local VB0 space. Red/X is
// the interpolated mesh normal (palm-facing direction); green/Y and blue/Z are
// deterministic tangents derived from that normal. The camera only projects
// this already-fixed frame -- it no longer decides the frame's orientation.
void BuildGizmoAxes(Calibration cal, float3 hitPoint,
                    float3 p0, float3 p1, float3 p2,
                    float3 n0, float3 n1, float3 n2,
                    out float4 xy, out float4 zValid)
{
    xy = 0.0f;
    zValid = 0.0f;
    float eps = clamp((length(p1-p0)+length(p2-p1)+length(p0-p2)) / 60.0f, 0.0005f, 0.05f);
    float3 normal = n0 + n1 + n2;
    if (dot(normal, normal) < 1e-10f)
        normal = cross(p1 - p0, p2 - p0);
    if (dot(normal, normal) < 1e-10f) return;
    normal = normalize(normal);

    // Pick the least parallel fixed local axis. This avoids a camera-derived
    // tangent and remains stable while the viewpoint moves around the mesh.
    float3 localRef = abs(normal.z) < 0.75f ? float3(0,0,1) :
                       (abs(normal.y) < 0.75f ? float3(0,1,0) : float3(1,0,0));
    float3 tangent = normalize(cross(localRef, normal));
    float3 bitangent = normalize(cross(normal, tangent));

    float2 c, normalUV, tangentUV, bitangentUV;
    if (!LocalToScreenUV(cal, hitPoint, c)
        || !LocalToScreenUV(cal, hitPoint + normal * eps, normalUV)
        || !LocalToScreenUV(cal, hitPoint + tangent * eps, tangentUV)
        || !LocalToScreenUV(cal, hitPoint + bitangent * eps, bitangentUV)) return;
    xy = float4((normalUV-c)/eps, (tangentUV-c)/eps);
    zValid = float4((bitangentUV-c)/eps, 1.0f, 0.0f);
}

StructuredBuffer<VertexAttributes> gVB0         : register(t0);
Buffer<uint>                       gIndexBuffer : register(t1);
Buffer<float4>                     gObjectMap   : register(t2);
Texture2D<float4>                  gCalibTex    : register(t3);
Buffer<float4>                     gJiggleMasks0 : register(t4);
Buffer<float4>                     gJiggleMasks1 : register(t5);
Buffer<float4>                     gJiggleMasks2 : register(t7);
// LLMenu's own scalar viewport contract. It is read directly on GPU so UI
// offset/resize never needs per-frame CPU synchronization.
Buffer<float>                      gViewportFrameAPI : register(t6);
RWBuffer<float4>                   gBestHit     : register(u0);
RWBuffer<float4>                   gComponentHit : register(u1);
RWBuffer<float4>                   gDebug       : register(u2);

Texture1D<float4> IniParams : register(t120);

#define CURSOR_PARAMS IniParams[24]
#define CLICK_PARAMS  IniParams[25]
#define DETECT_PARAMS IniParams[26]
#define ALT_CURSOR_PARAMS IniParams[27]
#define COMPONENT_TOKEN   IniParams[28].x
#define DEBUG_PARAMS      IniParams[74]
#define VIEWPORT_XFORM    IniParams[85]
#define VIEWPORT_VALID    IniParams[86].x

groupshared float  sBestDepth[THREADS_PER_GROUP];
groupshared float  sBestID[THREADS_PER_GROUP];
groupshared float  sBestFirstIndex[THREADS_PER_GROUP];
groupshared float  sHitCount[THREADS_PER_GROUP];
groupshared float4 sSlot1[THREADS_PER_GROUP];
groupshared float4 sSlot2[THREADS_PER_GROUP];
groupshared float4 sSlot3[THREADS_PER_GROUP];
groupshared float4 sSlot4[THREADS_PER_GROUP];
groupshared float4 sSlot5[THREADS_PER_GROUP];
groupshared float4 sSlot6[THREADS_PER_GROUP];
groupshared float4 sSlot7[THREADS_PER_GROUP];
groupshared float  sAnchorDistance[THREADS_PER_GROUP];
groupshared float  sAnchorDepth[THREADS_PER_GROUP];
groupshared float  sAnchorID[THREADS_PER_GROUP];
groupshared float4 sAnchorSlot1[THREADS_PER_GROUP];
groupshared float4 sAnchorSlot2[THREADS_PER_GROUP];
groupshared float4 sAnchorSlot3[THREADS_PER_GROUP];
groupshared float4 sAnchorSlot4[THREADS_PER_GROUP];
groupshared float4 sAnchorSlot5[THREADS_PER_GROUP];
groupshared float4 sAnchorSlot6[THREADS_PER_GROUP];
groupshared float4 sAnchorSlot7[THREADS_PER_GROUP];

float Det3(float3 a, float3 b, float3 c)
{
    return dot(a, cross(b, c));
}

float2 CursorUVFromParams(float4 cursorParams)
{
    float2 screenRes = max(cursorParams.zw, float2(1.0f, 1.0f));
    float2 cursor = cursorParams.xy;
    if (cursor.x > 1.0f || cursor.y > 1.0f)
        cursor /= screenRes;
    // State layout: min.xy, max.xy, extent.xy, generation, valid, ...
    // Invalid/missing/stale state deliberately means identity. A valid UI
    // record maps physical monitor UV into the unscaled character RT UV.
    if (gViewportFrameAPI[7u] < .5f)
        return cursor;
    float2 offset = float2(gViewportFrameAPI[0u], gViewportFrameAPI[1u]);
    float2 scale = float2(gViewportFrameAPI[4u], gViewportFrameAPI[5u]);
    float sourceW = gViewportFrameAPI[11u];
    float sourceH = gViewportFrameAPI[12u];
    if (offset.x != offset.x || offset.y != offset.y || scale.x != scale.x || scale.y != scale.y ||
        scale.x < .04f || scale.y < .04f || scale.x > 1.2f || scale.y > 1.2f ||
        sourceW <= 0.0f || sourceH <= 0.0f || sourceW > 8192.0f || sourceH > 8192.0f)
        return cursor;
    return (cursor - offset) / scale;
}

bool NormalizedCursorValid(float4 cursorParams)
{
    return cursorParams.z > 0.0f
        && cursorParams.w > 0.0f
        && cursorParams.x > 0.0f
        && cursorParams.y > 0.0f
        && cursorParams.x < 1.0f
        && cursorParams.y < 1.0f;
}

bool ReadCalibSample(uint slot, out float3 localPos, out float4 clipPos)
{
    localPos = float3(0.0f, 0.0f, 0.0f);
    clipPos = float4(0.0f, 0.0f, 0.0f, 0.0f);

    float4 clip = gCalibTex.Load(int3(slot, 0, 0));
    float4 meta = gCalibTex.Load(int3(slot, 1, 0));

    if (meta.w < 0.5f || abs(meta.y - (float)slot) > 0.5f)
        return false;

    uint vertexIndex = (uint)(meta.x + 0.5f);

    uint vertexCount;
    uint vertexStride;
    gVB0.GetDimensions(vertexCount, vertexStride);
    if (vertexIndex >= vertexCount)
        return false;

    if (!all(abs(clip) < float4(1000000.0f, 1000000.0f, 1000000.0f, 1000000.0f)))
        return false;

    if (abs(clip.w) <= 1e-5f)
        return false;

    localPos = gVB0[vertexIndex].position;
    clipPos = clip;
    return true;
}

void SelectCalibration(out Calibration cal)
{
    float3 p[CALIB_SAMPLES];
    float4 c[CALIB_SAMPLES];
    float  v[CALIB_SAMPLES];

    [unroll]
    for (uint i = 0u; i < CALIB_SAMPLES; ++i)
    {
        v[i] = ReadCalibSample(i, p[i], c[i]) ? 1.0f : 0.0f;
    }

    float bestVol = 0.0f;
    uint bestA = 0u;
    uint bestB = 1u;
    uint bestC = 2u;
    uint bestD = 3u;

    [loop]
    for (uint a = 0u; a < CALIB_SAMPLES - 3u; ++a)
    {
        [loop]
        for (uint b = a + 1u; b < CALIB_SAMPLES - 2u; ++b)
        {
            [loop]
            for (uint cc = b + 1u; cc < CALIB_SAMPLES - 1u; ++cc)
            {
                [loop]
                for (uint d = cc + 1u; d < CALIB_SAMPLES; ++d)
                {
                    float valid = v[a] * v[b] * v[cc] * v[d];
                    float3 e1 = p[b] - p[a];
                    float3 e2 = p[cc] - p[a];
                    float3 e3 = p[d] - p[a];
                    float vol = abs(Det3(e1, e2, e3)) * valid;

                    if (vol > bestVol)
                    {
                        bestVol = vol;
                        bestA = a;
                        bestB = b;
                        bestC = cc;
                        bestD = d;
                    }
                }
            }
        }
    }

    cal.p0 = p[bestA];
    cal.p1 = p[bestB];
    cal.p2 = p[bestC];
    cal.p3 = p[bestD];
    cal.c0 = c[bestA];
    cal.c1 = c[bestB];
    cal.c2 = c[bestC];
    cal.c3 = c[bestD];

    float det = Det3(cal.p1 - cal.p0, cal.p2 - cal.p0, cal.p3 - cal.p0);
    cal.valid = bestVol > 1e-8f && abs(det) > 1e-8f ? 1.0f : 0.0f;
    cal.invDet = cal.valid > 0.5f ? rcp(det) : 0.0f;
}

float4 LocalToClip(Calibration cal, float3 localPos)
{
    float3 e1 = cal.p1 - cal.p0;
    float3 e2 = cal.p2 - cal.p0;
    float3 e3 = cal.p3 - cal.p0;
    float3 d = localPos - cal.p0;

    float b1 = Det3(d, e2, e3) * cal.invDet;
    float b2 = Det3(e1, d, e3) * cal.invDet;
    float b3 = Det3(e1, e2, d) * cal.invDet;
    float b0 = 1.0f - b1 - b2 - b3;

    return cal.c0 * b0 + cal.c1 * b1 + cal.c2 * b2 + cal.c3 * b3;
}

bool ClipToScreenUV(float4 clipPos, out float2 uv, out float depth)
{
    uv = float2(0.0f, 0.0f);
    depth = 3.402823e+38f;

    if (clipPos.w <= 1e-5f)
        return false;

    float2 ndc = clipPos.xy / clipPos.w;
    uv = float2(ndc.x * 0.5f + 0.5f, 0.5f - ndc.y * 0.5f);
    depth = 1.0f - clipPos.z / clipPos.w;

    return all(abs(ndc) < float2(1000.0f, 1000.0f)) && abs(depth) < 1000.0f;
}

bool ValidViewportCursor(float4 cursorParams)
{
    // Ordered comparisons reject NaN. The explicit 8K cap also rejects Inf
    // and prevents a malformed layout from applying offset/resize semantics.
    return VIEWPORT_VALID > 0.5f
        && cursorParams.x == cursorParams.x && cursorParams.y == cursorParams.y
        && cursorParams.z == cursorParams.z && cursorParams.w == cursorParams.w
        && cursorParams.z > 0.0f && cursorParams.w > 0.0f
        && cursorParams.z <= 8192.0f && cursorParams.w <= 8192.0f
        && cursorParams.x >= 0.0f && cursorParams.y >= 0.0f
        && cursorParams.x <= cursorParams.z && cursorParams.y <= cursorParams.w
        && VIEWPORT_XFORM.x == VIEWPORT_XFORM.x && VIEWPORT_XFORM.y == VIEWPORT_XFORM.y
        && VIEWPORT_XFORM.z == VIEWPORT_XFORM.z && VIEWPORT_XFORM.w == VIEWPORT_XFORM.w;
}

bool LocalToScreenUV(Calibration cal, float3 localPos, out float2 uv)
{
    float depth;
    return ClipToScreenUV(LocalToClip(cal, localPos), uv, depth);
}

float4 BuildDepthPayload(Calibration cal, float3 localPos)
{
    float4 clipPos = LocalToClip(cal, localPos);
    float2 ignoredUV;
    float depth;
    if (!ClipToScreenUV(clipPos, ignoredUV, depth))
        return 0.0f;
    return float4(depth, clipPos.w, clipPos.z, 1.0f);
}

ScreenBasis BuildScreenBasis(Calibration cal, float3 hitPoint,
                             float3 p0, float3 p1, float3 p2)
{
    ScreenBasis basis;
    basis.rightLocal = float3(1.0f, 0.0f, 0.0f);
    basis.downLocal = float3(0.0f, 1.0f, 0.0f);
    basis.valid = 0.0f;
    basis.conditioning = 0.0f;

    float edgeScale = (length(p1 - p0) + length(p2 - p1) + length(p0 - p2)) / 3.0f;
    float eps = clamp(edgeScale * 0.05f, 0.0005f, 0.05f);
    float2 centerUV, ux, uy, uz;
    if (!LocalToScreenUV(cal, hitPoint, centerUV)
        || !LocalToScreenUV(cal, hitPoint + float3(eps, 0.0f, 0.0f), ux)
        || !LocalToScreenUV(cal, hitPoint + float3(0.0f, eps, 0.0f), uy)
        || !LocalToScreenUV(cal, hitPoint + float3(0.0f, 0.0f, eps), uz))
        return basis;

    float2 jx = (ux - centerUV) / eps;
    float2 jy = (uy - centerUV) / eps;
    float2 jz = (uz - centerUV) / eps;
    float a00 = jx.x * jx.x + jy.x * jy.x + jz.x * jz.x;
    float a01 = jx.x * jx.y + jy.x * jy.y + jz.x * jz.y;
    float a11 = jx.y * jx.y + jy.y * jy.y + jz.y * jz.y;
    float det = a00 * a11 - a01 * a01;
    basis.conditioning = det / max(a00 * a11, 1e-12f);
    if (det <= 1e-12f || basis.conditioning <= 1e-5f)
        return basis;

    float2 solveRight = float2(a11, -a01) / det;
    float2 solveDown  = float2(-a01, a00) / det;
    basis.rightLocal = float3(dot(jx, solveRight), dot(jy, solveRight), dot(jz, solveRight));
    basis.downLocal  = float3(dot(jx, solveDown),  dot(jy, solveDown),  dot(jz, solveDown));
    basis.valid = 1.0f;
    return basis;
}

float Cross2(float2 a, float2 b)
{
    return a.x * b.y - a.y * b.x;
}

float3 Barycentric2D(float2 p, float2 a, float2 b, float2 c)
{
    float2 v0 = b - a;
    float2 v1 = c - a;
    float2 v2 = p - a;
    float d00 = dot(v0, v0);
    float d01 = dot(v0, v1);
    float d11 = dot(v1, v1);
    float d20 = dot(v2, v0);
    float d21 = dot(v2, v1);
    float denom = d00 * d11 - d01 * d01;

    if (abs(denom) <= 1e-12f)
        return float3(1.0f, 0.0f, 0.0f);

    float v = (d11 * d20 - d01 * d21) / denom;
    float w = (d00 * d21 - d01 * d20) / denom;
    return float3(1.0f - v - w, v, w);
}

bool PointInTriangle(float2 p, float2 a, float2 b, float2 c)
{
    float e0 = Cross2(b - a, p - a);
    float e1 = Cross2(c - b, p - b);
    float e2 = Cross2(a - c, p - c);
    bool hasNeg = (e0 < 0.0f) || (e1 < 0.0f) || (e2 < 0.0f);
    bool hasPos = (e0 > 0.0f) || (e1 > 0.0f) || (e2 > 0.0f);
    return !(hasNeg && hasPos);
}

float DistSqPointSegment(float2 p, float2 a, float2 b, out float t)
{
    float2 ab = b - a;
    float denom = max(dot(ab, ab), 1e-10f);
    t = saturate(dot(p - a, ab) / denom);
    float2 d = p - (a + ab * t);
    return dot(d, d);
}

float DistSqPointTriangle(float2 p, float2 a, float2 b, float2 c)
{
    if (PointInTriangle(p, a, b, c))
        return 0.0f;

    float tAB, tBC, tCA;
    return min(DistSqPointSegment(p, a, b, tAB),
               min(DistSqPointSegment(p, b, c, tBC),
                   DistSqPointSegment(p, c, a, tCA)));
}

bool CursorHitsTriangle(
    float2 cursor,
    float2 a,
    float2 b,
    float2 c,
    float padUV,
    out float3 bary,
    out float inside)
{
    bary = float3(0.0f, 0.0f, 0.0f);
    inside = 0.0f;

    float2 pad = float2(padUV, padUV);
    float2 mn = min(a, min(b, c)) - pad;
    float2 mx = max(a, max(b, c)) + pad;

    if (any(cursor < mn) || any(cursor > mx))
        return false;

    if (PointInTriangle(cursor, a, b, c))
    {
        bary = Barycentric2D(cursor, a, b, c);
        inside = 1.0f;
        return true;
    }

    float tAB, tBC, tCA;
    float dAB = DistSqPointSegment(cursor, a, b, tAB);
    float dBC = DistSqPointSegment(cursor, b, c, tBC);
    float dCA = DistSqPointSegment(cursor, c, a, tCA);
    float bestD = min(dAB, min(dBC, dCA));

    if (bestD > padUV * padUV)
        return false;

    if (bestD == dAB)
        bary = float3(1.0f - tAB, tAB, 0.0f);
    else if (bestD == dBC)
        bary = float3(0.0f, 1.0f - tBC, tBC);
    else
        bary = float3(tCA, 0.0f, 1.0f - tCA);

    return true;
}

uint NearestVertexSlot(float2 cursor, float2 s0, float2 s1, float2 s2, out float distSq)
{
    float d0 = dot(cursor - s0, cursor - s0);
    float d1 = dot(cursor - s1, cursor - s1);
    float d2 = dot(cursor - s2, cursor - s2);

    distSq = min(d0, min(d1, d2));
    if (distSq == d0)
        return 0u;
    if (distSq == d1)
        return 1u;
    return 2u;
}

float3 SelectVertexPosition(uint slot, float3 p0, float3 p1, float3 p2)
{
    if (slot == 0u)
        return p0;
    if (slot == 1u)
        return p1;
    return p2;
}

uint SelectVertexIndex(uint slot, uint i0, uint i1, uint i2)
{
    if (slot == 0u)
        return i0;
    if (slot == 1u)
        return i1;
    return i2;
}

float3 SafeNormalize(float3 v)
{
    float lenSq = dot(v, v);
    if (lenSq <= 1e-20f)
        return float3(0.0f, 0.0f, 1.0f);
    return v * rsqrt(lenSq);
}

void InitPayload(out HitPayload payload)
{
    payload.slot1 = float4(0.0f, 0.0f, 0.0f, 0.0f);
    payload.slot2 = float4(0.0f, 0.0f, 0.0f, 0.0f);
    payload.slot3 = float4(0.0f, 0.0f, 0.0f, 0.0f);
    payload.slot4 = float4(0.0f, 0.0f, 0.0f, 0.0f);
    payload.slot5 = float4(0.0f, 0.0f, 1.0f, 0.0f);
    payload.slot6 = float4(0.0f, 0.0f, 0.0f, 0.0f);
    payload.slot7 = float4(RZM_DETECT_LAYOUT_VERSION, 0.0f, 0.0f, 0.0f);
}

// Strongest painted zone at the barycentric hit point. Zone IDs 0..11,
// packed 4-per-buffer across 3 float4 buffers. Each buffer's 3 triangle-
// corner samples are read, bary-blended, and folded into (zone, weight)
// before moving to the next buffer -- keeping only one blended float4 (plus
// its 3 short-lived corner samples) live at a time, to limit temp-register
// pressure (this function runs per-triangle in the raycast loop, so its
// register footprint sets a floor for this whole dispatch's occupancy, not
// just for triangles that actually call it).
float ResolveJiggleZone(uint i0, uint i1, uint i2, float3 bary, out float weight)
{
    uint count0;
    gJiggleMasks0.GetDimensions(count0);
    float4 low = (i0 < count0 ? gJiggleMasks0[i0] : 0.0f) * bary.x
               + (i1 < count0 ? gJiggleMasks0[i1] : 0.0f) * bary.y
               + (i2 < count0 ? gJiggleMasks0[i2] : 0.0f) * bary.z;
    weight = low.x;
    float zone = 0.0f;
    if (low.y > weight) { weight = low.y; zone = 1.0f; }
    if (low.z > weight) { weight = low.z; zone = 2.0f; }
    if (low.w > weight) { weight = low.w; zone = 3.0f; }

    uint count1;
    gJiggleMasks1.GetDimensions(count1);
    float4 high = (i0 < count1 ? gJiggleMasks1[i0] : 0.0f) * bary.x
                + (i1 < count1 ? gJiggleMasks1[i1] : 0.0f) * bary.y
                + (i2 < count1 ? gJiggleMasks1[i2] : 0.0f) * bary.z;
    if (high.x > weight) { weight = high.x; zone = 4.0f; }
    if (high.y > weight) { weight = high.y; zone = 5.0f; }
    if (high.z > weight) { weight = high.z; zone = 6.0f; }
    if (high.w > weight) { weight = high.w; zone = 7.0f; }

    uint count2;
    gJiggleMasks2.GetDimensions(count2);
    float4 r2band = (i0 < count2 ? gJiggleMasks2[i0] : 0.0f) * bary.x
                  + (i1 < count2 ? gJiggleMasks2[i1] : 0.0f) * bary.y
                  + (i2 < count2 ? gJiggleMasks2[i2] : 0.0f) * bary.z;
    if (r2band.x > weight) { weight = r2band.x; zone = 8.0f; }
    if (r2band.y > weight) { weight = r2band.y; zone = 9.0f; }
    if (r2band.z > weight) { weight = r2band.z; zone = 10.0f; }
    if (r2band.w > weight) { weight = r2band.w; zone = 11.0f; }
    return zone;
}

void TestTriangleRange(
    Calibration cal,
    uint tid,
    uint objectIndex,
    uint objectCount,
    uint firstIndex,
    uint indexCount,
    float objectMode,
    float objectID,
    float2 primaryCursor,
    float2 altCursor,
    bool useAltCursor,
    float padUV,
    float anchorPadUV,
    inout float bestDepth,
    inout float bestID,
    inout float bestFirstIndex,
    inout float hitCount,
    inout HitPayload payload,
    inout float anchorDistance,
    inout float anchorDepth,
    inout float anchorID,
    inout HitPayload anchorPayload)
{
    uint triangleCount = indexCount / 3u;

    [loop]
    for (uint tri = tid; tri < triangleCount; tri += THREADS_PER_GROUP)
    {
        uint indexBase = firstIndex + tri * 3u;
        uint i0 = gIndexBuffer[indexBase + 0u];
        uint i1 = gIndexBuffer[indexBase + 1u];
        uint i2 = gIndexBuffer[indexBase + 2u];

        float3 p0 = gVB0[i0].position;
        float3 p1 = gVB0[i1].position;
        float3 p2 = gVB0[i2].position;

        float2 s0, s1, s2;
        float d0, d1, d2;
        bool ok0 = ClipToScreenUV(LocalToClip(cal, p0), s0, d0);
        bool ok1 = ClipToScreenUV(LocalToClip(cal, p1), s1, d1);
        bool ok2 = ClipToScreenUV(LocalToClip(cal, p2), s2, d2);

        float3 primaryBary = float3(0.0f, 0.0f, 0.0f);
        float primaryInside = 0.0f;
        float3 altBary = float3(0.0f, 0.0f, 0.0f);
        float altInside = 0.0f;
        bool primaryHit = false;
        bool altHit = false;
        bool anchorHit = false;
        float3 anchorBary = float3(0.0f, 0.0f, 0.0f);
        float anchorInside = 0.0f;

        if (ok0 && ok1 && ok2)
        {
            primaryHit = CursorHitsTriangle(primaryCursor, s0, s1, s2, padUV, primaryBary, primaryInside);
            altHit = useAltCursor && CursorHitsTriangle(altCursor, s0, s1, s2, padUV, altBary, altInside);
            anchorHit = CursorHitsTriangle(primaryCursor, s0, s1, s2, anchorPadUV, anchorBary, anchorInside);
        }

        if (primaryHit || altHit)
        {
            float2 cursor = altHit ? altCursor : primaryCursor;
            float3 bary = altHit ? altBary : primaryBary;
            float inside = altHit ? altInside : primaryInside;

            float zoneWeight = 1.0f;
            float zone = 0.0f;
            if (objectMode == 7.0f)
                zone = ResolveJiggleZone(i0, i1, i2, bary, zoneWeight);
            if (objectMode == 7.0f && zoneWeight <= 1e-4f)
                continue;

            hitCount += 1.0f;
            float triDepth = min(d0, min(d1, d2));
            if (triDepth < bestDepth)
            {
                float nearestDistSq;
                uint nearestSlot = NearestVertexSlot(cursor, s0, s1, s2, nearestDistSq);
                float3 faceNormal = SafeNormalize(cross(p1 - p0, p2 - p0));
                float winding = Cross2(s1 - s0, s2 - s0) < 0.0f ? -1.0f : 1.0f;

                bestDepth = triDepth;
                bestID = objectID;
                bestFirstIndex = (float)firstIndex;

                payload.slot1 = float4(p0 * bary.x + p1 * bary.y + p2 * bary.z, objectMode);
                payload.slot2 = float4((float)firstIndex, (float)indexBase, (float)tri, (float)(indexBase / 3u));
                payload.slot3 = float4((float)i0, (float)i1, (float)i2, (float)nearestSlot);
                payload.slot4 = float4(bary, inside);
                payload.slot5 = float4(faceNormal, winding);
                payload.slot6 = float4(SelectVertexPosition(nearestSlot, p0, p1, p2), nearestDistSq);
                payload.slot7 = float4(RZM_DETECT_LAYOUT_VERSION, (float)objectIndex, (float)objectCount, objectMode == 7.0f ? zone : -1.0f);
            }
        }

        if (anchorHit)
        {
            float anchorZoneWeight = 1.0f;
            float anchorZone = 0.0f;
            if (objectMode == 7.0f)
                anchorZone = ResolveJiggleZone(i0, i1, i2, anchorBary, anchorZoneWeight);
            if (objectMode == 7.0f && anchorZoneWeight <= 1e-4f)
                continue;
            float candidateDistance = DistSqPointTriangle(primaryCursor, s0, s1, s2);
            float triDepth = min(d0, min(d1, d2));
            if (candidateDistance < anchorDistance ||
                (abs(candidateDistance - anchorDistance) <= 1e-12f && triDepth < anchorDepth))
            {
                float nearestDistSq;
                uint nearestSlot = NearestVertexSlot(primaryCursor, s0, s1, s2, nearestDistSq);
                float3 faceNormal = SafeNormalize(cross(p1 - p0, p2 - p0));
                float winding = Cross2(s1 - s0, s2 - s0) < 0.0f ? -1.0f : 1.0f;

                anchorDistance = candidateDistance;
                anchorDepth = triDepth;
                anchorID = objectID;
                anchorPayload.slot1 = float4(p0 * anchorBary.x + p1 * anchorBary.y + p2 * anchorBary.z, objectMode);
                anchorPayload.slot2 = float4((float)firstIndex, (float)indexBase, (float)tri, (float)(indexBase / 3u));
                anchorPayload.slot3 = float4((float)i0, (float)i1, (float)i2, (float)nearestSlot);
                anchorPayload.slot4 = float4(anchorBary, anchorInside);
                anchorPayload.slot5 = float4(faceNormal, winding);
                anchorPayload.slot6 = float4(SelectVertexPosition(nearestSlot, p0, p1, p2), nearestDistSq);
                anchorPayload.slot7 = float4(RZM_DETECT_LAYOUT_VERSION, (float)objectIndex, (float)objectCount, objectMode == 7.0f ? anchorZone : -1.0f);
            }
        }
    }
}

[numthreads(THREADS_PER_GROUP, 1, 1)]
void main(uint3 dispatchThreadID : SV_DispatchThreadID,
          uint3 groupThreadID    : SV_GroupThreadID)
{
    uint tid = groupThreadID.x;

    Calibration cal;
    SelectCalibration(cal);

    float bestDepth = 3.402823e+38f;
    float bestID = -1.0f;
    float bestFirstIndex = 0.0f;
    float hitCount = 0.0f;
    HitPayload payload;
    InitPayload(payload);
    float anchorDistance = 3.402823e+38f;
    float anchorDepth = 3.402823e+38f;
    float anchorID = -1.0f;
    HitPayload anchorPayload;
    InitPayload(anchorPayload);

    if (cal.valid > 0.5f && ValidViewportCursor(CURSOR_PARAMS))
    {
        uint objectCount = (uint)min(max(gObjectMap[0u].x, 0.0f), (float)MAX_OBJECTS);
        float2 primaryCursor = CursorUVFromParams(CURSOR_PARAMS);
        float2 altCursor = CursorUVFromParams(ALT_CURSOR_PARAMS);
        bool useAltCursor = NormalizedCursorValid(ALT_CURSOR_PARAMS);
        float2 screenRes = max(CURSOR_PARAMS.zw, float2(1.0f, 1.0f));
        float hitPadPixels = max(DETECT_PARAMS.w, 0.0f);
        float anchorPadPixels = max(DETECT_PARAMS.x, hitPadPixels);
        float padUV = hitPadPixels / max(screenRes.x, screenRes.y);
        float anchorPadUV = anchorPadPixels / max(screenRes.x, screenRes.y);
        bool isClicked = CLICK_PARAMS.x > 0.0f;

        [loop]
        for (uint objectIndex = 0u; objectIndex < objectCount; objectIndex++)
        {
            float4 entry = gObjectMap[1u + objectIndex];
            uint firstIndex = (uint)max(entry.x, 0.0f);
            uint indexCount = (uint)max(entry.y, 0.0f);
            float objectMode = entry.z;
            float objectID = entry.w > 0.0f ? entry.w : (float)firstIndex;

            bool isHoverType = (objectMode <= 3.0f) || (objectMode == 7.0f) || (objectMode == 8.0f);
            bool isClickType = (objectMode >= 4.0f && objectMode <= 6.0f);
            bool shouldProcess = isHoverType || (isClickType && isClicked);

            if (shouldProcess && indexCount >= 3u)
            {
                TestTriangleRange(
                    cal, tid, objectIndex, objectCount,
                    firstIndex, indexCount, objectMode, objectID,
                    primaryCursor, altCursor, useAltCursor, padUV, anchorPadUV,
                    bestDepth, bestID, bestFirstIndex, hitCount,
                    payload, anchorDistance, anchorDepth, anchorID, anchorPayload);
            }
        }
    }

    sBestDepth[tid] = bestDepth;
    sBestID[tid] = bestID;
    sBestFirstIndex[tid] = bestFirstIndex;
    sHitCount[tid] = hitCount;
    sSlot1[tid] = payload.slot1;
    sSlot2[tid] = payload.slot2;
    sSlot3[tid] = payload.slot3;
    sSlot4[tid] = payload.slot4;
    sSlot5[tid] = payload.slot5;
    sSlot6[tid] = payload.slot6;
    sSlot7[tid] = payload.slot7;
    sAnchorDistance[tid] = anchorDistance;
    sAnchorDepth[tid] = anchorDepth;
    sAnchorID[tid] = anchorID;
    sAnchorSlot1[tid] = anchorPayload.slot1;
    sAnchorSlot2[tid] = anchorPayload.slot2;
    sAnchorSlot3[tid] = anchorPayload.slot3;
    sAnchorSlot4[tid] = anchorPayload.slot4;
    sAnchorSlot5[tid] = anchorPayload.slot5;
    sAnchorSlot6[tid] = anchorPayload.slot6;
    sAnchorSlot7[tid] = anchorPayload.slot7;
    GroupMemoryBarrierWithGroupSync();

    for (uint stride = THREADS_PER_GROUP / 2u; stride > 0u; stride >>= 1u)
    {
        if (tid < stride)
        {
            sHitCount[tid] += sHitCount[tid + stride];
            if (sBestDepth[tid + stride] < sBestDepth[tid])
            {
                sBestDepth[tid] = sBestDepth[tid + stride];
                sBestID[tid] = sBestID[tid + stride];
                sBestFirstIndex[tid] = sBestFirstIndex[tid + stride];
                sSlot1[tid] = sSlot1[tid + stride];
                sSlot2[tid] = sSlot2[tid + stride];
                sSlot3[tid] = sSlot3[tid + stride];
                sSlot4[tid] = sSlot4[tid + stride];
                sSlot5[tid] = sSlot5[tid + stride];
                sSlot6[tid] = sSlot6[tid + stride];
                sSlot7[tid] = sSlot7[tid + stride];
            }
            if (sAnchorDistance[tid + stride] < sAnchorDistance[tid] ||
                (abs(sAnchorDistance[tid + stride] - sAnchorDistance[tid]) <= 1e-12f &&
                 sAnchorDepth[tid + stride] < sAnchorDepth[tid]))
            {
                sAnchorDistance[tid] = sAnchorDistance[tid + stride];
                sAnchorDepth[tid] = sAnchorDepth[tid + stride];
                sAnchorID[tid] = sAnchorID[tid + stride];
                sAnchorSlot1[tid] = sAnchorSlot1[tid + stride];
                sAnchorSlot2[tid] = sAnchorSlot2[tid + stride];
                sAnchorSlot3[tid] = sAnchorSlot3[tid + stride];
                sAnchorSlot4[tid] = sAnchorSlot4[tid + stride];
                sAnchorSlot5[tid] = sAnchorSlot5[tid + stride];
                sAnchorSlot6[tid] = sAnchorSlot6[tid + stride];
                sAnchorSlot7[tid] = sAnchorSlot7[tid + stride];
            }
        }
        GroupMemoryBarrierWithGroupSync();
    }

    if (tid == 0u && sBestID[0] >= 0.0f)
    {
        float4 previous = gBestHit[0];
        bool previousInvalid = previous.x < 0.0f || previous.y > 1e30f;

        if (previousInvalid || sBestDepth[0] < previous.y)
        {
            gBestHit[1] = sSlot1[0];
            gBestHit[2] = sSlot2[0];
            gBestHit[3] = sSlot3[0];
            gBestHit[4] = sSlot4[0];
            gBestHit[5] = sSlot5[0];
            gBestHit[6] = sSlot6[0];
            gBestHit[7] = sSlot7[0];
            uint i0 = (uint)max(sSlot3[0].x, 0.0f);
            uint i1 = (uint)max(sSlot3[0].y, 0.0f);
            uint i2 = (uint)max(sSlot3[0].z, 0.0f);
            ScreenBasis basis = BuildScreenBasis(
                cal, sSlot1[0].xyz,
                gVB0[i0].position, gVB0[i1].position, gVB0[i2].position);
            gBestHit[8] = float4(basis.rightLocal, basis.valid);
            gBestHit[9] = float4(basis.downLocal, basis.conditioning);
            BuildGizmoAxes(cal, sSlot1[0].xyz,
                gVB0[i0].position, gVB0[i1].position, gVB0[i2].position,
                gVB0[i0].normal, gVB0[i1].normal, gVB0[i2].normal,
                gBestHit[11], gBestHit[12]);
            gBestHit[13] = float4(COMPONENT_TOKEN, 0.0f, 0.0f, 0.0f);
            gBestHit[14] = BuildDepthPayload(cal, sSlot1[0].xyz);
            gBestHit[0] = float4(sBestID[0], sBestDepth[0], sBestFirstIndex[0], sHitCount[0]);
        }
    }

    // Component-local anchor: nearest eligible surface to the same cursor.
    // Its slot-0 depth stores screen distance, not clip depth, so each VB0
    // keeps a valid local hit/basis instead of borrowing another component's.
    if (tid == 0u && sAnchorID[0] >= 0.0f)
    {
        float4 previous = gComponentHit[0];
        bool previousInvalid = previous.x < 0.0f || previous.y > 1e30f || previous.w < 0.5f;
        if (previousInvalid || sAnchorDistance[0] < previous.y)
        {
            gComponentHit[1] = sAnchorSlot1[0];
            gComponentHit[2] = sAnchorSlot2[0];
            gComponentHit[3] = sAnchorSlot3[0];
            gComponentHit[4] = sAnchorSlot4[0];
            gComponentHit[5] = sAnchorSlot5[0];
            gComponentHit[6] = sAnchorSlot6[0];
            gComponentHit[7] = sAnchorSlot7[0];
            uint i0 = (uint)max(sAnchorSlot3[0].x, 0.0f);
            uint i1 = (uint)max(sAnchorSlot3[0].y, 0.0f);
            uint i2 = (uint)max(sAnchorSlot3[0].z, 0.0f);
            ScreenBasis basis = BuildScreenBasis(
                cal, sAnchorSlot1[0].xyz,
                gVB0[i0].position, gVB0[i1].position, gVB0[i2].position);
            gComponentHit[8] = float4(basis.rightLocal, basis.valid);
            gComponentHit[9] = float4(basis.downLocal, basis.conditioning);
            BuildGizmoAxes(cal, sAnchorSlot1[0].xyz,
                gVB0[i0].position, gVB0[i1].position, gVB0[i2].position,
                gVB0[i0].normal, gVB0[i1].normal, gVB0[i2].normal,
                gComponentHit[11], gComponentHit[12]);
            gComponentHit[13] = float4(COMPONENT_TOKEN, 0.0f, 0.0f, 0.0f);
            gComponentHit[14] = BuildDepthPayload(cal, sAnchorSlot1[0].xyz);
            gComponentHit[0] = float4(sAnchorID[0], sAnchorDistance[0], sAnchorSlot2[0].x, 1.0f);
        }
    }

    // Compact per-dispatch evidence; never dumps VB0 itself.  The owning INI
    // names the component, while this record preserves the candidate,
    // calibration and dimensions that produced it.
    if (tid == 0u && DEBUG_PARAMS.x > 0.5f)
    {
        uint debugCount;
        gDebug.GetDimensions(debugCount);
        if (debugCount >= 23u)
        {
            uint vertexCount, vertexStride;
            uint indexCount;
            uint objectMapCount;
            gVB0.GetDimensions(vertexCount, vertexStride);
            gIndexBuffer.GetDimensions(indexCount);
            gObjectMap.GetDimensions(objectMapCount);

            gDebug[0u]  = float4(RZM_DETECT_LAYOUT_VERSION, cal.valid, (float)vertexCount, (float)indexCount);
            gDebug[1u]  = float4(sBestID[0], sBestDepth[0], sBestFirstIndex[0], sHitCount[0]);
            gDebug[2u]  = sSlot1[0];
            gDebug[3u]  = sSlot2[0];
            gDebug[4u]  = sSlot3[0];
            gDebug[5u]  = sSlot4[0];
            gDebug[6u]  = sSlot5[0];
            gDebug[7u]  = sSlot6[0];
            gDebug[8u]  = sSlot7[0];
            gDebug[9u]  = float4(cal.p0, cal.invDet);
            gDebug[10u] = float4(cal.p1, cal.valid);
            gDebug[11u] = float4(cal.p2, (float)objectMapCount);
            gDebug[12u] = float4(cal.p3, 0.0f);
            gDebug[13u] = cal.c0;
            gDebug[14u] = cal.c1;
            gDebug[15u] = cal.c2;
            gDebug[16u] = cal.c3;
            gDebug[17u] = float4(CURSOR_PARAMS.xy, DETECT_PARAMS.w, CLICK_PARAMS.x);
            gDebug[18u] = gBestHit[0u];
            gDebug[19u] = float4((float)vertexStride, 0.0f, 0.0f, 0.0f);
            gDebug[20u] = CURSOR_PARAMS;
            gDebug[21u] = VIEWPORT_XFORM;
            gDebug[22u] = float4(VIEWPORT_VALID, ValidViewportCursor(CURSOR_PARAMS), 0.0f, 0.0f);
        }
    }
}


