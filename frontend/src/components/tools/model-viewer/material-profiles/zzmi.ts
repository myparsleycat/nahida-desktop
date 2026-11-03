import type { MeshStandardMaterial, WebGLProgramParametersWithUniforms } from "three";

import type { ModelViewerMaterialProfile } from "./profile";

export const zzmiMaterialProfile: ModelViewerMaterialProfile = {
    baseMaterial: { metalness: 0.05, roughness: 0.65 },
    derivativeTangentFrame: false,
    toonShadows: false,
    configureMaterial: configurePackedMaterialShader,

    // ZZMI's packed maps are not generic Three.js AO/PBR maps. LightMap.G is
    // metallic and MaterialMap.G is roughness, not glossiness: game textures
    // store it highest on cloth and hair, lower on skin, and lowest on metal.
    // The shader adapter below performs the required channel selection.
    bindMaps(material, maps, textures) {
        material.metalnessMap = maps.lightMapKey ? (textures.get(maps.lightMapKey) ?? null) : null;
        if (material.metalnessMap) material.metalness = 1;
        material.roughnessMap = maps.materialMapKey
            ? (textures.get(maps.materialMapKey) ?? null)
            : null;
        if (material.roughnessMap) material.roughness = 1;
    },
};

function configurePackedMaterialShader(material: MeshStandardMaterial): void {
    if (material.userData.zzmiPackedMaterial) {
        return;
    }
    material.userData.zzmiPackedMaterial = true;
    material.onBeforeCompile = (shader: WebGLProgramParametersWithUniforms) => {
        shader.fragmentShader = shader.fragmentShader
            .replace(
                "#include <roughnessmap_fragment>",
                `float roughnessFactor = roughness;
#ifdef USE_ROUGHNESSMAP
    vec4 texelRoughness = texture2D( roughnessMap, vRoughnessMapUv );
    roughnessFactor *= texelRoughness.g;
#endif`,
            )
            .replace(
                "#include <metalnessmap_fragment>",
                `float metalnessFactor = metalness;
#ifdef USE_METALNESSMAP
    vec4 texelMetalness = texture2D( metalnessMap, vMetalnessMapUv );
    metalnessFactor *= texelMetalness.g;
#endif`,
            );
    };
    material.customProgramCacheKey = () => "zzmi-packed-material-v1";
}
