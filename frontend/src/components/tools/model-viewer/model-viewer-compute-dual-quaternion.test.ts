import { readFileSync } from "node:fs";
import { join } from "node:path";

import type { ViewerComputeDeformer } from "@shared/mod-viewer/types";
import { afterEach, describe, expect, it, vi } from "vitest";

import type { GIMIShapePoseBuffers } from "./model-viewer-compute-kernel";

import {
    computePackedDualQuaternionFrame,
    validatePackedDualQuaternionBuffers,
} from "./model-viewer-compute-dual-quaternion";

function fixture(poseValues?: number[], boneCount = 1) {
    const identity = [1, 1, 1, 0, 0, 0, 0, 0, 0, 1, 0, 0, 0, 0];
    const pose = new Float32Array(poseValues ?? [...identity, ...identity]).buffer;
    const base = new ArrayBuffer(20);
    const vertex = new DataView(base);
    vertex.setUint16(0, 0x3c00, true); // 1
    vertex.setUint16(2, 0x4000, true); // 2
    vertex.setUint16(4, 0x4200, true); // 3
    vertex.setUint16(6, 0x4400, true); // w = 4 must be ignored
    vertex.setInt8(10, 127);
    vertex.setUint32(12, 0x12345678, true); // untouched tangent
    vertex.setUint32(16, 0x76543210, true); // untouched UV
    const blend = new ArrayBuffer(32);
    new DataView(blend).setFloat32(0, 1, true);
    const source = (url: string, byteLength: number, stride: number) => ({
        url,
        byteLength,
        stride,
    });
    const deformer: ViewerComputeDeformer = {
        id: "dq",
        kind: "gimi_packed_dual_quaternion_v1",
        meshIds: ["mesh"],
        vertexCount: 1,
        base: source("/base", 20, 20),
        shapePasses: [],
        shapeStages: [],
        pose: {
            blend: source("/blend", 32, 32),
            frames: source("/pose", pose.byteLength, 56),
            boneCount,
            frameCount: pose.byteLength / (boneCount * 56),
        },
    };
    const buffers: GIMIShapePoseBuffers = { base, blend, pose, shapeTargets: [] };
    return { deformer, buffers };
}

function expectVector(actual: Float32Array, expected: number[], precision = 3) {
    expect(actual.length).toBe(expected.length);
    actual.forEach((value, index) => expect(value).toBeCloseTo(expected[index]!, precision));
}

describe("packed dual-quaternion kernel", () => {
    it("preserves game coordinates and source tangent/UV while ignoring packed w", () => {
        const { deformer, buffers } = fixture();
        const original = buffers.base.slice(0);
        const result = computePackedDualQuaternionFrame(deformer, buffers, 0);
        expectVector(result.positions, [1, 2, 3]);
        expectVector(result.normals, [0, 0, 1]);
        expect(result.tangents).toBeUndefined();
        expect(buffers.base).toEqual(original);
    });

    it("applies scale, bias and dual-quaternion translation in Blender axes", () => {
        const pose = [2, 3, 4, 5, 6, 7, 0, 0, 0, 1, 1, 2, 3, 0];
        const { deformer, buffers } = fixture([...pose, ...pose]);
        // Blender input (1,-3,2) -> scale+bias (7,-3,15) -> translation (9,1,21).
        expectVector(computePackedDualQuaternionFrame(deformer, buffers, 0).positions, [9, 21, -1]);
    });

    it("rotates positions and signed normals about the Blender Z axis", () => {
        const q = Math.SQRT1_2;
        const pose = [1, 1, 1, 0, 0, 0, 0, 0, q, q, 0, 0, 0, 0];
        const { deformer, buffers } = fixture([...pose, ...pose]);
        const result = computePackedDualQuaternionFrame(deformer, buffers, 0);
        expectVector(result.positions, [3, 2, -1]);
        expectVector(result.normals, [1, 0, 0]);
    });

    it("interpolates adjacent poses and resolves opposite quaternion signs", () => {
        const first = [1, 1, 1, 0, 0, 0, 0, 0, 0, 1, 0, 0, 0, 0];
        const second = [1, 1, 1, 0, 0, 0, 0, 0, 0, -1, -2, 0, 0, 0];
        const { deformer, buffers } = fixture([...first, ...second]);
        expectVector(computePackedDualQuaternionFrame(deformer, buffers, 0.5).positions, [3, 2, 3]);
        expectVector(computePackedDualQuaternionFrame(deformer, buffers, 1).positions, [5, 2, 3]);
    });

    it("keeps orthogonal quaternion contributions at dot zero across multiple bones", () => {
        const identity = [1, 1, 1, 0, 0, 0, 0, 0, 0, 1, 0, 0, 0, 0];
        const halfTurn = [1, 1, 1, 0, 0, 0, 0, 0, 1, 0, 0, 0, 0, 0];
        const { deformer, buffers } = fixture(
            [...identity, ...halfTurn, ...identity, ...halfTurn],
            2,
        );
        const blend = new DataView(buffers.blend!);
        blend.setFloat32(0, 0.5, true);
        blend.setFloat32(4, 0.5, true);
        blend.setInt32(20, 1, true);
        expectVector(computePackedDualQuaternionFrame(deformer, buffers, 0).positions, [3, 2, -1]);
    });

    it("rounds float16 ties to even and preserves half subnormals", () => {
        const { deformer, buffers } = fixture();
        const palette = new Float32Array(buffers.pose!);
        palette[3] = 2 ** -11; // 1 + half a half-float ULP -> 1 (even).
        expect(computePackedDualQuaternionFrame(deformer, buffers, 0).positions[0]).toBe(1);
        palette[3] = 3 * 2 ** -11; // tie above an odd mantissa -> next even.
        expect(computePackedDualQuaternionFrame(deformer, buffers, 0).positions[0]).toBe(
            1 + 2 ** -9,
        );
        new DataView(buffers.base).setUint16(0, 1, true);
        palette[3] = 0;
        expect(computePackedDualQuaternionFrame(deformer, buffers, 0).positions[0]).toBe(2 ** -24);
    });

    it("truncates and saturates rotated packed normals before normalization", () => {
        const q = Math.sin(Math.PI / 8),
            w = Math.cos(Math.PI / 8);
        const pose = [1, 1, 1, 0, 0, 0, 0, 0, q, w, 0, 0, 0, 0];
        const { deformer, buffers } = fixture([...pose, ...pose]);
        const base = new DataView(buffers.base);
        base.setInt8(8, 127);
        base.setInt8(9, 100);
        base.setInt8(10, 127);
        // Blender (127,-127,100) rotates to (~179.6,0,100), X saturates at 127.
        const length = Math.hypot(127, 100);
        expectVector(computePackedDualQuaternionFrame(deformer, buffers, 0).normals, [
            127 / length,
            100 / length,
            0,
        ]);
    });

    it("rejects invalid dimensions, bones, non-finite data and out-of-range positions", () => {
        const { deformer, buffers } = fixture();
        expect(() =>
            validatePackedDualQuaternionBuffers(deformer, { ...buffers, pose: new ArrayBuffer(4) }),
        ).toThrow();
        expect(() =>
            validatePackedDualQuaternionBuffers(
                { ...deformer, base: { ...deformer.base, stride: 24 } },
                buffers,
            ),
        ).toThrow();
        expect(() => computePackedDualQuaternionFrame(deformer, buffers, Number.NaN)).toThrow();
        expect(() => computePackedDualQuaternionFrame(deformer, buffers, -1)).toThrow();
        new DataView(buffers.blend!).setInt32(20, 1, true);
        expect(() => computePackedDualQuaternionFrame(deformer, buffers, 0)).toThrow(/influence/);
        new DataView(buffers.blend!).setInt32(20, 0, true);
        const pose = new Float32Array(buffers.pose!);
        pose[3] = Number.NaN;
        expect(() => computePackedDualQuaternionFrame(deformer, buffers, 0)).toThrow(/pose/);
        pose[3] = 65520;
        expect(() => computePackedDualQuaternionFrame(deformer, buffers, 0)).toThrow(/float16/);
    });
});

describe("packed dual-quaternion worker", () => {
    afterEach(() => vi.unstubAllGlobals());

    it("loads the new kind, compacts source indices and reports invalid frames", async () => {
        const { deformer, buffers } = fixture();
        const scope = {
            onmessage: undefined as ((event: { data: unknown }) => void) | undefined,
            postMessage: vi.fn(),
        };
        const files: Record<string, ArrayBuffer> = {
            "/base": buffers.base,
            "/blend": buffers.blend!,
            "/pose": buffers.pose!,
            "/indices": new Uint32Array([0, 0]).buffer,
        };
        vi.stubGlobal("self", scope);
        vi.stubGlobal(
            "fetch",
            vi.fn(async (url: string) => ({ ok: true, arrayBuffer: async () => files[url]! })),
        );
        vi.resetModules();
        await import("./model-viewer-compute.worker");
        scope.onmessage!({
            data: {
                type: "init",
                generation: 1,
                deformer,
                meshes: [{ id: "mesh", vertexCount: 2, sourceIndicesUrl: "/indices" }],
            },
        });
        await vi.waitFor(() =>
            expect(scope.postMessage).toHaveBeenCalledWith({ type: "ready", generation: 1 }),
        );
        scope.onmessage!({
            data: { type: "frame", generation: 1, id: 1, poseFrame: 0, phaseSeconds: 0 },
        });
        expect(scope.postMessage).toHaveBeenCalledWith(
            expect.objectContaining({
                type: "frame",
                meshes: [
                    expect.objectContaining({
                        positions: new Float32Array([1, 2, 3, 1, 2, 3]).buffer,
                    }),
                ],
            }),
            expect.any(Array),
        );
        scope.onmessage!({
            data: { type: "frame", generation: 1, id: 2, poseFrame: -1, phaseSeconds: 0 },
        });
        expect(scope.postMessage).toHaveBeenCalledWith(
            expect.objectContaining({ type: "error", generation: 1 }),
        );
    });
});

it.skipIf(!process.env.MODEL_VIEWER_PACKED_DQ_MOD)(
    "computes start, middle and end frames of the local mod",
    () => {
        const dir = process.env.MODEL_VIEWER_PACKED_DQ_MOD!;
        const read = (name: string) => new Uint8Array(readFileSync(join(dir, name))).buffer;
        const { deformer } = fixture();
        const buffers = {
            base: read("NilouStandee.buf"),
            blend: read("NilouStandeeBlend.buf"),
            pose: read("pose.buf"),
            shapeTargets: [],
        };
        deformer.vertexCount = 23775;
        deformer.base.byteLength = buffers.base.byteLength;
        deformer.pose!.blend.byteLength = buffers.blend.byteLength;
        deformer.pose!.frames.byteLength = buffers.pose.byteLength;
        deformer.pose!.boneCount = 446;
        deformer.pose!.frameCount = 5160;
        const frames = [6, 2581, 5157].map((index) =>
            computePackedDualQuaternionFrame(deformer, buffers, index),
        );
        for (const frame of frames) {
            expect(frame.positions.length).toBe(23775 * 3);
            expect(frame.positions.every(Number.isFinite)).toBe(true);
            expect(frame.normals.every(Number.isFinite)).toBe(true);
        }
        expect(frames[0]!.positions).not.toEqual(frames[1]!.positions);
        expect(frames[1]!.positions).not.toEqual(frames[2]!.positions);
    },
);
