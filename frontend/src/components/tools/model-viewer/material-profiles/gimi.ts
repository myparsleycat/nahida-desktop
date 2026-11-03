/*
 * The GIMI shading below ports the lighting model of UnityGenshinToonShader
 * (https://github.com/kaze-mio/UnityGenshinToonShader) to Three.js.
 *
 * MIT License
 *
 * Copyright (c) 2023 风澪瑟
 *
 * Permission is hereby granted, free of charge, to any person obtaining a copy
 * of this software and associated documentation files (the "Software"), to deal
 * in the Software without restriction, including without limitation the rights
 * to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
 * copies of the Software, and to permit persons to whom the Software is
 * furnished to do so, subject to the following conditions:
 *
 * The above copyright notice and this permission notice shall be included in all
 * copies or substantial portions of the Software.
 *
 * THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
 * IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
 * FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
 * AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
 * LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
 * OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
 * SOFTWARE.
 */
import type { GenshinToonAssetsTransport } from "@bindings/tools/model_viewer";
import { Logger } from "@renderer/lib/logger";
import type { ModViewerTransport } from "@shared/mod-viewer/types";
import { pick } from "es-toolkit";
import type {
    BufferGeometry,
    Matrix3,
    MeshStandardMaterial,
    Texture,
    WebGLProgramParametersWithUniforms,
} from "three";
import {
    BackSide,
    ClampToEdgeWrapping,
    DataTexture,
    LinearFilter,
    Mesh,
    NoColorSpace,
    RGBAFormat,
    SRGBColorSpace,
    ShaderMaterial,
    TextureLoader,
    Vector3,
} from "three";

import type {
    MaterialProfileMeshData,
    ModelViewerMaterialProfile,
    ModelViewerToonStyleBase,
} from "./profile";

import { modelViewerDDSFormatOf } from "../model-viewer-dds";
import {
    averagedNormals,
    replaceShaderAnchor,
    textureUvTransform,
    TOON_VERTEX_COLOR_ATTRIBUTE,
} from "../model-viewer-shader-utils";
import { BASE_TOON_FLAGS, readToonFlags } from "./profile";

export const GENSHIN_FACE_TYPES = ["boy", "girl", "lady", "loli", "male"] as const;
export type GenshinFaceType = (typeof GENSHIN_FACE_TYPES)[number];

export type GIMIToonStyle = ModelViewerToonStyleBase & {
    daytime: boolean;
    shadowOffset: number;
    shadowSmoothness: number;
    shadowBrightness: number;
    brightness: number;
    specular: number;
    metallic: number;
    rimStrength: number;
    emission: number;
    outlineWidth: number;
    toneMapping: boolean;
    /** Name of a ramp in the user's Genshin texture folder; empty uses the built-in ramp. */
    ramp: string;
    /** "auto" takes the body type from the ramp name, such as Avatar_Loli_... */
    faceType: "auto" | GenshinFaceType;
    assetDir: string;
};

// Shading values follow kaze-mio's Nahida material in UnityGenshinToonShader, except
// the shadow offset: with its 0.1, frontal light shades limbs that the game keeps lit
// to the silhouette.
export const DEFAULT_GIMI_TOON_STYLE: GIMIToonStyle = {
    daytime: true,
    shadowOffset: 0,
    shadowSmoothness: 0.4,
    shadowBrightness: 1.1,
    brightness: 1,
    specular: 0.3,
    metallic: 8,
    rimStrength: 0.3,
    emission: 0.2,
    outlines: true,
    outlineWidth: 1.6,
    selfShadow: true,
    lightFollowsCamera: true,
    // The curve compresses bright skin channels unevenly and bleaches it.
    toneMapping: false,
    fieldOfView: 20,
    ramp: "",
    faceType: "auto",
    assetDir: "",
};

const GIMI_TOON_FLAGS = ["outlines", "selfShadow", "lightFollowsCamera"] as const;

type GenshinToonTextures = {
    bodyRamp?: Texture;
    hairRamp?: Texture;
    metalMap?: Texture;
    faceLightMap?: Texture;
    faceShadow?: Texture;
};

type GenshinToonMeshData = MaterialProfileMeshData & {
    genshinFace?: boolean;
    genshinHair?: boolean;
    genshinFaceForward?: Vector3;
    genshinOutline?: Mesh<BufferGeometry, ShaderMaterial>;
};

type GenshinToonShaderState = {
    enabled: { value: number };
    lightMap: { value: Texture };
    lightMapEnabled: { value: number };
    lightMapTransform: { value: Matrix3 };
    ramp: { value: Texture };
    metalMap: { value: Texture };
    face: { value: number };
    faceForward: { value: Vector3 };
    faceLightMap: { value: Texture };
    faceLightMapEnabled: { value: number };
    faceShadow: { value: Texture };
    faceShadowEnabled: { value: number };
    vertexColorEnabled: { value: number };
    daytime: { value: number };
    shadowOffset: { value: number };
    shadowSmoothness: { value: number };
    shadowBrightness: { value: number };
    brightness: { value: number };
    specular: { value: number };
    metallic: { value: number };
    rimStrength: { value: number };
    emission: { value: number };
    emissionInverted: { value: number };
    toneMapping: { value: number };
};

const RAMP_WIDTH = 256;
const RAMP_ROWS = 10;
const METAL_MAP_SIZE = 64;

// Ramp rows by material: lightmap alpha 0.7, 0.3, 0.5, 1.0 (skin), and 0.0. The
// shadow ends are sRGB multipliers; skin matches the shadow-to-lit ratio measured
// in an in-game character screen (0.85, 0.72, 0.70).
const RAMP_SHADOW_COLORS: Array<[number, number, number]> = [
    [0.8, 0.72, 0.84],
    [0.78, 0.71, 0.82],
    [0.76, 0.72, 0.82],
    [0.88, 0.72, 0.7],
    [0.79, 0.73, 0.84],
];
const RAMP_NIGHT_TINT: [number, number, number] = [0.82, 0.84, 0.97];

// The outline extrudes by 0.01 * width * mix(near, far, k) in view space, where k
// interpolates the field-of-view-normalized depth between these distances.
const OUTLINE_NEAR_DEPTH = 0;
const OUTLINE_FAR_DEPTH = 6;
const OUTLINE_NEAR_SCALE = 0.1;
const OUTLINE_FAR_SCALE = 0.6;

const whiteTexture = solidTexture(255, 255, 255, 255);
const neutralLightMap = solidTexture(0, 128, 0, 77);

let proceduralRamp: DataTexture | undefined;
let proceduralMetalMap: DataTexture | undefined;

const GENSHIN_COMMON_GLSL = `// A step whose edge spans one screen pixel, so toon boundaries do not alias.
float genshinStep( float edge, float x ) {
    float width = max( fwidth( x ), 1e-4 );
    return smoothstep( edge - width, edge + width, x );
}

float genshinMaterialRow( float id ) {
    if ( id < 0.2 ) return 4.0;
    if ( id < 0.4 ) return 1.0;
    if ( id < 0.6 ) return 2.0;
    if ( id < 0.8 ) return 0.0;
    return 3.0;
}

// Gran Turismo (Uchimura) curve: keeps saturation and rolls off highlights.
vec3 genshinToneMap( vec3 x ) {
    const float P = 1.0;
    const float a = 1.0;
    const float m = 0.22;
    const float l = 0.4;
    const float c = 1.33;
    float l0 = ( ( P - m ) * l ) / a;
    float S0 = m + l0;
    float S1 = m + a * l0;
    float C2 = ( a * P ) / ( P - S1 );
    float CP = -C2 / P;
    x = max( x, vec3( 0.0 ) );
    vec3 w0 = 1.0 - smoothstep( vec3( 0.0 ), vec3( m ), x );
    vec3 w2 = step( vec3( S0 ), x );
    vec3 w1 = 1.0 - w0 - w2;
    vec3 toe = m * pow( x / m, vec3( c ) );
    vec3 shoulder = P - ( P - S1 ) * exp( CP * ( x - S0 ) );
    vec3 linear = m + a * ( x - m );
    return toe * w0 + linear * w1 + shoulder * w2;
}`;

export const gimiMaterialProfile: ModelViewerMaterialProfile<GIMIToonStyle> = {
    baseMaterial: { metalness: 0.05, roughness: 0.65 },
    derivativeTangentFrame: false,
    toonShadows: true,
    toon: {
        defaultStyle: DEFAULT_GIMI_TOON_STYLE,

        // Keeps everything but the menu's flags and asset choices at the defaults.
        parse(record) {
            const style = readToonFlags(record, DEFAULT_GIMI_TOON_STYLE, GIMI_TOON_FLAGS);
            const faceType = record.faceType;
            return {
                ...style,
                ramp: typeof record.ramp === "string" ? record.ramp : style.ramp,
                faceType:
                    faceType === "auto" || GENSHIN_FACE_TYPES.includes(faceType as GenshinFaceType)
                        ? (faceType as GIMIToonStyle["faceType"])
                        : style.faceType,
                assetDir: typeof record.assetDir === "string" ? record.assetDir : style.assetDir,
            };
        },
        serialize: (style) => pick(style, [...GIMI_TOON_FLAGS, "ramp", "faceType", "assetDir"]),
        flags: BASE_TOON_FLAGS,
        createResourceLoader: createGenshinToonTextureLoader,
    },
    markMeshes: markGenshinMeshes,
    configureMaterial: configureGenshinToonMaterialShader,

    // One- and two-channel containers have no material ID or specular mask to read.
    bindMaps(material, maps, textures) {
        const state = material.userData.genshinToonMaterial as GenshinToonShaderState | undefined;
        if (!state) {
            return;
        }
        const texture = maps.lightMapKey ? textures.get(maps.lightMapKey) : undefined;
        const format = modelViewerDDSFormatOf(texture);
        const lightMap =
            format.startsWith("bc4-") || format.startsWith("bc5-") ? undefined : texture;
        state.lightMap.value = lightMap ?? neutralLightMap;
        state.lightMapEnabled.value = lightMap ? 1 : 0;
        state.lightMapTransform.value = textureUvTransform(state.lightMap.value);
    },
    sync: syncGenshinToon,
};

function configureGenshinToonMaterialShader(material: MeshStandardMaterial): void {
    if (material.userData.genshinToonMaterial) {
        return;
    }
    const defaults = DEFAULT_GIMI_TOON_STYLE;
    const state: GenshinToonShaderState = {
        enabled: { value: 0 },
        lightMap: { value: neutralLightMap },
        lightMapEnabled: { value: 0 },
        lightMapTransform: { value: neutralLightMap.matrix },
        ramp: { value: genshinProceduralRamp() },
        metalMap: { value: genshinProceduralMetalMap() },
        face: { value: 0 },
        faceForward: { value: new Vector3(0, 0, 1) },
        faceLightMap: { value: whiteTexture },
        faceLightMapEnabled: { value: 0 },
        faceShadow: { value: whiteTexture },
        faceShadowEnabled: { value: 0 },
        vertexColorEnabled: { value: 0 },
        daytime: { value: defaults.daytime ? 1 : 0 },
        shadowOffset: { value: defaults.shadowOffset },
        shadowSmoothness: { value: defaults.shadowSmoothness },
        shadowBrightness: { value: defaults.shadowBrightness },
        brightness: { value: defaults.brightness },
        specular: { value: defaults.specular },
        metallic: { value: defaults.metallic },
        rimStrength: { value: defaults.rimStrength },
        emission: { value: defaults.emission },
        emissionInverted: { value: 0 },
        toneMapping: { value: defaults.toneMapping ? 1 : 0 },
    };
    material.userData.genshinToonMaterial = state;
    material.onBeforeCompile = (shader: WebGLProgramParametersWithUniforms) => {
        shader.uniforms.genshinToonEnabled = state.enabled;
        shader.uniforms.genshinLightMap = state.lightMap;
        shader.uniforms.genshinLightMapTransform = state.lightMapTransform;
        shader.uniforms.genshinRamp = state.ramp;
        shader.uniforms.genshinMetalMap = state.metalMap;
        shader.uniforms.genshinFace = state.face;
        shader.uniforms.genshinFaceForward = state.faceForward;
        shader.uniforms.genshinFaceLightMap = state.faceLightMap;
        shader.uniforms.genshinFaceLightMapEnabled = state.faceLightMapEnabled;
        shader.uniforms.genshinFaceShadow = state.faceShadow;
        shader.uniforms.genshinFaceShadowEnabled = state.faceShadowEnabled;
        shader.uniforms.genshinVertexColorEnabled = state.vertexColorEnabled;
        shader.uniforms.genshinDaytime = state.daytime;
        shader.uniforms.genshinShadowOffset = state.shadowOffset;
        shader.uniforms.genshinShadowSmoothness = state.shadowSmoothness;
        shader.uniforms.genshinShadowBrightness = state.shadowBrightness;
        shader.uniforms.genshinBrightness = state.brightness;
        shader.uniforms.genshinSpecular = state.specular;
        shader.uniforms.genshinMetallic = state.metallic;
        shader.uniforms.genshinRimStrength = state.rimStrength;
        shader.uniforms.genshinEmission = state.emission;
        shader.uniforms.genshinEmissionInverted = state.emissionInverted;
        shader.uniforms.genshinToneMapping = state.toneMapping;
        shader.vertexShader = replaceShaderAnchor(
            shader.vertexShader,
            "#include <common>",
            `#include <common>
attribute vec4 ${TOON_VERTEX_COLOR_ATTRIBUTE};
uniform float genshinVertexColorEnabled;
uniform vec3 genshinFaceForward;
varying vec2 vGenshinUv;
varying vec4 vGenshinColor;
varying vec3 vGenshinFaceForward;`,
        );
        shader.vertexShader = replaceShaderAnchor(
            shader.vertexShader,
            "#include <uv_vertex>",
            `#include <uv_vertex>
vGenshinUv = uv;
vGenshinColor = genshinVertexColorEnabled > 0.5 ? ${TOON_VERTEX_COLOR_ATTRIBUTE} : vec4( 1.0, 0.5, 1.0, 0.5 );
vGenshinFaceForward = normalize( mat3( modelMatrix ) * genshinFaceForward );`,
        );
        shader.fragmentShader = replaceShaderAnchor(
            shader.fragmentShader,
            "#include <common>",
            `#include <common>
varying vec2 vGenshinUv;
varying vec4 vGenshinColor;
varying vec3 vGenshinFaceForward;
uniform float genshinToonEnabled;
uniform sampler2D genshinLightMap;
uniform mat3 genshinLightMapTransform;
uniform sampler2D genshinRamp;
uniform sampler2D genshinMetalMap;
uniform float genshinFace;
uniform sampler2D genshinFaceLightMap;
uniform float genshinFaceLightMapEnabled;
uniform sampler2D genshinFaceShadow;
uniform float genshinFaceShadowEnabled;
uniform float genshinDaytime;
uniform float genshinShadowOffset;
uniform float genshinShadowSmoothness;
uniform float genshinShadowBrightness;
uniform float genshinBrightness;
uniform float genshinSpecular;
uniform float genshinMetallic;
uniform float genshinRimStrength;
uniform float genshinEmission;
uniform float genshinEmissionInverted;
uniform float genshinToneMapping;

${GENSHIN_COMMON_GLSL}`,
        );

        // Replaces the lit result: half-Lambert scaled by the light map's AO picks a
        // column of the shadow ramp, and the light map's alpha picks its row.
        // directionalLights[0] is the key light, as in the EFMI shader.
        shader.fragmentShader = replaceShaderAnchor(
            shader.fragmentShader,
            "#include <opaque_fragment>",
            `#if NUM_DIR_LIGHTS > 0
if ( genshinToonEnabled > 0.5 ) {
    vec3 worldNormal = inverseTransformDirection( normal, viewMatrix );
    vec3 worldLight = inverseTransformDirection( directionalLights[ 0 ].direction, viewMatrix );
    vec3 worldView = inverseTransformDirection( normalize( vViewPosition ), viewMatrix );
    vec3 albedo = diffuseColor.rgb;
    vec4 lightMap = texture2D( genshinLightMap, ( genshinLightMapTransform * vec3( vGenshinUv, 1.0 ) ).xy );

    float occlusion = 1.0;
    #if defined( USE_SHADOWMAP ) && NUM_DIR_LIGHT_SHADOWS > 0
    if ( receiveShadow ) {
        occlusion = getShadow(
            directionalShadowMap[ 0 ],
            directionalLightShadows[ 0 ].shadowMapSize,
            directionalLightShadows[ 0 ].shadowIntensity,
            directionalLightShadows[ 0 ].shadowBias,
            directionalLightShadows[ 0 ].shadowRadius,
            vDirectionalShadowCoord[ 0 ]
        );
    }
    #endif

    // The game flattens the light's elevation so faces and limbs keep their shape.
    vec3 flatLight = normalize( worldLight * vec3( 1.0, 0.5, 1.0 ) );
    float halfLambert = dot( worldNormal, flatLight ) * 0.5 + 0.5;
    float ao = lightMap.g * vGenshinColor.r;
    float shadow = ao >= 0.9 ? 1.0 : clamp( 2.0 * halfLambert * ao, 0.0, 1.0 );
    float row = genshinMaterialRow( lightMap.a );
    float face = genshinFace;

    if ( genshinFace > 0.5 ) {
        row = 3.0;
        vec2 forward = vGenshinFaceForward.xz;
        forward = dot( forward, forward ) > 1e-8 ? normalize( forward ) : vec2( 0.0, 1.0 );
        vec2 light = worldLight.xz;
        light = dot( light, light ) > 1e-8 ? normalize( light ) : forward;
        float facing = dot( forward, light );
        if ( genshinFaceLightMapEnabled > 0.5 ) {
            vec2 faceUv = vGenshinUv;
            if ( forward.y * light.x - forward.x * light.y >= 0.0 ) faceUv.x = 1.0 - faceUv.x;
            shadow = genshinStep( -0.5 * facing + 0.5, texture2D( genshinFaceLightMap, faceUv ).r );
            shadow = mix( shadow, 1.0, texture2D( genshinFaceShadow, vGenshinUv ).a * genshinFaceShadowEnabled );
        } else {
            shadow = smoothstep( -0.35, -0.15, facing );
        }
    }
    shadow *= occlusion;

    float threshold = 0.5 + genshinShadowOffset;
    float rampU = smoothstep( threshold - genshinShadowSmoothness, threshold, shadow );
    float rampV = ( row + 5.0 * genshinDaytime + 0.5 ) / 10.0;
    vec3 ramp = texture2D( genshinRamp, vec2( clamp( rampU, 0.004, 0.996 ), rampV ) ).rgb;
    vec3 shade = ramp * mix( vec3( genshinShadowBrightness ), vec3( 1.0 ), smoothstep( 0.9, 1.0, rampU ) );
    float lit = genshinStep( threshold, shadow );
    shade = mix( shade, vec3( 1.0 ), lit );

    vec3 halfDirection = normalize( worldLight + worldView );
    float blinnPhong = pow( clamp( dot( worldNormal, halfDirection ), 0.0, 1.0 ), 5.0 );
    vec3 metalMap = texture2D( genshinMetalMap, normalize( normal ).xy * 0.5 + 0.5 ).rgb;
    // Derivatives are undefined inside the per-texel branch below.
    float highlight = genshinStep( 1.1, lightMap.b + blinnPhong );
    vec3 specular = lightMap.r >= 0.9
        ? blinnPhong * lightMap.b * albedo * metalMap * genshinMetallic
        : vec3( highlight * lightMap.r * genshinSpecular );
    specular *= 1.0 - face;

    // A thin rim on the lit side of the silhouette only.
    float viewCosine = clamp( dot( worldNormal, worldView ), 0.0, 1.0 );
    float rim = smoothstep( 0.72, 0.8, 1.0 - viewCosine ) * lit * ( 1.0 - face );

    // The emission mask is the texture's own alpha, before the viewer's alpha
    // inversion for diffuse maps that store opacity inverted.
    float emissionMask = mix( diffuseColor.a, 1.0 - diffuseColor.a, genshinEmissionInverted );

    vec3 toon = ( albedo * shade + specular ) * genshinBrightness;
    toon += albedo * rim * genshinRimStrength;
    toon += albedo * emissionMask * genshinEmission;
    outgoingLight = genshinToneMapping > 0.5 ? genshinToneMap( toon ) : toon;
}
#endif
#include <opaque_fragment>`,
        );

        // The toon result is display-referred, so the viewer's exposure and tone
        // mapping would only shift it.
        shader.fragmentShader = replaceShaderAnchor(
            shader.fragmentShader,
            "#include <tonemapping_fragment>",
            `if ( genshinToonEnabled < 0.5 ) {
    #include <tonemapping_fragment>
}`,
        );
    };
    material.customProgramCacheKey = () => "gimi-toon-v1";
    material.needsUpdate = true;
}

function syncGenshinToon(
    object: Mesh,
    material: MeshStandardMaterial,
    meshData: MaterialProfileMeshData,
): void {
    const data = meshData as GenshinToonMeshData;
    const state = material.userData.genshinToonMaterial as GenshinToonShaderState | undefined;
    if (!state) {
        return;
    }
    const style = (data.toonStyle as GIMIToonStyle | undefined) ?? DEFAULT_GIMI_TOON_STYLE;
    const textures = (data.toonResources as GenshinToonTextures | undefined) ?? {};
    const enabled = data.toonShadows;
    const face = Boolean(data.genshinFace) && state.lightMapEnabled.value === 0;
    state.enabled.value = enabled ? 1 : 0;
    state.ramp.value =
        (data.genshinHair ? (textures.hairRamp ?? textures.bodyRamp) : textures.bodyRamp) ??
        genshinProceduralRamp();
    state.metalMap.value = textures.metalMap ?? genshinProceduralMetalMap();
    state.face.value = face ? 1 : 0;
    if (data.genshinFaceForward) state.faceForward.value.copy(data.genshinFaceForward);
    state.faceLightMap.value = textures.faceLightMap ?? whiteTexture;
    state.faceLightMapEnabled.value = textures.faceLightMap ? 1 : 0;
    state.faceShadow.value = textures.faceShadow ?? whiteTexture;
    state.faceShadowEnabled.value = textures.faceShadow ? 1 : 0;
    state.vertexColorEnabled.value = object.geometry.attributes[TOON_VERTEX_COLOR_ATTRIBUTE]
        ? 1
        : 0;
    state.daytime.value = style.daytime ? 1 : 0;
    state.shadowOffset.value = style.shadowOffset;
    state.shadowSmoothness.value = style.shadowSmoothness;
    state.shadowBrightness.value = style.shadowBrightness;
    state.brightness.value = style.brightness;
    state.specular.value = style.specular;
    state.metallic.value = style.metallic;
    state.rimStrength.value = style.rimStrength;
    state.toneMapping.value = style.toneMapping ? 1 : 0;

    // BC1 and opaque images carry no emission mask; their alpha would light everything.
    const format = (material.map?.userData.modelViewerDDS as { format?: string } | undefined)
        ?.format;
    const masked = Boolean(material.map) && !format?.startsWith("bc1-");
    state.emission.value = masked && !face ? style.emission : 0;
    state.emissionInverted.value = material.map?.userData.modelViewerInvertAlpha ? 1 : 0;

    // Coverage cards are flat ink decals: they neither block light nor have a silhouette.
    const coverageMask = Boolean(material.map?.userData.modelViewerCoverageMask);
    object.castShadow = enabled && style.selfShadow && !coverageMask;
    object.receiveShadow = enabled && style.selfShadow;

    const showOutline = enabled && style.outlines && style.outlineWidth > 0 && !coverageMask;
    if (!showOutline && !data.genshinOutline) {
        return;
    }

    // The extrusion source follows the tangent check, which is unknown until the maps
    // are applied and is redone when the normal or tangent attributes change.
    const frameKnown = data.tangentFrameUsable !== undefined;
    const tangentOutline = data.tangentFrameUsable === false;
    const outline = data.genshinOutline;
    if (outline && frameKnown && outline.userData.genshinTangentOutline !== tangentOutline) {
        outline.removeFromParent();
        outline.material.dispose();
        data.genshinOutline = undefined;
    }
    if (!data.genshinOutline) {
        if (!frameKnown) {
            return;
        }
        data.genshinOutline = createGenshinToonOutline(object, tangentOutline);
    }
    const uniforms = data.genshinOutline.material.uniforms;
    data.genshinOutline.visible = showOutline;
    uniforms.map.value = material.map ?? whiteTexture;
    uniforms.mapTransform.value = textureUvTransform(uniforms.map.value);
    uniforms.outlineWidth.value = style.outlineWidth;
    uniforms.vertexColorEnabled.value = state.vertexColorEnabled.value;
}

/**
 * Marks faces and hair from mesh and texture names. GIMI mods name parts after the
 * game's own (Head, Body, Dress, and a separately drawn Face), so a name is the
 * only reliable signal. A face also gets its forward direction: the mean of its
 * normals, which points out of the face in whatever space the mod's buffers use.
 */
function markGenshinMeshes(transport: ModViewerTransport, objects: Mesh[]): void {
    transport.meshes.forEach((mesh, index) => {
        const object = objects[index];
        const names = [mesh.component, mesh.id, mesh.texKey ?? ""].join(" ");
        const data = object.userData as GenshinToonMeshData;
        data.genshinFace = /face/i.test(names);
        data.genshinHair = !data.genshinFace && /hair|head/i.test(names);
        if (!data.genshinFace) {
            return;
        }
        const normal = object.geometry.attributes.normal;
        const sum = new Vector3();
        for (let vertex = 0; vertex < normal.count; vertex++) {
            sum.x += normal.getX(vertex);
            sum.y += normal.getY(vertex);
            sum.z += normal.getZ(vertex);
        }
        data.genshinFaceForward = sum.lengthSq() > 0 ? sum.normalize() : undefined;
    });
}

export function genshinFaceTypeOf(style: GIMIToonStyle): GenshinFaceType | undefined {
    if (style.faceType !== "auto") {
        return style.faceType;
    }
    const bodyType = /^avatar_([a-z]+)_/i.exec(style.ramp)?.[1]?.toLowerCase();
    return GENSHIN_FACE_TYPES.find((type) => type === bodyType);
}

/** Loads textures from the user's Genshin folder once per URL. */
function createGenshinToonTextureLoader() {
    const loader = new TextureLoader();
    const cache = new Map<string, Promise<Texture | undefined>>();
    const load = (url: string | undefined, srgb: boolean) => {
        if (!url) {
            return Promise.resolve(undefined);
        }
        const key = `${srgb ? "srgb" : "linear"}:${url}`;
        let texture = cache.get(key);
        if (!texture) {
            texture = loader.loadAsync(url).then(
                (loaded) => {
                    loaded.colorSpace = srgb ? SRGBColorSpace : NoColorSpace;
                    loaded.wrapS = ClampToEdgeWrapping;
                    loaded.wrapT = ClampToEdgeWrapping;
                    loaded.minFilter = LinearFilter;
                    loaded.magFilter = LinearFilter;
                    loaded.generateMipmaps = false;
                    return loaded;
                },
                (error: unknown) => {
                    cache.delete(key);
                    Logger.capture("model-viewer:genshin-toon-texture", error);
                    return undefined;
                },
            );
            cache.set(key, texture);
        }
        return texture;
    };

    return {
        async resolve(listed: unknown, style: GIMIToonStyle): Promise<GenshinToonTextures> {
            const assets = listed as GenshinToonAssetsTransport | undefined;
            if (!assets) {
                return {};
            }
            const ramp = (assets.ramps ?? []).find((entry) => entry.name === style.ramp);
            const faceType = genshinFaceTypeOf(style);
            const [bodyRamp, hairRamp, metalMap, faceLightMap, faceShadow] = await Promise.all([
                load(ramp?.bodyUrl, true),
                load(ramp?.hairUrl, true),
                load(assets.metalMapUrl, true),
                load(faceType ? assets.faceLightmaps?.[faceType] : undefined, false),
                load(assets.faceShadowUrl, false),
            ]);
            return { bodyRamp, hairRamp, metalMap, faceLightMap, faceShadow };
        },
        dispose() {
            for (const texture of cache.values()) {
                void texture.then((loaded) => loaded?.dispose());
            }
            cache.clear();
        },
    };
}

// The inverted hull of UnityGenshinToonShader: it extrudes in view space along the
// smoothed normal GIMI stores in TANGENT, flattened onto the screen, by a width
// that grows with distance and scales with the vertex color's alpha.
function createGenshinToonOutline(
    object: Mesh,
    tangentHoldsOutlineNormal: boolean,
): Mesh<BufferGeometry, ShaderMaterial> {
    const useTangent = tangentHoldsOutlineNormal && Boolean(object.geometry.attributes.tangent);
    if (!useTangent && !object.geometry.attributes.outlineNormal) {
        object.geometry.setAttribute("outlineNormal", averagedNormals(object.geometry));
    }
    const outline = new Mesh(
        object.geometry,
        new ShaderMaterial({
            defines: useTangent ? { GENSHIN_OUTLINE_TANGENT: "" } : {},
            uniforms: {
                map: { value: whiteTexture },
                mapTransform: { value: whiteTexture.matrix },
                outlineWidth: { value: 0 },
                vertexColorEnabled: { value: 0 },
            },
            vertexShader: `#ifdef GENSHIN_OUTLINE_TANGENT
attribute vec4 tangent;
#else
attribute vec3 outlineNormal;
#endif
attribute vec4 ${TOON_VERTEX_COLOR_ATTRIBUTE};
uniform mat3 mapTransform;
uniform float outlineWidth;
uniform float vertexColorEnabled;
varying vec2 vOutlineUv;
varying float vOutlineFacing;
void main() {
    vOutlineUv = ( mapTransform * vec3( uv, 1.0 ) ).xy;
    #ifdef GENSHIN_OUTLINE_TANGENT
    vec3 direction = normalMatrix * tangent.xyz;
    #else
    vec3 direction = normalMatrix * outlineNormal;
    #endif
    vec3 smoothNormal = direction;
    direction.z = 0.0;
    float lengthSquared = dot( direction, direction );
    direction = lengthSquared > 1e-10 ? direction * inversesqrt( lengthSquared ) : vec3( 0.0 );

    vec4 viewPosition = modelViewMatrix * vec4( position, 1.0 );
    vOutlineFacing = dot( smoothNormal, normalize( -viewPosition.xyz ) ) * inversesqrt( max( dot( smoothNormal, smoothNormal ), 1e-10 ) );
    float depth = abs( viewPosition.z ) * ( 2.414 / projectionMatrix[ 1 ][ 1 ] );
    float distance = clamp( ( depth - ${OUTLINE_NEAR_DEPTH.toFixed(1)} ) / ${(OUTLINE_FAR_DEPTH - OUTLINE_NEAR_DEPTH).toFixed(1)}, 0.0, 1.0 );
    float widthScale = vertexColorEnabled > 0.5 ? ${TOON_VERTEX_COLOR_ATTRIBUTE}.a : 0.5;
    float width = 0.01 * outlineWidth * mix( ${OUTLINE_NEAR_SCALE.toFixed(2)}, ${OUTLINE_FAR_SCALE.toFixed(2)}, distance ) * widthScale;
    viewPosition.xyz += 0.001 * normalize( viewPosition.xyz ) + width * direction;
    gl_Position = projectionMatrix * viewPosition;
}`,
            fragmentShader: `uniform sampler2D map;
varying vec2 vOutlineUv;
varying float vOutlineFacing;
void main() {
    // The hull past the silhouette faces sideways. Its far side faces away and is
    // only visible through openings such as the face, which GIMI mods do not include.
    if ( vOutlineFacing < -0.5 ) discard;
    vec3 albedo = texture2D( map, vOutlineUv ).rgb;
    // A dark mauve over skin, as measured against the lit skin in game.
    gl_FragColor = vec4( albedo * vec3( 0.26, 0.16, 0.24 ), 1.0 );
    #include <colorspace_fragment>
}`,
            side: BackSide,
        }),
    );
    outline.raycast = () => {};
    outline.userData.genshinTangentOutline = tangentHoldsOutlineNormal;
    object.add(outline);
    return outline;
}

function genshinProceduralRamp(): DataTexture {
    if (proceduralRamp) {
        return proceduralRamp;
    }
    const data = new Uint8Array(RAMP_WIDTH * RAMP_ROWS * 4);
    for (let row = 0; row < RAMP_ROWS; row++) {
        const night = row < RAMP_ROWS / 2;
        const shadow = RAMP_SHADOW_COLORS[row % 5].map((value, channel) =>
            night ? value * RAMP_NIGHT_TINT[channel] : value,
        );
        for (let column = 0; column < RAMP_WIDTH; column++) {
            const u = column / (RAMP_WIDTH - 1);

            // A lighter tone toward the terminator, then a narrow darker and more
            // saturated band right before it, like the game's double-step ramps.
            const lighten = smoothstep(0.5, 0.95, u) * 0.2;
            const band = smoothstep(0.78, 0.9, u) * (1 - smoothstep(0.97, 1, u));
            const offset = (row * RAMP_WIDTH + column) * 4;
            for (let channel = 0; channel < 3; channel++) {
                const base = shadow[channel] + (1 - shadow[channel]) * lighten;
                const deep = shadow[channel] * shadow[channel];
                data[offset + channel] = Math.round((base + (deep - base) * band) * 255);
            }
            data[offset + 3] = 255;
        }
    }
    proceduralRamp = dataTexture(data, RAMP_WIDTH, RAMP_ROWS, SRGBColorSpace);
    return proceduralRamp;
}

// A gray matcap with a bright horizontal band above the center, the shape of the
// game's shared metal map, sampled with the view-space normal.
function genshinProceduralMetalMap(): DataTexture {
    if (proceduralMetalMap) {
        return proceduralMetalMap;
    }
    const data = new Uint8Array(METAL_MAP_SIZE * METAL_MAP_SIZE * 4);
    for (let y = 0; y < METAL_MAP_SIZE; y++) {
        const v = y / (METAL_MAP_SIZE - 1);
        for (let x = 0; x < METAL_MAP_SIZE; x++) {
            const u = x / (METAL_MAP_SIZE - 1) - 0.5;
            const band = Math.exp(-(((v - 0.65) / 0.1) ** 2)) * (1 - u * u * 2);
            const value = Math.round((0.15 + 0.6 * band) * 255);
            data.set([value, value, value, 255], (y * METAL_MAP_SIZE + x) * 4);
        }
    }
    proceduralMetalMap = dataTexture(data, METAL_MAP_SIZE, METAL_MAP_SIZE, SRGBColorSpace);
    return proceduralMetalMap;
}

function dataTexture(
    data: Uint8Array,
    width: number,
    height: number,
    colorSpace: DataTexture["colorSpace"],
): DataTexture {
    const texture = new DataTexture(data, width, height, RGBAFormat);
    texture.colorSpace = colorSpace;
    texture.wrapS = ClampToEdgeWrapping;
    texture.wrapT = ClampToEdgeWrapping;
    texture.minFilter = LinearFilter;
    texture.magFilter = LinearFilter;
    texture.needsUpdate = true;
    return texture;
}

function solidTexture(r: number, g: number, b: number, a: number): DataTexture {
    return dataTexture(new Uint8Array([r, g, b, a]), 1, 1, NoColorSpace);
}

function smoothstep(edge0: number, edge1: number, x: number): number {
    const t = Math.min(Math.max((x - edge0) / (edge1 - edge0), 0), 1);
    return t * t * (3 - 2 * t);
}
