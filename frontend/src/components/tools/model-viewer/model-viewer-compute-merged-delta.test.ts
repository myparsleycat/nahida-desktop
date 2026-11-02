import type { ViewerComputeDeformer } from "@shared/mod-viewer/types";
import { describe, expect, it } from "vitest";

import {
    createMergedDeltaComputer,
    MergedDeltaPlayback,
} from "./model-viewer-compute-merged-delta";

const group = {
    autoPlayVar: "play",
    speedVar: "speed",
    weightVar: "weight",
    steps: [
        { until: 0.5, step: 1 },
        { until: 1, step: 2 },
    ],
    stepLimit: 100,
    frameEnd: 10,
};

function deformer(): ViewerComputeDeformer {
    return {
        kind: "gimi_merged_delta_shape_v1",
        id: "deformer",
        meshIds: ["mesh"],
        vertexCount: 2,
        base: { url: "base", byteLength: 80, stride: 40 },
        shapePasses: [],
        shapeStages: [],
        mergedDeltas: {
            url: "deltas",
            groups: [group],
            keys: [
                { group: 0, scale: 2, offset: 0 },
                { group: 0, scale: 2, offset: 1 },
                { group: -1, scale: 0, offset: 0, weight: 0.5 },
            ],
        },
    };
}

function buffers() {
    const base = new Float32Array(20);
    base.set([1, 1, 1, 0, 1, 0, 1, 0, 0, 1], 0);
    base.set([2, 2, 2, 0, 1, 0, 1, 0, 0, 1], 10);
    const deltas = new ArrayBuffer(3 * 8 + 3 * 16);
    new Uint32Array(deltas).set([0, 1, 1, 1, 2, 1]);
    const records = (index: number) => 6 + index * 4;
    new Uint32Array(deltas)[records(0)] = 0;
    new Float32Array(deltas).set([1, 0, 0], records(0) + 1);
    new Uint32Array(deltas)[records(1)] = 1;
    new Float32Array(deltas).set([0, 1, 0], records(1) + 1);
    new Uint32Array(deltas)[records(2)] = 1;
    new Float32Array(deltas).set([0, 0, 4], records(2) + 1);
    return { base: base.buffer, deltas };
}

describe("createMergedDeltaComputer", () => {
    it("adds each key's deltas at the weight its group gives it", () => {
        const compute = createMergedDeltaComputer(deformer(), buffers());

        const half = compute({ groupWeights: [0.25] });
        expect([...half.positions]).toEqual([1.5, 1, 1, 2, 2, 4]);

        const full = compute({ groupWeights: [1] });
        expect([...full.positions]).toEqual([2, 1, 1, 2, 3, 4]);
        expect([...full.normals.slice(0, 3)]).toEqual([0, 1, 0]);
        expect(full.tangents?.length).toBe(8);
    });

    it("writes only the vertices the rendered meshes use", () => {
        const compute = createMergedDeltaComputer(deformer(), buffers());

        const frame = compute({ vertices: Uint32Array.of(1), groupWeights: [1] });
        expect([...frame.positions]).toEqual([2, 3, 4]);
    });

    it("rejects a delta buffer that does not match its key table", () => {
        const { base } = buffers();
        expect(() =>
            createMergedDeltaComputer(deformer(), { base, deltas: new ArrayBuffer(24 + 8) }),
        ).toThrow("key table");
    });
});

describe("MergedDeltaPlayback", () => {
    const tick = 1 / 60;
    const playing = { play: 1, speed: 100, weight: 0.8 };

    it("steps by the range the weight is in and runs back from the end", () => {
        const playback = new MergedDeltaPlayback([group]);
        const weights: number[] = [playback.advance(0, 10, playing)[0]!];
        for (let frame = 1; frame <= 9; frame += 1) {
            weights.push(playback.advance(frame * tick, 10, playing)[0]!);
        }

        expect(weights.map((weight) => Math.round(weight * 10))).toEqual([
            0, 1, 2, 3, 4, 5, 7, 9, 10, 9,
        ]);
    });

    it("keeps running when the clip wraps", () => {
        const playback = new MergedDeltaPlayback([group]);
        playback.advance(1, 1, playing);

        expect(playback.advance(0, 1, playing)[0]).toBeCloseTo(0);
        expect(playback.advance(tick, 1, playing)[0]).toBeCloseTo(0.1);
    });

    it("holds the pose on pause until the slider sets another weight", () => {
        const playback = new MergedDeltaPlayback([group]);
        playback.advance(0, 10, playing);
        playback.advance(tick, 10, playing);
        const paused = { ...playing, play: 0 };

        expect(playback.advance(2 * tick, 10, paused)[0]).toBeCloseTo(0.1);
        expect(playback.advance(3 * tick, 10, { ...paused, weight: 0.3 })[0]).toBeCloseTo(0.3);
        expect(playback.advance(4 * tick, 10, { ...paused, weight: 0.3 })[0]).toBeCloseTo(0.3);
    });

    it("starts a group paused by the INI at its saved weight", () => {
        const playback = new MergedDeltaPlayback([group]);
        expect(playback.advance(0, 10, { play: 0, speed: 100, weight: 0.4 })[0]).toBeCloseTo(0.4);
    });
});
