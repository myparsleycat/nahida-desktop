/*
 * The GIMI shading below follows the Genshin Impact shader of HoyoToon
 * (https://github.com/Hoyotoon/HoyoToon, GPL-3.0): its shadow area and ramp lookup,
 * face SDF, specular, metal matcap, rim light, outline width and tone curve. The
 * ramp rows, the procedural ramp and metal map, and the material structure come
 * from the earlier port of UnityGenshinToonShader
 * (https://github.com/kaze-mio/UnityGenshinToonShader), whose notice follows.
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

import {
    averagedNormals,
    meanNormal,
    replaceShaderAnchor,
    syncToonOutlineSource,
    textureUvTransform,
    TOON_FACE_GLSL,
    TOON_KEY_LIGHT_GLSL,
    TOON_STEP_GLSL,
    TOON_VERTEX_COLOR_ATTRIBUTE,
    toonLightMapOf,
} from "../model-viewer-shader-utils";
import { BASE_TOON_FLAGS, readToonFlags } from "./profile";

export const GENSHIN_FACE_TYPES = ["boy", "girl", "lady", "loli", "male"] as const;
export type GenshinFaceType = (typeof GENSHIN_FACE_TYPES)[number];

export type GIMIToonStyle = ModelViewerToonStyleBase & {
    daytime: boolean;
    /** HoyoToon's _LightArea: the shadow area below which a texel is in shadow. */
    lightArea: number;
    shadowBrightness: number;
    brightness: number;
    specular: number;
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

// Shading and outline values are HoyoToon's defaults, except the light area: with its
// 0.55, frontal light shades limbs that the game keeps lit to the silhouette. The
// shadow brightness and specular strength come from kaze-mio's Nahida material.
export const DEFAULT_GIMI_TOON_STYLE: GIMIToonStyle = {
    daytime: true,
    lightArea: 0.5,
    shadowBrightness: 1.1,
    brightness: 1,
    specular: 0.3,
    rimStrength: 1,
    emission: 0.2,
    outlines: true,
    outlineWidth: 0.03,
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
    lightArea: { value: number };
    shadowBrightness: { value: number };
    brightness: { value: number };
    specular: { value: number };
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

// HoyoToon's _OutlineWidthAdjustZs and _OutlineWidthAdjustScales: the outline width
// scale at these field-of-view-normalized depths, interpolated in two pieces.
const OUTLINE_ADJUST_DEPTHS = "vec3( 0.001, 2.0, 6.0 )";
const OUTLINE_ADJUST_SCALES = "vec3( 0.01, 0.245, 0.6 )";

// Pixels inside the silhouette that the rim covers, standing in for HoyoToon's
// 1.5 × 0.00044 screen-width depth offset at a 1080p-class viewport.
const RIM_PIXELS = "2.5";

const whiteTexture = solidTexture(255, 255, 255, 255);
const neutralLightMap = solidTexture(0, 128, 0, 77);

let proceduralRamp: DataTexture | undefined;
let proceduralMetalMap: DataTexture | undefined;

const GENSHIN_COMMON_GLSL = `${TOON_STEP_GLSL}

float genshinMaterialRow( float id ) {
    if ( id < 0.2 ) return 4.0;
    if ( id < 0.4 ) return 1.0;
    if ( id < 0.6 ) return 2.0;
    if ( id < 0.8 ) return 0.0;
    return 3.0;
}

// HoyoToon's Genshin post curve: a bloom-like lift above 0.6, a rational filmic
// curve, then the game's white balance.
vec3 genshinToneMap( vec3 x ) {
    x += max( x - 0.6, vec3( 0.0 ) ) * 0.7;
    x = clamp( ( ( 1.36 * x + 0.047 ) * x ) / ( ( 0.93 * x + 0.56 ) * x + 0.14 ), 0.0, 1.0 );
    return vec3(
        dot( vec3( 1.0, 0.021, -0.019 ), x ),
        dot( vec3( 0.001, 1.04, 0.01 ), x ),
        0.951 * x.b
    );
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

    bindMaps(material, maps, textures) {
        const state = material.userData.genshinToonMaterial as GenshinToonShaderState | undefined;
        if (!state) {
            return;
        }
        const lightMap = toonLightMapOf(
            maps.lightMapKey ? textures.get(maps.lightMapKey) : undefined,
        );
        state.lightMap.value = lightMap ?? neutralLightMap;
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
        lightArea: { value: defaults.lightArea },
        shadowBrightness: { value: defaults.shadowBrightness },
        brightness: { value: defaults.brightness },
        specular: { value: defaults.specular },
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
        shader.uniforms.genshinLightArea = state.lightArea;
        shader.uniforms.genshinShadowBrightness = state.shadowBrightness;
        shader.uniforms.genshinBrightness = state.brightness;
        shader.uniforms.genshinSpecular = state.specular;
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
varying vec3 vGenshinFaceForward;
varying float vGenshinDepthScale;`,
        );
        shader.vertexShader = replaceShaderAnchor(
            shader.vertexShader,
            "#include <uv_vertex>",
            `#include <uv_vertex>
vGenshinUv = uv;
vGenshinColor = genshinVertexColorEnabled > 0.5 ? ${TOON_VERTEX_COLOR_ATTRIBUTE} : vec4( 1.0, 0.5, 1.0, 0.5 );
vGenshinFaceForward = normalize( mat3( modelMatrix ) * genshinFaceForward );
vGenshinDepthScale = 2.414 / projectionMatrix[ 1 ][ 1 ];`,
        );
        shader.fragmentShader = replaceShaderAnchor(
            shader.fragmentShader,
            "#include <common>",
            `#include <common>
varying vec2 vGenshinUv;
varying vec4 vGenshinColor;
varying vec3 vGenshinFaceForward;
varying float vGenshinDepthScale;
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
uniform float genshinLightArea;
uniform float genshinShadowBrightness;
uniform float genshinBrightness;
uniform float genshinSpecular;
uniform float genshinRimStrength;
uniform float genshinEmission;
uniform float genshinEmissionInverted;
uniform float genshinToneMapping;

${GENSHIN_COMMON_GLSL}

${TOON_FACE_GLSL}`,
        );

        // Replaces the lit result with HoyoToon's ramp shading: the light map's AO
        // and the half-Lambert give a shadow area, whose distance below the light
        // area picks a column of the shadow ramp; the light map's alpha picks its row.
        shader.fragmentShader = replaceShaderAnchor(
            shader.fragmentShader,
            "#include <opaque_fragment>",
            `#if NUM_DIR_LIGHTS > 0
if ( genshinToonEnabled > 0.5 ) {
    ${TOON_KEY_LIGHT_GLSL}
    vec3 albedo = diffuseColor.rgb;
    vec4 lightMap = texture2D( genshinLightMap, ( genshinLightMapTransform * vec3( vGenshinUv, 1.0 ) ).xy );
    float NoL = dot( worldNormal, worldLight );
    float row = genshinMaterialRow( lightMap.a );
    float face = genshinFace;

    // The light map's AO is pushed away from 0.5 and scaled by the vertex AO; near
    // 0 or 1 it forces shadow or light.
    float aoOffset = lightMap.g - 0.5;
    float threshold = ( 2.0 * aoOffset * abs( aoOffset ) + 0.5 ) * vGenshinColor.r;
    float area = threshold > 0.95 ? 1.0 : ( threshold < 0.05 ? 0.0 : ( NoL * 0.5 + 0.5 + threshold ) * 0.5 );
    float lit = toonStep( genshinLightArea, area );
    float rampU = 1.0 - min( max( genshinLightArea - area, 0.0 ) / genshinLightArea, 1.0 );

    if ( genshinFace > 0.5 ) {
        row = 3.0;
        vec2 faceLight = toonFaceLight( vGenshinFaceForward, worldLight );
        if ( genshinFaceLightMapEnabled > 0.5 ) {
            vec2 faceUv = vGenshinUv;
            if ( faceLight.y > 0.5 ) faceUv.x = 1.0 - faceUv.x;
            float faceThreshold = smoothstep( 0.0, 1.0, 0.5 - 0.5 * faceLight.x );
            lit = toonStep( faceThreshold, texture2D( genshinFaceLightMap, faceUv ).a );
            lit = mix( lit, 1.0, texture2D( genshinFaceShadow, vGenshinUv ).a * genshinFaceShadowEnabled );
        } else {
            lit = smoothstep( -0.35, -0.15, faceLight.x );
        }
        rampU = 0.0;
    }

    // The cast shadow darkens like the ramp's deepest column.
    lit *= occlusion;
    rampU *= occlusion;
    float rampV = ( row + 5.0 * genshinDaytime + 0.5 ) / 10.0;
    vec3 ramp = texture2D( genshinRamp, vec2( clamp( rampU, 0.004, 0.996 ), rampV ) ).rgb;
    vec3 shade = ramp * mix( vec3( genshinShadowBrightness ), vec3( 1.0 ), smoothstep( 0.9, 1.0, rampU ) );
    shade = mix( shade, vec3( 1.0 ), lit );

    // Light map red at 0.9 and above marks metal, drawn from a view-space matcap
    // with a sharp highlight, both dimmed in shadow.
    vec3 halfDirection = normalize( worldLight + worldView );
    float NoH = max( dot( worldNormal, halfDirection ), 0.001 );
    float metal = step( 0.89, lightMap.r );
    float metalMap = clamp( texture2D( genshinMetalMap, normalize( normal ).xy * 0.5 + 0.5 ).r * 3.0, 0.0, 1.0 );
    float metalSpecular = clamp( pow( NoH, 90.0 ) * 15.0, 0.0, 1.0 ) * lightMap.r;
    metalSpecular *= mix( 0.2, 1.0, lit );
    vec3 metalColor = albedo * mix( vec3( 0.51, 0.3, 0.19 ), vec3( 1.0 ), metalMap ) + 0.5 * metalSpecular;
    metalColor *= mix( vec3( 0.78, 0.77, 0.82 ), vec3( 1.0 ), lit );

    // Other materials get a hard Blinn-Phong highlight past the light map's blue,
    // tinted 10% toward the surface color.
    float term = pow( NoH, 10.0 );
    float specular = toonStep( 1.015 - lightMap.b, term ) * term * lightMap.r * genshinSpecular * 0.5;
    specular *= ( 1.0 - metal ) * ( 1.0 - face );

    vec3 surface = mix( albedo, metalColor, metal * ( 1.0 - face ) );
    vec3 toon = ( surface * shade + ( 0.9 + 0.1 * surface ) * specular ) * genshinBrightness;

    // The 4.0+ rim: a thin band inside the silhouette, brightest where the normal
    // turns away from the camera-to-light direction, fading with distance.
    float NoV = clamp( dot( worldNormal, worldView ), 0.0, 1.0 );
    vec3 rimDirection = worldView - worldLight;
    rimDirection = dot( rimDirection, rimDirection ) > 1e-6 ? normalize( rimDirection ) : worldView;
    float rimFacing = dot( worldNormal, rimDirection );
    float rimLight = clamp( pow( max( 1.0 - rimFacing, 0.0 ), 7.5 ), 0.0, 1.0 ) + clamp( pow( max( rimFacing, 0.0 ), 7.5 ), 0.0, 1.0 );
    float viewDepth = abs( vViewPosition.z ) * vGenshinDepthScale;
    float rimBand = toonSilhouetteBand( NoV, ${RIM_PIXELS} ) * clamp( 1.0 - viewDepth / 5.0, 0.0, 1.0 );
    toon += clamp( albedo * 5.0 * rimLight * rimBand, 0.0, 1.0 ) * genshinRimStrength * ( 1.0 - face );

    // The emission mask is the texture's own alpha, before the viewer's alpha
    // inversion for diffuse maps that store opacity inverted.
    float emissionMask = mix( diffuseColor.a, 1.0 - diffuseColor.a, genshinEmissionInverted );
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
    const face = Boolean(data.genshinFace);
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
    state.lightArea.value = style.lightArea;
    state.shadowBrightness.value = style.shadowBrightness;
    state.brightness.value = style.brightness;
    state.specular.value = style.specular;
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

    data.genshinOutline = syncToonOutlineSource(
        data.genshinOutline,
        data.tangentFrameUsable,
        (tangent) => createGenshinToonOutline(object, tangent),
    );
    if (!data.genshinOutline) {
        return;
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
 * only reliable signal. A face also gets its forward direction from its normals.
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
        data.genshinFaceForward = meanNormal(object.geometry);
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
                load(assets.metalMapUrl, false),
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

// HoyoToon's inverted hull: it extrudes in view space along the smoothed normal GIMI
// stores in TANGENT, flattened onto the screen, by a width that grows with distance
// in two linear pieces and scales with the vertex color's alpha, and it is pushed
// slightly away from the camera.
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
    vec3 adjustDepths = ${OUTLINE_ADJUST_DEPTHS};
    vec3 adjustScales = ${OUTLINE_ADJUST_SCALES};
    vec4 piece = depth < adjustDepths.y
        ? vec4( adjustDepths.xy, adjustScales.xy )
        : vec4( adjustDepths.yz, adjustScales.yz );
    float depthScale = mix( piece.z, piece.w, clamp( ( depth - piece.x ) / max( piece.y - piece.x, 0.001 ), 0.0, 1.0 ) );
    float widthScale = vertexColorEnabled > 0.5 ? ${TOON_VERTEX_COLOR_ATTRIBUTE}.a : 0.5;
    float width = outlineWidth * 1.5 * depthScale * 0.414 * widthScale;
    viewPosition.xyz *= 1.0 + 0.01 * mix( 0.1, 1.0, clamp( 1.0 - depth, 0.0, 1.0 ) );
    viewPosition.xyz += width * direction;
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
