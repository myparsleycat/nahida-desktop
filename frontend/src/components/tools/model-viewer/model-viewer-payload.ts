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
    BackSide,
    BufferAttribute,
    BufferGeometry,
    DataTexture,
    DoubleSide,
    Group,
    Matrix3,
    Mesh,
    MeshStandardMaterial,
    Object3D,
    NoColorSpace,
    RG11_EAC_Format,
    RGFormat,
    RED_GREEN_RGTC2_Format,
    RepeatWrapping,
    RGBAFormat,
    SRGBColorSpace,
    ShaderChunk,
    ShaderMaterial,
    Texture,
    UniformsLib,
    UniformsUtils,
    Vector2,
} from "three";
import type { WebGLProgramParametersWithUniforms } from "three";

import type { ModelViewerToonStyle } from "./model-viewer-contract";
import type { ModelViewerTextureCapabilities } from "./model-viewer-dds";
import type { ModelViewerPositionGeometry } from "./model-viewer-position-codec";
import type { PositionVariantLoader } from "./model-viewer-position-loader";

import { DEFAULT_MODEL_VIEWER_TOON_STYLE } from "./model-viewer-contract";
import {
    canUploadModelViewerDDS,
    fetchModelViewerDDSBuffer,
    hasModelViewerDDSMipWithinLimit,
    parseModelViewerDDS,
} from "./model-viewer-dds";
import {
    configureGenshinToonMaterialShader,
    markGenshinMeshes,
    setGenshinToonLightMap,
    syncGenshinToon,
    TOON_VERTEX_COLOR_ATTRIBUTE,
    type GenshinToonMeshData,
    type GenshinToonSettings,
} from "./model-viewer-genshin-toon";
import { decodeModelViewerMesh } from "./model-viewer-mesh-codec";
import {
    averagedNormals,
    replaceShaderAnchor,
    textureUvTransform,
} from "./model-viewer-shader-utils";

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
    toonStyle?: ModelViewerToonStyle;
    efmiHeadMesh?: boolean;
    efmiOverlayShadow?: boolean;
    efmiEyeMesh?: boolean;
    toonOutline?: Mesh<BufferGeometry, ShaderMaterial>;
    highlightGameTextures?: boolean;
    normalMapsDisabled?: boolean;
    lastPositionVariantIndex?: number | null;
    lastShapeSignature?: string;
    geometryStale?: boolean;
    tangentFrameUsable?: boolean;
    tangentFrameAttributes?: {
        normal: BufferGeometry["attributes"][string] | undefined;
        normalVersion: number | undefined;
        tangent: BufferGeometry["attributes"][string] | undefined;
        tangentVersion: number | undefined;
    };
    lastMaps?: {
        texKey: string | null;
        normalMapKey: string | null;
        lightMapKey: string | null;
        materialMapKey: string | null;
        cutoutMapKey: string | null;
        highlightMapKey?: string | null;
        rampMapKey?: string | null;
    };
} & GenshinToonMeshData;

type RabbitFXShaderState = {
    lightMap: { value: Texture };
    lightMapEnabled: { value: number };
    lightMapTransform: { value: Matrix3 };
    cutoutMap: { value: Texture };
    cutoutEnabled: { value: number };
    cutoutTransform: { value: Matrix3 };
    toonEnabled: { value: number };
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

type DDSShaderState = {
    invertAlpha: { value: number };
    coverageMask: { value: number };
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

const noCompressedTextureCapabilities: ModelViewerTextureCapabilities = {
    maxTextureSize: 0,
    s3tc: false,
    s3tcSRGB: false,
    rgtc: false,
    bptc: false,
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

const rabbitFXFallbackLightMap = new DataTexture(
    new Uint8Array([255, 255, 255, 255]),
    1,
    1,
    RGBAFormat,
);
rabbitFXFallbackLightMap.colorSpace = NoColorSpace;
rabbitFXFallbackLightMap.needsUpdate = true;

const GAME_TEXTURE_HIGHLIGHT = 0xf59e0b;

// Eight workers overlap transfers and decoding without dispatching every payload request at once.
export const MODEL_PAYLOAD_LOAD_CONCURRENCY = 8;

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
    prepareTexture: (texture: Texture) => Texture = (texture) => texture,
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
        loadItems(transport.meshes, MODEL_PAYLOAD_LOAD_CONCURRENCY, async (mesh, index) => {
            objects[index] = await buildPayloadMesh(
                mesh,
                transport.materialProfile,
                doubleSided,
                toonShadows,
                built,
                signal,
            );
        }),
        loadItems(
            Object.entries(transport.textures),
            MODEL_PAYLOAD_LOAD_CONCURRENCY,
            async ([key, entry]) => {
                const texture = await loadTexture(entry, textureCache, signal, textureCapabilities);
                if (signal.aborted) {
                    texture?.dispose();
                    if (texture) releaseTextureImage(texture);
                    signal.throwIfAborted();
                }
                if (texture) {
                    texture.colorSpace = entry.role === "diffuse" ? SRGBColorSpace : NoColorSpace;
                    texture.wrapS = RepeatWrapping;
                    texture.wrapT = RepeatWrapping;
                    textures.set(key, prepareTexture(texture));
                }
            },
        ),
    ];
    try {
        await Promise.all(jobs);
        signal.throwIfAborted();
        for (const object of objects) {
            group.add(object);
        }
        if (transport.materialProfile === "efmi") {
            markEFMIHeadMeshes(transport, objects);
        } else if (transport.materialProfile === "gimi") {
            markGenshinMeshes(transport, objects);
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
        syncEFMIToon(object, material, userData);
        syncGenshinToon(object, material, userData);
    });
}

export function setPayloadGIMIToon(root: Object3D, settings: GenshinToonSettings): void {
    root.traverse((object) => {
        if (!(object instanceof Mesh) || !object.userData.meshId) {
            return;
        }
        const userData = object.userData as PayloadMeshUserData;
        userData.genshinToon = settings;
        if (object.material instanceof MeshStandardMaterial) {
            syncGenshinToon(object, object.material, userData);
        }
    });
}

export function setPayloadToonStyle(root: Object3D, style: ModelViewerToonStyle): void {
    root.traverse((object) => {
        if (!(object instanceof Mesh) || !object.userData.meshId) {
            return;
        }
        const userData = object.userData as PayloadMeshUserData;
        userData.toonStyle = style;
        if (object.material instanceof MeshStandardMaterial) {
            syncEFMIToon(object, object.material, userData);
        }
    });
}

export function setPayloadNormalMaps(root: Object3D, enabled: boolean): void {
    const textures = root.userData.payloadTextures as Map<string, Texture> | undefined;
    root.traverse((object) => {
        if (!(object instanceof Mesh) || !object.userData.meshId) {
            return;
        }
        const userData = object.userData as PayloadMeshUserData;
        userData.normalMapsDisabled = !enabled;
        if (object.material instanceof MeshStandardMaterial && textures && userData.lastMaps) {
            applyEvaluatedMaps(object.material, object, userData, userData.lastMaps, textures);
        }
    });
}

/**
 * Tints the meshes the mod binds no diffuse texture to. The game draws them
 * with its own textures, which the viewer cannot read.
 */
export function setPayloadGameTextureHighlight(root: Object3D, enabled: boolean): void {
    root.traverse((object) => {
        if (!(object instanceof Mesh) || !object.userData.meshId) {
            return;
        }
        const userData = object.userData as PayloadMeshUserData;
        userData.highlightGameTextures = enabled;
        if (object.material instanceof MeshStandardMaterial && userData.lastMaps) {
            applyGameTextureHighlight(object.material, userData, userData.lastMaps.texKey);
        }
    });
}

function applyGameTextureHighlight(
    material: MeshStandardMaterial,
    userData: PayloadMeshUserData,
    texKey: string | null,
): void {
    material.color.set(
        userData.highlightGameTextures && !texKey ? GAME_TEXTURE_HIGHLIGHT : 0xffffff,
    );
    material.visible = !userData.efmiOverlayShadow || Boolean(userData.highlightGameTextures);
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

export type CommitPayloadEvalOptions = {
    // Skip normal and bounds recomputation after a shape deformation. The caller
    // owns a follow-up finalizePayloadGeometry once interaction settles.
    deferGeometryUpdates?: boolean;
};

export function commitPayloadEval(
    root: Object3D,
    prepared: PreparedPayloadEval,
    options?: CommitPayloadEvalOptions,
): void {
    const textures = root.userData.payloadTextures as Map<string, Texture> | undefined;
    const evalById = new Map(prepared.evalResult.meshes.map((mesh) => [mesh.id, mesh]));
    const deferGeometryUpdates = options?.deferGeometryUpdates ?? false;
    root.traverse((object) => {
        if (!(object instanceof Mesh)) {
            return;
        }
        const meshId = (object.userData as PayloadMeshUserData).meshId;
        if (!meshId) {
            return;
        }
        applyEvaluatedMesh(
            object,
            evalById.get(meshId),
            textures,
            prepared.positions.get(meshId),
            deferGeometryUpdates,
        );
    });
}

export function finalizePayloadGeometry(root: Object3D): boolean {
    const textures = root.userData.payloadTextures as Map<string, Texture> | undefined;
    let changed = false;
    root.traverse((object) => {
        if (!(object instanceof Mesh) || !object.userData.meshId) {
            return;
        }
        const userData = object.userData as PayloadMeshUserData;
        if (!userData.geometryStale) {
            return;
        }
        refreshDeformedGeometry(object.geometry);
        userData.geometryStale = false;
        if (object.material instanceof MeshStandardMaterial && textures && userData.lastMaps) {
            applyEvaluatedMaps(object.material, object, userData, userData.lastMaps, textures);
        }
        changed = true;
    });
    return changed;
}

export function clearPayloadModelData(root: Object3D): void {
    const textures = root.userData.payloadTextures as Map<string, Texture> | undefined;
    for (const texture of new Set(textures?.values())) {
        texture.dispose();
        releaseTextureImage(texture);
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
        userData.geometryStale = undefined;
        userData.tangentFrameUsable = undefined;
        userData.tangentFrameAttributes = undefined;
    });
}

// A decoded ImageBitmap keeps its pixels alive until closed, independent of
// the GPU texture that three.js releases on dispose.
function releaseTextureImage(texture: Texture): void {
    const image: unknown = texture.image;
    if (
        image &&
        typeof image === "object" &&
        "close" in image &&
        typeof image.close === "function"
    ) {
        image.close();
    }
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
    deferGeometryUpdates = false,
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
    if (evaluated.positionVariantIndex === null) {
        applyShapeTargets(object, evaluated.shapeWeights, deferGeometryUpdates);
    }
    const material = object.material;
    if (material instanceof MeshStandardMaterial && textures) {
        applyEvaluatedMaps(material, object, userData, evaluated, textures);
    }
}

function applyEvaluatedMaps(
    material: MeshStandardMaterial,
    object: Mesh,
    userData: PayloadMeshUserData,
    evaluated: NonNullable<PayloadMeshUserData["lastMaps"]>,
    textures: Map<string, Texture>,
): void {
    if (userData.materialProfile === "zzmi") {
        configurePackedMaterialShader(material);
    } else if (userData.materialProfile === "wuwa:rabbitfx" && object.geometry.attributes.uv) {
        configureRabbitFXMaterialShader(material, userData.toonShadows);
    } else if (userData.materialProfile === "efmi") {
        configureEFMIToonMaterialShader(material);
    } else if (userData.materialProfile === "gimi") {
        configureGenshinToonMaterialShader(material);
    }
    configureDDSMaterialShader(material);
    const normal = object.geometry.attributes.normal;
    const tangent = object.geometry.attributes.tangent;
    const normalVersion = normal instanceof BufferAttribute ? normal.version : normal?.data.version;
    const tangentVersion =
        tangent instanceof BufferAttribute ? tangent.version : tangent?.data.version;
    const frame = userData.tangentFrameAttributes;
    if (
        !frame ||
        frame.normal !== normal ||
        frame.normalVersion !== normalVersion ||
        frame.tangent !== tangent ||
        frame.tangentVersion !== tangentVersion
    ) {
        userData.tangentFrameUsable = undefined;
        userData.tangentFrameAttributes = { normal, normalVersion, tangent, tangentVersion };
    }
    userData.tangentFrameUsable ??= hasUsableTangentFrame(object.geometry);
    const canUseDerivativeTangentFrame =
        (userData.materialProfile === "wuwa:rabbitfx" || userData.materialProfile === "efmi") &&
        normal &&
        object.geometry.attributes.uv;
    const normalMap =
        !userData.normalMapsDisabled &&
        evaluated.normalMapKey &&
        (userData.tangentFrameUsable || canUseDerivativeTangentFrame)
            ? (textures.get(evaluated.normalMapKey) ?? null)
            : null;
    const last = userData.lastMaps;
    if (
        last &&
        last.texKey === evaluated.texKey &&
        last.normalMapKey === evaluated.normalMapKey &&
        last.lightMapKey === evaluated.lightMapKey &&
        last.materialMapKey === evaluated.materialMapKey &&
        last.cutoutMapKey === evaluated.cutoutMapKey &&
        last.highlightMapKey === evaluated.highlightMapKey &&
        last.rampMapKey === evaluated.rampMapKey &&
        material.normalMap === normalMap
    ) {
        return;
    }
    const hadMap = Boolean(material.map);
    const hadNormalMap = Boolean(material.normalMap);
    const hadPackedNormalMap = usesPackedNormalMap(material.normalMap);
    const hadMetalnessMap = Boolean(material.metalnessMap);
    const hadRoughnessMap = Boolean(material.roughnessMap);
    const wasTransparent = material.transparent;
    material.map = evaluated.texKey ? (textures.get(evaluated.texKey) ?? null) : null;
    applyGameTextureHighlight(material, userData, evaluated.texKey);
    material.normalMap = normalMap;
    if (material.normalMap) {
        material.normalScale.y = -1;
    }
    // ZZMI's packed maps are not generic Three.js AO/PBR maps. LightMap.G is
    // metallic and MaterialMap.G is roughness, not glossiness: game textures
    // store it highest on cloth and hair, lower on skin, and lowest on metal.
    // The shader adapter below performs the required channel selection.
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
        rabbitFXState.lightMapTransform.value = textureUvTransform(rabbitFXState.lightMap.value);
        const cutoutMap = evaluated.cutoutMapKey ? textures.get(evaluated.cutoutMapKey) : undefined;
        rabbitFXState.cutoutMap.value = cutoutMap ?? rabbitFXFallbackLightMap;
        rabbitFXState.cutoutEnabled.value = cutoutMap ? 1 : 0;
        rabbitFXState.cutoutTransform.value = textureUvTransform(rabbitFXState.cutoutMap.value);
    }
    const efmiToonState = material.userData.efmiToonMaterial as EFMIToonShaderState | undefined;
    if (efmiToonState) {
        const lightMap = evaluated.lightMapKey ? textures.get(evaluated.lightMapKey) : undefined;

        // One- and two-channel containers have no mask or smoothness to read.
        const format = ddsTextureMetadata(lightMap)?.format ?? "";
        const outlineMask = Boolean(lightMap?.userData.modelViewerOutlineMask);
        const usable =
            lightMap && !outlineMask && !format.startsWith("bc4-") && !format.startsWith("bc5-");
        efmiToonState.lightMap.value = usable ? lightMap : rabbitFXFallbackLightMap;
        efmiToonState.lightMapEnabled.value = usable ? 1 : 0;
        efmiToonState.lightMapTransform.value = textureUvTransform(efmiToonState.lightMap.value);
        efmiToonState.outlineMask = outlineMask && lightMap ? lightMap : null;

        const faceMask = evaluated.materialMapKey
            ? textures.get(evaluated.materialMapKey)
            : undefined;
        efmiToonState.faceMask.value = faceMask ?? rabbitFXFallbackLightMap;
        efmiToonState.faceMaskEnabled.value = faceMask ? 1 : 0;
        efmiToonState.faceMaskTransform.value = textureUvTransform(efmiToonState.faceMask.value);
        const highlightMap = evaluated.highlightMapKey
            ? textures.get(evaluated.highlightMapKey)
            : undefined;
        efmiToonState.highlightMap.value = highlightMap ?? rabbitFXFallbackLightMap;
        efmiToonState.highlightEnabled.value = highlightMap ? 1 : 0;
        efmiToonState.highlightTransform.value = textureUvTransform(
            efmiToonState.highlightMap.value,
        );
        const rampMap = evaluated.rampMapKey ? textures.get(evaluated.rampMapKey) : undefined;
        efmiToonState.rampMap.value = rampMap ?? rabbitFXFallbackLightMap;
        efmiToonState.rampEnabled.value = rampMap ? 1 : 0;
    }
    if (userData.materialProfile === "gimi") {
        // One- and two-channel containers have no material ID or specular mask to read.
        const lightMap = evaluated.lightMapKey ? textures.get(evaluated.lightMapKey) : undefined;
        const format = ddsTextureMetadata(lightMap)?.format ?? "";
        setGenshinToonLightMap(
            material,
            format.startsWith("bc4-") || format.startsWith("bc5-") ? undefined : lightMap,
        );
    }
    updateDDSShaderState(material, userData.materialProfile);
    if (
        hadMap !== Boolean(material.map) ||
        hadNormalMap !== Boolean(material.normalMap) ||
        hadPackedNormalMap !== usesPackedNormalMap(material.normalMap) ||
        hadMetalnessMap !== Boolean(material.metalnessMap) ||
        hadRoughnessMap !== Boolean(material.roughnessMap) ||
        wasTransparent !== material.transparent
    ) {
        material.needsUpdate = true;
    }
    syncEFMIToon(object, material, userData);
    syncGenshinToon(object, material, userData);
    userData.lastMaps = {
        texKey: evaluated.texKey,
        normalMapKey: evaluated.normalMapKey,
        lightMapKey: evaluated.lightMapKey,
        materialMapKey: evaluated.materialMapKey,
        cutoutMapKey: evaluated.cutoutMapKey,
        highlightMapKey: evaluated.highlightMapKey,
        rampMapKey: evaluated.rampMapKey,
    };
}

// GIMI-style position buffers store the outline's smoothed normal in TANGENT.
// It runs along the surface normal, so the tangent frame it yields is
// degenerate and a normal map shaded through it comes out blotchy.
function hasUsableTangentFrame(geometry: BufferGeometry): boolean {
    const tangent = geometry.attributes.tangent;
    if (!tangent) return false;
    const normal = geometry.attributes.normal;

    const step = Math.max(1, Math.floor(tangent.count / 2000));
    let sampled = 0;
    let alongNormal = 0;
    for (let index = 0; index < tangent.count; index += step) {
        const x = tangent.getX(index);
        const y = tangent.getY(index);
        const z = tangent.getZ(index);
        if (
            !Number.isFinite(x) ||
            !Number.isFinite(y) ||
            !Number.isFinite(z) ||
            x * x + y * y + z * z === 0
        ) {
            continue;
        }
        sampled++;
        if (normal) {
            const dot = normal.getX(index) * x + normal.getY(index) * y + normal.getZ(index) * z;
            if (Math.abs(dot) > 0.5) alongNormal++;
        }
    }
    return alongNormal * 4 < sampled;
}

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
        const userData = objects[index].userData as PayloadMeshUserData;
        userData.efmiHeadMesh = true;
        userData.efmiOverlayShadow =
            !mesh.texKey &&
            mesh.textureVariants.length === 0 &&
            (indexCount(index) <= EFMI_EYE_SHADOW_MAX_INDICES ||
                (indexCount(index) <= EFMI_HAIR_SHADOW_MAX_INDICES &&
                    bounds.max[2] - bounds.min[2] >= EFMI_HAIR_SHADOW_MIN_HEIGHT));
        userData.efmiEyeMesh =
            !userData.efmiOverlayShadow && bounds.max[2] - bounds.min[2] <= EFMI_IRIS_MAX_HEIGHT;
    }
}

function configureDDSMaterialShader(material: MeshStandardMaterial): void {
    if (material.userData.modelViewerDDSMaterial) return;
    const state: DDSShaderState = {
        invertAlpha: { value: 0 },
        coverageMask: { value: 0 },
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
        shader.uniforms.modelViewerCoverageMask = state.coverageMask;
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
uniform float modelViewerCoverageMask;
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
    if ( modelViewerCoverageMask > 0.5 ) sampledDiffuseColor = vec4( vec3( 0.02 ), pow( sampledDiffuseColor.r, 0.4545 ) );
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
                "roughnessFactor *= texelRoughness.g;",
                `float modelViewerRoughnessSample = mix( texelRoughness.g, texelRoughness.r, modelViewerRoughnessBC4 );
    if ( modelViewerRoughnessSigned > 0.5 ) modelViewerRoughnessSample = modelViewerRoughnessSample * 0.5 + 0.5;
    roughnessFactor *= modelViewerRoughnessSample;`,
            )
            .replace(
                "metalnessFactor *= texelMetalness.g;",
                `float modelViewerMetalnessSample = mix( texelMetalness.g, texelMetalness.r, modelViewerMetalnessBC4 );
    if ( modelViewerMetalnessSigned > 0.5 ) modelViewerMetalnessSample = modelViewerMetalnessSample * 0.5 + 0.5;
    metalnessFactor *= modelViewerMetalnessSample;`,
            )
            .replace(
                "float mask = texture2D( rabbitFXLightMap, ( rabbitFXLightMapTransform * vec3( vRabbitFXUv, 1.0 ) ).xy ).g;",
                `vec4 modelViewerRabbitFXLightSample = texture2D( rabbitFXLightMap, ( rabbitFXLightMapTransform * vec3( vRabbitFXUv, 1.0 ) ).xy );
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

    // A mesh's cards overlap in no fixed order, which one ink color hides as
    // long as they do not write depth over each other.
    const coverageMask = Boolean(material.map?.userData.modelViewerCoverageMask);
    state.coverageMask.value = coverageMask ? 1 : 0;
    material.transparent = coverageMask;
    material.depthWrite = !coverageMask;
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
    if (
        !metadata ||
        (materialProfile !== "zzmi" &&
            materialProfile !== "wuwa:rabbitfx" &&
            materialProfile !== "efmi")
    ) {
        return 0;
    }
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
        lightMapTransform: { value: rabbitFXFallbackLightMap.matrix },
        cutoutMap: { value: rabbitFXFallbackLightMap },
        cutoutEnabled: { value: 0 },
        cutoutTransform: { value: rabbitFXFallbackLightMap.matrix },
        toonEnabled: { value: toonShadows ? 1 : 0 },
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

float rabbitFXDirectionalDiffuseFactor( vec3 surfaceNormal, vec3 lightDirection ) {
    if ( rabbitFXLightMapEnabled < 0.5 ) return 1.0;
    float mask = texture2D( rabbitFXLightMap, ( rabbitFXLightMapTransform * vec3( vRabbitFXUv, 1.0 ) ).xy ).g;
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
    material.customProgramCacheKey = () => "wuwa-rabbitfx-v2";
    material.needsUpdate = true;
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
    const defaults = DEFAULT_MODEL_VIEWER_TOON_STYLE;
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
        lightMap: { value: rabbitFXFallbackLightMap },
        lightMapEnabled: { value: 0 },
        lightMapTransform: { value: rabbitFXFallbackLightMap.matrix },
        face: { value: 0 },
        faceMask: { value: rabbitFXFallbackLightMap },
        faceMaskEnabled: { value: 0 },
        faceMaskTransform: { value: rabbitFXFallbackLightMap.matrix },
        highlightMap: { value: rabbitFXFallbackLightMap },
        highlightEnabled: { value: 0 },
        highlightTransform: { value: rabbitFXFallbackLightMap.matrix },
        rampMap: { value: rabbitFXFallbackLightMap },
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
    vec3 worldNormal = inverseTransformDirection( normal, viewMatrix );
    vec3 worldLight = inverseTransformDirection( directionalLights[ 0 ].direction, viewMatrix );
    vec3 worldView = inverseTransformDirection( normalize( vViewPosition ), viewMatrix );
    vec3 cameraBack = vec3( viewMatrix[ 0 ][ 2 ], viewMatrix[ 1 ][ 2 ], viewMatrix[ 2 ][ 2 ] );
    vec3 albedo = diffuseColor.rgb;
    vec2 lightMapUv = ( efmiLightMapTransform * vec3( vEfmiUv, 1.0 ) ).xy;
    vec4 packedMap = mix( vec4( 0.0, 1.0, 1.0, 0.5 ), texture2D( efmiLightMap, lightMapUv ), efmiLightMapEnabled );

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
    userData: PayloadMeshUserData,
): void {
    const state = material.userData.efmiToonMaterial as EFMIToonShaderState | undefined;
    if (!state) {
        return;
    }
    const style = userData.toonStyle ?? DEFAULT_MODEL_VIEWER_TOON_STYLE;
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
    object.castShadow = enabled && style.selfShadow && !coverageMask && !userData.efmiOverlayShadow;
    object.receiveShadow = enabled && style.selfShadow;

    const showOutline =
        enabled &&
        style.outlines &&
        style.outlineWidth > 0 &&
        !coverageMask &&
        !userData.efmiOverlayShadow &&
        !userData.efmiEyeMesh;
    if (!showOutline && !userData.toonOutline) {
        return;
    }
    userData.toonOutline ??= createToonOutline(object);
    const uniforms = userData.toonOutline.material.uniforms;
    userData.toonOutline.visible = showOutline;
    uniforms.map.value = material.map ?? rabbitFXFallbackLightMap;
    uniforms.mapTransform.value = textureUvTransform(uniforms.map.value);
    uniforms.widthMask.value = state.outlineMask ?? rabbitFXFallbackLightMap;
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
function createToonOutline(object: Mesh): Mesh<BufferGeometry, ShaderMaterial> {
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
                efmiRampMap: { value: rabbitFXFallbackLightMap },
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
                map: { value: rabbitFXFallbackLightMap },
                mapTransform: { value: rabbitFXFallbackLightMap.matrix },
                widthMask: { value: rabbitFXFallbackLightMap },
                widthMaskEnabled: { value: 0 },
                widthMaskTransform: { value: rabbitFXFallbackLightMap.matrix },
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
    userData.geometryStale = false;
}

function applyShapeTargets(
    object: Mesh,
    weights: Record<string, number>,
    deferGeometryUpdates: boolean,
): void {
    const userData = object.userData as PayloadMeshUserData;
    const targets = userData.shapeTargets;
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
    userData.lastShapeSignature = signature;
    if (!deformed) {
        object.geometry.attributes.normal.array.set(userData.baseNormals);
        object.geometry.attributes.normal.needsUpdate = true;
        if (userData.baseBounds) applyGeometryBounds(object.geometry, userData.baseBounds);
        userData.geometryStale = false;
        return;
    }
    if (deferGeometryUpdates) {
        userData.geometryStale = true;
        return;
    }
    refreshDeformedGeometry(object.geometry);
    userData.geometryStale = false;
}

function refreshDeformedGeometry(geometry: BufferGeometry): void {
    geometry.computeVertexNormals();
    geometry.computeBoundingBox();
    geometry.computeBoundingSphere();
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
    const { positions, normals, tangents, uvs, colors, indices } = decodeModelViewerMesh(
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
    if (colors) {
        geometry.setAttribute(TOON_VERTEX_COLOR_ATTRIBUTE, new BufferAttribute(colors, 4, true));
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
        entry.coverageMask ? "coverage" : "",
        entry.outlineMask ? "outline" : "",
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
            parsed.texture.userData.modelViewerCoverageMask = Boolean(entry.coverageMask);
            parsed.texture.userData.modelViewerOutlineMask = Boolean(entry.outlineMask);
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
        texture.userData.modelViewerCoverageMask = Boolean(entry.coverageMask);
        texture.userData.modelViewerOutlineMask = Boolean(entry.outlineMask);
    }
    return texture;
}

async function loadPayloadImageTexture(url: string, signal: AbortSignal): Promise<Texture | null> {
    const response = await fetch(url, { signal, cache: "no-store" });
    if (!response.ok) return null;
    const blob = await response.blob();
    signal.throwIfAborted();

    // Decode off the main thread. The flip replaces TextureLoader's default
    // flipY, which the GPU upload ignores for an ImageBitmap, and matches the
    // mesh-builder 1-v UV flip.
    const decoding = createImageBitmap(blob, {
        imageOrientation: "flipY",
        premultiplyAlpha: "none",
    });
    let onAbort: (() => void) | undefined;
    const aborted = new Promise<never>((_, reject) => {
        onAbort = () => reject(signal.reason);
        signal.addEventListener("abort", onAbort, { once: true });
    });
    aborted.catch(() => {});
    try {
        const image = await Promise.race([decoding, aborted]);
        const texture = new Texture(image);
        texture.flipY = false;
        texture.needsUpdate = true;
        return texture;
    } catch (error) {
        // Decoding cannot be cancelled; a bitmap finished after the abort won
        // must not keep its pixels alive.
        decoding.then(
            (image) => image.close(),
            () => {},
        );
        throw error;
    } finally {
        if (onAbort) signal.removeEventListener("abort", onAbort);
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
