import type { ViewerComputeDeformer } from "@shared/mod-viewer/types";
import { afterEach, describe, expect, it, vi } from "vitest";

import { createCyclicPackedComputer } from "./model-viewer-compute-cyclic";
import { createPackedDualQuaternionComputer } from "./model-viewer-compute-dual-quaternion";
import {
    compactGIMIShapePoseFrame,
    createGIMIShapePoseComputer,
    type GIMIComputeOptions,
    type GIMIShapePoseBuffers,
    remapSourceIndices,
} from "./model-viewer-compute-kernel";
import { computePackedShapeFrame } from "./model-viewer-compute-packed-shape";

afterEach(() => {
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
});

function fixture(kind: ViewerComputeDeformer["kind"]) {
    const unpacked = kind === "gimi_shape_pose_v1";
    const cyclic = kind === "gimi_cyclic_packed_v1";
    const source = (url: string, byteLength: number, stride: number) => ({
        url,
        byteLength,
        stride,
    });
    const base = new Float32Array(
        [0, 1, 2, 3].flatMap((i) =>
            unpacked
                ? [i + 1, i + 2, i + 3, 0, 1, 0, 1, 0, 0, 1]
                : [i + 1, i + 2, i + 3, 1, 0, 127, 0],
        ),
    ).buffer;
    const blend = new ArrayBuffer(4 * 32);
    const view = new DataView(blend);
    for (let i = 0; i < 4; i++) {
        view.setFloat32(i * 32, 1, true);
        view.setInt32(i * 32 + 16, i % 2, true);
    }
    const pose = new Float32Array(
        [0, 1, 2, 3].flatMap((i) =>
            cyclic
                ? [1, 0, 0, i, 0, 1, 0, i * 2, 0, 0, 1, -i]
                : [1, 1, 1, i, i * 2, -i, 0, 0, 0, 1, i / 2, 0, 0, 0],
        ),
    ).buffer;
    const deformer: ViewerComputeDeformer = {
        id: "fixture",
        kind,
        meshIds: ["first", "second"],
        vertexCount: 4,
        base: {
            ...source("/base", base.byteLength, unpacked ? 40 : 28),
            ...(unpacked ? {} : { encoding: "packed_f32_v1" as const }),
        },
        shapePasses: [],
        shapeStages: [],
        pose: {
            blend: source("/blend", blend.byteLength, 32),
            frames: source("/pose", pose.byteLength, cyclic ? 48 : 56),
            boneCount: 2,
            frameCount: 2,
        },
    };
    const buffers: GIMIShapePoseBuffers = { base, blend, pose, shapeTargets: [] };
    return { deformer, buffers };
}

const poseKinds = [
    "gimi_shape_pose_v1",
    "gimi_cyclic_packed_v1",
    "gimi_packed_dual_quaternion_v1",
] as const;

function prepare(deformer: ViewerComputeDeformer, buffers: GIMIShapePoseBuffers) {
    switch (deformer.kind) {
        case "gimi_shape_pose_v1": {
            const compute = createGIMIShapePoseComputer(deformer, buffers);
            return (frame: number, options?: GIMIComputeOptions) => compute(frame, frame, options);
        }
        case "gimi_cyclic_packed_v1":
            return createCyclicPackedComputer(deformer, buffers);
        case "gimi_packed_dual_quaternion_v1":
            return createPackedDualQuaternionComputer(deformer, buffers);
        default:
            throw new Error("Unexpected fixture kind");
    }
}

describe.each(poseKinds)("%s subset computation", (kind) => {
    it("matches full output for reordered and repeated indices while reusing buffers", () => {
        const { deformer, buffers } = fixture(kind);
        const compute = prepare(deformer, buffers);
        const indices = new Uint32Array([3, 1, 3]);
        let out = compute(0, { vertices: indices });
        const positions = out.positions;
        const normals = out.normals;
        const tangents = out.tangents;
        for (const frame of kind === "gimi_shape_pose_v1" ? [0, 1, 0] : [0, 0.25, 1, 0]) {
            const expected = compactGIMIShapePoseFrame(compute(frame), indices);
            out.positions.fill(Number.NaN);
            out.normals.fill(Number.NaN);
            out.tangents?.fill(Number.NaN);
            out = compute(frame, { vertices: indices, out });
            expect(out).toEqual(expected);
            expect(out.positions).toBe(positions);
            expect(out.normals).toBe(normals);
            expect(out.tangents).toBe(tangents);
        }
    });

    it("validates all influences at initialization without scanning them again per frame", () => {
        const { deformer, buffers } = fixture(kind);
        const reads = vi.spyOn(DataView.prototype, "getInt32");
        const compute = prepare(deformer, buffers);
        expect(reads.mock.calls.length).toBeGreaterThanOrEqual(4);
        reads.mockClear();
        compute(0, { vertices: new Uint32Array() });
        compute(1, { vertices: new Uint32Array() });
        expect(reads).not.toHaveBeenCalled();
        new DataView(buffers.blend!).setInt32(16, 99, true);
        expect(() => prepare(deformer, buffers)).toThrow(/bone/);
    });
});

it("matches packed shape stages for a reused subset output", () => {
    const { deformer, buffers } = fixture("gimi_cyclic_packed_shape_v1");
    delete deformer.pose;
    const target = new Float32Array(buffers.base.slice(0));
    target[7] += 4;
    target[21] += 8;
    deformer.shapeStages = [
        {
            base: deformer.base,
            target: { ...deformer.base, url: "/target" },
            phaseRate: 1,
            phaseStart: 0,
            phaseOffset: 0,
            wrapAt: 0,
            angularScale: 1,
            amplitude: 0.5,
            bias: 0.5,
            duration: 2,
        },
    ];
    const stages = [{ base: buffers.base, target: target.buffer }];
    const vertices = new Uint32Array([3, 1]);
    const out = computePackedShapeFrame(deformer, stages, 0, { vertices });
    for (const phase of [0, 0.5, 1, 0]) {
        const expected = compactGIMIShapePoseFrame(
            computePackedShapeFrame(deformer, stages, phase),
            vertices,
        );
        const actual = computePackedShapeFrame(deformer, stages, phase, { vertices, out });
        expect(actual).toEqual(expected);
        expect(actual.positions).toBe(out.positions);
        expect(actual.normals).toBe(out.normals);
    }
});

it("remaps multiple meshes with shared vertices and rejects out-of-range mappings", () => {
    const used = new Uint32Array([1, 3]);
    expect(remapSourceIndices([new Uint32Array([3, 1, 3]), new Uint32Array([1])], used, 4)).toEqual(
        [new Uint32Array([1, 0, 1]), new Uint32Array([0])],
    );
    expect(() => remapSourceIndices([new Uint32Array([4])], used, 4)).toThrow(/source index 4/);
});

it("round-trips worker output buffers across frames without detaching the compute scratch", async () => {
    const { deformer, buffers } = fixture("gimi_shape_pose_v1");
    type Output = {
        type: string;
        meshes: Array<{
            meshId: string;
            positions: ArrayBuffer;
            normals: ArrayBuffer;
            tangents: ArrayBuffer;
        }>;
    };
    const messages: Output[] = [];
    const scope = {
        onmessage: undefined as ((event: { data: unknown }) => void) | undefined,
        postMessage: vi.fn((message: Output, transfer: Transferable[] = []) => {
            messages.push(structuredClone(message, { transfer }));
        }),
    };
    const files: Record<string, ArrayBuffer> = {
        "/base": buffers.base,
        "/blend": buffers.blend!,
        "/pose": buffers.pose!,
        "/first": new Uint32Array([3, 1, 3]).buffer,
        "/second": new Uint32Array([1]).buffer,
    };
    vi.stubGlobal("self", scope);
    vi.stubGlobal(
        "fetch",
        vi.fn(async (url: string) => ({ ok: true, arrayBuffer: async () => files[url]!.slice(0) })),
    );
    vi.resetModules();
    await import("./model-viewer-compute.worker");
    scope.onmessage!({
        data: {
            type: "init",
            generation: 1,
            deformer,
            meshes: [
                { id: "first", vertexCount: 3, sourceIndicesUrl: "/first" },
                { id: "second", vertexCount: 1, sourceIndicesUrl: "/second" },
            ],
        },
    });
    await vi.waitFor(() => expect(messages.at(-1)?.type).toBe("ready"));
    const compute = prepare(deformer, buffers);
    for (const frame of [0, 1, 0]) {
        scope.onmessage!({
            data: {
                type: "frame",
                generation: 1,
                id: frame + 1,
                poseFrame: frame,
                phaseSeconds: frame,
            },
        });
        const output = messages.at(-1)!;
        expect(output.type).toBe("frame");
        for (const mesh of output.meshes) {
            const indices = new Uint32Array(files[`/${mesh.meshId}`]!);
            const expected = compactGIMIShapePoseFrame(compute(frame), indices);
            expect(new Float32Array(mesh.positions)).toEqual(expected.positions);
            expect(new Float32Array(mesh.normals)).toEqual(expected.normals);
            expect(new Float32Array(mesh.tangents)).toEqual(expected.tangents);
        }
        const transfer = output.meshes.flatMap((mesh) => [
            mesh.positions,
            mesh.normals,
            mesh.tangents,
        ]);
        const recycle = structuredClone(
            { type: "recycle", generation: 1, meshes: output.meshes },
            { transfer },
        );
        expect(transfer.every((buffer) => buffer.byteLength === 0)).toBe(true);
        scope.onmessage!({ data: recycle });
    }
});
