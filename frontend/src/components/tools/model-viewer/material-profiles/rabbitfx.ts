import type {
    Matrix3,
    MeshStandardMaterial,
    Texture,
    WebGLProgramParametersWithUniforms,
} from "three";
import { ShaderChunk } from "three";

import type { ModelViewerMaterialProfile } from "./profile";

import { modelViewerDDSMetadataOf } from "../model-viewer-dds";
import {
    fallbackWhiteTexture,
    replaceShaderAnchor,
    textureUvTransform,
} from "../model-viewer-shader-utils";

type RabbitFXShaderState = {
    lightMap: { value: Texture };
    lightMapEnabled: { value: number };
    lightMapTransform: { value: Matrix3 };
    cutoutMap: { value: Texture };
    cutoutEnabled: { value: number };
    cutoutTransform: { value: Matrix3 };
    toonEnabled: { value: number };
    lightBC4: { value: number };
    lightSigned: { value: number };
};

export const rabbitFXMaterialProfile: ModelViewerMaterialProfile = {
    baseMaterial: { metalness: 0, roughness: 1 },
    derivativeTangentFrame: true,
    toonShadows: true,
    configureMaterial(material, object, data) {
        if (object.geometry.attributes.uv) {
            configureRabbitFXMaterialShader(material, data.toonShadows);
        }
    },
    bindMaps(material, maps, textures) {
        const state = material.userData.rabbitFXMaterial as RabbitFXShaderState | undefined;
        if (!state) {
            return;
        }
        const lightMap = maps.lightMapKey ? (textures.get(maps.lightMapKey) ?? null) : null;
        state.lightMap.value = lightMap ?? fallbackWhiteTexture;
        state.lightMapEnabled.value = lightMap ? 1 : 0;
        state.lightMapTransform.value = textureUvTransform(state.lightMap.value);
        const dds = modelViewerDDSMetadataOf(lightMap);
        state.lightBC4.value = dds?.direct && dds.format.startsWith("bc4-") ? 1 : 0;
        state.lightSigned.value = dds?.direct && dds.format.endsWith("snorm") ? 1 : 0;
        const cutoutMap = maps.cutoutMapKey ? textures.get(maps.cutoutMapKey) : undefined;
        state.cutoutMap.value = cutoutMap ?? fallbackWhiteTexture;
        state.cutoutEnabled.value = cutoutMap ? 1 : 0;
        state.cutoutTransform.value = textureUvTransform(state.cutoutMap.value);
    },
    sync(_object, material, data) {
        const state = material.userData.rabbitFXMaterial as RabbitFXShaderState | undefined;
        if (state) {
            state.toonEnabled.value = data.toonShadows ? 1 : 0;
        }
    },
};

function configureRabbitFXMaterialShader(
    material: MeshStandardMaterial,
    toonShadows: boolean,
): void {
    if (material.userData.rabbitFXMaterial) {
        return;
    }
    const state: RabbitFXShaderState = {
        lightMap: { value: fallbackWhiteTexture },
        lightMapEnabled: { value: 0 },
        lightMapTransform: { value: fallbackWhiteTexture.matrix },
        cutoutMap: { value: fallbackWhiteTexture },
        cutoutEnabled: { value: 0 },
        cutoutTransform: { value: fallbackWhiteTexture.matrix },
        toonEnabled: { value: toonShadows ? 1 : 0 },
        lightBC4: { value: 0 },
        lightSigned: { value: 0 },
    };
    material.userData.rabbitFXMaterial = state;
    material.onBeforeCompile = (shader: WebGLProgramParametersWithUniforms) => {
        shader.uniforms.rabbitFXLightMap = state.lightMap;
        shader.uniforms.rabbitFXLightMapEnabled = state.lightMapEnabled;
        shader.uniforms.rabbitFXLightMapTransform = state.lightMapTransform;
        shader.uniforms.rabbitFXCutoutMap = state.cutoutMap;
        shader.uniforms.rabbitFXCutoutEnabled = state.cutoutEnabled;
        shader.uniforms.rabbitFXCutoutTransform = state.cutoutTransform;
        shader.uniforms.rabbitFXToonEnabled = state.toonEnabled;
        shader.uniforms.rabbitFXLightBC4 = state.lightBC4;
        shader.uniforms.rabbitFXLightSigned = state.lightSigned;
        shader.vertexShader = replaceShaderAnchor(
            shader.vertexShader,
            "#include <common>",
            `#include <common>
varying vec2 vRabbitFXUv;`,
        );
        shader.vertexShader = replaceShaderAnchor(
            shader.vertexShader,
            "#include <uv_vertex>",
            `#include <uv_vertex>
vRabbitFXUv = uv;`,
        );
        shader.fragmentShader = replaceShaderAnchor(
            shader.fragmentShader,
            "#include <common>",
            `#include <common>
varying vec2 vRabbitFXUv;
uniform sampler2D rabbitFXLightMap;
uniform float rabbitFXLightMapEnabled;
uniform mat3 rabbitFXLightMapTransform;
uniform sampler2D rabbitFXCutoutMap;
uniform float rabbitFXCutoutEnabled;
uniform mat3 rabbitFXCutoutTransform;
uniform float rabbitFXToonEnabled;
uniform float rabbitFXLightBC4;
uniform float rabbitFXLightSigned;

float rabbitFXDirectionalDiffuseFactor( vec3 surfaceNormal, vec3 lightDirection ) {
    if ( rabbitFXLightMapEnabled < 0.5 ) return 1.0;
    vec4 lightSample = texture2D( rabbitFXLightMap, ( rabbitFXLightMapTransform * vec3( vRabbitFXUv, 1.0 ) ).xy );
    float mask = mix( lightSample.g, lightSample.r, rabbitFXLightBC4 );
    if ( rabbitFXLightSigned > 0.5 ) mask = mask * 0.5 + 0.5;
    if ( mask <= 0.01 || mask >= 0.99 ) return 1.0;
    float visibility = step( 0.1, mask );
    float boundary = smoothstep( 0.55, 0.81, dot( surfaceNormal, lightDirection ) + 0.4 );
    return mix( 1.0, boundary * visibility, rabbitFXToonEnabled );
}`,
        );

        // Red also holds a BC4 mask, which has no green channel to sample.
        shader.fragmentShader = replaceShaderAnchor(
            shader.fragmentShader,
            "#include <map_fragment>",
            `if ( rabbitFXCutoutEnabled > 0.5 && texture2D( rabbitFXCutoutMap, ( rabbitFXCutoutTransform * vec3( vRabbitFXUv, 1.0 ) ).xy ).r < 0.5 ) discard;
#include <map_fragment>`,
        );
        shader.fragmentShader = replaceShaderAnchor(
            shader.fragmentShader,
            "#include <lights_fragment_begin>",
            patchRabbitFXDirectionalLights(ShaderChunk.lights_fragment_begin),
        );
    };
    material.customProgramCacheKey = () => "wuwa-rabbitfx-v3";
    material.needsUpdate = true;
}

function patchRabbitFXDirectionalLights(source: string): string {
    const startAnchor = "#if ( NUM_DIR_LIGHTS > 0 ) && defined( RE_Direct )";
    const endAnchor = "#if ( NUM_RECT_AREA_LIGHTS > 0 ) && defined( RE_Direct_RectArea )";
    const start = source.indexOf(startAnchor);
    const end = source.indexOf(endAnchor, start + startAnchor.length);
    if (start < 0 || end < 0 || source.indexOf(startAnchor, start + 1) >= 0) {
        throw new Error("Three.js directional-light shader anchor changed");
    }
    const declarationsAnchor = "\tDirectionalLight directionalLight;";
    const section = replaceShaderAnchor(
        source.slice(start, end),
        declarationsAnchor,
        `${declarationsAnchor}
    vec3 rabbitFXDirectDiffuseBefore;
    vec3 rabbitFXDirectDiffuseContribution;`,
    );
    const directCall =
        "\t\tRE_Direct( directLight, geometryPosition, geometryNormal, geometryViewDir, geometryClearcoatNormal, material, reflectedLight );";
    const replacement = `        rabbitFXDirectDiffuseBefore = reflectedLight.directDiffuse;
        RE_Direct( directLight, geometryPosition, geometryNormal, geometryViewDir, geometryClearcoatNormal, material, reflectedLight );
        rabbitFXDirectDiffuseContribution = reflectedLight.directDiffuse - rabbitFXDirectDiffuseBefore;
        reflectedLight.directDiffuse = rabbitFXDirectDiffuseBefore + rabbitFXDirectDiffuseContribution * rabbitFXDirectionalDiffuseFactor( geometryNormal, directLight.direction );`;
    const patchedSection = replaceShaderAnchor(section, directCall, replacement);
    return source.slice(0, start) + patchedSection + source.slice(end);
}
