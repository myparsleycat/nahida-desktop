// Generic material of the native model viewer: three.js r186's MeshStandardMaterial under the React viewer's
// ambient, hemisphere and two directional lights, its RoomEnvironment image-based light and tone mapping,
// with the texture decoding the React viewer applied to DDS maps. Toon Shadows replace it with the toon shading
// of the model's material profile, as the React viewer's gimi.ts, srmi.ts, zzmi.ts and wwmi.ts ported HoyoToon's
// Genshin Impact, Star Rail, Zenless Zone Zero and Wuthering Waves shaders and efmi.ts reconstructed Endfield's,
// with their outline hulls and a shadow map.
#pragma pack_matrix(row_major)

static const float PI = 3.14159265359;

cbuffer Frame : register(b0) {
    float4x4 viewProj;
    float4x4 world;
    float4 eye;
    float4 keyLight;    // xyz: direction towards the light, w: intensity
    float4 fillLight;
    float4 skyLight;    // rgb: hemisphere sky colour times intensity
    float4 groundLight;
    float4 shading;     // x: ambient intensity, y: environment intensity, z: exposure, w: tone mapping
    float4 surface;     // x: metalness, y: roughness
    float4 envSH[9];    // x: radiance SH coefficient
    float4 envLevels[11]; // xyz: share of SH bands 0-2 kept at each PMREM roughness level
    float4x4 view;
    float4x4 proj;
    float4x4 shadowViewProj;
    float4 toonLight;   // xyz: direction towards the key light, w: TOON_* shading, 0 without toon
    float4 toonShadow;  // x: 1 while receiving the shadow map, y: depth bias, z: normal bias, w: map size
    float4 gimiShade;   // x: daytime, y: light area, z: shadow brightness, w: brightness
    float4 gimiExtra;   // x: specular, y: rim strength, z: emission, w: tone curve
    float4 gimiOutline; // x: outline width, y: 2.414 tan(fov / 2), z: face light map, w: face shadow
    float4 viewport;    // xy: size in pixels
};

cbuffer Mesh : register(b1) {
    float4 tint;
    float4 faceForward; // xyz: mean normal of a face mesh in model space
    uint flags;
    uint normalMode;
    uint2 meshPad;
};

static const uint FLAG_DIFFUSE = 1;
static const uint FLAG_DIFFUSE_SRGB = 2;
static const uint FLAG_MAP_BC4 = 4;
static const uint FLAG_MAP_SIGNED = 8;
static const uint FLAG_INVERT_ALPHA = 16;
static const uint FLAG_COVERAGE = 32;
static const uint FLAG_NORMAL_MAP = 64;
static const uint FLAG_CUTOUT = 128;
static const uint FLAG_VERTEX_TANGENT = 256;
static const uint FLAG_FACE = 512;
static const uint FLAG_VERTEX_COLOR = 1024;
static const uint FLAG_EMISSION = 2048;
static const uint FLAG_LIGHT_MAP = 4096;
// The body shape editor's heatmap: the vertex colour, stored sRGB-encoded, is the albedo.
static const uint FLAG_OVERLAY_COLOR = 8192;
static const uint FLAG_HAIR = 16384;
static const uint FLAG_EYE = 32768;
static const uint FLAG_UV1 = 65536;
static const uint FLAG_MATERIAL_MAP = 131072;
static const uint FLAG_DATA_MAP = 262144;
static const uint FLAG_HIGHLIGHT_MAP = 524288;
static const uint FLAG_RAMP_MAP = 1048576;
// The light map is a direct BC4 or signed texture, as FLAG_MAP_BC4 and FLAG_MAP_SIGNED mark the diffuse.
static const uint FLAG_LIGHT_BC4 = 2097152;
static const uint FLAG_LIGHT_SIGNED = 4194304;
// The light map has the channels of WWMI's packed mask, not one or two.
static const uint FLAG_PACKED_MASK = 8388608;
static const uint FLAG_OUTLINE_MASK = 16777216;

// The body shape editor's brush cursor: a circle around centre spanned by u and v, in model space.
cbuffer Ring : register(b2) {
    float4 ringCenter;
    float4 ringU;
    float4 ringV;
    float4 ringColor; // premultiplied, display-referred
};

static const uint RING_SEGMENTS = 64;

static const uint TOON_GIMI = 1;
static const uint TOON_SRMI = 2;
static const uint TOON_ZZMI = 3;
static const uint TOON_WWMI = 4;
static const uint TOON_EFMI = 5;

uint toonMode() { return (uint)(toonLight.w + 0.5); }

static const uint TONE_NEUTRAL = 0;
static const uint TONE_ACES = 1;

Texture2D diffuseMap : register(t0);
Texture2D normalMap : register(t1);
Texture2D cutoutMap : register(t2);
Texture2D<float2> dfgLUT : register(t3);
Texture2D lightMap : register(t4);
// Ramps and the metal map run from the top as images do, so they are read at 1 - v of the React viewer's
// bottom-up coordinates.
Texture2D rampMap : register(t5);
Texture2D metalMap : register(t6);
Texture2D faceLightMap : register(t7);
Texture2D faceShadowMap : register(t8);
Texture2D<float> shadowMap : register(t9);
// The material map, the normal map read as shading data and EFMI's face highlight; the shaders that read them
// check their flags, as the zero views of missing maps sample black.
Texture2D materialMap : register(t10);
Texture2D dataMap : register(t11);
Texture2D highlightMap : register(t12);
// EFMI's outline width mask, read by the outline hull's vertex shader.
Texture2D outlineWidthMap : register(t13);
SamplerState linearWrap : register(s0);
SamplerState linearClamp : register(s1);
SamplerComparisonState shadowCompare : register(s2);

struct VSIn {
    float3 pos : POSITION;
    float3 normal : NORMAL;
    float2 uv : TEXCOORD0;
    float2 uv1 : TEXCOORD2; // the second UV set, or the first where the mesh has none
    float4 tangent : TANGENT;
    float3 outline : TEXCOORD1; // direction of the outline hull
    float4 color : COLOR;
};

struct VSOut {
    float4 pos : SV_Position;
    float3 worldPos : POSITION1;
    float3 normal : NORMAL;
    float2 uv : TEXCOORD0;
    float2 uv1 : TEXCOORD2;
    float4 tangent : TANGENT;
    float3 bitangent : BINORMAL;
    float4 color : COLOR;
    float4 shadowCoord : TEXCOORD1;
};

// The mesh builder flips V for WebGL's bottom-up rows; Direct3D rows run top-down.
float2 textureUV(float2 uv) { return float2(uv.x, 1 - uv.y); }

VSOut vs_main(VSIn i) {
    VSOut o;
    float4 worldPos = mul(world, float4(i.pos, 1));
    o.pos = mul(viewProj, worldPos);
    o.worldPos = worldPos.xyz;
    o.normal = mul((float3x3)world, i.normal);
    o.uv = textureUV(i.uv);
    o.uv1 = textureUV(i.uv1);
    float3 tangent = mul((float3x3)world, i.tangent.xyz);
    o.tangent = float4(tangent, i.tangent.w);
    o.bitangent = cross(o.normal, tangent) * i.tangent.w;
    o.color = i.color;
    float3 normal = dot(o.normal, o.normal) > 0 ? normalize(o.normal) : 0;
    o.shadowCoord = mul(shadowViewProj, float4(worldPos.xyz + normal * toonShadow.z, 1));
    return o;
}

// The depth of the shadow map, seen from the key light.
float4 vs_shadow(VSIn i) : SV_Position {
    return mul(shadowViewProj, mul(world, float4(i.pos, 1)));
}

float3 srgbToLinear(float3 c) {
    return c <= 0.04045 ? c / 12.92 : pow((c + 0.055) / 1.055, 2.4);
}

float3 linearToSrgb(float3 c) {
    c = saturate(c);
    return c <= 0.0031308 ? c * 12.92 : 1.055 * pow(c, 1 / 2.4) - 0.055;
}

// Perturbs n by a tangent-space normal using a frame derived from screen-space derivatives, as three.js
// perturbNormal2Arb does; vertex tangents of game meshes often hold outline normals instead. face flips the
// frame of back faces back, as three.js does for double-sided materials.
float3 perturbNormal(float3 n, float face, float3 worldPos, float2 uv, float3 mapN) {
    // ddy runs down the screen; the frame below expects it to run up, as in GLSL.
    float3 q0 = ddx(worldPos);
    float3 q1 = -ddy(worldPos);
    float2 st0 = ddx(uv);
    float2 st1 = -ddy(uv);
    float3 q1perp = cross(q1, n);
    float3 q0perp = cross(n, q0);
    float3 t = q1perp * st0.x + q0perp * st1.x;
    float3 b = q1perp * st0.y + q0perp * st1.y;
    float det = max(dot(t, t), dot(b, b));
    float scale = det == 0 ? 0 : rsqrt(det) * face;
    return normalize(t * (mapN.x * scale) + b * (mapN.y * scale) + n * mapN.z);
}

// Perturbs n with the vertex tangent frame, as three.js does whenever the geometry has tangents.
float3 perturbNormalTangent(float3 n, float face, VSOut i, float3 mapN) {
    float3 t = normalize(i.tangent.xyz) * face;
    float3 b = normalize(i.bitangent) * face;
    return normalize(t * mapN.x + b * mapN.y + n * mapN.z);
}

float3 sampleNormal(float2 uv) {
    float3 texel = normalMap.Sample(linearWrap, uv).xyz;
    float3 n = texel * 2 - 1;
    if (normalMode == 2) n = texel;
    if (normalMode == 3) n.xy = n.xx;
    if (normalMode == 4) n.xy = texel.xx;
    if (normalMode > 0) n.z = sqrt(max(0, 1 - dot(n.xy, n.xy)));
    n.y = -n.y;
    return n;
}

// environmentRadiance is the PMREM environment towards d at a roughness, from its SH: three.js maps the
// roughness to a mip of the cube-UV atlas, and mip m holds PMREM level 8 - m.
float environmentRadiance(float3 d, float roughness) {
    float mip;
    if (roughness >= 0.8) mip = (1.0 - roughness) * 1.0 / 0.2 - 2.0;
    else if (roughness >= 0.4) mip = (0.8 - roughness) * 3.0 / 0.4 - 1.0;
    else if (roughness >= 0.305) mip = (0.4 - roughness) * 1.0 / 0.095 + 2.0;
    else if (roughness >= 0.21) mip = (0.305 - roughness) * 1.0 / 0.095 + 3.0;
    else mip = -2.0 * log2(1.16 * roughness);
    float level = clamp(8.0 - mip, 0.0, 10.0);
    uint lower = (uint)floor(level);
    float3 bands = lerp(envLevels[lower].xyz, envLevels[min(lower + 1, 10)].xyz, level - lower);

    float x = d.x, y = d.y, z = d.z;
    float radiance = envSH[0].x * 0.282095 * bands.x;
    radiance += (envSH[1].x * y + envSH[2].x * z + envSH[3].x * x) * 0.488603 * bands.y;
    radiance += (envSH[4].x * 1.092548 * x * y + envSH[5].x * 1.092548 * y * z +
                 envSH[6].x * 0.315392 * (3 * z * z - 1) + envSH[7].x * 1.092548 * x * z +
                 envSH[8].x * 0.546274 * (x * x - y * y)) * bands.z;
    return max(radiance, 0) * shading.y;
}

float3 fresnelSchlick(float3 f0, float dotVH) {
    float fresnel = exp2((-5.55473 * dotVH - 6.98316) * dotVH);
    return f0 * (1 - fresnel) + fresnel;
}

float3 brdfGGX(float3 l, float3 v, float3 n, float3 f0, float roughness) {
    float alpha = roughness * roughness;
    float a2 = alpha * alpha;
    float3 h = normalize(l + v);
    float dotNL = saturate(dot(n, l));
    float dotNV = saturate(dot(n, v));
    float dotNH = saturate(dot(n, h));
    float dotVH = saturate(dot(v, h));
    float gv = dotNL * sqrt(a2 + (1 - a2) * dotNV * dotNV);
    float gl = dotNV * sqrt(a2 + (1 - a2) * dotNL * dotNL);
    float visibility = 0.5 / max(gv + gl, 1e-6);
    float denom = dotNH * dotNH * (a2 - 1) + 1;
    float distribution = a2 / (PI * denom * denom);
    return fresnelSchlick(f0, dotVH) * visibility * distribution;
}

void multiscattering(float2 dfg, float3 f0, out float3 single, out float3 multi) {
    float3 fssEss = f0 * dfg.x + dfg.y;
    float ems = 1 - (dfg.x + dfg.y);
    float3 favg = f0 + (1 - f0) * 0.047619;
    single = fssEss;
    multi = fssEss * favg / (1 - ems * favg) * ems;
}

float3 directLight(float4 light, float3 n, float3 v, float3 diffuse, float3 f0, float3 compensation,
                   float roughness) {
    float3 l = light.xyz;
    float3 irradiance = saturate(dot(n, l)) * light.w;
    float3 specular = irradiance * brdfGGX(l, v, n, f0, roughness) * compensation;
    float dotVH = saturate(dot(v, normalize(l + v)));
    return specular + irradiance * diffuse / PI * (1 - fresnelSchlick(0.04, dotVH));
}

float3 neutralToneMapping(float3 color) {
    const float startCompression = 0.8 - 0.04;
    const float desaturation = 0.15;
    float x = min(color.r, min(color.g, color.b));
    float offset = x < 0.08 ? x - 6.25 * x * x : 0.04;
    color -= offset;
    float peak = max(color.r, max(color.g, color.b));
    if (peak < startCompression) return color;
    float d = 1 - startCompression;
    float newPeak = 1 - d * d / (peak + d - startCompression);
    color *= newPeak / peak;
    float g = 1 - 1 / (desaturation * (peak - newPeak) + 1);
    return lerp(color, newPeak, g);
}

float3 acesFilmicToneMapping(float3 color) {
    color /= 0.6;
    color = float3(0.59719, 0.07600, 0.02840) * color.r + float3(0.35458, 0.90834, 0.13383) * color.g +
            float3(0.04823, 0.01566, 0.83777) * color.b;
    float3 a = color * (color + 0.0245786) - 0.000090537;
    float3 b = color * (0.983729 * color + 0.4329510) + 0.238081;
    color = a / b;
    color = float3(1.60475, -0.10208, -0.00327) * color.r + float3(-0.53108, 1.10813, -0.07276) * color.g +
            float3(-0.07367, -0.00605, 1.07602) * color.b;
    return saturate(color);
}

// A step whose edge spans one screen pixel, so toon boundaries do not alias.
float toonStep(float edge, float x) {
    float width = max(fwidth(x), 1e-4);
    return smoothstep(edge - width, edge + width, x);
}

// The game's rims compare scene depth a few pixels outward along the normal. Without a depth texture this is
// 1 within a band of that many pixels inside the silhouette, measured as N.V over its screen derivative.
float toonSilhouetteBand(float NoV, float pixels) {
    float distance = NoV / max(fwidth(NoV), 1e-4);
    return 1 - smoothstep(pixels - 1, pixels, distance);
}

// Facing of the light in the horizontal plane, 1 when it shines from the front, and whether it comes from
// the face's right, where the SDF is read mirrored.
float2 toonFaceLight(float3 forward3, float3 light3) {
    float2 forward = dot(forward3, forward3) > 1e-8 ? normalize(forward3).xz : float2(0, 1);
    float2 light = dot(light3, light3) > 1e-8 ? normalize(light3).xz : forward;
    return float2(dot(forward, light), forward.y * light.x - forward.x * light.y >= 0 ? 1 : 0);
}

// three.js's PCF shadow: five comparisons on a Vogel disk of one texel, turned by interleaved gradient noise.
float shadowOcclusion(float4 coord, float2 pixel) {
    coord.xyz /= coord.w;
    float2 uv = float2(coord.x * 0.5 + 0.5, 0.5 - coord.y * 0.5);
    float depth = coord.z + toonShadow.y;
    if (any(uv < 0) || any(uv > 1) || depth > 1) return 1;
    float radius = 1 / toonShadow.w;
    float phi = frac(52.9829189 * frac(dot(pixel, float2(0.06711056, 0.00583715)))) * 2 * PI;
    float shadow = 0;
    [unroll] for (int k = 0; k < 5; k++) {
        float r = sqrt((k + 0.5) / 5);
        float theta = k * 2.399963229728653 + phi;
        shadow += shadowMap.SampleCmpLevelZero(shadowCompare, uv + float2(cos(theta), sin(theta)) * r * radius, depth);
    }
    return shadow * 0.2;
}

float genshinMaterialRow(float id) {
    if (id < 0.2) return 4;
    if (id < 0.4) return 1;
    if (id < 0.6) return 2;
    if (id < 0.8) return 0;
    return 3;
}

// HoyoToon's Genshin post curve: a bloom-like lift above 0.6, a rational filmic curve, then the game's white
// balance.
float3 genshinToneMap(float3 x) {
    x += max(x - 0.6, 0) * 0.7;
    x = saturate(((1.36 * x + 0.047) * x) / ((0.93 * x + 0.56) * x + 0.14));
    return float3(dot(float3(1.0, 0.021, -0.019), x), dot(float3(0.001, 1.04, 0.01), x), 0.951 * x.b);
}

// gimiToon is HoyoToon's ramp shading: the light map's AO and the half-Lambert give a shadow area, whose
// distance below the light area picks a column of the shadow ramp; the light map's alpha picks its row.
float3 gimiToon(VSOut i, float3 n, float3 v, float3 albedo, float emissionMask) {
    float3 l = toonLight.xyz;
    float4 lightTexel = lightMap.Sample(linearWrap, i.uv);
    float4 vertexColor = (flags & FLAG_VERTEX_COLOR) != 0 ? i.color : float4(1, 0.5, 1, 0.5);
    float face = (flags & FLAG_FACE) != 0 ? 1 : 0;
    float lightArea = gimiShade.y;
    float NoL = dot(n, l);
    float row = genshinMaterialRow(lightTexel.a);

    // The light map's AO is pushed away from 0.5 and scaled by the vertex AO; near 0 or 1 it forces shadow or
    // light.
    float aoOffset = lightTexel.g - 0.5;
    float threshold = (2 * aoOffset * abs(aoOffset) + 0.5) * vertexColor.r;
    float area = threshold > 0.95 ? 1 : (threshold < 0.05 ? 0 : (NoL * 0.5 + 0.5 + threshold) * 0.5);
    float lit = toonStep(lightArea, area);
    float rampU = 1 - min(max(lightArea - area, 0) / lightArea, 1);

    if (face > 0.5) {
        row = 3;
        float2 faceLight = toonFaceLight(mul((float3x3)world, faceForward.xyz), l);
        if (gimiOutline.z > 0.5) {
            float2 faceUV = i.uv;
            if (faceLight.y > 0.5) faceUV.x = 1 - faceUV.x;
            float faceThreshold = smoothstep(0, 1, 0.5 - 0.5 * faceLight.x);
            lit = toonStep(faceThreshold, faceLightMap.SampleLevel(linearClamp, faceUV, 0).a);
            lit = lerp(lit, 1, faceShadowMap.SampleLevel(linearClamp, i.uv, 0).a * gimiOutline.w);
        } else {
            lit = smoothstep(-0.35, -0.15, faceLight.x);
        }
        rampU = 0;
    }

    // The cast shadow darkens like the ramp's deepest column.
    float occlusion = toonShadow.x > 0.5 ? shadowOcclusion(i.shadowCoord, i.pos.xy) : 1;
    lit *= occlusion;
    rampU *= occlusion;
    float rampV = (row + 5 * gimiShade.x + 0.5) / 10;
    float3 ramp = rampMap.SampleLevel(linearClamp, float2(clamp(rampU, 0.004, 0.996), 1 - rampV), 0).rgb;
    float3 shade = ramp * lerp(gimiShade.z, 1, smoothstep(0.9, 1, rampU));
    shade = lerp(shade, 1, lit);

    // Light map red at 0.9 and above marks metal, drawn from a view-space matcap with a sharp highlight, both
    // dimmed in shadow.
    float3 halfDirection = normalize(l + v);
    float NoH = max(dot(n, halfDirection), 0.001);
    float metal = step(0.89, lightTexel.r);
    float2 matcap = normalize(mul((float3x3)view, n)).xy * 0.5 + 0.5;
    float metalTexel = saturate(metalMap.SampleLevel(linearClamp, float2(matcap.x, 1 - matcap.y), 0).r * 3);
    float metalSpecular = saturate(pow(NoH, 90) * 15) * lightTexel.r * lerp(0.2, 1, lit);
    float3 metalColor = albedo * lerp(float3(0.51, 0.3, 0.19), 1, metalTexel) + 0.5 * metalSpecular;
    metalColor *= lerp(float3(0.78, 0.77, 0.82), 1, lit);

    // Other materials get a hard Blinn-Phong highlight past the light map's blue, tinted 10% toward the surface
    // colour.
    float term = pow(NoH, 10);
    float specular = toonStep(1.015 - lightTexel.b, term) * term * lightTexel.r * gimiExtra.x * 0.5;
    specular *= (1 - metal) * (1 - face);

    float3 surface = lerp(albedo, metalColor, metal * (1 - face));
    float3 toon = (surface * shade + (0.9 + 0.1 * surface) * specular) * gimiShade.w;

    // The 4.0+ rim: a thin band inside the silhouette, brightest where the normal turns away from the
    // camera-to-light direction, fading with distance.
    float NoV = saturate(dot(n, v));
    float3 rimDirection = v - l;
    rimDirection = dot(rimDirection, rimDirection) > 1e-6 ? normalize(rimDirection) : v;
    float rimFacing = dot(n, rimDirection);
    float rimLight = saturate(pow(max(1 - rimFacing, 0), 7.5)) + saturate(pow(max(rimFacing, 0), 7.5));
    float viewDepth = abs(mul(view, float4(i.worldPos, 1)).z) * gimiOutline.y;
    float rimBand = toonSilhouetteBand(NoV, 2.5) * saturate(1 - viewDepth / 5);
    toon += saturate(albedo * 5 * rimLight * rimBand) * gimiExtra.y * (1 - face);

    if ((flags & FLAG_EMISSION) != 0) toon += albedo * emissionMask * gimiExtra.z;
    return gimiExtra.w > 0.5 ? genshinToneMap(toon) : toon;
}

// srmiToon is HoyoToon's Star Rail shading with one shadow tint for every material, as the game's ramps are per
// character and not in mods. The light map holds the rim mask in R, AO in G, the specular threshold in B and the
// material in A; the face reads its SDF from the alpha of its own light map, the game's FaceMap.
float3 srmiToon(VSOut i, float3 n, float3 v, float3 albedo) {
    float3 l = toonLight.xyz;
    float4 lightTexel = lightMap.Sample(linearWrap, i.uv);
    float4 vertexColor = (flags & FLAG_VERTEX_COLOR) != 0 ? i.color : float4(1, 0.5, 0, 0.5);
    float face = (flags & FLAG_FACE) != 0 ? 1 : 0;
    float occlusion = toonShadow.x > 0.5 ? shadowOcclusion(i.shadowCoord, i.pos.xy) : 1;

    // HoyoToon's shadow rate: half-Lambert with the cast shadow, times twice the light map AO and the vertex AO,
    // doubled again by the two-component dot, mapped into 0.15 to 1. The shadow turns lit where the unmapped
    // product reaches 0.5.
    float NoL = dot(n, l);
    float rate = min(1, (NoL * 0.5 + 0.5) * occlusion * 4 * lightTexel.g * vertexColor.r);
    rate = max(0.001, rate) * 0.85 + 0.15;
    float lit = toonStep(0.575, rate);
    float band = smoothstep(0.405, 0.53, rate) * (1 - lit);

    if (face > 0.5) {
        float2 faceLight = toonFaceLight(mul((float3x3)world, faceForward.xyz), l);
        if ((flags & FLAG_LIGHT_MAP) != 0) {
            float2 faceUV = i.uv;
            if (faceLight.y > 0.5) faceUV.x = 1 - faceUV.x;
            float threshold = lightMap.Sample(linearWrap, faceUV).a;
            lit = toonStep(1 - threshold, faceLight.x * 0.5 + 0.5);
        } else {
            lit = smoothstep(-0.35, -0.15, faceLight.x);
        }
        lit *= occlusion;
        band = 0;
    }

    // A darker band right before the terminator, like the game's ramps.
    const float3 shadowColor = float3(0.85, 0.74, 0.78);
    float3 shade = lerp(lerp(shadowColor, shadowColor * shadowColor, band), 1, lit);

    // A Blinn-Phong highlight past the light map's blue, faded in over the roughness and scaled by the shadow
    // rate, with HoyoToon's default shininess, roughness and intensity.
    float3 halfDirection = normalize(l + v);
    float NoV = saturate(dot(n, v));
    float term = rate * pow(max(0.01, dot(n, halfDirection)), 10);
    float softness = max(0.02, fwidth(term));
    float edge = 1 - lightTexel.b - softness;
    float specular = smoothstep(0, 1, (term - edge) / (2 * softness)) * 0.5 * 0.35 * (1 - face);

    // The depth rim, masked by the light map's red and fading with distance.
    float rim = toonSilhouetteBand(NoV, 2) * lightTexel.r;
    rim *= 0.1 * max(0.5, saturate(1 / length(eye.xyz - i.worldPos))) * (1 - face);

    return albedo * shade + specular + rim;
}

float toonOcclusion(VSOut i) {
    return toonShadow.x > 0.5 ? shadowOcclusion(i.shadowCoord, i.pos.xy) : 1;
}

// HoyoToon's ZZZ material defaults; the game's per-material values come from material files that mods do not
// ship. Metallic is 1 so the light map's green marks metal, as the game's metal materials set it.
static const float ZZMI_ALBEDO_SMOOTHNESS = 0.05;
static const float ZZMI_METALLIC = 1;
static const float ZZMI_GLOSSINESS = 0.5;
static const float ZZMI_SPECULAR_INTENSITY = 0.1;
static const float ZZMI_TOON_SPECULAR = 0.01;
static const float ZZMI_SHAPE_SOFTNESS = 0.1;
// Neutral texels: the light map holds the material ID in R, metallic in G and the specular mask in B; the material
// map opacity in R, smoothness in G and the emission mask in B; the normal map the normal in RG and the shadow bias
// in B, and on the face the SDF in R and the chin AO in A. The vertex colour holds the outline width in R and face
// flags in B.
static const float4 ZZMI_NEUTRAL_LIGHT = float4(178.0 / 255, 0, 0, 1);
static const float4 ZZMI_NEUTRAL_MATERIAL = float4(1, 0, 0, 1);
static const float4 ZZMI_NEUTRAL_DATA = float4(0.5, 0.5, 0.5, 1);
static const float4 ZZMI_NEUTRAL_COLOR = float4(0.5, 0.5, 0, 1);

// HoyoToon's colour normalization: pulls a shadow colour toward its own hue at full brightness.
float3 zzmiNormalizeColor(float3 color) {
    color += 6e-5;
    float average = (color.r + color.g + color.b) / 3;
    return saturate(color / average) * 0.56275 + color * 0.43725;
}

// HoyoToon's body shadow: the shadow value runs from -3 to 3 and splits into deep shadow, shadow, shallow,
// transition, subsurface, front and lit bands, each with its tint.
float3 zzmiShadowTint(float shade) {
    const float smoothness = ZZMI_ALBEDO_SMOOTHNESS;
    float k = 3 / (2 - 3 * smoothness);
    float shallowEdge = 0.5 * smoothness - 0.3333;
    float litEdge = 1.5 * smoothness + 0.3333;
    float p = (shade + 0.3333) / smoothness + 0.5;
    float q = (shade - 0.3333) / smoothness + 0.5;
    float r = q - 1;
    float deep = saturate(1 - (shade + 1) * k);
    float shadow = saturate(min(1 - p, (shade + 1) * k));
    float shallow = saturate(min(p, 1 - (shade - shallowEdge) * k));
    float transition = saturate(min(1 - q, (shade - shallowEdge) * k));
    float middle = saturate(min(1 - r, q));
    float front = saturate(min(r, 1 - (shade - litEdge) * k));
    float lit = saturate((shade - litEdge) * k);
    float3 shadowColor = zzmiNormalizeColor(0.6);
    float3 shallowColor = zzmiNormalizeColor(0.8);
    float3 tint = float3(0.850980, 0.780392, 0.772549) * shadowColor * (deep + shadow) +
                  float3(0.874510, 0.8, 0.792157) * shallowColor * shallow +
                  float3(0.956863, 0.960784, 0.901961) * shallowColor * transition +
                  float3(1.0, 0.996078, 0.929412) * front + float3(1.0, 0.948171, 0.929412) * middle + lit;
    return saturate(tint);
}

// HoyoToon's toon GGX on the shaded colour, or on hair a highlight band that the specular mask cuts from the
// half-Lambert of the half vector.
float3 zzmiSpecular(float3 color, float3 n, float3 l, float3 v, float4 lightTexel, float smoothness, float shade,
                    bool hair) {
    float NoL = dot(n, l);
    float lambert = saturate(NoL);
    float luminance = dot(color, float3(0.29, 0.6, 0.11)) * 0.2875 + 1.4375;
    float3 contrast = pow(color, lerp(luminance, 1, 0.5 * (NoL * 0.5 + 0.5) + 0.5 * lambert));
    float3 surface = lerp(color, contrast, 0.5);
    surface = lerp(surface, contrast, lambert);
    float3 f0 = lerp(0.04, surface, ZZMI_METALLIC * lightTexel.g);

    float roughness = 1 - smoothness * ZZMI_GLOSSINESS;
    float a = roughness * roughness;
    float3 halfDirection = normalize(l + v);
    float NoH = dot(n, halfDirection);
    float mask = lightTexel.b;
    if (hair) {
        float band = saturate(NoH * 0.5 + 0.5) * saturate(shade * 1.5 + 0.5);
        mask = saturate((lightTexel.b - (1 - band)) / ZZMI_SHAPE_SOFTNESS);
    }

    float wrappedNoL = saturate(NoL * 0.75 + 0.25);
    float wrappedNoH = saturate(NoH * 0.75 + 0.25);
    float wrappedLoH = saturate(dot(l, halfDirection) * 0.75 + 0.25);
    float LoH2 = max(0.1, wrappedLoH * wrappedLoH);
    float d = wrappedNoH * wrappedNoH * (a * a - 1) + 1.00001;
    float D = (a * a) / (d * d * LoH2 * (4 * a + 2));
    float toon = saturate(D - smoothness * ZZMI_GLOSSINESS) * wrappedNoL / max(1e-5, a);
    toon = saturate(10 * toon * ZZMI_TOON_SPECULAR) * 100;
    float shaped = min(1, 0.16666 / LoH2) * wrappedNoL * 100;
    float lobe = hair ? shaped : toon;
    return lobe * 0.5 * ZZMI_SPECULAR_INTENSITY * 10 * mask * f0 * color;
}

// HoyoToon's fresnel rim: brighter toward the camera-facing light and on upward normals, wider off skin, tinted by
// the sun colour normalized to its brightness.
float3 zzmiRim(float3 n, float3 v, float3 l, bool skin) {
    float frontLight = pow(dot(v, l) * 0.5 + 0.5, 4);
    float upward = lerp(n.y, 1, 0.5) * 0.5 + 0.5;
    upward = saturate(skin ? upward : upward * upward);
    upward = upward * upward * (3 - 2 * upward);
    float upward5 = pow(upward, 5);
    upward *= skin ? 1 - 0.5 * upward5 : 0.9 + 0.1 * upward5;
    float weight = (frontLight * 0.5 + 0.5) * upward;

    float2 fresnel = saturate((1 - dot(v, n) - float2(0.70833, 0.45833)) * float2(5, 3.33333));
    fresnel = fresnel * fresnel * (3 - 2 * fresnel);
    float rim = weight * (skin ? fresnel.x : fresnel.y);
    rim *= 1 + 0.5 * rim * rim;

    float3 sun = float3(1.0, 0.92, 0.9);
    float grey = skin ? dot(sun, 0.33) : dot(sun, float3(0.3, 0.6, 0.1));
    sun = skin ? sun : grey;
    sun = pow(sun, 8);
    sun *= grey / (6.1e-5 + dot(sun, 0.7));
    sun = lerp(sun, 1, pow(frontLight, 20));
    return rim * sun;
}

// zzmiToon is HoyoToon's Zenless Zone Zero shading in its order: the banded shadow tint over the albedo, specular,
// rim, then emission. The face reads its SDF from its normal map, at the second UV set unless the vertex colour's
// face flag says otherwise.
float3 zzmiToon(VSOut i, float3 n, float3 v, float3 albedo) {
    float3 l = toonLight.xyz;
    float4 lightTexel = (flags & FLAG_LIGHT_MAP) != 0 ? lightMap.Sample(linearWrap, i.uv) : ZZMI_NEUTRAL_LIGHT;
    float4 materialTexel = (flags & FLAG_MATERIAL_MAP) != 0 ? materialMap.Sample(linearWrap, i.uv)
                                                              : ZZMI_NEUTRAL_MATERIAL;
    float4 normalData = (flags & FLAG_DATA_MAP) != 0 ? dataMap.Sample(linearWrap, i.uv) : ZZMI_NEUTRAL_DATA;
    bool vertexColors = (flags & FLAG_VERTEX_COLOR) != 0;
    float4 vertexColor = vertexColors ? i.color : ZZMI_NEUTRAL_COLOR;
    bool face = (flags & FLAG_FACE) != 0;
    bool skin = 4 - floor(4.5 * lightTexel.r) < 0.5;
    float occlusion = toonOcclusion(i);
    float NoL = dot(n, l);

    // The normal map's blue biases the shadow: 0.5 is neutral.
    float shade = (normalData.b * occlusion * 2 - 1) * 2 + NoL * occlusion;
    if (face) {
        // The face SDF turns soft past its threshold; the bands sharpen it again.
        float faceFlag = fmod(floor(floor(vertexColor.b * 255 + 0.5) / 16), 2);
        float2 faceUV = vertexColors && (flags & FLAG_UV1) != 0 && faceFlag < 0.5 ? i.uv1 : i.uv;
        float2 faceLight = toonFaceLight(mul((float3x3)world, faceForward.xyz), l);
        if (faceLight.y > 0.5) faceUV.x = 1 - faceUV.x;
        float4 faceMap = (flags & FLAG_DATA_MAP) != 0 ? dataMap.Sample(linearWrap, faceUV) : ZZMI_NEUTRAL_DATA;
        float threshold = 0.5 - 0.5 * faceLight.x;
        float faceAO = faceFlag > 0.5 ? faceMap.a : 1;
        float faceLit = smoothstep(threshold - 0.5, threshold + 0.5, faceMap.r * 0.9 + 0.1) * faceAO;
        shade = (faceLit * 2 - 1) * occlusion;
    }

    float3 color = saturate(albedo * zzmiShadowTint(shade));
    float3 specular = zzmiSpecular(color, n, l, v, lightTexel, materialTexel.g, shade, (flags & FLAG_HAIR) != 0);
    color += face ? 0 : specular * (skin ? 0.1 : 1);

    // HoyoToon's rim covers the band whose depth steps away past the silhouette, on the lit side.
    float NoV = saturate(dot(n, v));
    color += zzmiRim(n, v, l, skin) * 1.5 * toonSilhouetteBand(NoV, 4) * saturate(NoL * 10);

    // The material map's blue above 0.2 marks emission; below it is the matcap mask, and the game's matcaps are not
    // in mods.
    return color + albedo * saturate((materialTexel.b - 0.2) * 1.25);
}

// HoyoToon's WuWa material defaults; the game's shadow ramp and matcaps are not in mods, so the ramp is HoyoToon's
// white default and the matcap is left out.
static const float WWMI_SHADOW_PROCESS = 0.55;
static const float WWMI_SHADOW_WIDTH = 0.01;
static const float3 WWMI_SKIN_SUBSURFACE = float3(0.9387, 0.6038, 0.4072);

// HoyoToon's base shadow: N.L past the front offset steps over the shadow width, is cut by the light map's mask and
// then sharpened, so unmasked shade keeps half the lit colour.
float wwmiShadowArea(float NoL, float mask) {
    float area = smoothstep(WWMI_SHADOW_PROCESS, WWMI_SHADOW_PROCESS + saturate(WWMI_SHADOW_WIDTH + 0.25), NoL + 0.4);
    return saturate(saturate(area * mask) * 4.99999905 + 0.5);
}

// HoyoToon's base specular: a Blinn-Phong lobe of power 1 kept only above 0.1, where the normal map's blue reaches
// 0.5, plus a quarter-strength step where it stays at or below 0.85, tinted by the albedo and removed on metal.
float3 wwmiSpecular(float3 albedo, float3 n, float3 halfDirection, float mask, float4 normalData, float nonSkin) {
    float lobe = max(dot(n, halfDirection), 0);
    lobe = max(lobe * lobe * step(0.1, lobe) * step(0.1, mask), 0.001);
    float first = saturate(lobe * step(0.5, normalData.b));
    float second = step(0.1, saturate(lobe * step(normalData.b, 0.85) * nonSkin));
    float specular = saturate(first + second * 0.25);
    return specular * pow(albedo, lerp(0.5, 2, normalData.b)) * (1 - normalData.a);
}

// wwmiToon is HoyoToon's Wuthering Waves shading in its order: the rim over the albedo, the shadow, then specular.
// The light map holds the shadow mask in G, read from R in one-channel containers, and the face SDF in A; the normal
// map holds the specular mask in B and metal in A. HoyoToon leaves bump mapping off, so n is the interpolated
// normal.
float3 wwmiToon(VSOut i, float3 n, float3 v, float3 albedo) {
    float3 l = toonLight.xyz;
    float4 lightTexel = (flags & FLAG_LIGHT_MAP) != 0 ? lightMap.Sample(linearWrap, i.uv) : 1;
    float4 normalData = (flags & FLAG_DATA_MAP) != 0 ? dataMap.Sample(linearWrap, i.uv) : float4(0.5, 0.5, 1, 1);
    bool packed = (flags & FLAG_PACKED_MASK) != 0;
    float mask = (flags & FLAG_LIGHT_BC4) != 0 ? lightTexel.r : lightTexel.g;
    if ((flags & FLAG_LIGHT_SIGNED) != 0) mask = mask * 0.5 + 0.5;
    float occlusion = toonOcclusion(i);
    float NoL = dot(n, l);
    float shadowArea = wwmiShadowArea(NoL * occlusion, mask);
    float3 skinType = float3(0, 0, 1);
    if ((flags & FLAG_MATERIAL_MAP) != 0) {
        float id = materialMap.Sample(linearWrap, i.uv).r;
        skinType = float3(id > 0.9 ? 1 : 0, id > 0.5 && id <= 0.9 ? 1 : 0, id <= 0.5 ? 1 : 0);
    }
    float3 shadowColor = lerp(lerp(0.5, WWMI_SKIN_SUBSURFACE, skinType.x), 1, 0.3);
    float3 halfDirection = normalize(l + v);
    float3 specular = wwmiSpecular(albedo, n, halfDirection, mask, normalData, skinType.z);

    if ((flags & FLAG_FACE) != 0) {
        float2 faceLight = toonFaceLight(mul((float3x3)world, faceForward.xyz), l);
        float faceLit = smoothstep(-0.35, -0.15, faceLight.x);
        if (packed) {
            float2 faceUV = i.uv;
            if (faceLight.y > 0.5) faceUV.x = 1 - faceUV.x;
            float sdf = lightMap.Sample(linearWrap, faceUV).a;
            float edge = 0.5 - faceLight.x * 0.5;
            faceLit = smoothstep(edge - 0.05, edge + 0.05, sdf) * step(-0.5, faceLight.x);
        }
        shadowArea = saturate(faceLit * occlusion + 0.5);
        shadowColor = lerp(WWMI_SKIN_SUBSURFACE, 1, 0.3);
        specular = 0;
    } else if ((flags & FLAG_HAIR) != 0 && packed) {
        float offset = pow(smoothstep(0.1, 1, lightTexel.g), 0.56) * 0.42;
        float hairShadow = NoL * occlusion * 0.5 + 0.5 + offset;
        shadowArea = smoothstep(WWMI_SHADOW_PROCESS - WWMI_SHADOW_WIDTH, WWMI_SHADOW_PROCESS + WWMI_SHADOW_WIDTH,
                                hairShadow + 0.1) * step(0.05, lightTexel.g);
        shadowColor = lerp(saturate(0.5 + float3(0.090033, 0.168722, 0.193576)), WWMI_SKIN_SUBSURFACE, skinType.x);
        shadowColor = sqrt(sqrt(shadowColor));
        float NoH = saturate(dot(n, halfDirection));
        float highlight = (max(lightTexel.r, 1 - NoH) - (1 - NoH)) / max(0.0001, NoH);
        specular = highlight * NoH * 0.2;
    }

    // HoyoToon's rim covers the band whose depth steps away past the silhouette, on the lit side.
    float NoV = saturate(dot(n, v));
    float3 rim = 0.5 * toonSilhouetteBand(NoV, 4) * smoothstep(0, 0.5, saturate(NoL));
    float3 color = albedo + rim;
    return lerp(color * shadowColor, color, shadowArea) + specular;
}

// EFMI's toon values. Only the switches are settings; Warfarin's brightness default of 2 clips less because its
// materials carry tints that mods lack, and 1.5 approximates its output for the same textures.
static const float EFMI_RANGE_BIAS = -0.4;
static const float EFMI_SOFTNESS = 0.02;
static const float EFMI_SHADOW_BRIGHTNESS = 0.5;
static const float EFMI_SHADOW_SATURATION = 1;
static const float EFMI_AMBIENT = 0.8;
static const float EFMI_LIGHT_STRENGTH = 0.9;
static const float EFMI_BRIGHTNESS = 1.5;
static const float EFMI_SPECULAR = 1;
static const float EFMI_RIM_STRENGTH = 0.5;
static const float EFMI_RIM_WIDTH = 0.3;
static const float EFMI_OUTLINE_WIDTH = 0.25;
static const float EFMI_OUTLINE_BRIGHTNESS = 0.5;
static const float3 EFMI_SHADOW_TINT = float3(0.878, 0.93, 1.122);
static const float3 EFMI_SKIN_SHADOW_TINT = float3(1.187, 0.927, 0.813);
// The game pushes the face's hull back so it cannot poke through the nose and lips: Warfarin reads _OutlineOffsetZ
// 0.236 and scales it by 0.1 in view space.
static const float EFMI_FACE_OUTLINE_DEPTH_OFFSET = 0.0236;

float efmiLuma(float3 color) { return dot(color, float3(0.212673, 0.715152, 0.072175)); }

// Without a mod's _DiffRampMap, the ramp reduces to its coverage step.
float4 efmiRamp(float cosine) {
    float position = clamp(cosine, -1, 1) * 0.5 + 0.5;
    if ((flags & FLAG_RAMP_MAP) != 0) {
        return rampMap.SampleLevel(linearClamp, float2(clamp(position, 0.002, 0.998), 0.5), 0);
    }
    return float4(1, 1, 1, smoothstep(0.5 - EFMI_SOFTNESS - 0.001, 0.5 + EFMI_SOFTNESS + 0.001, position));
}

// The ramp's colour tints the terminator but keeps the luminance.
float3 efmiRampTint(float3 color, float3 ramp) {
    float chroma = max(ramp.r, max(ramp.g, ramp.b)) - min(ramp.r, min(ramp.g, ramp.b));
    float3 tinted = color * lerp(1, ramp, chroma);
    return tinted * clamp(efmiLuma(color) / max(efmiLuma(tinted), 0.001), 0, 1.5);
}

// The deep and shadow tones of an albedo.
void efmiTones(float3 albedo, out float3 shadowTone, out float3 deepTone) {
    shadowTone = lerp(efmiLuma(albedo), albedo, EFMI_SHADOW_SATURATION);
    shadowTone = max(shadowTone, 0) * EFMI_SHADOW_BRIGHTNESS * 0.96 * EFMI_AMBIENT;
    deepTone = 0.65 * shadowTone;
    float deepLuma = efmiLuma(deepTone);
    deepTone = deepLuma + 1.2 * (deepTone - deepLuma);
}

// The lit colour's saturation boost above mid grey, then the rim on the light's side of the screen.
float3 efmiFinish(float3 color, float3 albedo, float3 n, float3 l, float3 cameraBack, float viewCosine, float rim) {
    float luma = efmiLuma(color);
    float vivid = clamp(luma - 0.5, 0, 0.5);
    color = luma + (color - luma) * (vivid * vivid + 1);

    float3 screenLight = l - cameraBack * dot(l, cameraBack);
    float screenLength = length(screenLight);
    float rimSide = screenLength > 0.00001 ? saturate(dot(n, screenLight / screenLength)) : 0;
    float grazing = 1 - abs(viewCosine);
    float rimBand = smoothstep(0, 1, (grazing - (0.8 - 0.6 * EFMI_RIM_WIDTH)) / (0.1 + 0.2 * EFMI_RIM_WIDTH));
    return color + rimBand * rimSide * albedo * 0.96 * EFMI_RIM_STRENGTH * rim;
}

// efmiToon is the HGRP character shading as Warfarin Studio reconstructed it: a ramp lookup over N.L, a darker tone
// for surfaces also facing away from the camera, and a light map packed like _MetallicGlossMap (R metallic,
// G specular, B shadow mask, A smoothness). A flat face takes its light from the face mask instead of N.L.
float3 efmiToon(VSOut i, float3 n, float3 v, float3 albedo) {
    float3 l = toonLight.xyz;
    float3 cameraBack = view[2].xyz;
    float4 packedMap = (flags & FLAG_LIGHT_MAP) != 0 ? lightMap.Sample(linearWrap, i.uv) : float4(0, 1, 1, 0.5);
    float face = (flags & FLAG_FACE) != 0 ? 1 : 0;
    float faceMaskEnabled = (flags & FLAG_MATERIAL_MAP) != 0 ? 1 : 0;
    float occlusion = toonOcclusion(i);

    // _SDFMask.G keeps N.L where the SDF does not reach, such as the jaw edge.
    float4 faceMask = float4(1, 0, 1, 0);
    if (face > 0.5 && faceMaskEnabled > 0.5) faceMask = materialMap.Sample(linearWrap, i.uv);
    float flatLight = face * (1 - faceMask.g);
    float4 ramp = efmiRamp(dot(n, l) + EFMI_RANGE_BIAS);
    float rampCoverage = lerp(ramp.a, 1, flatLight);
    float lit = lerp(min(rampCoverage, min(packedMap.b, occlusion)), 1, flatLight);
    float cameraFacing = efmiRamp(dot(n, cameraBack)).a * packedMap.b * occlusion;

    float3 shadowTone, deepTone;
    efmiTones(albedo, shadowTone, deepTone);
    float3 surface = lerp(deepTone, shadowTone, saturate(rampCoverage + cameraFacing + face));
    float viewCosine = saturate(dot(n, v));
    float faceRim = face * faceMaskEnabled * faceMask.r * lerp(0.7, 0.3, faceMask.b);
    float3 litTone = albedo * lerp(1, float3(0.637, 0.376, 0.296), saturate(0.85 * (1 - viewCosine) * faceRim));
    surface = efmiRampTint(lerp(surface, litTone * 0.96, lit), lerp(ramp.rgb, 1, flatLight));

    float sky = lerp(saturate(n.y + 0.15) * 1.5 + 0.5, 0.725, face);
    float3 shadowTint = lerp(EFMI_SHADOW_TINT, EFMI_SKIN_SHADOW_TINT, face);
    float3 irradiance = lerp(shadowTint, 1, lit) * sky * 0.2877 * EFMI_LIGHT_STRENGTH;
    irradiance += EFMI_LIGHT_STRENGTH * EFMI_BRIGHTNESS;
    float3 toon = surface * irradiance;
    if (face > 0.5 && (flags & FLAG_HIGHLIGHT_MAP) != 0) {
        float3 right = mul((float3x3)world, float3(1, 0, 0));
        float2 highlightUV = i.uv + float2(dot(v, normalize(right)) * 0.03, 0);
        toon += highlightMap.Sample(linearWrap, highlightUV).rgb * irradiance * lerp(EFMI_AMBIENT, 1, lit) *
                (0.5 + 0.5 * lit);
    }

    float roughness = 1 - packedMap.a;
    float alpha = max(roughness * roughness, 0.007812);
    float alphaSquared = alpha * alpha;
    float3 specularHalf = normalize(l + 2 * normalize(float3(cameraBack.x, l.y, cameraBack.z)) + 3 * v);
    float halfCosine = dot(n, specularHalf);
    float distribution = halfCosine * halfCosine * (alphaSquared - 1) + 1;
    distribution *= distribution;
    distribution = distribution != alphaSquared ? alphaSquared / distribution : 1;
    float specularTerm = clamp(distribution * 0.5 / (2 * viewCosine + alpha + 0.0001) - 0.000061, 0, 20);
    float3 reflectance = lerp(0.04 * packedMap.g, albedo, packedMap.r);
    toon += irradiance * (0.5 + 0.5 * lit) * lerp(EFMI_AMBIENT, 1, lit) * specularTerm * reflectance *
            EFMI_SPECULAR * (1 - flatLight);
    return efmiFinish(toon, albedo, n, l, cameraBack, viewCosine, 1 - flatLight);
}

float4 decodeDiffuse(float2 uv) {
    float4 c = diffuseMap.Sample(linearWrap, uv);
    if ((flags & FLAG_MAP_SIGNED) != 0) c = c * 0.5 + 0.5;
    if ((flags & FLAG_MAP_BC4) != 0) c = float4(c.rrr, 1);
    return c;
}

float4 ps_main(VSOut i, bool front : SV_IsFrontFace) : SV_Target {
    if ((flags & FLAG_CUTOUT) != 0 && cutoutMap.Sample(linearWrap, i.uv).r < 0.5) discard;

    float4 albedo = 1;
    // The emission mask is the texture's own alpha, before the inversion for maps that store opacity inverted.
    float emissionMask = 1;
    if ((flags & FLAG_DIFFUSE) != 0) {
        float4 c = decodeDiffuse(i.uv);
        emissionMask = c.a;
        if ((flags & FLAG_INVERT_ALPHA) != 0) c.a = 1 - c.a;
        if ((flags & FLAG_COVERAGE) != 0) {
            albedo = float4(0.02, 0.02, 0.02, c.r);
        } else {
            albedo = float4((flags & FLAG_DIFFUSE_SRGB) != 0 ? srgbToLinear(c.rgb) : c.rgb, 1);
        }
    }
    if ((flags & FLAG_OVERLAY_COLOR) != 0) albedo.rgb = srgbToLinear(i.color.rgb);
    albedo.rgb *= tint.rgb;

    // Back faces only reach here on double-sided meshes, which light them from their own side.
    float face = front ? 1 : -1;
    float3 v = normalize(eye.xyz - i.worldPos);
    float3 geometryNormal = normalize(i.normal) * face;
    float3 dxy = max(abs(ddx(geometryNormal)), abs(ddy(geometryNormal)));
    float3 n = geometryNormal;
    if ((flags & FLAG_NORMAL_MAP) != 0) {
        float3 mapN = sampleNormal(i.uv);
        n = (flags & FLAG_VERTEX_TANGENT) != 0 ? perturbNormalTangent(n, face, i, mapN)
                                                : perturbNormal(n, face, i.worldPos, i.uv, mapN);
    }

    // The toon result is display-referred, so exposure and tone mapping would only shift it.
    uint mode = toonMode();
    if (mode != 0) {
        float3 toon;
        if (mode == TOON_SRMI) toon = srmiToon(i, n, v, albedo.rgb);
        else if (mode == TOON_ZZMI) toon = zzmiToon(i, n, v, albedo.rgb);
        else if (mode == TOON_WWMI) toon = wwmiToon(i, geometryNormal, v, albedo.rgb);
        else if (mode == TOON_EFMI) toon = efmiToon(i, n, v, albedo.rgb);
        else toon = gimiToon(i, n, v, albedo.rgb, (flags & FLAG_COVERAGE) != 0 ? albedo.a : emissionMask);
        return float4(linearToSrgb(toon) * albedo.a, albedo.a);
    }

    // Only ZZMI sets these flags without toon shading: green of its light map is metalness and green of its
    // material map smoothness.
    float metalness = (flags & FLAG_LIGHT_MAP) != 0 ? lightMap.Sample(linearWrap, i.uv).g : surface.x;
    float baseRoughness = (flags & FLAG_MATERIAL_MAP) != 0 ? 1 - materialMap.Sample(linearWrap, i.uv).g : surface.y;
    float roughness = min(max(baseRoughness, 0.0525) + max(max(dxy.x, dxy.y), dxy.z), 1);
    float3 diffuse = albedo.rgb * (1 - metalness);
    float3 f0 = lerp(0.04, albedo.rgb, metalness);
    float dotNV = saturate(dot(n, v));
    float2 dfg = dfgLUT.SampleLevel(linearClamp, float2(roughness, dotNV), 0);
    float3 compensation = 1 + f0 * (1 / (dfg.x + dfg.y) - 1);

    float3 color = directLight(keyLight, n, v, diffuse, f0, compensation, roughness);
    color += directLight(fillLight, n, v, diffuse, f0, compensation, roughness);

    float3 singleDielectric, multiDielectric, singleMetallic, multiMetallic;
    multiscattering(dfg, 0.04, singleDielectric, multiDielectric);
    multiscattering(dfg, albedo.rgb, singleMetallic, multiMetallic);
    float3 dielectricScattering = singleDielectric + multiDielectric;

    float3 irradiance = shading.x + lerp(groundLight.rgb, skyLight.rgb, n.y * 0.5 + 0.5);
    color += irradiance * diffuse / PI * (1 - dielectricScattering);

    float3 reflected = normalize(lerp(reflect(-v, n), n, pow(roughness, 4)));
    float radiance = environmentRadiance(reflected, roughness);
    float cosineWeightedIrradiance = environmentRadiance(n, 1);
    float3 single = lerp(singleDielectric, singleMetallic, metalness);
    float3 multi = lerp(multiDielectric, multiMetallic, metalness);
    color += radiance * single + multi * cosineWeightedIrradiance;
    color += diffuse * (1 - dielectricScattering) * cosineWeightedIrradiance;

    uint tone = (uint)shading.w;
    if (tone == TONE_NEUTRAL) color = neutralToneMapping(color * shading.z);
    else if (tone == TONE_ACES) color = acesFilmicToneMapping(color * shading.z);
    color = linearToSrgb(color);
    return float4(color * albedo.a, albedo.a);
}

struct OutlineOut {
    float4 pos : SV_Position;
    float2 uv : TEXCOORD0;
    float facing : TEXCOORD1;
    float width : TEXCOORD2; // vertex colour alpha, below 0.05 of which Star Rail draws no outline
    float3 normal : NORMAL;  // the hull's smoothed normal, for the profiles that light it
    float3 worldPos : POSITION1;
};

// The hull's smoothed normal in view space, or the view axis where it has none.
float3 outlineViewNormal(VSIn i) {
    float3 n = mul((float3x3)view, mul((float3x3)world, i.outline));
    float lengthSquared = dot(n, n);
    return lengthSquared > 1e-10 ? n * rsqrt(lengthSquared) : float3(0, 0, 1);
}

OutlineOut outlineBase(VSIn i, float4 viewPos) {
    OutlineOut o;
    o.width = 1;
    o.facing = dot(outlineViewNormal(i), normalize(-viewPos.xyz));
    o.normal = mul((float3x3)world, i.outline);
    o.worldPos = mul(world, float4(i.pos, 1)).xyz;
    o.uv = textureUV(i.uv);
    return o;
}

// HoyoToon's ZZZ hull: it extrudes in view space along the smoothed normal, laid flat on the screen, by a width
// scaled by the vertex colour's red and, unlike the Genshin and Star Rail hulls, not by depth or field of view,
// and it is pushed slightly away from the camera. The game keeps that normal in a UV channel the viewer does not
// decode, so seam-averaged normals stand in; HoyoToon's width defaults to 0 and takes the game's material value,
// so 0.5 is a guess.
OutlineOut zzmiOutline(VSIn i) {
    float4 color = (flags & FLAG_VERTEX_COLOR) != 0 ? i.color : ZZMI_NEUTRAL_COLOR;
    float4 viewPos = mul(view, mul(world, float4(i.pos, 1)));
    OutlineOut o = outlineBase(i, viewPos);
    float3 smoothNormal = outlineViewNormal(i);
    float3 direction = float3(normalize(float3(smoothNormal.xy, 0.0001)).xy, 0.0001);
    float depth = abs(viewPos.z) * (2.414 / proj[1][1]);
    viewPos.xyz *= 1 + 0.0001 * lerp(0.1, 1, saturate(1 - depth));
    viewPos.xyz += direction * 0.5 * color.r * 0.0015;
    o.pos = mul(proj, viewPos);
    return o;
}

// HoyoToon's WuWa hull: it extrudes in view space along the smoothed normal by a fixed width.
OutlineOut wwmiOutline(VSIn i) {
    float4 viewPos = mul(view, mul(world, float4(i.pos, 1)));
    OutlineOut o = outlineBase(i, viewPos);
    viewPos.xyz += outlineViewNormal(i) * 0.00055;
    o.pos = mul(proj, viewPos);
    return o;
}

// EFMI's hull expands in clip space by a width that shrinks with distance and never drops below half a pixel,
// scaled by the light map slot's outline mask where the mod binds one there.
OutlineOut efmiOutline(VSIn i) {
    float4 viewPos = mul(view, mul(world, float4(i.pos, 1)));
    OutlineOut o = outlineBase(i, viewPos);
    o.facing = 1;
    o.pos = mul(proj, viewPos);
    float2 direction = mul(proj, float4(mul((float3x3)view, mul((float3x3)world, i.outline)), 0)).xy;
    float directionSquared = dot(direction, direction);
    if (directionSquared > 0) {
        float2 resolution = max(viewport.xy, 1);
        direction *= rsqrt(directionSquared);
        direction.x *= resolution.y / resolution.x;
        float halfFov = atan(1 / proj[1][1]);
        float w = o.pos.w;
        float2 offset = direction * (0.392699 / halfFov * EFMI_OUTLINE_WIDTH * saturate(w * halfFov * 4.583663) * 0.005);
        float2 minimum = min(max(w, 0), 1.570796 / halfFov) / resolution;
        offset = abs(offset) < minimum ? sign(offset) * minimum : offset;
        if ((flags & FLAG_OUTLINE_MASK) != 0) offset *= outlineWidthMap.SampleLevel(linearWrap, o.uv, 0).r;
        o.pos.xy += offset;
        float depthOffset = (flags & FLAG_FACE) != 0 ? EFMI_FACE_OUTLINE_DEPTH_OFFSET : 0;
        float4 pushed = mul(proj, float4(viewPos.xy, viewPos.z - depthOffset, 1));
        o.pos.z = pushed.z * w / pushed.w;
    }
    return o;
}

// The EFMI hull is lit like the surface without its maps: a darker tone where it faces away from the camera, and
// the rim.
float3 efmiOutlineColor(OutlineOut i, float3 albedo) {
    albedo *= EFMI_OUTLINE_BRIGHTNESS;
    float face = (flags & FLAG_FACE) != 0 ? 1 : 0;
    float3 n = dot(i.normal, i.normal) > 1e-10 ? normalize(i.normal) : float3(0, 0, 1);
    float3 l = toonLight.xyz;
    float3 v = normalize(eye.xyz - i.worldPos);
    float3 cameraBack = view[2].xyz;

    float4 ramp = efmiRamp(dot(n, l) + EFMI_RANGE_BIAS);
    float facing = saturate((dot(n, cameraBack) - 0.25) * 1.333333);
    float deep = saturate(ramp.a + facing * facing * (3 - 2 * facing) + face);
    float3 shadowTone, deepTone;
    efmiTones(albedo, shadowTone, deepTone);
    float3 surface = efmiRampTint(lerp(lerp(deepTone, shadowTone, deep), albedo * 0.96, ramp.a), ramp.rgb);

    float sky = lerp(saturate(n.y + 0.15) * 1.5 + 0.5, 0.725, face);
    float3 shadowTint = lerp(EFMI_SHADOW_TINT, EFMI_SKIN_SHADOW_TINT, face);
    float3 irradiance = lerp(shadowTint, 1, ramp.a) * sky * 0.2877 * EFMI_LIGHT_STRENGTH;
    float3 color = surface * (irradiance + EFMI_LIGHT_STRENGTH * EFMI_BRIGHTNESS);
    return efmiFinish(color, albedo, n, l, cameraBack, dot(n, v), 1 - face);
}

static const float3 OUTLINE_ADJUST_DEPTHS = float3(0.001, 2.0, 6.0);
static const float3 OUTLINE_ADJUST_SCALES = float3(0.01, 0.245, 0.6);

// HoyoToon's Star Rail hull: it extrudes in view space along the smoothed normal, tilted away from the camera,
// by a width that grows with the square root of the lens-scaled depth and scales with the vertex colour's
// alpha. On the face, StarRailNPRShader's mask hides it with the vertex colour's blue where the face turns to the
// camera.
OutlineOut srmiOutline(VSIn i) {
    float4 color = (flags & FLAG_VERTEX_COLOR) != 0 ? i.color : float4(1, 0.5, 0, 0.5);
    float3 smoothNormal = outlineViewNormal(i);
    float4 viewPos = mul(view, mul(world, float4(i.pos, 1)));
    OutlineOut o = outlineBase(i, viewPos);
    o.width = color.a;
    float3 toCamera = normalize(-viewPos.xyz);
    o.facing = dot(smoothNormal, toCamera);
    float width = sqrt(abs(viewPos.z) / proj[1][1]) * color.a * 0.05 * 0.187;
    if ((flags & FLAG_FACE) != 0) {
        float3 forward = mul((float3x3)view, mul((float3x3)world, faceForward.xyz));
        forward = dot(forward, forward) > 1e-8 ? normalize(forward) : float3(0, 0, 1);
        width *= smoothstep(-0.02, 0, 1 - dot(forward, toCamera) - color.b);
    }
    viewPos.xyz += normalize(float3(smoothNormal.xy, -1)) * width;
    o.pos = mul(proj, viewPos);
    o.uv = textureUV(i.uv);
    return o;
}

// HoyoToon's Genshin hull: it extrudes in view space along the smoothed normal, flattened onto the screen, by a
// width that grows with distance in two linear pieces and scales with the vertex colour's alpha, and it is
// pushed slightly away from the camera.
OutlineOut vs_outline(VSIn i) {
    uint mode = toonMode();
    if (mode == TOON_SRMI) return srmiOutline(i);
    if (mode == TOON_ZZMI) return zzmiOutline(i);
    if (mode == TOON_WWMI) return wwmiOutline(i);
    if (mode == TOON_EFMI) return efmiOutline(i);
    float3 direction = mul((float3x3)view, mul((float3x3)world, i.outline));
    direction.z = 0;
    float lengthSquared = dot(direction, direction);
    direction = lengthSquared > 1e-10 ? direction * rsqrt(lengthSquared) : 0;

    float4 viewPos = mul(view, mul(world, float4(i.pos, 1)));
    OutlineOut o = outlineBase(i, viewPos);
    float depth = abs(viewPos.z) * gimiOutline.y;
    float4 piece = depth < OUTLINE_ADJUST_DEPTHS.y
        ? float4(OUTLINE_ADJUST_DEPTHS.xy, OUTLINE_ADJUST_SCALES.xy)
        : float4(OUTLINE_ADJUST_DEPTHS.yz, OUTLINE_ADJUST_SCALES.yz);
    float depthScale = lerp(piece.z, piece.w, saturate((depth - piece.x) / max(piece.y - piece.x, 0.001)));
    float widthScale = (flags & FLAG_VERTEX_COLOR) != 0 ? i.color.a : 0.5;
    float width = gimiOutline.x * 1.5 * depthScale * 0.414 * widthScale;
    viewPos.xyz *= 1 + 0.01 * lerp(0.1, 1, saturate(1 - depth));
    viewPos.xyz += width * direction;
    o.pos = mul(proj, viewPos);
    o.uv = textureUV(i.uv);
    return o;
}

float4 ps_outline(OutlineOut i) : SV_Target {
    // The hull past the silhouette faces sideways. Its far side faces away and is only visible through
    // openings such as a missing face.
    if (i.facing < -0.5 || i.width < 0.05) discard;
    float3 albedo = 1;
    if ((flags & FLAG_DIFFUSE) != 0) {
        albedo = decodeDiffuse(i.uv).rgb;
        if ((flags & FLAG_DIFFUSE_SRGB) != 0) albedo = srgbToLinear(albedo);
    }
    uint mode = toonMode();
    if (mode == TOON_EFMI) return float4(linearToSrgb(efmiOutlineColor(i, albedo)), 1);
    float3 tint = float3(0.26, 0.16, 0.24);
    if (mode == TOON_ZZMI) {
        // The albedo under a dimmed outline tint, darker on the side away from the light.
        float NoL = dot(i.normal, i.normal) > 1e-10 ? saturate(dot(normalize(i.normal), toonLight.xyz)) : 1;
        tint = lerp(0.01, 1, NoL * 0.5 + 0.5) * pow(0.5, 1.5);
    } else if (mode == TOON_WWMI) {
        tint = 0.765;
    } else if (mode == TOON_SRMI) {
        // Star Rail's tint is not measured; StarRailNPRShader's default black reads too harsh. GIMI's dark mauve
        // was measured against the lit skin in game.
        tint = float3(0.3, 0.22, 0.24);
    }
    return float4(linearToSrgb(albedo * tint), 1);
}

float4 vs_ring(uint id : SV_VertexID) : SV_Position {
    float angle = 2 * PI * (id % RING_SEGMENTS) / RING_SEGMENTS;
    float3 p = ringCenter.xyz + ringU.xyz * cos(angle) + ringV.xyz * sin(angle);
    return mul(viewProj, mul(world, float4(p, 1)));
}

float4 ps_ring() : SV_Target {
    return ringColor;
}

// A tool's markers over the model, such as the liquid emitter's: unlit, in model space, display-referred and
// premultiplied.
struct OverlayIn {
    float3 pos : POSITION;
    float4 color : COLOR;
};

struct OverlayOut {
    float4 pos : SV_Position;
    float4 color : COLOR;
};

OverlayOut vs_overlay(OverlayIn i) {
    OverlayOut o;
    o.pos = mul(viewProj, mul(world, float4(i.pos, 1)));
    o.color = i.color;
    return o;
}

float4 ps_overlay(OverlayOut i) : SV_Target {
    return i.color;
}
