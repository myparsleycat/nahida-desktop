import { fetchFloat32, fetchUint32 } from "@renderer/wails/binary-memory";
import type {
    EvaluatedViewerState,
    ModViewerTransport,
    ViewerMeshTransport,
} from "@shared/mod-viewer/types";
import {
    BufferAttribute,
    BufferGeometry,
    DataTexture,
    DoubleSide,
    Group,
    Mesh,
    MeshStandardMaterial,
    Object3D,
    NoColorSpace,
    RGBAFormat,
    SRGBColorSpace,
    ShaderChunk,
    Texture,
    TextureLoader,
} from "three";
import type { WebGLProgramParametersWithUniforms } from "three";

import type { PositionVariantLoader } from "./model-viewer-position-loader";

type PayloadMeshUserData = {
    meshId: string;
    basePositions: Float32Array;
    shapeTargets: Array<{
        var: string;
        positions: Float32Array;
        mode?: "midpoint_pair";
        lowPositions?: Float32Array;
    }>;
    positionVariants: ViewerMeshTransport["positionVariants"];
    sourceIndicesUrl?: string;
    normalCache: Array<{ key: number; normal: Float32Array }>;
    materialProfile?: ModViewerTransport["materialProfile"];
    toonShadows: boolean;
    lastPositionVariantIndex?: number | null;
    lastMaps?: {
        texKey: string | null;
        normalMapKey: string | null;
        lightMapKey: string | null;
        materialMapKey: string | null;
    };
};

type RabbitFXShaderState = {
    lightMap: { value: Texture };
    lightMapEnabled: { value: number };
    toonEnabled: { value: number };
};

const textureLoader = new TextureLoader();
const rabbitFXFallbackLightMap = new DataTexture(
    new Uint8Array([255, 255, 255, 255]),
    1,
    1,
    RGBAFormat,
);
rabbitFXFallbackLightMap.colorSpace = NoColorSpace;
rabbitFXFallbackLightMap.needsUpdate = true;

export type PreparedPayloadEval = {
    evalResult: EvaluatedViewerState;
    positions: Map<string, { variantIndex: number; positions: Float32Array }>;
};

export async function buildPayloadModel(
    transport: ModViewerTransport,
    evalResult: EvaluatedViewerState,
    doubleSided: boolean,
    positionLoader: PositionVariantLoader,
    toonShadows = false,
): Promise<Group> {
    const textureCache = new Map<string, Promise<Texture | null>>();
    const textures = new Map<string, Texture>();
    await Promise.all(
        Object.entries(transport.textures).map(async ([key, entry]) => {
            const texture = await loadTexture(entry.url, textureCache);
            if (texture) {
                texture.colorSpace = entry.role === "diffuse" ? SRGBColorSpace : NoColorSpace;
                textures.set(key, texture);
            }
        }),
    );

    const group = new Group();
    group.userData.payloadTextures = textures;
    const evalById = new Map(evalResult.meshes.map((mesh) => [mesh.id, mesh]));
    try {
        for (const mesh of transport.meshes) {
            const geometry = await buildGeometry(mesh);
            const rabbitFX = transport.materialProfile === "wuwa:rabbitfx";
            const material = new MeshStandardMaterial({
                color: 0xffffff,
                metalness: rabbitFX ? 0 : 0.05,
                roughness: rabbitFX ? 1 : 0.65,
                side: doubleSided ? DoubleSide : undefined,
            });
            const object = new Mesh(geometry, material);
            object.name = mesh.id;
            const evaluated = evalById.get(mesh.id);
            object.visible = evaluated?.visible ?? true;
            group.add(object);
            object.userData = {
                meshId: mesh.id,
                basePositions: new Float32Array(geometry.attributes.position.array as Float32Array),
                shapeTargets: await Promise.all(
                    mesh.shapeTargets.map(async (target) => ({
                        var: target.var,
                        positions: await fetchFloat32(target.positionsUrl),
                        mode: target.mode,
                        lowPositions: target.lowPositionsUrl
                            ? await fetchFloat32(target.lowPositionsUrl)
                            : undefined,
                    })),
                ),
                positionVariants: [...mesh.positionVariants],
                sourceIndicesUrl: mesh.sourceIndicesUrl,
                normalCache: [],
                materialProfile: transport.materialProfile,
                toonShadows,
            } satisfies PayloadMeshUserData;
        }
        commitPayloadEval(group, await preparePayloadEval(group, evalResult, positionLoader));
        return group;
    } catch (error) {
        disposeIncompletePayloadModel(group);
        throw error;
    }
}

export function applyPayloadEval(root: Object3D, evalResult: EvaluatedViewerState): void {
    commitPayloadEval(root, { evalResult, positions: new Map() });
}

export function setPayloadToonShadows(root: Object3D, enabled: boolean): void {
    root.traverse((object) => {
        if (!(object instanceof Mesh) || !object.userData.meshId) {
            return;
        }
        const userData = object.userData as PayloadMeshUserData;
        userData.toonShadows = enabled;
        const material = object.material;
        if (!(material instanceof MeshStandardMaterial)) {
            return;
        }
        const state = material.userData.rabbitFXMaterial as RabbitFXShaderState | undefined;
        if (state) {
            state.toonEnabled.value = enabled ? 1 : 0;
        }
    });
}

export async function preparePayloadEval(
    root: Object3D,
    evalResult: EvaluatedViewerState,
    positionLoader: PositionVariantLoader,
    signal?: AbortSignal,
    forcePositions = false,
): Promise<PreparedPayloadEval> {
    const evaluatedById = new Map(evalResult.meshes.map((mesh) => [mesh.id, mesh]));
    const requests: Array<Promise<[string, { variantIndex: number; positions: Float32Array }]>> =
        [];
    root.traverse((object) => {
        if (!(object instanceof Mesh)) {
            return;
        }
        const userData = object.userData as PayloadMeshUserData;
        const evaluated = evaluatedById.get(userData.meshId);
        const variantIndex = evaluated?.positionVariantIndex;
        if (
            !evaluated?.visible ||
            variantIndex === null ||
            variantIndex === undefined ||
            (!forcePositions && userData.lastPositionVariantIndex === variantIndex)
        ) {
            return;
        }
        const descriptor = userData.positionVariants[variantIndex];
        if (!descriptor) {
            return;
        }
        requests.push(
            positionLoader
                .load(
                    descriptor,
                    userData.sourceIndicesUrl,
                    userData.basePositions.length / 3,
                    signal,
                )
                .then((positions) => [userData.meshId, { variantIndex, positions }]),
        );
    });
    return { evalResult, positions: new Map(await Promise.all(requests)) };
}

export function commitPayloadEval(root: Object3D, prepared: PreparedPayloadEval): void {
    const textures = root.userData.payloadTextures as Map<string, Texture> | undefined;
    const evalById = new Map(prepared.evalResult.meshes.map((mesh) => [mesh.id, mesh]));
    root.traverse((object) => {
        if (!(object instanceof Mesh)) {
            return;
        }
        const meshId = (object.userData as PayloadMeshUserData).meshId;
        if (!meshId) {
            return;
        }
        applyEvaluatedMesh(object, evalById.get(meshId), textures, prepared.positions.get(meshId));
    });
}

export function clearPayloadModelData(root: Object3D): void {
    const textures = root.userData.payloadTextures as Map<string, Texture> | undefined;
    for (const texture of textures?.values() ?? []) {
        texture.dispose();
    }
    textures?.clear();
    delete root.userData.payloadTextures;
    root.traverse((object) => {
        if (!(object instanceof Mesh) || !object.userData.meshId) {
            return;
        }
        const userData = object.userData as PayloadMeshUserData;
        userData.basePositions = new Float32Array();
        userData.shapeTargets.length = 0;
        userData.positionVariants.length = 0;
        userData.normalCache.length = 0;
        userData.lastMaps = undefined;
        userData.lastPositionVariantIndex = undefined;
    });
}

function disposeIncompletePayloadModel(root: Object3D): void {
    root.traverse((object) => {
        if (!(object instanceof Mesh)) {
            return;
        }
        object.geometry.dispose();
        for (const material of Array.isArray(object.material)
            ? object.material
            : [object.material]) {
            for (const value of Object.values(material)) {
                if (value instanceof Texture) {
                    value.dispose();
                }
            }
            material.dispose();
        }
    });
    clearPayloadModelData(root);
}

function applyEvaluatedMesh(
    object: Mesh,
    evaluated: EvaluatedViewerState["meshes"][number] | undefined,
    textures?: Map<string, Texture>,
    preparedPosition?: { variantIndex: number; positions: Float32Array },
): void {
    if (!evaluated) {
        return;
    }
    object.visible = evaluated.visible;
    if (!evaluated.visible) {
        return;
    }
    const userData = object.userData as PayloadMeshUserData;
    applyPositionVariant(object, userData, evaluated.positionVariantIndex, preparedPosition);
    const material = object.material;
    if (material instanceof MeshStandardMaterial && textures) {
        applyEvaluatedMaps(material, object, userData, evaluated, textures);
    }
    if (evaluated.positionVariantIndex === null) {
        applyShapeTargets(object, evaluated.shapeWeights);
    }
}

function applyEvaluatedMaps(
    material: MeshStandardMaterial,
    object: Mesh,
    userData: PayloadMeshUserData,
    evaluated: EvaluatedViewerState["meshes"][number],
    textures: Map<string, Texture>,
): void {
    if (userData.materialProfile === "zzmi") {
        configurePackedMaterialShader(material);
    } else if (userData.materialProfile === "wuwa:rabbitfx" && object.geometry.attributes.uv) {
        configureRabbitFXMaterialShader(material, userData.toonShadows);
    }
    const last = userData.lastMaps;
    if (
        last &&
        last.texKey === evaluated.texKey &&
        last.normalMapKey === evaluated.normalMapKey &&
        last.lightMapKey === evaluated.lightMapKey &&
        last.materialMapKey === evaluated.materialMapKey
    ) {
        return;
    }
    const hadMap = Boolean(material.map);
    const hadNormalMap = Boolean(material.normalMap);
    const hadMetalnessMap = Boolean(material.metalnessMap);
    const hadRoughnessMap = Boolean(material.roughnessMap);
    material.map = evaluated.texKey ? (textures.get(evaluated.texKey) ?? null) : null;
    const canUseDerivativeTangentFrame =
        userData.materialProfile === "wuwa:rabbitfx" &&
        object.geometry.attributes.normal &&
        object.geometry.attributes.uv;
    material.normalMap =
        evaluated.normalMapKey &&
        (object.geometry.attributes.tangent || canUseDerivativeTangentFrame)
            ? (textures.get(evaluated.normalMapKey) ?? null)
            : null;
    if (material.normalMap) {
        material.normalScale.y = -1;
    }
    // ZZMI's packed maps are not generic Three.js AO/PBR maps. LightMap.G is
    // metallic and MaterialMap.G is glossiness; the shader adapter below
    // performs the required channel selection and glossiness inversion.
    material.aoMap = null;
    material.metalnessMap =
        userData.materialProfile === "zzmi" && evaluated.lightMapKey
            ? (textures.get(evaluated.lightMapKey) ?? null)
            : null;
    material.metalness = material.metalnessMap
        ? 1
        : userData.materialProfile === "wuwa:rabbitfx"
          ? 0
          : 0.05;
    material.roughnessMap =
        userData.materialProfile === "zzmi" && evaluated.materialMapKey
            ? (textures.get(evaluated.materialMapKey) ?? null)
            : null;
    material.roughness = material.roughnessMap
        ? 1
        : userData.materialProfile === "wuwa:rabbitfx"
          ? 1
          : 0.65;
    const rabbitFXState = material.userData.rabbitFXMaterial as RabbitFXShaderState | undefined;
    const rabbitFXLightMap =
        userData.materialProfile === "wuwa:rabbitfx" && evaluated.lightMapKey
            ? (textures.get(evaluated.lightMapKey) ?? null)
            : null;
    if (rabbitFXState) {
        rabbitFXState.lightMap.value = rabbitFXLightMap ?? rabbitFXFallbackLightMap;
        rabbitFXState.lightMapEnabled.value = rabbitFXLightMap ? 1 : 0;
    }
    if (
        hadMap !== Boolean(material.map) ||
        hadNormalMap !== Boolean(material.normalMap) ||
        hadMetalnessMap !== Boolean(material.metalnessMap) ||
        hadRoughnessMap !== Boolean(material.roughnessMap)
    ) {
        material.needsUpdate = true;
    }
    userData.lastMaps = {
        texKey: evaluated.texKey,
        normalMapKey: evaluated.normalMapKey,
        lightMapKey: evaluated.lightMapKey,
        materialMapKey: evaluated.materialMapKey,
    };
}

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
    roughnessFactor *= 1.0 - texelRoughness.g;
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

function configureRabbitFXMaterialShader(
    material: MeshStandardMaterial,
    toonShadows: boolean,
): void {
    if (material.userData.rabbitFXMaterial) {
        return;
    }
    const state: RabbitFXShaderState = {
        lightMap: { value: rabbitFXFallbackLightMap },
        lightMapEnabled: { value: 0 },
        toonEnabled: { value: toonShadows ? 1 : 0 },
    };
    material.userData.rabbitFXMaterial = state;
    material.onBeforeCompile = (shader: WebGLProgramParametersWithUniforms) => {
        shader.uniforms.rabbitFXLightMap = state.lightMap;
        shader.uniforms.rabbitFXLightMapEnabled = state.lightMapEnabled;
        shader.uniforms.rabbitFXToonEnabled = state.toonEnabled;
        shader.vertexShader = replaceRabbitFXShaderAnchor(
            shader.vertexShader,
            "#include <common>",
            `#include <common>
varying vec2 vRabbitFXUv;`,
        );
        shader.vertexShader = replaceRabbitFXShaderAnchor(
            shader.vertexShader,
            "#include <uv_vertex>",
            `#include <uv_vertex>
vRabbitFXUv = uv;`,
        );
        shader.fragmentShader = replaceRabbitFXShaderAnchor(
            shader.fragmentShader,
            "#include <common>",
            `#include <common>
varying vec2 vRabbitFXUv;
uniform sampler2D rabbitFXLightMap;
uniform float rabbitFXLightMapEnabled;
uniform float rabbitFXToonEnabled;

float rabbitFXDirectionalDiffuseFactor( vec3 surfaceNormal, vec3 lightDirection ) {
    if ( rabbitFXLightMapEnabled < 0.5 ) return 1.0;
    float mask = texture2D( rabbitFXLightMap, vRabbitFXUv ).g;
    if ( mask <= 0.01 || mask >= 0.99 ) return 1.0;
    float visibility = step( 0.1, mask );
    float boundary = smoothstep( 0.55, 0.81, dot( surfaceNormal, lightDirection ) + 0.4 );
    return mix( 1.0, boundary * visibility, rabbitFXToonEnabled );
}`,
        );
        shader.fragmentShader = replaceRabbitFXShaderAnchor(
            shader.fragmentShader,
            "#include <lights_fragment_begin>",
            patchRabbitFXDirectionalLights(ShaderChunk.lights_fragment_begin),
        );
    };
    material.customProgramCacheKey = () => "wuwa-rabbitfx-v1";
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
    const section = replaceRabbitFXShaderAnchor(
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
    const patchedSection = replaceRabbitFXShaderAnchor(section, directCall, replacement);
    return source.slice(0, start) + patchedSection + source.slice(end);
}

function replaceRabbitFXShaderAnchor(source: string, anchor: string, replacement: string): string {
    const first = source.indexOf(anchor);
    if (first < 0 || source.indexOf(anchor, first + anchor.length) >= 0) {
        throw new Error(`Three.js shader anchor changed: ${anchor}`);
    }
    return source.slice(0, first) + replacement + source.slice(first + anchor.length);
}

function applyPositionVariant(
    object: Mesh,
    userData: PayloadMeshUserData,
    variantIndex: number | null,
    prepared?: { variantIndex: number; positions: Float32Array },
): void {
    if (userData.lastPositionVariantIndex === variantIndex) {
        return;
    }
    const next =
        variantIndex === null
            ? userData.basePositions
            : prepared?.variantIndex === variantIndex
              ? prepared.positions
              : undefined;
    if (!next || next.length !== userData.basePositions.length) {
        return;
    }
    const position = object.geometry.attributes.position;
    position.array.set(next);
    position.needsUpdate = true;
    userData.lastPositionVariantIndex = variantIndex;

    const cacheKey = variantIndex ?? -1;
    const cached = userData.normalCache.find((entry) => entry.key === cacheKey)?.normal;
    const normal = object.geometry.attributes.normal;
    if (cached && normal) {
        normal.array.set(cached);
        normal.needsUpdate = true;
        return;
    }
    // Animation frames reuse these meshes; cache normals instead of recomputing every tick.
    object.geometry.computeVertexNormals();
    if (normal) {
        userData.normalCache = [
            { key: cacheKey, normal: new Float32Array(normal.array as Float32Array) },
            ...userData.normalCache.filter((entry) => entry.key !== cacheKey),
        ].slice(0, 2);
    }
}

function applyShapeTargets(object: Mesh, weights: Record<string, number>): void {
    const userData = object.userData as PayloadMeshUserData;
    const targets = userData.shapeTargets ?? [];
    if (targets.length === 0) {
        return;
    }
    const attr = object.geometry.attributes.position;
    const base = userData.basePositions;
    attr.array.set(base);
    const midpointTargets = targets.filter((target) => {
        if (target.mode !== "midpoint_pair") {
            return false;
        }
        const weight = Number(weights[target.var] ?? 0);
        const endpoint =
            weight <= 0.5 ? (target.lowPositions ?? target.positions) : target.positions;
        return endpoint.length === base.length;
    });
    const divisor = midpointTargets.length || 1;
    for (const target of targets) {
        const weight = Number(weights[target.var] ?? 0);
        if (!Number.isFinite(weight)) {
            continue;
        }
        if (target.mode === "midpoint_pair") {
            const endpoint =
                weight <= 0.5 ? (target.lowPositions ?? target.positions) : target.positions;
            if (endpoint.length !== base.length) {
                continue;
            }
            const factor = weight <= 0.5 ? 2 - weight * 4 : weight * 4 - 2;
            for (let index = 0; index < attr.array.length; index++) {
                const shaped = base[index] + (endpoint[index] - base[index]) * factor;
                attr.array[index] += (shaped - base[index]) / divisor;
            }
            continue;
        }
        if (weight === 0) {
            continue;
        }
        if (target.positions.length !== base.length) {
            continue;
        }
        for (let index = 0; index < attr.array.length; index++) {
            attr.array[index] += (target.positions[index] - base[index]) * weight;
        }
    }
    attr.needsUpdate = true;
    object.geometry.computeVertexNormals();
    object.geometry.computeBoundingBox();
    object.geometry.computeBoundingSphere();
}

async function buildGeometry(mesh: ViewerMeshTransport): Promise<BufferGeometry> {
    const [positions, normals, tangents, uvs, indices] = await Promise.all([
        fetchFloat32(mesh.positionsUrl),
        mesh.normalsUrl ? fetchFloat32(mesh.normalsUrl) : Promise.resolve(undefined),
        mesh.tangentsUrl ? fetchFloat32(mesh.tangentsUrl) : Promise.resolve(undefined),
        mesh.uvsUrl ? fetchFloat32(mesh.uvsUrl) : Promise.resolve(undefined),
        fetchUint32(mesh.indicesUrl),
    ]);
    const geometry = new BufferGeometry();
    geometry.setAttribute("position", new BufferAttribute(positions, 3));
    if (normals) {
        geometry.setAttribute("normal", new BufferAttribute(normals, 3));
    }
    if (tangents) {
        geometry.setAttribute("tangent", new BufferAttribute(tangents, 4));
    }
    if (uvs) {
        geometry.setAttribute("uv", new BufferAttribute(uvs, 2));
    }
    geometry.setIndex(new BufferAttribute(indices, 1));
    if (!normals) {
        geometry.computeVertexNormals();
    }
    geometry.computeBoundingBox();
    geometry.computeBoundingSphere();
    return geometry;
}

function loadTexture(
    url: string,
    textureCache: Map<string, Promise<Texture | null>>,
): Promise<Texture | null> {
    const cached = textureCache.get(url);
    if (cached) {
        return cached;
    }
    const request = new Promise<Texture | null>((resolve) => {
        textureLoader.load(
            url,
            (texture) => {
                // TextureLoader default flipY=true matches the mesh-builder 1-v UV flip.
                resolve(texture);
            },
            undefined,
            () => resolve(null),
        );
    });
    textureCache.set(url, request);
    return request;
}
