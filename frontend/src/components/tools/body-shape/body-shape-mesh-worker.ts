import { fetchBinaryBytes, fetchFloat32, fetchUint32 } from "@renderer/wails/binary-memory";
import {
    buildConnectedComponents,
    buildSymmetryMap,
    buildVertexAdjacency,
    computeBoundingCenter,
    computeVertexNormals,
} from "@shared/body-shape";

export type BodyShapeMeshSegmentInput = {
    meshId: string;
    vertexCount: number;
    positionsUrl: string;
    positionsCount: number;
    indicesUrl?: string;
    indexCount: number;
    blendUrl?: string;
    blendBytes: number;
    blendStride: number;
};

export type BodyShapeMeshProcessInput = {
    sessionId: string;
    meshes: BodyShapeMeshSegmentInput[];
};

export type BodyShapeMeshSegment = {
    meshId: string;
    vertexOffset: number;
    vertexCount: number;
    blendBytes?: ArrayBuffer;
    blendStride: number;
};

export type BodyShapeMeshProcessResult = {
    originalPositions: ArrayBuffer;
    indices?: ArrayBuffer;
    segments: BodyShapeMeshSegment[];
    adjacencyOffsets?: ArrayBuffer;
    adjacencyNeighbors?: ArrayBuffer;
    originalNormals?: ArrayBuffer;
    componentIds?: ArrayBuffer;
    symmetryMap: ArrayBuffer;
    boundingCenter: [number, number, number];
};

/**
 * Concatenates the selected meshes into one vertex range so selection, symmetry and
 * deformation treat them as a single surface; segments map the ranges back to buffers.
 */
export async function processBodyShapeMesh(
    input: BodyShapeMeshProcessInput,
    signal?: AbortSignal,
): Promise<BodyShapeMeshProcessResult> {
    validateInput(input);
    const loaded = await Promise.all(input.meshes.map((mesh) => fetchSegment(mesh, signal)));

    const vertexCount = loaded.reduce((total, segment) => total + segment.mesh.vertexCount, 0);
    const positions = new Float32Array(vertexCount * 3);
    // A segment without indices cannot share an indexed draw, so the whole mesh falls back.
    const indices = loaded.every((segment) => segment.indices)
        ? new Uint32Array(loaded.reduce((total, segment) => total + segment.mesh.indexCount, 0))
        : undefined;
    const segments: BodyShapeMeshSegment[] = [];
    let vertexOffset = 0;
    let indexOffset = 0;
    for (const segment of loaded) {
        positions.set(segment.positions, vertexOffset * 3);
        if (indices && segment.indices) {
            for (let index = 0; index < segment.indices.length; index++) {
                indices[indexOffset + index] = segment.indices[index] + vertexOffset;
            }
            indexOffset += segment.indices.length;
        }
        segments.push({
            meshId: segment.mesh.meshId,
            vertexOffset,
            vertexCount: segment.mesh.vertexCount,
            blendBytes: segment.blendBytes ? asArrayBuffer(segment.blendBytes) : undefined,
            blendStride: segment.mesh.blendStride,
        });
        vertexOffset += segment.mesh.vertexCount;
    }

    const adjacency = indices ? buildVertexAdjacency(vertexCount, indices) : undefined;
    if (adjacency) validateAdjacency(adjacency, vertexCount);
    return {
        originalPositions: asArrayBuffer(positions),
        indices: indices ? asArrayBuffer(indices) : undefined,
        segments,
        adjacencyOffsets: adjacency ? asArrayBuffer(adjacency.offsets) : undefined,
        adjacencyNeighbors: adjacency ? asArrayBuffer(adjacency.neighbors) : undefined,
        originalNormals: indices
            ? asArrayBuffer(computeVertexNormals(positions, indices))
            : undefined,
        componentIds: adjacency
            ? asArrayBuffer(buildConnectedComponents(vertexCount, adjacency))
            : undefined,
        symmetryMap: asArrayBuffer(buildSymmetryMap(positions, "x", 1e-3)),
        boundingCenter: computeBoundingCenter(positions),
    };
}

async function fetchSegment(mesh: BodyShapeMeshSegmentInput, signal?: AbortSignal) {
    const [positions, indices, blendBytes] = await Promise.all([
        fetchFloat32(mesh.positionsUrl, mesh.positionsCount, signal),
        mesh.indicesUrl
            ? fetchUint32(mesh.indicesUrl, mesh.indexCount, signal)
            : Promise.resolve(undefined),
        mesh.blendUrl
            ? fetchBinaryBytes(mesh.blendUrl, mesh.blendBytes, signal)
            : Promise.resolve(undefined),
    ]);
    if (positions.length !== mesh.vertexCount * 3) {
        throw new Error(
            `Body shape position count mismatch for ${mesh.meshId}: expected ${mesh.vertexCount * 3}, received ${positions.length}`,
        );
    }
    if (indices) validateIndices(indices, mesh.vertexCount);
    return { mesh, positions, indices, blendBytes };
}

function asArrayBuffer(view: ArrayBufferView<ArrayBufferLike>): ArrayBuffer {
    if (
        view.buffer instanceof ArrayBuffer &&
        view.byteOffset === 0 &&
        view.byteLength === view.buffer.byteLength
    ) {
        return view.buffer;
    }
    return view.buffer.slice(view.byteOffset, view.byteOffset + view.byteLength) as ArrayBuffer;
}

function validateInput(input: BodyShapeMeshProcessInput): void {
    if (!input.sessionId) throw new Error("Body shape worker identity is missing");
    if (input.meshes.length === 0) throw new Error("Body shape worker received no meshes");
    if (new Set(input.meshes.map((mesh) => mesh.meshId)).size !== input.meshes.length) {
        throw new Error("Body shape worker received the same mesh twice");
    }
    for (const mesh of input.meshes) validateSegment(mesh);
    const vertexCount = input.meshes.reduce((total, mesh) => total + mesh.vertexCount, 0);
    if (vertexCount * 3 > Number.MAX_SAFE_INTEGER / Float32Array.BYTES_PER_ELEMENT) {
        throw new Error(
            "Body shape combined position byte length overflows the safe integer range",
        );
    }
}

function validateSegment(mesh: BodyShapeMeshSegmentInput): void {
    if (!mesh.meshId) throw new Error("Body shape worker identity is missing");
    for (const [label, value] of [
        ["vertex count", mesh.vertexCount],
        ["position count", mesh.positionsCount],
        ["index count", mesh.indexCount],
        ["blend byte count", mesh.blendBytes],
        ["blend stride", mesh.blendStride],
    ] as const) {
        if (!Number.isSafeInteger(value) || value < 0) {
            throw new Error(`Invalid body shape ${label}: ${value}`);
        }
    }
    if (mesh.vertexCount === 0 || mesh.positionsCount !== mesh.vertexCount * 3) {
        throw new Error("Body shape position descriptor does not match its vertex count");
    }
    if (mesh.indicesUrl && mesh.indexCount % 3 !== 0) {
        throw new Error("Body shape index count is not divisible by 3");
    }
    if (mesh.indexCount > 0 && !mesh.indicesUrl) {
        throw new Error("Body shape index URL is missing");
    }
    if (mesh.blendBytes > 0 && !mesh.blendUrl) {
        throw new Error("Body shape blend URL is missing");
    }
    if (mesh.blendUrl && mesh.blendStride <= 0) {
        throw new Error("Body shape blend stride must be positive");
    }
    for (const [label, elements, bytesPerElement] of [
        ["position", mesh.positionsCount, Float32Array.BYTES_PER_ELEMENT],
        ["index", mesh.indexCount, Uint32Array.BYTES_PER_ELEMENT],
    ] as const) {
        if (elements > Number.MAX_SAFE_INTEGER / bytesPerElement) {
            throw new Error(`Body shape ${label} byte length overflows the safe integer range`);
        }
    }
}

function validateAdjacency(
    adjacency: { offsets: Uint32Array; neighbors: Uint32Array },
    vertexCount: number,
): void {
    if (
        adjacency.offsets.length !== vertexCount + 1 ||
        adjacency.offsets[0] !== 0 ||
        adjacency.offsets[vertexCount] !== adjacency.neighbors.length
    ) {
        throw new Error("Body shape adjacency offsets are invalid");
    }
    for (let vertex = 0; vertex < vertexCount; vertex++) {
        if (adjacency.offsets[vertex] > adjacency.offsets[vertex + 1]) {
            throw new Error("Body shape adjacency offsets are not monotonic");
        }
    }
    for (const neighbor of adjacency.neighbors) {
        if (neighbor >= vertexCount)
            throw new Error("Body shape adjacency contains an invalid vertex");
    }
}

function validateIndices(indices: Uint32Array, vertexCount: number): void {
    for (const index of indices) {
        if (index >= vertexCount) {
            throw new Error(`Body shape index ${index} exceeds vertex count ${vertexCount}`);
        }
    }
}
