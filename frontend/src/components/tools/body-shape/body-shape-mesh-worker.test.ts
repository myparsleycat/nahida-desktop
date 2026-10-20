import { describe, expect, it, vi } from "vitest";

import { processBodyShapeMesh, type BodyShapeMeshSegmentInput } from "./body-shape-mesh-worker";

const buffers = vi.hoisted(() => new Map<string, ArrayBufferView>());
vi.mock("@renderer/wails/binary-memory", () => ({
    fetchFloat32: async (url: string) => buffers.get(url),
    fetchUint32: async (url: string) => buffers.get(url),
    fetchBinaryBytes: async (url: string) => buffers.get(url),
}));

function triangle(meshId: string, x: number, blend?: Uint8Array): BodyShapeMeshSegmentInput {
    buffers.set(`${meshId}:positions`, new Float32Array([x, 0, 0, x + 1, 0, 0, x, 1, 0]));
    buffers.set(`${meshId}:indices`, new Uint32Array([0, 1, 2]));
    if (blend) buffers.set(`${meshId}:blend`, blend);
    return {
        meshId,
        vertexCount: 3,
        positionsUrl: `${meshId}:positions`,
        positionsCount: 9,
        indicesUrl: `${meshId}:indices`,
        indexCount: 3,
        blendUrl: blend ? `${meshId}:blend` : undefined,
        blendBytes: blend?.byteLength ?? 0,
        blendStride: 8,
    };
}

describe("processBodyShapeMesh", () => {
    it("offsets later segments so their triangles and neighbors stay inside their range", async () => {
        const result = await processBodyShapeMesh({
            sessionId: "session",
            meshes: [triangle("body", 2), triangle("dress", 5, new Uint8Array(24))],
        });

        expect([...new Uint32Array(result.indices!)]).toEqual([0, 1, 2, 3, 4, 5]);
        expect(result.segments).toMatchObject([
            { meshId: "body", vertexOffset: 0, vertexCount: 3, blendBytes: undefined },
            { meshId: "dress", vertexOffset: 3, vertexCount: 3, blendStride: 8 },
        ]);
        expect(result.segments[1].blendBytes?.byteLength).toBe(24);
        const offsets = new Uint32Array(result.adjacencyOffsets!);
        const neighbors = new Uint32Array(result.adjacencyNeighbors!);
        expect([...neighbors.subarray(offsets[3], offsets[4])].toSorted()).toEqual([4, 5]);
        expect(new Set(new Uint32Array(result.componentIds!)).size).toBe(2);
    });
});
