import type { ModViewerTransport } from "@shared/mod-viewer/types";
import { pick } from "es-toolkit";
import type {
    BufferGeometry,
    Matrix3,
    MeshStandardMaterial,
    Texture,
    WebGLProgramParametersWithUniforms,
} from "three";
import { BackSide, Mesh, ShaderMaterial, UniformsLib, UniformsUtils, Vector2 } from "three";

import type {
    MaterialProfileMeshData,
    ModelViewerMaterialProfile,
    ModelViewerToonFlagOf,
    ModelViewerToonStyleBase,
} from "./profile";

import { modelViewerDDSFormatOf } from "../model-viewer-dds";
import {
    averagedNormals,
    fallbackWhiteTexture,
    replaceShaderAnchor,
    textureUvTransform,
    TOON_KEY_LIGHT_GLSL,
} from "../model-viewer-shader-utils";
import { BASE_TOON_FLAGS, readToonFlags } from "./profile";

export type EFMIToonStyle = ModelViewerToonStyleBase & {
    rangeBias: number;
    softness: number;
    shadowBrightness: number;
    shadowSaturation: number;
    ambient: number;
    lightStrength: number;
    brightness: number;
    specular: number;
    rimStrength: number;
    rimWidth: number;
    outlineWidth: number;
    outlineBrightness: number;
    flatFace: boolean;
};

export const DEFAULT_EFMI_TOON_STYLE: EFMIToonStyle = {
    rangeBias: -0.4,
    softness: 0.02,
    shadowBrightness: 0.5,
    shadowSaturation: 1,
    ambient: 0.8,
    lightStrength: 0.9,
    // Warfarin defaults to 2 and clips less because its materials carry tints that mods
    // lack. This value approximates its output for the same textures.
    brightness: 1.5,
    specular: 1,
    rimStrength: 0.5,
    rimWidth: 0.3,
    outlines: true,
    outlineWidth: 0.5,
    outlineBrightness: 0.5,
    selfShadow: true,
    lightFollowsCamera: true,
    flatFace: true,
    fieldOfView: 20,
};

const EFMI_TOON_FLAGS = ["outlines", "selfShadow", "lightFollowsCamera", "flatFace"] as const;

type EFMIMeshData = MaterialProfileMeshData & {
    efmiHeadMesh?: boolean;
    efmiEyeMesh?: boolean;
    efmiOutline?: Mesh<BufferGeometry, ShaderMaterial>;
};

type EFMIToonShaderState = {
    enabled: { value: number };
    rangeBias: { value: number };
    softness: { value: number };
    shadowBrightness: { value: number };
    shadowSaturation: { value: number };
    ambient: { value: number };
    lightStrength: { value: number };
    brightness: { value: number };
    specular: { value: number };
    rimStrength: { value: number };
    rimWidth: { value: number };
    lightMap: { value: Texture };
    lightMapEnabled: { value: number };
    lightMapTransform: { value: Matrix3 };
    face: { value: number };
    faceMask: { value: Texture };
    faceMaskEnabled: { value: number };
    faceMaskTransform: { value: Matrix3 };
    highlightMap: { value: Texture };
    highlightEnabled: { value: number };
    highlightTransform: { value: Matrix3 };
    rampMap: { value: Texture };
    rampEnabled: { value: number };
    outlineMask: Texture | null;
};

const EFMI_HEAD_MIN_HEIGHT = 0.75;
const EFMI_HEAD_MAX_WIDTH = 0.3;

// Stock faces span 8040 to 12876 indices across the 34 characters, leaving room for edits.
const EFMI_FACE_MIN_INDICES = 8000;
const EFMI_FACE_MAX_INDICES = 16000;
const EFMI_BOUND_FACE_MIN_HEIGHT = 0.55;
const EFMI_SIZED_FACE_MIN_HEIGHT = 0.66;

// Overlay shadows and hair reach below the chin by about 5 cm.
const EFMI_HEAD_BELOW_FACE = 0.08;

// Across the 34 stock characters eye shadows stop at 156 indices and hair shadows at
// 1158. Irises reach down to 480 indices but are at most 3 cm tall, and hair shadows at
// least 7.8 cm, so a mod that keeps the game's iris or face texture still draws them.
const EFMI_EYE_SHADOW_MAX_INDICES = 200;
const EFMI_HAIR_SHADOW_MAX_INDICES = 1200;
const EFMI_HAIR_SHADOW_MIN_HEIGHT = 0.075;
const EFMI_IRIS_MAX_HEIGHT = 0.035;

// The game pushes the face's inverted hull back so it cannot poke through the nose
// and lips: Warfarin reads _OutlineOffsetZ 0.236 and scales it by 0.1 in view space.
const EFMI_FACE_OUTLINE_DEPTH_OFFSET = 0.0236;

export const efmiMaterialProfile: ModelViewerMaterialProfile<EFMIToonStyle> = {
    baseMaterial: { metalness: 0.05, roughness: 0.65 },
    derivativeTangentFrame: true,
    toonShadows: true,
    toon: {
        defaultStyle: DEFAULT_EFMI_TOON_STYLE,

        // Only the flags are user settings. Shading values stay at the defaults even when
        // older versions stored edited values, because the menu no longer shows them.
        parse: (record) => readToonFlags(record, DEFAULT_EFMI_TOON_STYLE, EFMI_TOON_FLAGS),
        serialize: (style) => pick(style, EFMI_TOON_FLAGS),
        flags: [
            ...BASE_TOON_FLAGS,
            { key: "flatFace", label: "toon_flat_face" },
        ] satisfies ModelViewerToonFlagOf<EFMIToonStyle>[],
    },
    markMeshes: markEFMIHeadMeshes,
    configureMaterial: configureEFMIToonMaterialShader,
    bindMaps(material, maps, textures) {
        const state = material.userData.efmiToonMaterial as EFMIToonShaderState | undefined;
        if (!state) {
            return;
        }
        const lightMap = maps.lightMapKey ? textures.get(maps.lightMapKey) : undefined;

        // One- and two-channel containers have no mask or smoothness to read.
        const format = modelViewerDDSFormatOf(lightMap);
        const outlineMask = Boolean(lightMap?.userData.modelViewerOutlineMask);
        const usable =
            lightMap && !outlineMask && !format.startsWith("bc4-") && !format.startsWith("bc5-");
        state.lightMap.value = usable ? lightMap : fallbackWhiteTexture;
        state.lightMapEnabled.value = usable ? 1 : 0;
        state.lightMapTransform.value = textureUvTransform(state.lightMap.value);
        state.outlineMask = outlineMask && lightMap ? lightMap : null;

        const faceMask = maps.materialMapKey ? textures.get(maps.materialMapKey) : undefined;
        state.faceMask.value = faceMask ?? fallbackWhiteTexture;
        state.faceMaskEnabled.value = faceMask ? 1 : 0;
        state.faceMaskTransform.value = textureUvTransform(state.faceMask.value);
        const highlightMap = maps.highlightMapKey ? textures.get(maps.highlightMapKey) : undefined;
        state.highlightMap.value = highlightMap ?? fallbackWhiteTexture;
        state.highlightEnabled.value = highlightMap ? 1 : 0;
        state.highlightTransform.value = textureUvTransform(state.highlightMap.value);
        const rampMap = maps.rampMapKey ? textures.get(maps.rampMapKey) : undefined;
        state.rampMap.value = rampMap ?? fallbackWhiteTexture;
        state.rampEnabled.value = rampMap ? 1 : 0;
    },
    sync: syncEFMIToon,
};

// The game lights faces from an SDF texture bound outside the mod, because their
// normals are not meant for N.L. Mods name no parts, so the head zone is measured from
// the face: the mesh binding the face's mask or highlight, else a mesh sized like a
// stock face. A prop taller than the character would skew any fraction of the model's
// height, so that fraction is only the last resort. Small head meshes that bind no
// diffuse either are the game's overlay shadows, which multiply the face through a
// shared mask and stencil that mods do not carry; drawn as surfaces they would be
// opaque plates over the eyes and forehead.
export function markEFMIHeadMeshes(transport: ModViewerTransport, objects: Mesh[]): void {
    const forms = new Set(transport.meshes.map((mesh) => mesh.form));
    forms.delete(undefined);
    for (const form of forms.size > 0 ? forms : [undefined]) {
        const entries = transport.meshes.flatMap((mesh, index) =>
            mesh.bounds && (!mesh.form || mesh.form === form)
                ? [{ mesh, index, bounds: mesh.bounds }]
                : [],
        );
        markEFMIHeadMeshesOfForm(entries, objects);
    }
}

function markEFMIHeadMeshesOfForm(
    entries: Array<{
        mesh: ModViewerTransport["meshes"][number];
        index: number;
        bounds: NonNullable<ModViewerTransport["meshes"][number]["bounds"]>;
    }>,
    objects: Mesh[],
): void {
    const top = Math.max(...entries.map(({ bounds }) => bounds.max[2]));
    const bottom = Math.min(...entries.map(({ bounds }) => bounds.min[2]));
    const heightOf = (z: number) => (z - bottom) / (top - bottom);
    const indexCount = (index: number) => objects[index].geometry.index?.count ?? Infinity;
    const narrow = entries.filter(
        ({ bounds }) =>
            Math.max(bounds.max[0] - bounds.min[0], bounds.max[1] - bounds.min[1]) <=
            EFMI_HEAD_MAX_WIDTH,
    );

    const unlit = narrow.filter(
        ({ mesh }) => !mesh.lightMapKey && mesh.lightMapVariants.length === 0,
    );
    const bound = unlit.filter(
        ({ mesh, bounds }) =>
            (mesh.materialMapKey || mesh.highlightMapKey) &&
            heightOf(bounds.min[2]) >= EFMI_BOUND_FACE_MIN_HEIGHT,
    );
    const sized = unlit.filter(
        ({ index, bounds }) =>
            indexCount(index) >= EFMI_FACE_MIN_INDICES &&
            indexCount(index) <= EFMI_FACE_MAX_INDICES &&
            heightOf(bounds.min[2]) >= EFMI_SIZED_FACE_MIN_HEIGHT,
    );
    const faces = bound.length > 0 ? bound : sized;
    const headFloor =
        faces.length > 0
            ? Math.max(...faces.map(({ bounds }) => bounds.min[2])) - EFMI_HEAD_BELOW_FACE
            : bottom + (top - bottom) * EFMI_HEAD_MIN_HEIGHT;

    for (const { mesh, index, bounds } of narrow) {
        if (bounds.min[2] < headFloor) continue;
        const userData = objects[index].userData as EFMIMeshData;
        userData.efmiHeadMesh = true;
        userData.overlayShadow =
            !mesh.texKey &&
            mesh.textureVariants.length === 0 &&
            (indexCount(index) <= EFMI_EYE_SHADOW_MAX_INDICES ||
                (indexCount(index) <= EFMI_HAIR_SHADOW_MAX_INDICES &&
                    bounds.max[2] - bounds.min[2] >= EFMI_HAIR_SHADOW_MIN_HEIGHT));
        userData.efmiEyeMesh =
            !userData.overlayShadow && bounds.max[2] - bounds.min[2] <= EFMI_IRIS_MAX_HEIGHT;
    }
}

const EFMI_SHADOW_TINT = "vec3( 0.878, 0.93, 1.122 )";
const EFMI_SKIN_SHADOW_TINT = "vec3( 1.187, 0.927, 0.813 )";

// Without a mod's _DiffRampMap, the ramp reduces to its coverage step. Its color tints
// the terminator but keeps the luminance.
const EFMI_RAMP_GLSL = `vec4 efmiRamp( float cosine ) {
    float position = clamp( cosine, -1.0, 1.0 ) * 0.5 + 0.5;
    if ( efmiRampEnabled > 0.5 ) {
        return texture2D( efmiRampMap, vec2( clamp( position, 0.002, 0.998 ), 0.5 ) );
    }
    return vec4( 1.0, 1.0, 1.0, smoothstep( 0.5 - efmiSoftness - 0.001, 0.5 + efmiSoftness + 0.001, position ) );
}

vec3 efmiRampTint( vec3 color, vec3 ramp ) {
    float chroma = max( ramp.r, max( ramp.g, ramp.b ) ) - min( ramp.r, min( ramp.g, ramp.b ) );
    vec3 tinted = color * mix( vec3( 1.0 ), ramp, chroma );
    return tinted * clamp( efmiLuma( color ) / max( efmiLuma( tinted ), 0.001 ), 0.0, 1.5 );
}`;

// Replaces the lit result, so Environment does not affect the two tones. The
// lighting model follows the HGRP character shaders as reconstructed by Warfarin
// Studio: a ramp lookup over N.L, a darker tone for surfaces also facing away from
// the camera, and a light map packed like _MetallicGlossMap (R metallic, G specular,
// B shadow mask, A smoothness). directionalLights[0] is the key light: three sorts
// shadow casters first and keeps scene order otherwise.
function configureEFMIToonMaterialShader(material: MeshStandardMaterial): void {
    if (material.userData.efmiToonMaterial) {
        return;
    }
    const defaults = DEFAULT_EFMI_TOON_STYLE;
    const state: EFMIToonShaderState = {
        enabled: { value: 0 },
        rangeBias: { value: defaults.rangeBias },
        softness: { value: defaults.softness },
        shadowBrightness: { value: defaults.shadowBrightness },
        shadowSaturation: { value: defaults.shadowSaturation },
        ambient: { value: defaults.ambient },
        lightStrength: { value: defaults.lightStrength },
        brightness: { value: defaults.brightness },
        specular: { value: defaults.specular },
        rimStrength: { value: defaults.rimStrength },
        rimWidth: { value: defaults.rimWidth },
        lightMap: { value: fallbackWhiteTexture },
        lightMapEnabled: { value: 0 },
        lightMapTransform: { value: fallbackWhiteTexture.matrix },
        face: { value: 0 },
        faceMask: { value: fallbackWhiteTexture },
        faceMaskEnabled: { value: 0 },
        faceMaskTransform: { value: fallbackWhiteTexture.matrix },
        highlightMap: { value: fallbackWhiteTexture },
        highlightEnabled: { value: 0 },
        highlightTransform: { value: fallbackWhiteTexture.matrix },
        rampMap: { value: fallbackWhiteTexture },
        rampEnabled: { value: 0 },
        outlineMask: null,
    };
    material.userData.efmiToonMaterial = state;
    material.onBeforeCompile = (shader: WebGLProgramParametersWithUniforms) => {
        shader.uniforms.efmiToonEnabled = state.enabled;
        shader.uniforms.efmiRangeBias = state.rangeBias;
        shader.uniforms.efmiSoftness = state.softness;
        shader.uniforms.efmiShadowBrightness = state.shadowBrightness;
        shader.uniforms.efmiShadowSaturation = state.shadowSaturation;
        shader.uniforms.efmiAmbient = state.ambient;
        shader.uniforms.efmiLightStrength = state.lightStrength;
        shader.uniforms.efmiBrightness = state.brightness;
        shader.uniforms.efmiSpecular = state.specular;
        shader.uniforms.efmiRimStrength = state.rimStrength;
        shader.uniforms.efmiRimWidth = state.rimWidth;
        shader.uniforms.efmiLightMap = state.lightMap;
        shader.uniforms.efmiLightMapEnabled = state.lightMapEnabled;
        shader.uniforms.efmiLightMapTransform = state.lightMapTransform;
        shader.uniforms.efmiFace = state.face;
        shader.uniforms.efmiFaceMask = state.faceMask;
        shader.uniforms.efmiFaceMaskEnabled = state.faceMaskEnabled;
        shader.uniforms.efmiFaceMaskTransform = state.faceMaskTransform;
        shader.uniforms.efmiHighlightMap = state.highlightMap;
        shader.uniforms.efmiHighlightEnabled = state.highlightEnabled;
        shader.uniforms.efmiHighlightTransform = state.highlightTransform;
        shader.uniforms.efmiRampMap = state.rampMap;
        shader.uniforms.efmiRampEnabled = state.rampEnabled;
        shader.vertexShader = replaceShaderAnchor(
            shader.vertexShader,
            "#include <common>",
            `#include <common>
varying vec2 vEfmiUv;
varying vec3 vEfmiModelRight;`,
        );
        shader.vertexShader = replaceShaderAnchor(
            shader.vertexShader,
            "#include <uv_vertex>",
            `#include <uv_vertex>
vEfmiUv = uv;
vEfmiModelRight = normalize( mat3( modelMatrix ) * vec3( 1.0, 0.0, 0.0 ) );`,
        );
        shader.fragmentShader = replaceShaderAnchor(
            shader.fragmentShader,
            "#include <common>",
            `#include <common>
varying vec2 vEfmiUv;
varying vec3 vEfmiModelRight;
uniform float efmiToonEnabled;
uniform float efmiRangeBias;
uniform float efmiSoftness;
uniform float efmiShadowBrightness;
uniform float efmiShadowSaturation;
uniform float efmiAmbient;
uniform float efmiLightStrength;
uniform float efmiBrightness;
uniform float efmiSpecular;
uniform float efmiRimStrength;
uniform float efmiRimWidth;
uniform sampler2D efmiLightMap;
uniform float efmiLightMapEnabled;
uniform mat3 efmiLightMapTransform;
uniform float efmiFace;
uniform sampler2D efmiFaceMask;
uniform float efmiFaceMaskEnabled;
uniform mat3 efmiFaceMaskTransform;
uniform sampler2D efmiHighlightMap;
uniform float efmiHighlightEnabled;
uniform mat3 efmiHighlightTransform;
uniform sampler2D efmiRampMap;
uniform float efmiRampEnabled;

float efmiLuma( vec3 color ) {
    return dot( color, vec3( 0.212673, 0.715152, 0.072175 ) );
}

${EFMI_RAMP_GLSL}`,
        );
        shader.fragmentShader = replaceShaderAnchor(
            shader.fragmentShader,
            "#include <opaque_fragment>",
            `#if NUM_DIR_LIGHTS > 0
if ( efmiToonEnabled > 0.5 ) {
    ${TOON_KEY_LIGHT_GLSL}
    vec3 cameraBack = vec3( viewMatrix[ 0 ][ 2 ], viewMatrix[ 1 ][ 2 ], viewMatrix[ 2 ][ 2 ] );
    vec3 albedo = diffuseColor.rgb;
    vec2 lightMapUv = ( efmiLightMapTransform * vec3( vEfmiUv, 1.0 ) ).xy;
    vec4 packedMap = mix( vec4( 0.0, 1.0, 1.0, 0.5 ), texture2D( efmiLightMap, lightMapUv ), efmiLightMapEnabled );

    float lightCosine = dot( worldNormal, worldLight );

    // _SDFMask.G keeps N.L where the SDF does not reach, such as the jaw edge.
    vec4 faceMask = vec4( 1.0, 0.0, 1.0, 0.0 );
    if ( efmiFace > 0.5 && efmiFaceMaskEnabled > 0.5 ) {
        faceMask = texture2D( efmiFaceMask, ( efmiFaceMaskTransform * vec3( vEfmiUv, 1.0 ) ).xy );
    }
    float flatLight = efmiFace * ( 1.0 - faceMask.g );
    vec4 ramp = efmiRamp( lightCosine + efmiRangeBias );
    float rampCoverage = mix( ramp.a, 1.0, flatLight );
    float lit = mix( min( rampCoverage, min( packedMap.b, occlusion ) ), 1.0, flatLight );
    float cameraFacing = efmiRamp( dot( worldNormal, cameraBack ) ).a * packedMap.b * occlusion;

    vec3 shadowTone = mix( vec3( efmiLuma( albedo ) ), albedo, efmiShadowSaturation );
    shadowTone = max( shadowTone, vec3( 0.0 ) ) * efmiShadowBrightness * 0.96 * efmiAmbient;
    vec3 deepTone = 0.65 * shadowTone;
    float deepLuma = efmiLuma( deepTone );
    deepTone = vec3( deepLuma ) + 1.2 * ( deepTone - deepLuma );
    vec3 surface = mix( deepTone, shadowTone, clamp( rampCoverage + cameraFacing + efmiFace, 0.0, 1.0 ) );
    float viewCosine = clamp( dot( worldNormal, worldView ), 0.0, 1.0 );
    float faceRim = efmiFace * efmiFaceMaskEnabled * faceMask.r * mix( 0.7, 0.3, faceMask.b );
    vec3 litTone = albedo * mix( vec3( 1.0 ), vec3( 0.637, 0.376, 0.296 ), clamp( 0.85 * ( 1.0 - viewCosine ) * faceRim, 0.0, 1.0 ) );
    surface = efmiRampTint( mix( surface, litTone * 0.96, lit ), mix( ramp.rgb, vec3( 1.0 ), flatLight ) );

    float sky = mix( clamp( worldNormal.y + 0.15, 0.0, 1.0 ) * 1.5 + 0.5, 0.725, efmiFace );
    vec3 shadowTint = mix( ${EFMI_SHADOW_TINT}, ${EFMI_SKIN_SHADOW_TINT}, efmiFace );
    vec3 irradiance = mix( shadowTint, vec3( 1.0 ), lit ) * sky * 0.2877 * efmiLightStrength;
    irradiance += vec3( efmiLightStrength * efmiBrightness );
    vec3 toon = surface * irradiance;
    if ( efmiFace > 0.5 && efmiHighlightEnabled > 0.5 ) {
        vec2 highlightUv = vEfmiUv + vec2( dot( worldView, normalize( vEfmiModelRight ) ) * 0.03, 0.0 );
        toon += texture2D( efmiHighlightMap, ( efmiHighlightTransform * vec3( highlightUv, 1.0 ) ).xy ).rgb * irradiance * mix( efmiAmbient, 1.0, lit ) * ( 0.5 + 0.5 * lit );
    }

    float roughness = 1.0 - packedMap.a;
    float alpha = max( roughness * roughness, 0.007812 );
    float alphaSquared = alpha * alpha;
    vec3 specularHalf = normalize( worldLight + 2.0 * normalize( vec3( cameraBack.x, worldLight.y, cameraBack.z ) ) + 3.0 * worldView );
    float halfCosine = dot( worldNormal, specularHalf );
    float distribution = halfCosine * halfCosine * ( alphaSquared - 1.0 ) + 1.0;
    distribution = distribution * distribution;
    distribution = distribution != alphaSquared ? alphaSquared / distribution : 1.0;
    float specularTerm = clamp( distribution * 0.5 / ( 2.0 * viewCosine + alpha + 0.0001 ) - 0.000061, 0.0, 20.0 );
    vec3 reflectance = mix( vec3( 0.04 * packedMap.g ), albedo, packedMap.r );
    toon += irradiance * ( 0.5 + 0.5 * lit ) * mix( efmiAmbient, 1.0, lit ) * specularTerm * reflectance * efmiSpecular * ( 1.0 - flatLight );

    float luma = efmiLuma( toon );
    float vivid = clamp( luma - 0.5, 0.0, 0.5 );
    toon = luma + ( toon - luma ) * ( vivid * vivid + 1.0 );

    vec3 screenLight = worldLight - cameraBack * dot( worldLight, cameraBack );
    float screenLength = length( screenLight );
    float rimSide = screenLength > 0.00001 ? clamp( dot( worldNormal, screenLight / screenLength ), 0.0, 1.0 ) : 0.0;
    float grazing = 1.0 - abs( viewCosine );
    float rimBand = smoothstep( 0.0, 1.0, ( grazing - ( 0.8 - 0.6 * efmiRimWidth ) ) / ( 0.1 + 0.2 * efmiRimWidth ) );
    toon += rimBand * rimSide * albedo * 0.96 * efmiRimStrength * ( 1.0 - flatLight );

    outgoingLight = toon;
}
#endif
#include <opaque_fragment>`,
        );

        // The toon result is display-referred like Warfarin's, which applies no tone
        // mapping, so the viewer's exposure and tone mapping would only dim it.
        shader.fragmentShader = replaceShaderAnchor(
            shader.fragmentShader,
            "#include <tonemapping_fragment>",
            `if ( efmiToonEnabled < 0.5 ) {
    #include <tonemapping_fragment>
}`,
        );
    };
    material.customProgramCacheKey = () => "efmi-toon-v7";
    material.needsUpdate = true;
}

function syncEFMIToon(
    object: Mesh,
    material: MeshStandardMaterial,
    data: MaterialProfileMeshData,
): void {
    const userData = data as EFMIMeshData;
    const state = material.userData.efmiToonMaterial as EFMIToonShaderState | undefined;
    if (!state) {
        return;
    }
    const style = (userData.toonStyle as EFMIToonStyle | undefined) ?? DEFAULT_EFMI_TOON_STYLE;
    const enabled = userData.toonShadows;
    state.enabled.value = enabled ? 1 : 0;
    state.rangeBias.value = style.rangeBias;
    state.softness.value = style.softness;
    state.shadowBrightness.value = style.shadowBrightness;
    state.shadowSaturation.value = style.shadowSaturation;
    state.ambient.value = style.ambient;
    state.lightStrength.value = style.lightStrength;
    state.brightness.value = style.brightness;
    state.specular.value = style.specular;
    state.rimStrength.value = style.rimStrength;
    state.rimWidth.value = style.rimWidth;
    state.face.value =
        style.flatFace && userData.efmiHeadMesh && state.lightMapEnabled.value === 0 ? 1 : 0;

    // Coverage cards are flat ink decals: they neither block light nor have a silhouette.
    const coverageMask = Boolean(material.map?.userData.modelViewerCoverageMask);
    object.castShadow = enabled && style.selfShadow && !coverageMask && !userData.overlayShadow;
    object.receiveShadow = enabled && style.selfShadow;

    const showOutline =
        enabled &&
        style.outlines &&
        style.outlineWidth > 0 &&
        !coverageMask &&
        !userData.overlayShadow &&
        !userData.efmiEyeMesh;
    if (!showOutline && !userData.efmiOutline) {
        return;
    }
    userData.efmiOutline ??= createEFMIToonOutline(object);
    const uniforms = userData.efmiOutline.material.uniforms;
    userData.efmiOutline.visible = showOutline;
    uniforms.map.value = material.map ?? fallbackWhiteTexture;
    uniforms.mapTransform.value = textureUvTransform(uniforms.map.value);
    uniforms.widthMask.value = state.outlineMask ?? fallbackWhiteTexture;
    uniforms.widthMaskEnabled.value = state.outlineMask ? 1 : 0;
    uniforms.widthMaskTransform.value = textureUvTransform(uniforms.widthMask.value);
    uniforms.outlineWidth.value = style.outlineWidth;
    uniforms.outlineBrightness.value = style.outlineBrightness;
    uniforms.outlineDepthOffset.value = state.face.value * EFMI_FACE_OUTLINE_DEPTH_OFFSET;
    uniforms.efmiFace.value = state.face.value;
    uniforms.efmiRampMap.value = state.rampMap.value;
    uniforms.efmiRampEnabled.value = state.rampEnabled.value;
    uniforms.efmiSoftness.value = style.softness;
    uniforms.efmiRangeBias.value = style.rangeBias;
    uniforms.efmiShadowBrightness.value = style.shadowBrightness;
    uniforms.efmiShadowSaturation.value = style.shadowSaturation;
    uniforms.efmiAmbient.value = style.ambient;
    uniforms.efmiLightStrength.value = style.lightStrength;
    uniforms.efmiBrightness.value = style.brightness;
    uniforms.efmiRimStrength.value = style.rimStrength;
    uniforms.efmiRimWidth.value = style.rimWidth;
}

// An inverted hull sharing the mesh's geometry, so position variants, shape keys
// and compute deformation move it too. It expands in clip space by a width that
// shrinks with distance and never drops below half a pixel.
function createEFMIToonOutline(object: Mesh): Mesh<BufferGeometry, ShaderMaterial> {
    if (!object.geometry.attributes.outlineNormal) {
        object.geometry.setAttribute("outlineNormal", averagedNormals(object.geometry));
    }
    const resolution = new Vector2(1, 1);
    const outline = new Mesh(
        object.geometry,
        new ShaderMaterial({
            lights: true,
            uniforms: {
                ...UniformsUtils.clone(UniformsLib.lights),
                efmiFace: { value: 0 },
                efmiRampMap: { value: fallbackWhiteTexture },
                efmiRampEnabled: { value: 0 },
                efmiSoftness: { value: 0 },
                efmiRangeBias: { value: 0 },
                efmiShadowBrightness: { value: 0 },
                efmiShadowSaturation: { value: 0 },
                efmiAmbient: { value: 0 },
                efmiLightStrength: { value: 0 },
                efmiBrightness: { value: 0 },
                efmiRimStrength: { value: 0 },
                efmiRimWidth: { value: 0 },
                map: { value: fallbackWhiteTexture },
                mapTransform: { value: fallbackWhiteTexture.matrix },
                widthMask: { value: fallbackWhiteTexture },
                widthMaskEnabled: { value: 0 },
                widthMaskTransform: { value: fallbackWhiteTexture.matrix },
                outlineWidth: { value: 0 },
                outlineBrightness: { value: 0 },
                outlineDepthOffset: { value: 0 },
                outlineResolution: { value: resolution },
            },
            vertexShader: `attribute vec3 outlineNormal;
uniform float outlineWidth;
uniform float outlineDepthOffset;
uniform vec2 outlineResolution;
uniform mat3 mapTransform;
uniform sampler2D widthMask;
uniform float widthMaskEnabled;
uniform mat3 widthMaskTransform;
varying vec2 vOutlineUv;
varying vec3 vOutlineNormal;
varying vec3 vOutlinePosition;
void main() {
    vOutlineUv = ( mapTransform * vec3( uv, 1.0 ) ).xy;
    vOutlineNormal = normalize( mat3( modelMatrix ) * outlineNormal );
    vOutlinePosition = ( modelMatrix * vec4( position, 1.0 ) ).xyz;
    vec4 viewPosition = modelViewMatrix * vec4( position, 1.0 );
    gl_Position = projectionMatrix * viewPosition;
    vec2 direction = ( projectionMatrix * vec4( normalMatrix * outlineNormal, 0.0 ) ).xy;
    float directionSquared = dot( direction, direction );
    if ( directionSquared > 0.0 ) {
        direction /= sqrt( directionSquared );
        direction.x *= outlineResolution.y / outlineResolution.x;
        float halfFov = atan( 1.0 / projectionMatrix[ 1 ][ 1 ] );
        float w = gl_Position.w;
        vec2 offset = direction * ( 0.392699 / halfFov * outlineWidth * clamp( w * halfFov * 4.583663, 0.0, 1.0 ) * 0.005 );
        vec2 minimum = min( max( w, 0.0 ), 1.570796 / halfFov ) / outlineResolution;
        offset = mix( offset, sign( offset ) * minimum, vec2( lessThan( abs( offset ), minimum ) ) );
        if ( widthMaskEnabled > 0.5 ) {
            offset *= texture2D( widthMask, ( widthMaskTransform * vec3( uv, 1.0 ) ).xy ).r;
        }
        gl_Position.xy += offset;
        vec4 pushed = projectionMatrix * vec4( viewPosition.xy, viewPosition.z - outlineDepthOffset, 1.0 );
        gl_Position.z = pushed.z * w / pushed.w;
    }
}`,
            fragmentShader: `#include <common>
#include <lights_pars_begin>
uniform sampler2D map;
uniform float outlineBrightness;
uniform float efmiFace;
uniform sampler2D efmiRampMap;
uniform float efmiRampEnabled;
uniform float efmiSoftness;
uniform float efmiRangeBias;
uniform float efmiShadowBrightness;
uniform float efmiShadowSaturation;
uniform float efmiAmbient;
uniform float efmiLightStrength;
uniform float efmiBrightness;
uniform float efmiRimStrength;
uniform float efmiRimWidth;
varying vec2 vOutlineUv;
varying vec3 vOutlineNormal;
varying vec3 vOutlinePosition;

float efmiLuma( vec3 color ) {
    return dot( color, vec3( 0.212673, 0.715152, 0.072175 ) );
}

${EFMI_RAMP_GLSL}

void main() {
    vec3 albedo = texture2D( map, vOutlineUv ).rgb * outlineBrightness;
    vec3 color = albedo;
    #if NUM_DIR_LIGHTS > 0
    vec3 worldNormal = normalize( vOutlineNormal );
    vec3 worldLight = inverseTransformDirection( directionalLights[ 0 ].direction, viewMatrix );
    vec3 worldView = normalize( cameraPosition - vOutlinePosition );
    vec3 cameraBack = vec3( viewMatrix[ 0 ][ 2 ], viewMatrix[ 1 ][ 2 ], viewMatrix[ 2 ][ 2 ] );

    vec4 ramp = efmiRamp( dot( worldNormal, worldLight ) + efmiRangeBias );
    float facing = clamp( ( dot( worldNormal, cameraBack ) - 0.25 ) * 1.333333, 0.0, 1.0 );
    float deep = clamp( ramp.a + facing * facing * ( 3.0 - 2.0 * facing ) + efmiFace, 0.0, 1.0 );

    vec3 shadowTone = mix( vec3( efmiLuma( albedo ) ), albedo, efmiShadowSaturation );
    shadowTone = max( shadowTone, vec3( 0.0 ) ) * efmiShadowBrightness * 0.96 * efmiAmbient;
    vec3 deepTone = 0.65 * shadowTone;
    float deepLuma = efmiLuma( deepTone );
    deepTone = vec3( deepLuma ) + 1.2 * ( deepTone - deepLuma );
    vec3 surface = efmiRampTint( mix( mix( deepTone, shadowTone, deep ), albedo * 0.96, ramp.a ), ramp.rgb );

    float sky = mix( clamp( worldNormal.y + 0.15, 0.0, 1.0 ) * 1.5 + 0.5, 0.725, efmiFace );
    vec3 shadowTint = mix( ${EFMI_SHADOW_TINT}, ${EFMI_SKIN_SHADOW_TINT}, efmiFace );
    vec3 irradiance = mix( shadowTint, vec3( 1.0 ), ramp.a ) * sky * 0.2877 * efmiLightStrength;
    color = surface * ( irradiance + vec3( efmiLightStrength * efmiBrightness ) );

    float luma = efmiLuma( color );
    float vivid = clamp( luma - 0.5, 0.0, 0.5 );
    color = luma + ( color - luma ) * ( vivid * vivid + 1.0 );

    vec3 screenLight = worldLight - cameraBack * dot( worldLight, cameraBack );
    float screenLength = length( screenLight );
    float rimSide = screenLength > 0.00001 ? clamp( dot( worldNormal, screenLight / screenLength ), 0.0, 1.0 ) : 0.0;
    float grazing = 1.0 - abs( dot( worldNormal, worldView ) );
    float rimBand = smoothstep( 0.0, 1.0, ( grazing - ( 0.8 - 0.6 * efmiRimWidth ) ) / ( 0.1 + 0.2 * efmiRimWidth ) );
    color += rimBand * rimSide * albedo * 0.96 * efmiRimStrength * ( 1.0 - efmiFace );
    #endif
    gl_FragColor = vec4( color, 1.0 );
    #include <colorspace_fragment>
}`,
            side: BackSide,
        }),
    );
    outline.raycast = () => {};
    outline.onBeforeRender = (renderer) => {
        renderer.getDrawingBufferSize(resolution);
    };
    object.add(outline);
    return outline;
}
