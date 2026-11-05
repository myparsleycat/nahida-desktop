/*
 * The SRMI shading below follows the Star Rail shader of HoyoToon
 * (https://github.com/Hoyotoon/HoyoToon, GPL-3.0): its shadow rate, specular, rim
 * light and outline. The face outline mask, the face SDF read from the light map and
 * the shadow band come from StarRailNPRShader
 * (https://github.com/stalomeow/StarRailNPRShader), with HonkaiStarRailToonShader
 * (https://github.com/Gaolingx/HonkaiStarRailToonShader) as a secondary reference.
 *
 * Copyright (C) 2023 Stalo
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
    TOON_VERTEX_COLOR_ATTRIBUTE,
    toonLightMapOf,
} from "../model-viewer-shader-utils";
import { BASE_TOON_FLAGS, readToonFlags } from "./profile";

export const DEFAULT_SRMI_TOON_STYLE: ModelViewerToonStyleBase = {
    outlines: true,
    selfShadow: true,
    lightFollowsCamera: true,
    fieldOfView: 20,
};

const SRMI_TOON_FLAGS = ["outlines", "selfShadow", "lightFollowsCamera"] as const;

type SRMIToonMeshData = MaterialProfileMeshData & {
    srmiFace?: boolean;
    srmiHair?: boolean;
    srmiFaceForward?: Vector3;
    srmiOutline?: Mesh<BufferGeometry, ShaderMaterial>;
};

type SRMIToonShaderState = {
    enabled: { value: number };
    lightMap: { value: Texture };
    lightMapEnabled: { value: number };
    lightMapTransform: { value: Matrix3 };
    face: { value: number };
    faceForward: { value: Vector3 };
    vertexColorEnabled: { value: number };
};

// Specular and rim values are HoyoToon's defaults. The game's per-material
// values come from its material value LUT, which mods do not ship.
const SPECULAR_SHININESS = "10.0";
const SPECULAR_ROUGHNESS = "0.02";
const SPECULAR_INTENSITY = "0.5";
const RIM_INTENSITY = "0.1";
const OUTLINE_WIDTH = "0.05";
const OUTLINE_SCALE = "0.187";

// Pixels inside the silhouette that the rim covers, standing in for HoyoToon's depth
// texture offset of 0.25 × 0.0055 screen units per unit of inverse distance.
const RIM_PIXELS = "2.0";

// The game's ramps are per character and not in mods. This is one tint for every
// material until it is measured against game frames.
const SHADOW_COLOR = "vec3( 0.85, 0.74, 0.78 )";

// HoyoToon maps half-Lambert × AO into 0.15 to 1 before the ramp lookup. The
// procedural ramp turns lit where the unmapped product reaches 0.5.
const RAMP_LIT = "0.575";

// Not measured in game; StarRailNPRShader's default black reads too harsh here.
const OUTLINE_TINT = "vec3( 0.3, 0.22, 0.24 )";

// A part name ends its override section, optionally followed by one submesh letter
// or digit, so accessories such as Headwear or HairClip do not match. Star Rail
// calls the face Head.
const SRMI_HAIR_PART = /hair[a-z\d]?$/i;
const SRMI_FACE_PART = /(?:face|head)[a-z\d]?$/i;

// Lit AO, no specular or rim, material 0; the vertex color fallback has no face mask.
const neutralLightMap = new DataTexture(new Uint8Array([0, 128, 0, 0]), 1, 1, RGBAFormat);
neutralLightMap.colorSpace = NoColorSpace;
neutralLightMap.needsUpdate = true;
const NEUTRAL_VERTEX_COLOR = "vec4( 1.0, 0.5, 0.0, 0.5 )";

export const srmiMaterialProfile: ModelViewerMaterialProfile = {
    baseMaterial: { metalness: 0.05, roughness: 0.65 },
    derivativeTangentFrame: false,
    toonShadows: true,
    toon: {
        defaultStyle: DEFAULT_SRMI_TOON_STYLE,
        parse: (record) => readToonFlags(record, DEFAULT_SRMI_TOON_STYLE, SRMI_TOON_FLAGS),
        serialize: (style) => pick(style, SRMI_TOON_FLAGS),
        flags: BASE_TOON_FLAGS,
    },
    markMeshes: markSRMIMeshes,
    configureMaterial: configureSRMIToonMaterialShader,
    bindMaps(material, maps, textures) {
        const state = material.userData.srmiToonMaterial as SRMIToonShaderState | undefined;
        if (!state) {
            return;
        }
        const lightMap = toonLightMapOf(
            maps.lightMapKey ? textures.get(maps.lightMapKey) : undefined,
        );
        state.lightMap.value = lightMap ?? neutralLightMap;
        state.lightMapEnabled.value = lightMap ? 1 : 0;
        state.lightMapTransform.value = textureUvTransform(state.lightMap.value);
    },
    sync: syncSRMIToon,
};

function configureSRMIToonMaterialShader(material: MeshStandardMaterial): void {
    if (material.userData.srmiToonMaterial) {
        return;
    }
    const state: SRMIToonShaderState = {
        enabled: { value: 0 },
        lightMap: { value: neutralLightMap },
        lightMapEnabled: { value: 0 },
        lightMapTransform: { value: neutralLightMap.matrix },
        face: { value: 0 },
        faceForward: { value: new Vector3(0, 0, 1) },
        vertexColorEnabled: { value: 0 },
    };
    material.userData.srmiToonMaterial = state;
    material.onBeforeCompile = (shader: WebGLProgramParametersWithUniforms) => {
        shader.uniforms.srmiToonEnabled = state.enabled;
        shader.uniforms.srmiLightMap = state.lightMap;
        shader.uniforms.srmiLightMapEnabled = state.lightMapEnabled;
        shader.uniforms.srmiLightMapTransform = state.lightMapTransform;
        shader.uniforms.srmiFace = state.face;
        shader.uniforms.srmiFaceForward = state.faceForward;
        shader.uniforms.srmiVertexColorEnabled = state.vertexColorEnabled;
        shader.vertexShader = replaceShaderAnchor(
            shader.vertexShader,
            "#include <common>",
            `#include <common>
attribute vec4 ${TOON_VERTEX_COLOR_ATTRIBUTE};
uniform float srmiVertexColorEnabled;
uniform vec3 srmiFaceForward;
varying vec2 vSRMIUv;
varying vec4 vSRMIColor;
varying vec3 vSRMIFaceForward;`,
        );
        shader.vertexShader = replaceShaderAnchor(
            shader.vertexShader,
            "#include <uv_vertex>",
            `#include <uv_vertex>
vSRMIUv = uv;
vSRMIColor = srmiVertexColorEnabled > 0.5 ? ${TOON_VERTEX_COLOR_ATTRIBUTE} : ${NEUTRAL_VERTEX_COLOR};
vSRMIFaceForward = normalize( mat3( modelMatrix ) * srmiFaceForward );`,
        );
        shader.fragmentShader = replaceShaderAnchor(
            shader.fragmentShader,
            "#include <common>",
            `#include <common>
varying vec2 vSRMIUv;
varying vec4 vSRMIColor;
varying vec3 vSRMIFaceForward;
uniform float srmiToonEnabled;
uniform sampler2D srmiLightMap;
uniform float srmiLightMapEnabled;
uniform mat3 srmiLightMapTransform;
uniform float srmiFace;

${TOON_STEP_GLSL}

${TOON_FACE_GLSL}`,
        );

        // Replaces the lit result. The light map holds the rim mask in R, AO in G,
        // the specular threshold in B and the material in A.
        shader.fragmentShader = replaceShaderAnchor(
            shader.fragmentShader,
            "#include <opaque_fragment>",
            `#if NUM_DIR_LIGHTS > 0
if ( srmiToonEnabled > 0.5 ) {
    ${TOON_KEY_LIGHT_GLSL}
    vec3 albedo = diffuseColor.rgb;
    vec4 lightMap = texture2D( srmiLightMap, ( srmiLightMapTransform * vec3( vSRMIUv, 1.0 ) ).xy );

    // HoyoToon's shadow rate: half-Lambert with the cast shadow, times twice the
    // light map AO and the vertex AO, doubled again by the two-component dot.
    float NoL = dot( worldNormal, worldLight );
    float rate = min( 1.0, ( NoL * 0.5 + 0.5 ) * occlusion * 4.0 * lightMap.g * vSRMIColor.r );
    rate = max( 0.001, rate ) * 0.85 + 0.15;
    float lit = toonStep( ${RAMP_LIT}, rate );
    float band = smoothstep( 0.405, 0.53, rate ) * ( 1.0 - lit );

    if ( srmiFace > 0.5 ) {
        vec2 faceLight = toonFaceLight( vSRMIFaceForward, worldLight );
        if ( srmiLightMapEnabled > 0.5 ) {
            // A map bound on the face is the game's FaceMap: its alpha is the SDF
            // threshold at which the face turns lit, mirrored for light from the
            // other side.
            vec2 faceUv = vSRMIUv;
            if ( faceLight.y > 0.5 ) faceUv.x = 1.0 - faceUv.x;
            float threshold = texture2D( srmiLightMap, ( srmiLightMapTransform * vec3( faceUv, 1.0 ) ).xy ).a;
            lit = toonStep( 1.0 - threshold, faceLight.x * 0.5 + 0.5 );
        } else {
            // Without a FaceMap, the face turns as one piece.
            lit = smoothstep( -0.35, -0.15, faceLight.x );
        }
        lit *= occlusion;
        band = 0.0;
    }

    // A darker band right before the terminator, like the game's ramps.
    vec3 shadowColor = ${SHADOW_COLOR};
    vec3 shade = mix( mix( shadowColor, shadowColor * shadowColor, band ), vec3( 1.0 ), lit );

    // A Blinn-Phong highlight past the light map's blue, faded in over the roughness
    // and scaled by the shadow rate.
    vec3 halfDirection = normalize( worldLight + worldView );
    float NoV = clamp( dot( worldNormal, worldView ), 0.0, 1.0 );
    float NoH = dot( worldNormal, halfDirection );
    float term = rate * pow( max( 0.01, NoH ), ${SPECULAR_SHININESS} );
    float specularSoftness = max( ${SPECULAR_ROUGHNESS}, fwidth( term ) );
    float specularEdge = 1.0 - lightMap.b - specularSoftness;
    float specular = smoothstep( 0.0, 1.0, ( term - specularEdge ) / ( 2.0 * specularSoftness ) );
    specular *= ${SPECULAR_INTENSITY} * 0.35 * ( 1.0 - srmiFace );

    // The depth rim, masked by the light map's red and fading with distance.
    float viewDistance = length( vViewPosition );
    float rim = toonSilhouetteBand( NoV, ${RIM_PIXELS} ) * lightMap.r;
    rim *= ${RIM_INTENSITY} * max( 0.5, clamp( 1.0 / viewDistance, 0.0, 1.0 ) ) * ( 1.0 - srmiFace );

    outgoingLight = albedo * shade + vec3( specular + rim );
}
#endif
#include <opaque_fragment>`,
        );

        // The toon result is display-referred, so the viewer's exposure and tone
        // mapping would only shift it.
        shader.fragmentShader = replaceShaderAnchor(
            shader.fragmentShader,
            "#include <tonemapping_fragment>",
            `if ( srmiToonEnabled < 0.5 ) {
    #include <tonemapping_fragment>
}`,
        );
    };
    material.customProgramCacheKey = () => "srmi-toon-v1";
    material.needsUpdate = true;
}

function syncSRMIToon(
    object: Mesh,
    material: MeshStandardMaterial,
    meshData: MaterialProfileMeshData,
): void {
    const data = meshData as SRMIToonMeshData;
    const state = material.userData.srmiToonMaterial as SRMIToonShaderState | undefined;
    if (!state) {
        return;
    }
    const style = data.toonStyle ?? DEFAULT_SRMI_TOON_STYLE;
    const enabled = data.toonShadows;
    state.enabled.value = enabled ? 1 : 0;
    state.face.value = data.srmiFace ? 1 : 0;
    if (data.srmiFaceForward) state.faceForward.value.copy(data.srmiFaceForward);
    state.vertexColorEnabled.value = object.geometry.attributes[TOON_VERTEX_COLOR_ATTRIBUTE]
        ? 1
        : 0;

    // Coverage cards are flat ink decals: they neither block light nor have a silhouette.
    const coverageMask = Boolean(material.map?.userData.modelViewerCoverageMask);
    object.castShadow = enabled && style.selfShadow && !coverageMask;
    object.receiveShadow = enabled && style.selfShadow;

    const showOutline = enabled && style.outlines && !coverageMask;
    if (!showOutline && !data.srmiOutline) {
        return;
    }
    data.srmiOutline = syncToonOutlineSource(data.srmiOutline, data.tangentFrameUsable, (tangent) =>
        createSRMIToonOutline(object, tangent),
    );
    if (!data.srmiOutline) {
        return;
    }
    const uniforms = data.srmiOutline.material.uniforms;
    data.srmiOutline.visible = showOutline;
    uniforms.map.value = material.map ?? fallbackWhiteTexture;
    uniforms.mapTransform.value = textureUvTransform(uniforms.map.value);
    uniforms.vertexColorEnabled.value = state.vertexColorEnabled.value;
    uniforms.face.value = state.face.value;
    uniforms.faceForward.value.copy(state.faceForward.value);
}

function markSRMIMeshes(transport: ModViewerTransport, objects: Mesh[]): void {
    transport.meshes.forEach((mesh, index) => {
        const data = objects[index].userData as SRMIToonMeshData;
        data.srmiHair = SRMI_HAIR_PART.test(mesh.component);
        data.srmiFace = !data.srmiHair && SRMI_FACE_PART.test(mesh.component);
        data.srmiFaceForward = data.srmiFace ? meanNormal(objects[index].geometry) : undefined;
    });
}

// HoyoToon's inverted hull: it extrudes in view space along the smoothed normal stored
// in TANGENT, tilted away from the camera, by a width that grows with the square root
// of the field-of-view-scaled depth and scales with the vertex color's alpha; the
// game drops it where that alpha is below 0.05. On the face, StarRailNPRShader's mask
// hides it with the vertex color's blue where the face turns to the camera.
function createSRMIToonOutline(
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
            defines: useTangent ? { SRMI_OUTLINE_TANGENT: "" } : {},
            uniforms: {
                map: { value: fallbackWhiteTexture },
                mapTransform: { value: fallbackWhiteTexture.matrix },
                vertexColorEnabled: { value: 0 },
                face: { value: 0 },
                faceForward: { value: new Vector3(0, 0, 1) },
            },
            vertexShader: `#ifdef SRMI_OUTLINE_TANGENT
attribute vec4 tangent;
#else
attribute vec3 outlineNormal;
#endif
attribute vec4 ${TOON_VERTEX_COLOR_ATTRIBUTE};
uniform mat3 mapTransform;
uniform float vertexColorEnabled;
uniform float face;
uniform vec3 faceForward;
varying vec2 vOutlineUv;
varying float vOutlineFacing;
varying float vOutlineWidth;
void main() {
    vOutlineUv = ( mapTransform * vec3( uv, 1.0 ) ).xy;
    vec4 color = vertexColorEnabled > 0.5 ? ${TOON_VERTEX_COLOR_ATTRIBUTE} : ${NEUTRAL_VERTEX_COLOR};
    vOutlineWidth = color.a;
    #ifdef SRMI_OUTLINE_TANGENT
    vec3 smoothNormal = normalMatrix * tangent.xyz;
    #else
    vec3 smoothNormal = normalMatrix * outlineNormal;
    #endif
    float lengthSquared = dot( smoothNormal, smoothNormal );
    smoothNormal = lengthSquared > 1e-10 ? smoothNormal * inversesqrt( lengthSquared ) : vec3( 0.0, 0.0, 1.0 );

    vec4 viewPosition = modelViewMatrix * vec4( position, 1.0 );
    vec3 toCamera = normalize( -viewPosition.xyz );
    vOutlineFacing = dot( smoothNormal, toCamera );
    float width = sqrt( abs( viewPosition.z ) / projectionMatrix[ 1 ][ 1 ] ) * color.a * ${OUTLINE_WIDTH} * ${OUTLINE_SCALE};
    if ( face > 0.5 ) {
        float faceFacing = dot( normalize( normalMatrix * faceForward ), toCamera );
        width *= smoothstep( -0.02, 0.0, 1.0 - faceFacing - color.b );
    }
    viewPosition.xyz += normalize( vec3( smoothNormal.xy, -1.0 ) ) * width;
    gl_Position = projectionMatrix * viewPosition;
}`,
            fragmentShader: `uniform sampler2D map;
varying vec2 vOutlineUv;
varying float vOutlineFacing;
varying float vOutlineWidth;
void main() {
    // The hull past the silhouette faces sideways. Its far side faces away and is
    // only visible through openings such as a missing face.
    if ( vOutlineFacing < -0.5 || vOutlineWidth < 0.05 ) discard;
    gl_FragColor = vec4( texture2D( map, vOutlineUv ).rgb * ${OUTLINE_TINT}, 1.0 );
    #include <colorspace_fragment>
}`,
            side: BackSide,
        }),
    );
    outline.raycast = () => {};
    object.add(outline);
    return outline;
}
