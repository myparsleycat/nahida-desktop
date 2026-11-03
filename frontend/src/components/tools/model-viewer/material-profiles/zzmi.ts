/*
 * The ZZMI toon shading below follows the Zenless Zone Zero shader of HoyoToon
 * (https://github.com/Hoyotoon/HoyoToon): its banded body shadow with post tints,
 * toon GGX and hair highlight specular, fresnel rim, emission and outline.
 *
 * This program is free software: you can redistribute it and/or modify it under
 * the terms of the GNU General Public License as published by the Free Software
 * Foundation, either version 3 of the License, or (at your option) any later
 * version.
 *
 * This program is distributed in the hope that it will be useful, but WITHOUT ANY
 * WARRANTY; without even the implied warranty of MERCHANTABILITY or FITNESS FOR A
 * PARTICULAR PURPOSE. See the GNU General Public License for more details.
 */
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
    DataTexture,
    Mesh,
    NoColorSpace,
    RGBAFormat,
    ShaderMaterial,
    UniformsLib,
    UniformsUtils,
    Vector3,
} from "three";

import type {
    MaterialProfileMeshData,
    ModelViewerMaterialProfile,
    ModelViewerToonStyleBase,
} from "./profile";

import {
    averagedNormals,
    fallbackWhiteTexture,
    meanNormal,
    replaceShaderAnchor,
    syncToonOutlineSource,
    textureUvTransform,
    TOON_FACE_GLSL,
    TOON_KEY_LIGHT_GLSL,
    TOON_STEP_GLSL,
    TOON_UV1_ATTRIBUTE,
    TOON_VERTEX_COLOR_ATTRIBUTE,
    toonLightMapOf,
} from "../model-viewer-shader-utils";
import { BASE_TOON_FLAGS, readToonFlags } from "./profile";

export const DEFAULT_ZZMI_TOON_STYLE: ModelViewerToonStyleBase = {
    outlines: true,
    selfShadow: true,
    lightFollowsCamera: true,
    fieldOfView: 20,
};

const ZZMI_TOON_FLAGS = ["outlines", "selfShadow", "lightFollowsCamera"] as const;

type ZZMIToonMeshData = MaterialProfileMeshData & {
    zzmiFace?: boolean;
    zzmiHair?: boolean;
    zzmiFaceForward?: Vector3;
    zzmiOutline?: Mesh<BufferGeometry, ShaderMaterial>;
};

type ZZMIToonShaderState = {
    enabled: { value: number };
    lightMap: { value: Texture };
    lightMapTransform: { value: Matrix3 };
    materialMap: { value: Texture };
    materialMapTransform: { value: Matrix3 };
    normalData: { value: Texture };
    normalDataTransform: { value: Matrix3 };
    face: { value: number };
    hair: { value: number };
    faceForward: { value: Vector3 };
    vertexColorEnabled: { value: number };
    uv1Enabled: { value: number };
};

// HoyoToon's material defaults. The game's per-material values come from material
// files that mods do not ship. Metallic is 1 instead of 0 so the light map's green
// marks metal, as the game's metal materials set it.
const ALBEDO_SMOOTHNESS = "0.05";
const SHADOW_COLOR = "vec3( 0.6 )";
const SHALLOW_COLOR = "vec3( 0.8 )";
const POST_SHALLOW_TINT = "vec3( 0.956863, 0.960784, 0.901961 )";
const POST_SHALLOW_FADE_TINT = "vec3( 0.874510, 0.8, 0.792157 )";
const POST_SHADOW_TINT = "vec3( 0.850980, 0.780392, 0.772549 )";
const POST_FRONT_TINT = "vec3( 1.0, 0.996078, 0.929412 )";
const POST_SSS_TINT = "vec3( 1.0, 0.948171, 0.929412 )";
const METALLIC = "1.0";
const GLOSSINESS = "0.5";
const SPECULAR_INTENSITY = "0.1";
const TOON_SPECULAR = "0.01";
const SHAPE_SOFTNESS = "0.1";
const UI_SUN_COLOR = "vec3( 1.0, 0.92, 0.9 )";

// HoyoToon's outline width defaults to 0 and takes the game's material value; this
// is a guess until it is measured against game frames.
const OUTLINE_WIDTH = "1.0";

// Pixels inside the silhouette that the rim covers, standing in for HoyoToon's depth
// texture offset of 0.0025 screen units per unit of inverse distance.
const RIM_PIXELS = "4.0";

// The light map holds the material ID in R, metallic in G and the specular mask in B;
// the material map holds opacity in R, smoothness in G and the emission and matcap
// mask in B; the normal map holds the normal in RG and the diffuse bias in B. On the
// face, the normal map holds the SDF in R and the chin AO in A.
const neutralLightMap = solidTexture(178, 0, 0, 255);
const neutralMaterialMap = solidTexture(255, 0, 0, 255);
const neutralNormalData = solidTexture(128, 128, 128, 255);

// Outline width in R, no face flags in B.
const NEUTRAL_VERTEX_COLOR = "vec4( 0.5, 0.5, 0.0, 1.0 )";

// Components end in a part name, such as EllenHair or AnbySFaceHead.
const ZZMI_HAIR_PART = /hair[a-z\d]?$/i;
const ZZMI_FACE_PART = /face(?:head)?[a-z\d]?$/i;

const ZZMI_TOON_GLSL = `${TOON_STEP_GLSL}

${TOON_FACE_GLSL}

// HoyoToon's color normalization: pulls a shadow color toward its own hue at full
// brightness.
vec3 zzmiNormalizeColor( vec3 color ) {
    color += 6e-5;
    float average = ( color.r + color.g + color.b ) / 3.0;
    return clamp( color / average, 0.0, 1.0 ) * 0.56275 + color * 0.43725;
}

// HoyoToon's body shadow: the shadow value runs from -3 to 3 and splits into deep
// shadow, shadow, shallow, transition, subsurface, front and lit bands, each with
// its tint.
vec3 zzmiShadowTint( float shade ) {
    float smoothness = ${ALBEDO_SMOOTHNESS};
    float k = 3.0 / ( 2.0 - 3.0 * smoothness );
    float shallowEdge = 0.5 * smoothness - 0.3333;
    float litEdge = 1.5 * smoothness + 0.3333;
    float p = ( shade + 0.3333 ) / smoothness + 0.5;
    float q = ( shade - 0.3333 ) / smoothness + 0.5;
    float r = q - 1.0;
    float deep = clamp( 1.0 - ( shade + 1.0 ) * k, 0.0, 1.0 );
    float shadow = clamp( min( 1.0 - p, ( shade + 1.0 ) * k ), 0.0, 1.0 );
    float shallow = clamp( min( p, 1.0 - ( shade - shallowEdge ) * k ), 0.0, 1.0 );
    float transition = clamp( min( 1.0 - q, ( shade - shallowEdge ) * k ), 0.0, 1.0 );
    float middle = clamp( min( 1.0 - r, q ), 0.0, 1.0 );
    float front = clamp( min( r, 1.0 - ( shade - litEdge ) * k ), 0.0, 1.0 );
    float lit = clamp( ( shade - litEdge ) * k, 0.0, 1.0 );
    vec3 shadowColor = zzmiNormalizeColor( ${SHADOW_COLOR} );
    vec3 shallowColor = zzmiNormalizeColor( ${SHALLOW_COLOR} );
    vec3 tint = ${POST_SHADOW_TINT} * shadowColor * ( deep + shadow )
        + ${POST_SHALLOW_FADE_TINT} * shallowColor * shallow
        + ${POST_SHALLOW_TINT} * shallowColor * transition
        + ${POST_FRONT_TINT} * front
        + ${POST_SSS_TINT} * middle
        + vec3( lit );
    return clamp( tint, 0.0, 1.0 );
}

// HoyoToon's toon GGX on the shaded color, or on hair a highlight band that the
// specular mask cuts from the half-Lambert of the half vector.
vec3 zzmiSpecular( vec3 color, vec3 normal, vec3 light, vec3 view, vec4 lightMap, float smoothness, float shade, float hair ) {
    float NoL = dot( normal, light );
    float lambert = clamp( NoL, 0.0, 1.0 );
    float luminance = dot( color, vec3( 0.29, 0.6, 0.11 ) ) * 0.2875 + 1.4375;
    vec3 contrast = pow( color, vec3( mix( luminance, 1.0, 0.5 * ( NoL * 0.5 + 0.5 ) + 0.5 * lambert ) ) );
    vec3 surface = mix( color, contrast, 0.5 );
    surface = mix( surface, contrast, lambert );
    vec3 f0 = mix( vec3( 0.04 ), surface, ${METALLIC} * lightMap.g );

    float roughness = 1.0 - smoothness * ${GLOSSINESS};
    float a = roughness * roughness;
    vec3 halfDirection = normalize( light + view );
    float NoH = dot( normal, halfDirection );
    float mask = lightMap.b;
    if ( hair > 0.5 ) {
        float band = clamp( NoH * 0.5 + 0.5, 0.0, 1.0 ) * clamp( shade * 1.5 + 0.5, 0.0, 1.0 );
        mask = clamp( ( lightMap.b - ( 1.0 - band ) ) / ${SHAPE_SOFTNESS}, 0.0, 1.0 );
    }

    float wrappedNoL = clamp( NoL * 0.75 + 0.25, 0.0, 1.0 );
    float wrappedNoH = clamp( NoH * 0.75 + 0.25, 0.0, 1.0 );
    float wrappedLoH = clamp( dot( light, halfDirection ) * 0.75 + 0.25, 0.0, 1.0 );
    float LoH2 = max( 0.1, wrappedLoH * wrappedLoH );
    float d = wrappedNoH * wrappedNoH * ( a * a - 1.0 ) + 1.00001;
    float D = ( a * a ) / ( d * d * LoH2 * ( 4.0 * a + 2.0 ) );
    float toon = clamp( D - smoothness * ${GLOSSINESS}, 0.0, 1.0 ) * wrappedNoL / max( 1e-5, a );
    toon = clamp( 10.0 * toon * ${TOON_SPECULAR}, 0.0, 1.0 ) * 100.0;
    float shaped = min( 1.0, 0.16666 / LoH2 ) * wrappedNoL * 100.0;
    float lobe = hair > 0.5 ? shaped : toon;
    return lobe * 0.5 * ${SPECULAR_INTENSITY} * 10.0 * mask * f0 * color;
}

// HoyoToon's fresnel rim: brighter toward the camera-facing light and on upward
// normals, wider off skin, tinted by the sun color normalized to its brightness.
vec3 zzmiRim( vec3 normal, vec3 view, vec3 light, float skin ) {
    float frontLight = pow( dot( view, light ) * 0.5 + 0.5, 4.0 );
    float upward = mix( normal.y, 1.0, 0.5 ) * 0.5 + 0.5;
    upward = clamp( skin > 0.5 ? upward : upward * upward, 0.0, 1.0 );
    upward = upward * upward * ( 3.0 - 2.0 * upward );
    float upward5 = pow( upward, 5.0 );
    upward *= skin > 0.5 ? 1.0 - 0.5 * upward5 : 0.9 + 0.1 * upward5;
    float weight = ( frontLight * 0.5 + 0.5 ) * upward;

    vec2 fresnel = clamp( ( 1.0 - dot( view, normal ) - vec2( 0.70833, 0.45833 ) ) * vec2( 5.0, 3.33333 ), 0.0, 1.0 );
    fresnel = fresnel * fresnel * ( 3.0 - 2.0 * fresnel );
    float rim = weight * ( skin > 0.5 ? fresnel.x : fresnel.y );
    rim *= 1.0 + 0.5 * rim * rim;

    vec3 sun = ${UI_SUN_COLOR};
    float grey = skin > 0.5 ? dot( sun, vec3( 0.33 ) ) : dot( sun, vec3( 0.3, 0.6, 0.1 ) );
    sun = skin > 0.5 ? sun : vec3( grey );
    sun = pow( sun, vec3( 8.0 ) );
    sun *= grey / ( 6.1e-5 + dot( sun, vec3( 0.7 ) ) );
    sun = mix( sun, vec3( 1.0 ), pow( frontLight, 20.0 ) );
    return rim * sun;
}`;

export const zzmiMaterialProfile: ModelViewerMaterialProfile = {
    baseMaterial: { metalness: 0.05, roughness: 0.65 },
    derivativeTangentFrame: false,
    toonShadows: true,
    toon: {
        defaultStyle: DEFAULT_ZZMI_TOON_STYLE,
        parse: (record) => readToonFlags(record, DEFAULT_ZZMI_TOON_STYLE, ZZMI_TOON_FLAGS),
        serialize: (style) => pick(style, ZZMI_TOON_FLAGS),
        flags: BASE_TOON_FLAGS,
    },
    markMeshes: markZZMIMeshes,
    configureMaterial: configureZZMIMaterialShader,

    // Without toon shading the packed maps drive the stock PBR model: LightMap.G is
    // metallic and MaterialMap.G is smoothness. The shader adapter below performs the
    // channel selection and inversion.
    bindMaps(material, maps, textures) {
        material.metalnessMap = maps.lightMapKey ? (textures.get(maps.lightMapKey) ?? null) : null;
        if (material.metalnessMap) material.metalness = 1;
        material.roughnessMap = maps.materialMapKey
            ? (textures.get(maps.materialMapKey) ?? null)
            : null;
        if (material.roughnessMap) material.roughness = 1;

        const state = material.userData.zzmiToonMaterial as ZZMIToonShaderState | undefined;
        if (!state) {
            return;
        }
        const bind = (
            texture: { value: Texture },
            transform: { value: Matrix3 },
            key: string | null | undefined,
            neutral: Texture,
        ) => {
            texture.value = toonLightMapOf(key ? textures.get(key) : undefined) ?? neutral;
            transform.value = textureUvTransform(texture.value);
        };
        bind(state.lightMap, state.lightMapTransform, maps.lightMapKey, neutralLightMap);
        bind(
            state.materialMap,
            state.materialMapTransform,
            maps.materialMapKey,
            neutralMaterialMap,
        );
        bind(state.normalData, state.normalDataTransform, maps.normalMapKey, neutralNormalData);
    },
    sync: syncZZMIToon,
};

function configureZZMIMaterialShader(material: MeshStandardMaterial): void {
    if (material.userData.zzmiToonMaterial) {
        return;
    }
    const state: ZZMIToonShaderState = {
        enabled: { value: 0 },
        lightMap: { value: neutralLightMap },
        lightMapTransform: { value: neutralLightMap.matrix },
        materialMap: { value: neutralMaterialMap },
        materialMapTransform: { value: neutralMaterialMap.matrix },
        normalData: { value: neutralNormalData },
        normalDataTransform: { value: neutralNormalData.matrix },
        face: { value: 0 },
        hair: { value: 0 },
        faceForward: { value: new Vector3(0, 0, 1) },
        vertexColorEnabled: { value: 0 },
        uv1Enabled: { value: 0 },
    };
    material.userData.zzmiToonMaterial = state;
    material.onBeforeCompile = (shader: WebGLProgramParametersWithUniforms) => {
        // LightMap.G is metallic and MaterialMap.G is smoothness for the stock PBR
        // path, not Three.js's B and G. The roughness map is bound with roughness 1,
        // so the inversion yields 1 - smoothness.
        shader.fragmentShader = replaceShaderAnchor(
            shader.fragmentShader,
            "#include <roughnessmap_fragment>",
            `float roughnessFactor = roughness;
#ifdef USE_ROUGHNESSMAP
    vec4 texelRoughness = texture2D( roughnessMap, vRoughnessMapUv );
    roughnessFactor *= texelRoughness.g;
    roughnessFactor = 1.0 - roughnessFactor;
#endif`,
        );
        shader.fragmentShader = replaceShaderAnchor(
            shader.fragmentShader,
            "#include <metalnessmap_fragment>",
            `float metalnessFactor = metalness;
#ifdef USE_METALNESSMAP
    vec4 texelMetalness = texture2D( metalnessMap, vMetalnessMapUv );
    metalnessFactor *= texelMetalness.g;
#endif`,
        );

        shader.uniforms.zzmiToonEnabled = state.enabled;
        shader.uniforms.zzmiLightMap = state.lightMap;
        shader.uniforms.zzmiLightMapTransform = state.lightMapTransform;
        shader.uniforms.zzmiMaterialMap = state.materialMap;
        shader.uniforms.zzmiMaterialMapTransform = state.materialMapTransform;
        shader.uniforms.zzmiNormalData = state.normalData;
        shader.uniforms.zzmiNormalDataTransform = state.normalDataTransform;
        shader.uniforms.zzmiFace = state.face;
        shader.uniforms.zzmiHair = state.hair;
        shader.uniforms.zzmiFaceForward = state.faceForward;
        shader.uniforms.zzmiVertexColorEnabled = state.vertexColorEnabled;
        shader.uniforms.zzmiUv1Enabled = state.uv1Enabled;
        shader.vertexShader = replaceShaderAnchor(
            shader.vertexShader,
            "#include <common>",
            `#include <common>
attribute vec4 ${TOON_VERTEX_COLOR_ATTRIBUTE};
attribute vec2 ${TOON_UV1_ATTRIBUTE};
uniform float zzmiVertexColorEnabled;
uniform float zzmiUv1Enabled;
uniform vec3 zzmiFaceForward;
varying vec2 vZZMIUv;
varying vec2 vZZMIFaceUv;
varying float vZZMIFaceFlag;
varying vec4 vZZMIColor;
varying vec3 vZZMIFaceForward;`,
        );
        shader.vertexShader = replaceShaderAnchor(
            shader.vertexShader,
            "#include <uv_vertex>",
            `#include <uv_vertex>
vZZMIUv = uv;
vZZMIColor = zzmiVertexColorEnabled > 0.5 ? ${TOON_VERTEX_COLOR_ATTRIBUTE} : ${NEUTRAL_VERTEX_COLOR};
vZZMIFaceFlag = mod( floor( floor( vZZMIColor.b * 255.0 + 0.5 ) / 16.0 ), 2.0 );
vZZMIFaceUv = zzmiVertexColorEnabled > 0.5 && zzmiUv1Enabled > 0.5 && vZZMIFaceFlag < 0.5 ? ${TOON_UV1_ATTRIBUTE} : uv;
vZZMIFaceForward = normalize( mat3( modelMatrix ) * zzmiFaceForward );`,
        );
        shader.fragmentShader = replaceShaderAnchor(
            shader.fragmentShader,
            "#include <common>",
            `#include <common>
varying vec2 vZZMIUv;
varying vec2 vZZMIFaceUv;
varying float vZZMIFaceFlag;
varying vec4 vZZMIColor;
varying vec3 vZZMIFaceForward;
uniform float zzmiToonEnabled;
uniform sampler2D zzmiLightMap;
uniform mat3 zzmiLightMapTransform;
uniform sampler2D zzmiMaterialMap;
uniform mat3 zzmiMaterialMapTransform;
uniform sampler2D zzmiNormalData;
uniform mat3 zzmiNormalDataTransform;
uniform float zzmiFace;
uniform float zzmiHair;

${ZZMI_TOON_GLSL}`,
        );

        // Replaces the lit result in HoyoToon's order: the shadow tint over the albedo,
        // specular, rim, then emission.
        shader.fragmentShader = replaceShaderAnchor(
            shader.fragmentShader,
            "#include <opaque_fragment>",
            `#if NUM_DIR_LIGHTS > 0
if ( zzmiToonEnabled > 0.5 ) {
    ${TOON_KEY_LIGHT_GLSL}
    vec3 albedo = diffuseColor.rgb;
    vec4 lightMap = texture2D( zzmiLightMap, ( zzmiLightMapTransform * vec3( vZZMIUv, 1.0 ) ).xy );
    vec4 materialMap = texture2D( zzmiMaterialMap, ( zzmiMaterialMapTransform * vec3( vZZMIUv, 1.0 ) ).xy );
    vec4 normalData = texture2D( zzmiNormalData, ( zzmiNormalDataTransform * vec3( vZZMIUv, 1.0 ) ).xy );
    float skin = 4.0 - floor( 4.5 * lightMap.r ) < 0.5 ? 1.0 : 0.0;
    float NoL = dot( worldNormal, worldLight );

    // The normal map's blue biases the shadow: 0.5 is neutral.
    float shade = ( normalData.b * occlusion * 2.0 - 1.0 ) * 2.0 + NoL * occlusion;

    if ( zzmiFace > 0.5 ) {
        // The face SDF turns soft past its threshold; the bands sharpen it again.
        vec2 faceLight = toonFaceLight( vZZMIFaceForward, worldLight );
        vec2 faceUv = vZZMIFaceUv;
        if ( faceLight.y > 0.5 ) faceUv.x = 1.0 - faceUv.x;
        vec4 faceMap = texture2D( zzmiNormalData, ( zzmiNormalDataTransform * vec3( faceUv, 1.0 ) ).xy );
        float threshold = 0.5 - 0.5 * faceLight.x;
        float faceAO = vZZMIFaceFlag > 0.5 ? faceMap.a : 1.0;
        float faceLit = smoothstep( threshold - 0.5, threshold + 0.5, faceMap.r * 0.9 + 0.1 ) * faceAO;
        shade = ( faceLit * 2.0 - 1.0 ) * occlusion;
    }

    vec3 color = clamp( albedo * zzmiShadowTint( shade ), 0.0, 1.0 );
    vec3 specular = zzmiSpecular( color, worldNormal, worldLight, worldView, lightMap, materialMap.g, shade, zzmiHair );
    color += specular * ( skin > 0.5 ? 0.1 : 1.0 ) * ( 1.0 - zzmiFace );

    // HoyoToon's rim covers the band whose depth steps away past the silhouette,
    // on the lit side.
    float NoV = clamp( dot( worldNormal, worldView ), 0.0, 1.0 );
    float rimMask = toonSilhouetteBand( NoV, ${RIM_PIXELS} ) * clamp( NoL * 10.0, 0.0, 1.0 );
    color += zzmiRim( worldNormal, worldView, worldLight, skin ) * 1.5 * rimMask;

    // The material map's blue above 0.2 marks emission; below it is the matcap
    // mask, and the game's matcaps are not in mods.
    color += albedo * clamp( ( materialMap.b - 0.2 ) * 1.25, 0.0, 1.0 );
    outgoingLight = color;
}
#endif
#include <opaque_fragment>`,
        );

        // The toon result is display-referred, so the viewer's exposure and tone
        // mapping would only shift it.
        shader.fragmentShader = replaceShaderAnchor(
            shader.fragmentShader,
            "#include <tonemapping_fragment>",
            `if ( zzmiToonEnabled < 0.5 ) {
    #include <tonemapping_fragment>
}`,
        );
    };
    material.customProgramCacheKey = () => "zzmi-toon-v1";
    material.needsUpdate = true;
}

function syncZZMIToon(
    object: Mesh,
    material: MeshStandardMaterial,
    meshData: MaterialProfileMeshData,
): void {
    const data = meshData as ZZMIToonMeshData;
    const state = material.userData.zzmiToonMaterial as ZZMIToonShaderState | undefined;
    if (!state) {
        return;
    }
    const style = data.toonStyle ?? DEFAULT_ZZMI_TOON_STYLE;
    const enabled = data.toonShadows;
    state.enabled.value = enabled ? 1 : 0;
    state.face.value = data.zzmiFace ? 1 : 0;
    state.hair.value = data.zzmiHair ? 1 : 0;
    if (data.zzmiFaceForward) state.faceForward.value.copy(data.zzmiFaceForward);
    state.vertexColorEnabled.value = object.geometry.attributes[TOON_VERTEX_COLOR_ATTRIBUTE]
        ? 1
        : 0;
    state.uv1Enabled.value = object.geometry.attributes[TOON_UV1_ATTRIBUTE] ? 1 : 0;

    // Coverage cards are flat ink decals: they neither block light nor have a silhouette.
    const coverageMask = Boolean(material.map?.userData.modelViewerCoverageMask);
    object.castShadow = enabled && style.selfShadow && !coverageMask;
    object.receiveShadow = enabled && style.selfShadow;

    const showOutline = enabled && style.outlines && !coverageMask;
    if (!showOutline && !data.zzmiOutline) {
        return;
    }
    data.zzmiOutline = syncToonOutlineSource(data.zzmiOutline, data.tangentFrameUsable, () =>
        createZZMIToonOutline(object),
    );
    if (!data.zzmiOutline) {
        return;
    }
    const uniforms = data.zzmiOutline.material.uniforms;
    data.zzmiOutline.visible = showOutline;
    uniforms.map.value = material.map ?? fallbackWhiteTexture;
    uniforms.mapTransform.value = textureUvTransform(uniforms.map.value);
    uniforms.vertexColorEnabled.value = state.vertexColorEnabled.value;
}

function markZZMIMeshes(transport: ModViewerTransport, objects: Mesh[]): void {
    transport.meshes.forEach((mesh, index) => {
        const data = objects[index].userData as ZZMIToonMeshData;
        data.zzmiFace = ZZMI_FACE_PART.test(mesh.component);
        data.normalMapUnused = data.zzmiFace;
        data.zzmiHair = !data.zzmiFace && ZZMI_HAIR_PART.test(mesh.component);
        data.zzmiFaceForward = data.zzmiFace ? meanNormal(objects[index].geometry) : undefined;
    });
}

// HoyoToon's inverted hull: it extrudes in view space along a smoothed normal, laid
// flat on the screen, by a width scaled by the vertex color's red and, unlike the
// Genshin and Star Rail hulls, not by depth or field of view, and is pushed
// slightly away from the camera. The game keeps that normal in a UV channel the viewer
// does not decode, so seam-averaged normals stand in. The color is the albedo under
// a dimmed outline tint, darker on the side away from the light.
function createZZMIToonOutline(object: Mesh): Mesh<BufferGeometry, ShaderMaterial> {
    if (!object.geometry.attributes.outlineNormal) {
        object.geometry.setAttribute("outlineNormal", averagedNormals(object.geometry));
    }
    const outline = new Mesh(
        object.geometry,
        new ShaderMaterial({
            lights: true,
            uniforms: UniformsUtils.merge([
                UniformsLib.lights,
                {
                    map: { value: fallbackWhiteTexture },
                    mapTransform: { value: fallbackWhiteTexture.matrix },
                    vertexColorEnabled: { value: 0 },
                },
            ]),
            vertexShader: `attribute vec3 outlineNormal;
attribute vec4 ${TOON_VERTEX_COLOR_ATTRIBUTE};
uniform mat3 mapTransform;
uniform float vertexColorEnabled;
varying vec2 vOutlineUv;
varying vec3 vOutlineNormal;
varying float vOutlineFacing;
void main() {
    vOutlineUv = ( mapTransform * vec3( uv, 1.0 ) ).xy;
    vec4 color = vertexColorEnabled > 0.5 ? ${TOON_VERTEX_COLOR_ATTRIBUTE} : ${NEUTRAL_VERTEX_COLOR};
    vec3 smoothNormal = normalMatrix * outlineNormal;
    float lengthSquared = dot( smoothNormal, smoothNormal );
    smoothNormal = lengthSquared > 1e-10 ? smoothNormal * inversesqrt( lengthSquared ) : vec3( 0.0, 0.0, 1.0 );
    vOutlineNormal = smoothNormal;

    vec4 viewPosition = modelViewMatrix * vec4( position, 1.0 );
    vOutlineFacing = dot( smoothNormal, normalize( -viewPosition.xyz ) );
    vec3 direction = vec3( normalize( vec3( smoothNormal.xy, 0.0001 ) ).xy, 0.0001 );
    float depth = abs( viewPosition.z ) * ( 2.414 / projectionMatrix[ 1 ][ 1 ] );
    viewPosition.xyz *= 1.0 + 0.0001 * mix( 0.1, 1.0, clamp( 1.0 - depth, 0.0, 1.0 ) );
    viewPosition.xyz += direction * ${OUTLINE_WIDTH} * color.r * 0.0015;
    gl_Position = projectionMatrix * viewPosition;
}`,
            fragmentShader: `#include <common>
#include <lights_pars_begin>
uniform sampler2D map;
varying vec2 vOutlineUv;
varying vec3 vOutlineNormal;
varying float vOutlineFacing;
void main() {
    // The hull past the silhouette faces sideways. Its far side faces away and is
    // only visible through openings such as a missing face.
    if ( vOutlineFacing < -0.5 ) discard;
    float NoL = 1.0;
    #if NUM_DIR_LIGHTS > 0
    NoL = clamp( dot( normalize( vOutlineNormal ), directionalLights[ 0 ].direction ), 0.0, 1.0 );
    #endif
    vec3 tint = vec3( pow( 0.5, 1.5 ) );
    vec3 albedo = texture2D( map, vOutlineUv ).rgb;
    gl_FragColor = vec4( mix( tint * 0.01, tint, NoL * 0.5 + 0.5 ) * albedo, 1.0 );
    #include <colorspace_fragment>
}`,
            side: BackSide,
        }),
    );
    outline.raycast = () => {};
    object.add(outline);
    return outline;
}

function solidTexture(r: number, g: number, b: number, a: number): DataTexture {
    const texture = new DataTexture(new Uint8Array([r, g, b, a]), 1, 1, RGBAFormat);
    texture.colorSpace = NoColorSpace;
    texture.needsUpdate = true;
    return texture;
}
