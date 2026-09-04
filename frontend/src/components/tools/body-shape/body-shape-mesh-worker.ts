import { fetchBinaryBytes, fetchFloat32, fetchUint32 } from "@renderer/wails/binary-memory";
import {
    buildConnectedComponents,
    buildSymmetryMap,
    buildVertexAdjacency,
    computeBoundingCenter,
    computeVertexNormals,
} from "@shared/body-shape";

export type BodyShapeMeshProcessInput = {
    sessionId: string;
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

export type BodyShapeMeshProcessResult = {
    originalPositions: ArrayBuffer;
    indices?: ArrayBuffer;
    blendBytes?: ArrayBuffer;
    adjacencyOffsets?: ArrayBuffer;
    adjacencyNeighbors?: ArrayBuffer;
    originalNormals?: ArrayBuffer;
    componentIds?: ArrayBuffer;
    symmetryMap: ArrayBuffer;
    boundingCenter: [number, number, number];
};

export async function processBodyShapeMesh(
    input: BodyShapeMeshProcessInput,
    signal?: AbortSignal,
): Promise<BodyShapeMeshProcessResult> {
    validateInput(input);
    const [positions, indices, blendBytes] = await Promise.all([
        fetchFloat32(input.positionsUrl, input.positionsCount, signal),
        input.indicesUrl
            ? fetchUint32(input.indicesUrl, input.indexCount, signal)
            : Promise.resolve(undefined),
        input.blendUrl
            ? fetchBinaryBytes(input.blendUrl, input.blendBytes, signal)
            : Promise.resolve(undefined),
    ]);
    if (positions.length !== input.vertexCount * 3) {
        throw new Error(
            `Body shape position count mismatch: expected ${input.vertexCount * 3}, received ${positions.length}`,
        );
    }
    if (indices) validateIndices(indices, input.vertexCount);

    const adjacency = indices ? buildVertexAdjacency(input.vertexCount, indices) : undefined;
    if (adjacency) validateAdjacency(adjacency, input.vertexCount);
    return {
        originalPositions: asArrayBuffer(positions),
        indices: indices ? asArrayBuffer(indices) : undefined,
        blendBytes: blendBytes ? asArrayBuffer(blendBytes) : undefined,
        adjacencyOffsets: adjacency ? asArrayBuffer(adjacency.offsets) : undefined,
        adjacencyNeighbors: adjacency ? asArrayBuffer(adjacency.neighbors) : undefined,
        originalNormals: indices
            ? asArrayBuffer(computeVertexNormals(positions, indices))
            : undefined,
        componentIds: adjacency
            ? asArrayBuffer(buildConnectedComponents(input.vertexCount, adjacency))
            : undefined,
        symmetryMap: asArrayBuffer(buildSymmetryMap(positions, "x", 1e-3)),
        boundingCenter: computeBoundingCenter(positions),
    };
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
    if (!input.sessionId || !input.meshId) throw new Error("Body shape worker identity is missing");
    for (const [label, value] of [
        ["vertex count", input.vertexCount],
        ["position count", input.positionsCount],
        ["index count", input.indexCount],
        ["blend byte count", input.blendBytes],
        ["blend stride", input.blendStride],
    ] as const) {
        if (!Number.isSafeInteger(value) || value < 0) {
            throw new Error(`Invalid body shape ${label}: ${value}`);
        }
    }
    if (input.vertexCount === 0 || input.positionsCount !== input.vertexCount * 3) {
        throw new Error("Body shape position descriptor does not match its vertex count");
    }
    if (input.indicesUrl && input.indexCount % 3 !== 0) {
        throw new Error("Body shape index count is not divisible by 3");
    }
    if (input.indexCount > 0 && !input.indicesUrl) {
        throw new Error("Body shape index URL is missing");
    }
    if (input.blendBytes > 0 && !input.blendUrl) {
        throw new Error("Body shape blend URL is missing");
    }
    if (input.blendUrl && input.blendStride <= 0) {
        throw new Error("Body shape blend stride must be positive");
    }
    for (const [label, elements, bytesPerElement] of [
        ["position", input.positionsCount, Float32Array.BYTES_PER_ELEMENT],
        ["index", input.indexCount, Uint32Array.BYTES_PER_ELEMENT],
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
