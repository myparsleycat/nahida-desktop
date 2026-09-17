import type { ModelViewerBounds } from "@bindings/tools/model_viewer";
import { fetchBinaryBytes, fetchFloat32 } from "@renderer/wails/binary-memory";
import type {
    EvaluatedViewerState,
    ModViewerTransport,
    ViewerMeshTransport,
} from "@shared/mod-viewer/types";
import {
    Box3,
    Sphere,
    Vector3,
    BufferAttribute,
    BufferGeometry,
    DataTexture,
    DoubleSide,
    Group,
    Mesh,
    MeshStandardMaterial,
    Object3D,
    NoColorSpace,
    RG11_EAC_Format,
    RGFormat,
    RED_GREEN_RGTC2_Format,
    RGBAFormat,
    SRGBColorSpace,
    ShaderChunk,
    Texture,
    TextureLoader,
} from "three";
import type { WebGLProgramParametersWithUniforms } from "three";

import type { ModelViewerTextureCapabilities } from "./model-viewer-dds";
import type { ModelViewerPositionGeometry } from "./model-viewer-position-codec";
import type { PositionVariantLoader } from "./model-viewer-position-loader";

import {
    canUploadModelViewerDDS,
    fetchModelViewerDDSBuffer,
    hasModelViewerDDSMipWithinLimit,
    parseModelViewerDDS,
} from "./model-viewer-dds";
import { decodeModelViewerMesh } from "./model-viewer-mesh-codec";

type PayloadMeshUserData = {
    meshId: string;
    basePositions: Float32Array;
    baseNormals: Float32Array;
    baseBounds?: ModelViewerBounds;
    shapeTargets: Array<{
        var: string;
        positions: Float32Array;
        mode?: "midpoint_pair";
        lowPositions?: Float32Array;
    }>;
    positionVariants: ViewerMeshTransport["positionVariants"];
    sourceIndicesUrl?: string;
    materialProfile?: ModViewerTransport["materialProfile"];
    toonShadows: boolean;
    lastPositionVariantIndex?: number | null;
    lastShapeSignature?: string;
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

type DDSShaderState = {
    invertAlpha: { value: number };
    mapBC4: { value: number };
    mapSigned: { value: number };
    normalMode: { value: number };
    metalnessBC4: { value: number };
    metalnessSigned: { value: number };
    roughnessBC4: { value: number };
    roughnessSigned: { value: number };
    rabbitFXLightBC4: { value: number };
    rabbitFXLightSigned: { value: number };
};

type DDSTextureMetadata = {
    format: string;
    direct: boolean;
};

const textureLoader = new TextureLoader();
const noCompressedTextureCapabilities: ModelViewerTextureCapabilities = {
    maxTextureSize: 0,
    s3tc: false,
    s3tcSRGB: false,
    rgtc: false,
    bptc: false,
};
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
    positions: Map<string, { variantIndex: number } & ModelViewerPositionGeometry>;
};

export async function buildPayloadModel(
    transport: ModViewerTransport,
    evalResult: EvaluatedViewerState,
    doubleSided: boolean,
    positionLoader: PositionVariantLoader,
    toonShadows = false,
    loadSignal?: AbortSignal,
    textureCapabilities: ModelViewerTextureCapabilities = noCompressedTextureCapabilities,
): Promise<Group> {
    const controller = new AbortController();
    const signal = loadSignal
        ? AbortSignal.any([loadSignal, controller.signal])
        : controller.signal;
    signal.throwIfAborted();
    const textureCache = new Map<string, Promise<Texture | null>>();
    const textures = new Map<string, Texture>();
    const group = new Group();
    group.userData.payloadTextures = textures;
    const built: Mesh[] = [];
    const objects = new Array<Mesh>(transport.meshes.length);

    // Bound active work while overlapping image decoding with geometry transfers.
    const jobs = [
        loadItems(transport.meshes, 8, async (mesh, index) => {
            objects[index] = await buildPayloadMesh(
                mesh,
                transport.materialProfile,
                doubleSided,
                toonShadows,
                built,
                signal,
            );
        }),
        loadItems(Object.entries(transport.textures), 8, async ([key, entry]) => {
            const texture = await loadTexture(entry, textureCache, signal, textureCapabilities);
            if (signal.aborted) {
                texture?.dispose();
                signal.throwIfAborted();
            }
            if (texture) {
                texture.colorSpace = entry.role === "diffuse" ? SRGBColorSpace : NoColorSpace;
                textures.set(key, texture);
            }
        }),
    ];
    try {
        await Promise.all(jobs);
        signal.throwIfAborted();
        for (const object of objects) {
            group.add(object);
        }
        const prepared = await preparePayloadEval(group, evalResult, positionLoader, signal);
        signal.throwIfAborted();
        commitPayloadEval(group, prepared);
        return group;
    } catch (error) {
        controller.abort(error);

        // Let active jobs settle before disposing resources they may still own.
        await Promise.allSettled(jobs);
        for (const object of built) {
            if (!object.parent) {
                disposeMeshObject(object);
            }
        }
        disposeIncompletePayloadModel(group);
        throw error;
    }

    async function loadItems<T>(
        items: readonly T[],
        concurrency: number,
        load: (item: T, index: number) => Promise<void>,
    ): Promise<void> {
        let next = 0;
        await Promise.all(
            Array.from({ length: Math.min(concurrency, items.length) }, async () => {
                try {
                    while (next < items.length) {
                        signal.throwIfAborted();
                        const index = next++;
                        await load(items[index], index);
                    }
                } catch (error) {
                    controller.abort(error);
                }
            }),
        );
        signal.throwIfAborted();
    }
}

async function buildPayloadMesh(
    mesh: ViewerMeshTransport,
    materialProfile: ModViewerTransport["materialProfile"],
    doubleSided: boolean,
    toonShadows: boolean,
    built: Mesh[],
    signal: AbortSignal,
): Promise<Mesh> {
    const geometry = await buildGeometry(mesh, signal);
    const rabbitFX = materialProfile === "wuwa:rabbitfx";
    const material = new MeshStandardMaterial({
        color: 0xffffff,
        metalness: rabbitFX ? 0 : 0.05,
        roughness: rabbitFX ? 1 : 0.65,
        side: doubleSided ? DoubleSide : undefined,
    });
    const object = new Mesh(geometry, material);
    object.name = mesh.id;
    built.push(object);
    const shapeTargets: PayloadMeshUserData["shapeTargets"] = [];

    // Keep a mesh with many shape targets from bypassing the geometry work limit.
    for (const target of mesh.shapeTargets) {
        signal.throwIfAborted();
        const [positions, lowPositions] = await Promise.all([
            fetchFloat32(target.positionsUrl, undefined, signal),
            target.lowPositionsUrl
                ? fetchFloat32(target.lowPositionsUrl, undefined, signal)
                : undefined,
        ]);
        shapeTargets.push({ var: target.var, positions, mode: target.mode, lowPositions });
    }
    object.userData = {
        meshId: mesh.id,
        basePositions: new Float32Array(geometry.attributes.position.array as Float32Array),
        baseNormals: new Float32Array(geometry.attributes.normal.array as Float32Array),
        baseBounds: mesh.bounds,
        lastPositionVariantIndex: null,
        shapeTargets,
        positionVariants: [...mesh.positionVariants],
        sourceIndicesUrl: mesh.sourceIndicesUrl,
        materialProfile,
        toonShadows,
    } satisfies PayloadMeshUserData;
    return object;
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
    const requests: Array<
        Promise<[string, { variantIndex: number } & ModelViewerPositionGeometry]>
    > = [];
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
                .load(descriptor, userData.basePositions.length / 3, signal)
                .then((geometry) => [userData.meshId, { variantIndex, ...geometry }]),
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
        userData.baseNormals = new Float32Array();
        userData.baseBounds = undefined;
        userData.lastMaps = undefined;
        userData.lastPositionVariantIndex = undefined;
        userData.lastShapeSignature = undefined;
    });
}

function disposeIncompletePayloadModel(root: Object3D): void {
    root.traverse((object) => {
        if (!(object instanceof Mesh)) {
            return;
        }
        disposeMeshObject(object);
    });
    clearPayloadModelData(root);
}

function disposeMeshObject(object: Mesh): void {
    object.geometry.dispose();
    for (const material of Array.isArray(object.material) ? object.material : [object.material]) {
        for (const value of Object.values(material)) {
            if (value instanceof Texture) {
                value.dispose();
            }
        }
        material.dispose();
    }
}

function applyEvaluatedMesh(
    object: Mesh,
    evaluated: EvaluatedViewerState["meshes"][number] | undefined,
    textures?: Map<string, Texture>,
    preparedPosition?: { variantIndex: number } & ModelViewerPositionGeometry,
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
    configureDDSMaterialShader(material);
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
    const hadPackedNormalMap = usesPackedNormalMap(material.normalMap);
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
    updateDDSShaderState(material, userData.materialProfile);
    if (
        hadMap !== Boolean(material.map) ||
        hadNormalMap !== Boolean(material.normalMap) ||
        hadPackedNormalMap !== usesPackedNormalMap(material.normalMap) ||
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

function configureDDSMaterialShader(material: MeshStandardMaterial): void {
    if (material.userData.modelViewerDDSMaterial) return;
    const state: DDSShaderState = {
        invertAlpha: { value: 0 },
        mapBC4: { value: 0 },
        mapSigned: { value: 0 },
        normalMode: { value: 0 },
        metalnessBC4: { value: 0 },
        metalnessSigned: { value: 0 },
        roughnessBC4: { value: 0 },
        roughnessSigned: { value: 0 },
        rabbitFXLightBC4: { value: 0 },
        rabbitFXLightSigned: { value: 0 },
    };
    material.userData.modelViewerDDSMaterial = state;
    const previousCompile = material.onBeforeCompile.bind(material);
    const previousCacheKey = material.customProgramCacheKey.bind(material);
    material.onBeforeCompile = (shader: WebGLProgramParametersWithUniforms, renderer) => {
        previousCompile(shader, renderer);
        shader.uniforms.modelViewerInvertAlpha = state.invertAlpha;
        shader.uniforms.modelViewerMapBC4 = state.mapBC4;
        shader.uniforms.modelViewerMapSigned = state.mapSigned;
        shader.uniforms.modelViewerNormalMode = state.normalMode;
        shader.uniforms.modelViewerMetalnessBC4 = state.metalnessBC4;
        shader.uniforms.modelViewerMetalnessSigned = state.metalnessSigned;
        shader.uniforms.modelViewerRoughnessBC4 = state.roughnessBC4;
        shader.uniforms.modelViewerRoughnessSigned = state.roughnessSigned;
        shader.uniforms.modelViewerRabbitFXLightBC4 = state.rabbitFXLightBC4;
        shader.uniforms.modelViewerRabbitFXLightSigned = state.rabbitFXLightSigned;
        shader.fragmentShader = shader.fragmentShader.replace(
            "#include <common>",
            `#include <common>
uniform float modelViewerInvertAlpha;
uniform float modelViewerMapBC4;
uniform float modelViewerMapSigned;
uniform float modelViewerNormalMode;
uniform float modelViewerMetalnessBC4;
uniform float modelViewerMetalnessSigned;
uniform float modelViewerRoughnessBC4;
uniform float modelViewerRoughnessSigned;
uniform float modelViewerRabbitFXLightBC4;
uniform float modelViewerRabbitFXLightSigned;`,
        );
        shader.fragmentShader = shader.fragmentShader.replace(
            "#include <map_fragment>",
            `#ifdef USE_MAP
    vec4 sampledDiffuseColor = texture2D( map, vMapUv );
    if ( modelViewerMapSigned > 0.5 ) sampledDiffuseColor = sampledDiffuseColor * 0.5 + 0.5;
    if ( modelViewerMapBC4 > 0.5 ) sampledDiffuseColor = vec4( sampledDiffuseColor.rrr, 1.0 );
    if ( modelViewerInvertAlpha > 0.5 ) sampledDiffuseColor.a = 1.0 - sampledDiffuseColor.a;
    diffuseColor *= sampledDiffuseColor;
#endif`,
        );
        const normalChunk = ShaderChunk.normal_fragment_maps.replace(
            "vec3 mapN = texture2D( normalMap, vNormalMapUv ).xyz * 2.0 - 1.0;",
            `vec3 modelViewerNormalTexel = texture2D( normalMap, vNormalMapUv ).xyz;
    vec3 mapN = modelViewerNormalTexel * 2.0 - 1.0;
    if ( modelViewerNormalMode > 1.5 && modelViewerNormalMode < 2.5 ) mapN = modelViewerNormalTexel;
    if ( modelViewerNormalMode > 2.5 && modelViewerNormalMode < 3.5 ) mapN.xy = vec2( mapN.x );
    if ( modelViewerNormalMode > 3.5 ) mapN.xy = vec2( modelViewerNormalTexel.x );
    if ( modelViewerNormalMode > 0.5 ) {
        mapN.z = sqrt( max( 0.0, 1.0 - dot( mapN.xy, mapN.xy ) ) );
    }`,
        );
        shader.fragmentShader = shader.fragmentShader
            .replace("#include <normal_fragment_maps>", normalChunk)
            .replace(
                "roughnessFactor *= 1.0 - texelRoughness.g;",
                `float modelViewerRoughnessSample = mix( texelRoughness.g, texelRoughness.r, modelViewerRoughnessBC4 );
    if ( modelViewerRoughnessSigned > 0.5 ) modelViewerRoughnessSample = modelViewerRoughnessSample * 0.5 + 0.5;
    roughnessFactor *= 1.0 - modelViewerRoughnessSample;`,
            )
            .replace(
                "metalnessFactor *= texelMetalness.g;",
                `float modelViewerMetalnessSample = mix( texelMetalness.g, texelMetalness.r, modelViewerMetalnessBC4 );
    if ( modelViewerMetalnessSigned > 0.5 ) modelViewerMetalnessSample = modelViewerMetalnessSample * 0.5 + 0.5;
    metalnessFactor *= modelViewerMetalnessSample;`,
            )
            .replace(
                "float mask = texture2D( rabbitFXLightMap, vRabbitFXUv ).g;",
                `vec4 modelViewerRabbitFXLightSample = texture2D( rabbitFXLightMap, vRabbitFXUv );
    float mask = mix( modelViewerRabbitFXLightSample.g, modelViewerRabbitFXLightSample.r, modelViewerRabbitFXLightBC4 );
    if ( modelViewerRabbitFXLightSigned > 0.5 ) mask = mask * 0.5 + 0.5;`,
            );
    };
    material.customProgramCacheKey = () => `${previousCacheKey()}|model-viewer-dds-v1`;
    material.needsUpdate = true;
}

function updateDDSShaderState(
    material: MeshStandardMaterial,
    materialProfile: ModViewerTransport["materialProfile"],
): void {
    const state = material.userData.modelViewerDDSMaterial as DDSShaderState;
    const map = ddsTextureMetadata(material.map);
    const normal = ddsTextureMetadata(material.normalMap);
    const metalness = ddsTextureMetadata(material.metalnessMap);
    const roughness = ddsTextureMetadata(material.roughnessMap);
    const rabbitFX = material.userData.rabbitFXMaterial as RabbitFXShaderState | undefined;
    const rabbitFXMap = ddsTextureMetadata(rabbitFX?.lightMap.value);
    state.invertAlpha.value = material.map?.userData.modelViewerInvertAlpha ? 1 : 0;
    state.mapBC4.value = map?.direct && map.format.startsWith("bc4-") ? 1 : 0;
    state.mapSigned.value = map?.direct && map.format.endsWith("snorm") ? 1 : 0;
    state.normalMode.value = normalMode(normal, materialProfile);
    state.metalnessBC4.value = metalness?.direct && metalness.format.startsWith("bc4-") ? 1 : 0;
    state.metalnessSigned.value = metalness?.direct && metalness.format.endsWith("snorm") ? 1 : 0;
    state.roughnessBC4.value = roughness?.direct && roughness.format.startsWith("bc4-") ? 1 : 0;
    state.roughnessSigned.value = roughness?.direct && roughness.format.endsWith("snorm") ? 1 : 0;
    state.rabbitFXLightBC4.value =
        rabbitFXMap?.direct && rabbitFXMap.format.startsWith("bc4-") ? 1 : 0;
    state.rabbitFXLightSigned.value =
        rabbitFXMap?.direct && rabbitFXMap.format.endsWith("snorm") ? 1 : 0;
}

function ddsTextureMetadata(texture: Texture | null | undefined): DDSTextureMetadata | undefined {
    return texture?.userData.modelViewerDDS as DDSTextureMetadata | undefined;
}

function usesPackedNormalMap(texture: Texture | null | undefined): boolean {
    return (
        texture?.format === RGFormat ||
        texture?.format === RG11_EAC_Format ||
        texture?.format === RED_GREEN_RGTC2_Format
    );
}

function normalMode(
    metadata: DDSTextureMetadata | undefined,
    materialProfile: ModViewerTransport["materialProfile"],
): number {
    if (!metadata || (materialProfile !== "zzmi" && materialProfile !== "wuwa:rabbitfx")) return 0;
    const bc4 = metadata.format.startsWith("bc4-");
    const signed = metadata.direct && metadata.format.endsWith("snorm");
    if (bc4) return signed ? 4 : 3;
    return signed ? 2 : 1;
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
    prepared?: { variantIndex: number } & ModelViewerPositionGeometry,
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
    userData.lastShapeSignature = undefined;
    if (variantIndex === null && userData.shapeTargets.length > 0) {
        return;
    }

    const normals = variantIndex === null ? userData.baseNormals : prepared?.normals;
    const bounds = variantIndex === null ? userData.baseBounds : prepared?.bounds;
    if (normals) {
        object.geometry.attributes.normal.array.set(normals);
        object.geometry.attributes.normal.needsUpdate = true;
    }
    if (bounds) applyGeometryBounds(object.geometry, bounds);
}

function applyShapeTargets(object: Mesh, weights: Record<string, number>): void {
    const userData = object.userData as PayloadMeshUserData;
    const targets = userData.shapeTargets ?? [];
    if (targets.length === 0) {
        return;
    }
    const signature = targets
        .map((target) => `${target.var}:${normalizeShapeWeight(weights[target.var])}`)
        .join("|");
    if (userData.lastShapeSignature === signature) {
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
    let deformed = false;
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
            if (factor === 0) continue;
            deformed = true;
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
        deformed = true;
        for (let index = 0; index < attr.array.length; index++) {
            attr.array[index] += (target.positions[index] - base[index]) * weight;
        }
    }
    attr.needsUpdate = true;
    if (!deformed) {
        object.geometry.attributes.normal.array.set(userData.baseNormals);
        object.geometry.attributes.normal.needsUpdate = true;
        if (userData.baseBounds) applyGeometryBounds(object.geometry, userData.baseBounds);
        userData.lastShapeSignature = signature;
        return;
    }
    object.geometry.computeVertexNormals();
    object.geometry.computeBoundingBox();
    object.geometry.computeBoundingSphere();
    userData.lastShapeSignature = signature;
}

function normalizeShapeWeight(value: number | undefined): string {
    const numeric = Number(value ?? 0);
    if (!Number.isFinite(numeric)) return "invalid";
    return String(Object.is(numeric, -0) ? 0 : numeric);
}

async function buildGeometry(
    mesh: ViewerMeshTransport,
    signal: AbortSignal,
): Promise<BufferGeometry> {
    const bytes = await fetchBinaryBytes(mesh.geometryUrl, undefined, signal);
    signal.throwIfAborted();
    const { positions, normals, tangents, uvs, indices } = decodeModelViewerMesh(
        bytes.buffer as ArrayBuffer,
    );
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
    if (mesh.bounds) applyGeometryBounds(geometry, mesh.bounds);
    else {
        geometry.computeBoundingBox();
        geometry.computeBoundingSphere();
    }
    return geometry;
}

function loadTexture(
    entry: ModViewerTransport["textures"][string],
    textureCache: Map<string, Promise<Texture | null>>,
    signal: AbortSignal,
    capabilities: ModelViewerTextureCapabilities,
): Promise<Texture | null> {
    const cacheKey = [
        entry.url,
        entry.fallbackUrl ?? "",
        entry.role,
        entry.encoding,
        entry.format ?? "",
        entry.invertAlpha ? "invert" : "",
    ].join("|");
    const cached = textureCache.get(cacheKey);
    if (cached) {
        return cached;
    }
    const request = loadPayloadTexture(entry, signal, capabilities).catch(() => {
        signal.throwIfAborted();
        return null;
    });
    textureCache.set(cacheKey, request);
    return request;
}

async function loadPayloadTexture(
    entry: ModViewerTransport["textures"][string],
    signal: AbortSignal,
    capabilities: ModelViewerTextureCapabilities,
): Promise<Texture | null> {
    if (
        entry.encoding === "dds" &&
        entry.format &&
        entry.width &&
        entry.height &&
        entry.mipCount &&
        canUploadModelViewerDDS(entry.format, entry.role, capabilities) &&
        hasModelViewerDDSMipWithinLimit(
            entry.width,
            entry.height,
            entry.mipCount,
            capabilities.maxTextureSize,
        )
    ) {
        let directTexture: Texture | undefined;
        try {
            const parsed = parseModelViewerDDS(
                await fetchModelViewerDDSBuffer(
                    entry.url,
                    entry.format,
                    capabilities.maxTextureSize,
                    signal,
                ),
                entry.format,
                capabilities.maxTextureSize,
            );
            directTexture = parsed.texture;
            signal.throwIfAborted();
            parsed.texture.userData.modelViewerDDS = { format: parsed.format, direct: true };
            parsed.texture.userData.modelViewerInvertAlpha = Boolean(entry.invertAlpha);
            return parsed.texture;
        } catch {
            directTexture?.dispose();
            signal.throwIfAborted();
        }
    }

    const url = entry.encoding === "dds" ? entry.fallbackUrl : entry.url;
    if (!url) return null;
    const texture = await loadPayloadImageTexture(url, signal);
    if (texture && entry.encoding === "dds") {
        texture.userData.modelViewerDDS = { format: entry.format ?? "", direct: false };
        texture.userData.modelViewerInvertAlpha = Boolean(entry.invertAlpha);
    }
    return texture;
}

async function loadPayloadImageTexture(url: string, signal: AbortSignal): Promise<Texture | null> {
    const response = await fetch(url, { signal, cache: "no-store" });
    if (!response.ok) return null;
    const blob = await response.blob();
    signal.throwIfAborted();
    const objectUrl = URL.createObjectURL(blob);
    let onAbort: (() => void) | undefined;
    try {
        return await new Promise<Texture>((resolve, reject) => {
            onAbort = () => reject(signal.reason);
            signal.addEventListener("abort", onAbort, { once: true });
            textureLoader.load(
                objectUrl,
                (texture) => {
                    // Image decoding may finish after cancellation; it must not retain a texture.
                    if (signal.aborted) {
                        texture.dispose();
                        reject(signal.reason);
                        return;
                    }

                    // TextureLoader's default flipY matches the mesh-builder 1-v UV flip.
                    resolve(texture);
                },
                undefined,
                reject,
            );
        });
    } finally {
        if (onAbort) signal.removeEventListener("abort", onAbort);
        URL.revokeObjectURL(objectUrl);
    }
}

function applyGeometryBounds(geometry: BufferGeometry, bounds: ModelViewerBounds): void {
    geometry.boundingBox ??= new Box3();
    geometry.boundingBox.min.fromArray(bounds.min);
    geometry.boundingBox.max.fromArray(bounds.max);
    geometry.boundingSphere ??= new Sphere(new Vector3());
    geometry.boundingSphere.center.fromArray(bounds.center);
    geometry.boundingSphere.radius = bounds.radius;
}
