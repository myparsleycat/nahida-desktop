/*
 * The WWMI toon shading below follows the Wuthering Waves shader of HoyoToon
 * (https://github.com/Hoyotoon/HoyoToon): the base material's masked shadow step,
 * toon specular, rim and outline.
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

import { modelViewerDDSMetadataOf } from "../model-viewer-dds";
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
    toonLightMapOf,
} from "../model-viewer-shader-utils";
import { BASE_TOON_FLAGS, readToonFlags } from "./profile";

export const DEFAULT_WWMI_TOON_STYLE: ModelViewerToonStyleBase = {
    outlines: true,
    selfShadow: true,
    lightFollowsCamera: true,
    fieldOfView: 20,
};

const WWMI_TOON_FLAGS = ["outlines", "selfShadow", "lightFollowsCamera"] as const;

type WWMIToonMeshData = MaterialProfileMeshData & {
    wwmiMaterialType?: number;
    wwmiFaceForward?: Vector3;
    wwmiOutline?: Mesh<BufferGeometry, ShaderMaterial>;
};

type WWMIToonShaderState = {
    enabled: { value: number };
    lightMap: { value: Texture };
    lightMapTransform: { value: Matrix3 };
    lightBC4: { value: number };
    lightSigned: { value: number };
    packedMaskEnabled: { value: number };
    typeMask: { value: Texture };
    typeMaskTransform: { value: Matrix3 };
    typeMaskEnabled: { value: number };
    materialType: { value: number };
    faceForward: { value: Vector3 };
    normalData: { value: Texture };
    normalDataTransform: { value: Matrix3 };
    cutoutMap: { value: Texture };
    cutoutEnabled: { value: number };
    cutoutTransform: { value: Matrix3 };
};

// HoyoToon's material defaults. The game's per-material values come from material
// files that mods do not ship, and its shadow ramp and matcap textures are not in
// mods either, so the ramp is HoyoToon's white default and the matcap is left out.
const SHADOW_PROCESS = "0.55";
const FRONT_SHADOW_OFFSET = "0.4";
const SHADOW_WIDTH = "0.01";
const SKIN_SUBSURFACE = "vec3( 0.9387, 0.6038, 0.4072 )";
const TOON_MAX_SPECULAR = "0.1";
const RIM_COLOR = "vec3( 0.5 )";
const OUTLINE_WIDTH = "0.0011";
const OUTLINE_COLOR = "vec3( 0.765 )";

// Pixels inside the silhouette that the rim covers, standing in for HoyoToon's depth
// texture offset of 0.0025 screen units per unit of inverse distance.
const RIM_PIXELS = "4.0";

// The light map holds the shadow mask in G, read from R in one-channel containers;
// the normal map holds the normal in RG, the specular mask in B and metal in A.
// Without a normal map, A of 1 turns the specular off.
const neutralLightMap = solidTexture(255, 255, 255, 255);
const neutralNormalData = solidTexture(128, 128, 255, 255);

const WWMI_FACE_PART = /face(?:head)?[a-z\d]?$/i;
const WWMI_HAIR_PART = /(?:hair|bangs)[a-z\d]?$/i;
const WWMI_EYE_PART = /eyes?[a-z\d]?$/i;

const WWMI_TOON_GLSL = `${TOON_STEP_GLSL}

${TOON_FACE_GLSL}

// HoyoToon's base shadow: N·L past the front offset steps over the shadow width, is
// cut by the light map's mask and then sharpened, so unmasked shade keeps half the
// lit color.
float wwmiShadowArea( float NoL, float mask ) {
    float area = smoothstep( ${SHADOW_PROCESS}, ${SHADOW_PROCESS} + clamp( ${SHADOW_WIDTH} + 0.25, 0.0, 1.0 ), NoL + ${FRONT_SHADOW_OFFSET} );
    area = clamp( area * mask, 0.0, 1.0 );
    return clamp( area * 4.99999905 + 0.5, 0.0, 1.0 );
}

// HoyoToon's base specular: a Blinn-Phong lobe of power 1 kept only above 0.1, where
// the normal map's blue reaches 0.5, plus a quarter-strength step where it stays at
// or below 0.85, tinted by the albedo and removed on metal.
vec3 wwmiSpecular( vec3 albedo, vec3 normal, vec3 halfDirection, float mask, vec4 normalData, float nonSkin ) {
    float lobe = max( dot( normal, halfDirection ), 0.0 );
    lobe = max( lobe * lobe * step( 0.1, lobe ) * step( 0.1, mask ), 0.001 );
    float first = clamp( lobe * step( 0.5, normalData.b ), 0.0, 1.0 );
    float second = step( ${TOON_MAX_SPECULAR}, clamp( lobe * step( normalData.b, 0.85 ) * nonSkin, 0.0, 1.0 ) );
    float specular = clamp( first + second * 0.25, 0.0, 1.0 );
    return specular * pow( albedo, vec3( mix( 0.5, 2.0, normalData.b ) ) ) * ( 1.0 - normalData.a );
}`;

export const wwmiMaterialProfile: ModelViewerMaterialProfile = {
    baseMaterial: { metalness: 0, roughness: 1 },
    derivativeTangentFrame: true,
    toonShadows: true,
    toon: {
        defaultStyle: DEFAULT_WWMI_TOON_STYLE,
        parse: (record) => readToonFlags(record, DEFAULT_WWMI_TOON_STYLE, WWMI_TOON_FLAGS),
        serialize: (style) => pick(style, WWMI_TOON_FLAGS),
        flags: BASE_TOON_FLAGS,
    },
    markMeshes: markWWMIMeshes,
    configureMaterial(material, object) {
        if (object.geometry.attributes.uv) {
            configureWWMIMaterialShader(material);
        }
    },
    bindMaps(material, maps, textures) {
        const state = material.userData.wwmiToonMaterial as WWMIToonShaderState | undefined;
        if (!state) {
            return;
        }
        const lightMap = maps.lightMapKey ? textures.get(maps.lightMapKey) : undefined;
        state.lightMap.value = lightMap ?? neutralLightMap;
        state.lightMapTransform.value = textureUvTransform(state.lightMap.value);
        const dds = modelViewerDDSMetadataOf(lightMap ?? null);
        state.lightBC4.value = dds?.direct && dds.format.startsWith("bc4-") ? 1 : 0;
        state.lightSigned.value = dds?.direct && dds.format.endsWith("snorm") ? 1 : 0;
        state.packedMaskEnabled.value =
            toonLightMapOf(lightMap) && !dds?.format.startsWith("bc6h-") ? 1 : 0;

        const typeMask = toonLightMapOf(
            maps.materialMapKey ? textures.get(maps.materialMapKey) : undefined,
        );
        state.typeMask.value = typeMask ?? fallbackWhiteTexture;
        state.typeMaskTransform.value = textureUvTransform(state.typeMask.value);
        state.typeMaskEnabled.value = typeMask ? 1 : 0;

        const normalMap = maps.normalMapKey ? textures.get(maps.normalMapKey) : undefined;
        state.normalData.value = toonLightMapOf(normalMap) ?? neutralNormalData;
        state.normalDataTransform.value = textureUvTransform(state.normalData.value);

        const cutoutMap = maps.cutoutMapKey ? textures.get(maps.cutoutMapKey) : undefined;
        state.cutoutMap.value = cutoutMap ?? fallbackWhiteTexture;
        state.cutoutEnabled.value = cutoutMap ? 1 : 0;
        state.cutoutTransform.value = textureUvTransform(state.cutoutMap.value);
    },
    sync: syncWWMIToon,
};

function configureWWMIMaterialShader(material: MeshStandardMaterial): void {
    if (material.userData.wwmiToonMaterial) {
        return;
    }
    const state: WWMIToonShaderState = {
        enabled: { value: 0 },
        lightMap: { value: neutralLightMap },
        lightMapTransform: { value: neutralLightMap.matrix },
        lightBC4: { value: 0 },
        lightSigned: { value: 0 },
        packedMaskEnabled: { value: 0 },
        typeMask: { value: fallbackWhiteTexture },
        typeMaskTransform: { value: fallbackWhiteTexture.matrix },
        typeMaskEnabled: { value: 0 },
        materialType: { value: 0 },
        faceForward: { value: new Vector3(0, 0, 1) },
        normalData: { value: neutralNormalData },
        normalDataTransform: { value: neutralNormalData.matrix },
        cutoutMap: { value: fallbackWhiteTexture },
        cutoutEnabled: { value: 0 },
        cutoutTransform: { value: fallbackWhiteTexture.matrix },
    };
    material.userData.wwmiToonMaterial = state;
    material.onBeforeCompile = (shader: WebGLProgramParametersWithUniforms) => {
        shader.uniforms.wwmiToonEnabled = state.enabled;
        shader.uniforms.wwmiLightMap = state.lightMap;
        shader.uniforms.wwmiLightMapTransform = state.lightMapTransform;
        shader.uniforms.wwmiLightBC4 = state.lightBC4;
        shader.uniforms.wwmiLightSigned = state.lightSigned;
        shader.uniforms.wwmiPackedMaskEnabled = state.packedMaskEnabled;
        shader.uniforms.wwmiTypeMask = state.typeMask;
        shader.uniforms.wwmiTypeMaskTransform = state.typeMaskTransform;
        shader.uniforms.wwmiTypeMaskEnabled = state.typeMaskEnabled;
        shader.uniforms.wwmiMaterialType = state.materialType;
        shader.uniforms.wwmiFaceForward = state.faceForward;
        shader.uniforms.wwmiNormalData = state.normalData;
        shader.uniforms.wwmiNormalDataTransform = state.normalDataTransform;
        shader.uniforms.wwmiCutoutMap = state.cutoutMap;
        shader.uniforms.wwmiCutoutEnabled = state.cutoutEnabled;
        shader.uniforms.wwmiCutoutTransform = state.cutoutTransform;
        shader.vertexShader = replaceShaderAnchor(
            shader.vertexShader,
            "#include <common>",
            `#include <common>
uniform vec3 wwmiFaceForward;
varying vec3 vWWMIFaceForward;
varying vec2 vWWMIUv;`,
        );
        shader.vertexShader = replaceShaderAnchor(
            shader.vertexShader,
            "#include <uv_vertex>",
            `#include <uv_vertex>
vWWMIUv = uv;
vWWMIFaceForward = normalize( mat3( modelMatrix ) * wwmiFaceForward );`,
        );
        shader.fragmentShader = replaceShaderAnchor(
            shader.fragmentShader,
            "#include <common>",
            `#include <common>
varying vec2 vWWMIUv;
varying vec3 vWWMIFaceForward;
uniform float wwmiToonEnabled;
uniform sampler2D wwmiLightMap;
uniform mat3 wwmiLightMapTransform;
uniform float wwmiLightBC4;
uniform float wwmiLightSigned;
uniform float wwmiPackedMaskEnabled;
uniform sampler2D wwmiTypeMask;
uniform mat3 wwmiTypeMaskTransform;
uniform float wwmiTypeMaskEnabled;
uniform float wwmiMaterialType;
uniform sampler2D wwmiNormalData;
uniform mat3 wwmiNormalDataTransform;
uniform sampler2D wwmiCutoutMap;
uniform float wwmiCutoutEnabled;
uniform mat3 wwmiCutoutTransform;

${WWMI_TOON_GLSL}`,
        );

        // Red also holds a BC4 mask, which has no green channel to sample.
        shader.fragmentShader = replaceShaderAnchor(
            shader.fragmentShader,
            "#include <map_fragment>",
            `if ( wwmiCutoutEnabled > 0.5 && texture2D( wwmiCutoutMap, ( wwmiCutoutTransform * vec3( vWWMIUv, 1.0 ) ).xy ).r < 0.5 ) discard;
#include <map_fragment>`,
        );

        // Replaces the lit result in HoyoToon's order: the rim over the albedo, the
        // shadow, then specular.
        shader.fragmentShader = replaceShaderAnchor(
            shader.fragmentShader,
            "#include <opaque_fragment>",
            `#if NUM_DIR_LIGHTS > 0
if ( wwmiToonEnabled > 0.5 ) {
    ${TOON_KEY_LIGHT_GLSL}

    // HoyoToon leaves bump mapping off by default, so the toon terms use the
    // interpolated normal.
    worldNormal = inverseTransformDirection( nonPerturbedNormal, viewMatrix );
    vec3 albedo = diffuseColor.rgb;
    vec4 lightMap = texture2D( wwmiLightMap, ( wwmiLightMapTransform * vec3( vWWMIUv, 1.0 ) ).xy );
    vec4 normalData = texture2D( wwmiNormalData, ( wwmiNormalDataTransform * vec3( vWWMIUv, 1.0 ) ).xy );
    float mask = mix( lightMap.g, lightMap.r, wwmiLightBC4 );
    if ( wwmiLightSigned > 0.5 ) mask = mask * 0.5 + 0.5;
    float NoL = dot( worldNormal, worldLight );
    float shadowArea = wwmiShadowArea( NoL * occlusion, mask );
    vec3 skinType = vec3( 0.0, 0.0, 1.0 );
    if ( wwmiTypeMaskEnabled > 0.5 ) {
        float id = texture2D( wwmiTypeMask, ( wwmiTypeMaskTransform * vec3( vWWMIUv, 1.0 ) ).xy ).r;
        skinType = vec3( id > 0.9 ? 1.0 : 0.0, id > 0.5 && id <= 0.9 ? 1.0 : 0.0, id <= 0.5 ? 1.0 : 0.0 );
    }
    vec3 shadowColor = mix( mix( vec3( 0.5 ), ${SKIN_SUBSURFACE}, skinType.x ), vec3( 1.0 ), 0.3 );
    vec3 halfDirection = normalize( worldLight + worldView );
    vec3 specular = wwmiSpecular( albedo, worldNormal, halfDirection, mask, normalData, skinType.z );

    if ( wwmiMaterialType == 1.0 ) {
        vec2 faceLight = toonFaceLight( vWWMIFaceForward, worldLight );
        float faceLit = smoothstep( -0.35, -0.15, faceLight.x );
        if ( wwmiPackedMaskEnabled > 0.5 ) {
            vec2 faceUv = vWWMIUv;
            if ( faceLight.y > 0.5 ) faceUv.x = 1.0 - faceUv.x;
            float sdf = texture2D( wwmiLightMap, ( wwmiLightMapTransform * vec3( faceUv, 1.0 ) ).xy ).a;
            float edge = 0.5 - faceLight.x * 0.5;
            faceLit = smoothstep( edge - 0.05, edge + 0.05, sdf ) * step( -0.5, faceLight.x );
        }
        shadowArea = clamp( faceLit * occlusion + 0.5, 0.0, 1.0 );
        shadowColor = mix( ${SKIN_SUBSURFACE}, vec3( 1.0 ), 0.3 );
        specular = vec3( 0.0 );
    } else if ( wwmiMaterialType == 3.0 && wwmiPackedMaskEnabled > 0.5 ) {
        float offset = pow( smoothstep( 0.1, 1.0, lightMap.g ), 0.56 ) * 0.42;
        float hairShadow = NoL * occlusion * 0.5 + 0.5 + offset;
        shadowArea = smoothstep( ${SHADOW_PROCESS} - ${SHADOW_WIDTH}, ${SHADOW_PROCESS} + ${SHADOW_WIDTH}, hairShadow + 0.1 ) * step( 0.05, lightMap.g );
        shadowColor = mix( clamp( vec3( 0.5 ) + vec3( 0.090033, 0.168722, 0.193576 ), 0.0, 1.0 ), ${SKIN_SUBSURFACE}, skinType.x );
        shadowColor = sqrt( sqrt( shadowColor ) );
        float NoH = clamp( dot( worldNormal, halfDirection ), 0.0, 1.0 );
        float highlight = ( max( lightMap.r, 1.0 - NoH ) - ( 1.0 - NoH ) ) / max( 0.0001, NoH );
        specular = vec3( highlight * NoH * 0.2 );
    }

    // HoyoToon's rim covers the band whose depth steps away past the silhouette,
    // on the lit side.
    float NoV = clamp( dot( worldNormal, worldView ), 0.0, 1.0 );
    vec3 rim = ${RIM_COLOR} * toonSilhouetteBand( NoV, ${RIM_PIXELS} ) * smoothstep( 0.0, 0.5, clamp( NoL, 0.0, 1.0 ) );

    vec3 color = albedo + rim;
    color = mix( color * shadowColor, color, shadowArea );
    color += specular;
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
            `if ( wwmiToonEnabled < 0.5 ) {
    #include <tonemapping_fragment>
}`,
        );
    };
    material.customProgramCacheKey = () => "wwmi-toon-v1";
    material.needsUpdate = true;
}

function syncWWMIToon(
    object: Mesh,
    material: MeshStandardMaterial,
    meshData: MaterialProfileMeshData,
): void {
    const data = meshData as WWMIToonMeshData;
    const state = material.userData.wwmiToonMaterial as WWMIToonShaderState | undefined;
    if (!state) {
        return;
    }
    const style = data.toonStyle ?? DEFAULT_WWMI_TOON_STYLE;
    const enabled = data.toonShadows;
    state.enabled.value = enabled ? 1 : 0;
    state.materialType.value = data.wwmiMaterialType ?? 0;
    if (data.wwmiFaceForward) state.faceForward.value.copy(data.wwmiFaceForward);

    // Coverage cards are flat ink decals: they neither block light nor have a silhouette.
    const coverageMask = Boolean(material.map?.userData.modelViewerCoverageMask);
    object.castShadow = enabled && style.selfShadow && !coverageMask;
    object.receiveShadow = enabled && style.selfShadow;

    const showOutline =
        enabled && style.outlines && !coverageMask && Boolean(object.geometry.attributes.normal);
    if (!showOutline && !data.wwmiOutline) {
        return;
    }
    data.wwmiOutline = syncToonOutlineSource(data.wwmiOutline, data.tangentFrameUsable, () =>
        createWWMIToonOutline(object),
    );
    if (!data.wwmiOutline) {
        return;
    }
    const uniforms = data.wwmiOutline.material.uniforms;
    data.wwmiOutline.visible = showOutline;
    uniforms.map.value = material.map ?? fallbackWhiteTexture;
    uniforms.mapTransform.value = textureUvTransform(uniforms.map.value);
}

function markWWMIMeshes(transport: ModViewerTransport, objects: Mesh[]): void {
    transport.meshes.forEach((mesh, index) => {
        const data = objects[index].userData as WWMIToonMeshData;
        data.wwmiMaterialType = WWMI_FACE_PART.test(mesh.component)
            ? 1
            : WWMI_HAIR_PART.test(mesh.component)
              ? 3
              : WWMI_EYE_PART.test(mesh.component)
                ? 2
                : 0;
        data.wwmiFaceForward =
            data.wwmiMaterialType === 1 ? meanNormal(objects[index].geometry) : undefined;
    });
}

// HoyoToon's inverted hull: it extrudes in view space along the outline normal by a
// fixed width, not scaled by depth or field of view, and takes the albedo under the
// outline color. The game keeps that normal in the tangent, which the viewer does
// not decode for WWMI, so seam-averaged normals stand in.
function createWWMIToonOutline(object: Mesh): Mesh<BufferGeometry, ShaderMaterial> {
    if (!object.geometry.attributes.outlineNormal) {
        object.geometry.setAttribute("outlineNormal", averagedNormals(object.geometry));
    }
    const outline = new Mesh(
        object.geometry,
        new ShaderMaterial({
            uniforms: {
                map: { value: fallbackWhiteTexture },
                mapTransform: { value: fallbackWhiteTexture.matrix },
            },
            vertexShader: `attribute vec3 outlineNormal;
uniform mat3 mapTransform;
varying vec2 vOutlineUv;
varying float vOutlineFacing;
void main() {
    vOutlineUv = ( mapTransform * vec3( uv, 1.0 ) ).xy;
    vec3 smoothNormal = normalMatrix * outlineNormal;
    float lengthSquared = dot( smoothNormal, smoothNormal );
    smoothNormal = lengthSquared > 1e-10 ? smoothNormal * inversesqrt( lengthSquared ) : vec3( 0.0, 0.0, 1.0 );

    vec4 viewPosition = modelViewMatrix * vec4( position, 1.0 );
    vOutlineFacing = dot( smoothNormal, normalize( -viewPosition.xyz ) );
    viewPosition.xyz += smoothNormal * ${OUTLINE_WIDTH};
    gl_Position = projectionMatrix * viewPosition;
}`,
            fragmentShader: `#include <common>
uniform sampler2D map;
varying vec2 vOutlineUv;
varying float vOutlineFacing;
void main() {
    // The hull past the silhouette faces sideways. Its far side faces away and is
    // only visible through openings such as a missing face.
    if ( vOutlineFacing < -0.5 ) discard;
    gl_FragColor = vec4( texture2D( map, vOutlineUv ).rgb * ${OUTLINE_COLOR}, 1.0 );
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
