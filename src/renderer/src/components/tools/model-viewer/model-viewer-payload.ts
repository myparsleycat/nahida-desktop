import type {
    EvaluatedViewerState,
    ModViewerTransport,
    ViewerMeshTransport,
} from "@shared/mod-viewer/types";
import {
    BufferAttribute,
    BufferGeometry,
    DoubleSide,
    Group,
    Mesh,
    MeshStandardMaterial,
    Object3D,
    LinearSRGBColorSpace,
    SRGBColorSpace,
    Texture,
    TextureLoader,
} from "three";

type PayloadMeshUserData = {
    meshId: string;
    basePositions: Float32Array;
    shapeTargets: Array<{
        var: string;
        positions: Float32Array;
        mode?: "midpoint_pair";
        lowPositions?: Float32Array;
    }>;
    positionVariants: Float32Array[];
    normalCache: Map<number, Float32Array>;
    lastPositionVariantIndex?: number | null;
    lastMaps?: {
        texKey: string | null;
        normalMapKey: string | null;
        lightMapKey: string | null;
        materialMapKey: string | null;
    };
};

const textureLoader = new TextureLoader();

export async function buildPayloadModel(
    transport: ModViewerTransport,
    evalResult: EvaluatedViewerState,
    doubleSided: boolean,
): Promise<Group> {
    const textureCache = new Map<string, Promise<Texture | null>>();
    const textures = new Map<string, Texture>();
    await Promise.all(
        Object.entries(transport.textures).map(async ([key, entry]) => {
            const texture = await loadTexture(entry.url, textureCache);
            if (texture) {
                texture.colorSpace =
                    entry.role === "diffuse" ? SRGBColorSpace : LinearSRGBColorSpace;
                textures.set(key, texture);
            }
        }),
    );

    const group = new Group();
    group.userData.payloadTextures = textures;
    const evalById = new Map(evalResult.meshes.map((mesh) => [mesh.id, mesh]));
    for (const mesh of transport.meshes) {
        const geometry = await buildGeometry(mesh);
        const material = new MeshStandardMaterial({
            color: 0xffffff,
            metalness: 0.05,
            roughness: 0.65,
            side: doubleSided ? DoubleSide : undefined,
        });
        const object = new Mesh(geometry, material);
        object.name = mesh.id;
        const evaluated = evalById.get(mesh.id);
        object.visible = evaluated?.visible ?? true;
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
            positionVariants: await Promise.all(
                mesh.positionVariants.map((variant) => fetchFloat32(variant.positionsUrl)),
            ),
            normalCache: new Map(),
            lastPositionVariantIndex: null,
        } satisfies PayloadMeshUserData;
        applyEvaluatedMesh(object, evaluated, textures);
        group.add(object);
    }
    return group;
}

export function applyPayloadEval(root: Object3D, evalResult: EvaluatedViewerState): void {
    const textures = root.userData.payloadTextures as Map<string, Texture> | undefined;
    const evalById = new Map(evalResult.meshes.map((mesh) => [mesh.id, mesh]));
    root.traverse((object) => {
        if (!(object instanceof Mesh)) {
            return;
        }
        const meshId = (object.userData as PayloadMeshUserData).meshId;
        if (!meshId) {
            return;
        }
        applyEvaluatedMesh(object, evalById.get(meshId), textures);
    });
}

function applyEvaluatedMesh(
    object: Mesh,
    evaluated: EvaluatedViewerState["meshes"][number] | undefined,
    textures?: Map<string, Texture>,
): void {
    if (!evaluated) {
        return;
    }
    object.visible = evaluated.visible;
    if (!evaluated.visible) {
        return;
    }
    const userData = object.userData as PayloadMeshUserData;
    applyPositionVariant(object, userData, evaluated.positionVariantIndex);
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
    material.map = evaluated.texKey ? (textures.get(evaluated.texKey) ?? null) : null;
    material.normalMap = evaluated.normalMapKey
        ? (textures.get(evaluated.normalMapKey) ?? null)
        : null;
    if (material.normalMap) {
        material.normalScale.y = -1;
    }
    material.aoMap = evaluated.lightMapKey ? (textures.get(evaluated.lightMapKey) ?? null) : null;
    material.aoMapIntensity = material.aoMap ? 0.5 : 1;
    if (material.aoMap && object.geometry.attributes.uv && !object.geometry.attributes.uv2) {
        object.geometry.setAttribute("uv2", object.geometry.attributes.uv);
    }
    material.needsUpdate = true;
    userData.lastMaps = {
        texKey: evaluated.texKey,
        normalMapKey: evaluated.normalMapKey,
        lightMapKey: evaluated.lightMapKey,
        materialMapKey: evaluated.materialMapKey,
    };
}

function applyPositionVariant(
    object: Mesh,
    userData: PayloadMeshUserData,
    variantIndex: number | null,
): void {
    if (userData.lastPositionVariantIndex === variantIndex) {
        return;
    }
    const next =
        variantIndex === null
            ? userData.basePositions
            : (userData.positionVariants[variantIndex] ?? userData.basePositions);
    const position = object.geometry.attributes.position;
    position.array.set(next.length === position.array.length ? next : userData.basePositions);
    position.needsUpdate = true;
    userData.lastPositionVariantIndex = variantIndex;

    const cacheKey = variantIndex ?? -1;
    const cached = userData.normalCache.get(cacheKey);
    const normal = object.geometry.attributes.normal;
    if (cached && normal) {
        normal.array.set(cached);
        normal.needsUpdate = true;
        return;
    }
    // Animation frames reuse these meshes; cache normals instead of recomputing every tick.
    object.geometry.computeVertexNormals();
    if (normal) {
        userData.normalCache.set(cacheKey, new Float32Array(normal.array as Float32Array));
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
    const [positions, uvs, indices] = await Promise.all([
        fetchFloat32(mesh.positionsUrl),
        mesh.uvsUrl ? fetchFloat32(mesh.uvsUrl) : Promise.resolve(undefined),
        fetchUint32(mesh.indicesUrl),
    ]);
    const geometry = new BufferGeometry();
    geometry.setAttribute("position", new BufferAttribute(positions, 3));
    if (uvs) {
        geometry.setAttribute("uv", new BufferAttribute(uvs, 2));
    }
    geometry.setIndex(new BufferAttribute(indices, 1));
    geometry.computeVertexNormals();
    geometry.computeBoundingBox();
    geometry.computeBoundingSphere();
    return geometry;
}

async function fetchFloat32(url: string): Promise<Float32Array> {
    const response = await fetch(url);
    if (!response.ok) {
        throw new Error(`Failed to load float32 buffer: ${url} (${response.status})`);
    }
    const buffer = await response.arrayBuffer();
    if (buffer.byteLength % Float32Array.BYTES_PER_ELEMENT !== 0) {
        throw new Error(
            `Invalid float32 buffer length for ${url}: ${buffer.byteLength} is not divisible by ${Float32Array.BYTES_PER_ELEMENT}`,
        );
    }
    return new Float32Array(buffer);
}

async function fetchUint32(url: string): Promise<Uint32Array> {
    const response = await fetch(url);
    if (!response.ok) {
        throw new Error(`Failed to load uint32 buffer: ${url} (${response.status})`);
    }
    const buffer = await response.arrayBuffer();
    if (buffer.byteLength % Uint32Array.BYTES_PER_ELEMENT !== 0) {
        throw new Error(
            `Invalid uint32 buffer length for ${url}: ${buffer.byteLength} is not divisible by ${Uint32Array.BYTES_PER_ELEMENT}`,
        );
    }
    return new Uint32Array(buffer);
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
