import type { ViewerComputeDeformer, ViewerStateValue } from "@shared/mod-viewer/types";

import {
    createGIMIShapePoseComputer,
    ensureGIMIShapePoseFrame,
    type GIMIComputeOptions,
    type GIMIShapePoseFrame,
} from "./model-viewer-compute-kernel";

export type MergedDeltaBuffers = {
    base: ArrayBuffer;
    deltas: ArrayBuffer;
};

type MergedDeltas = NonNullable<ViewerComputeDeformer["mergedDeltas"]>;

const RECORD_WORDS = 4;
const GAME_FRAMES_PER_SECOND = 60;

function clampUnit(value: number): number {
    return Math.min(Math.max(value, 0), 1);
}

export function mergedDeltaKeyWeight(
    key: MergedDeltas["keys"][number],
    groupWeights: readonly number[],
): number {
    return key.group < 0
        ? (key.weight ?? 0)
        : clampUnit((groupWeights[key.group] ?? 0) * key.scale - key.offset);
}

/**
 * The served deltas hold a (first record, record count) pair per key, then records
 * of the vertex and its position delta. Only positions change, as in the shader.
 */
export function createMergedDeltaComputer(
    deformer: ViewerComputeDeformer,
    buffers: MergedDeltaBuffers,
) {
    const merged = deformer.mergedDeltas;
    if (deformer.kind !== "gimi_merged_delta_shape_v1" || !merged) {
        throw new Error("Merged shape delta deformer is invalid.");
    }
    if (deformer.base.stride !== 40) {
        throw new Error("Merged shape delta base buffer must use a 40-byte vertex stride.");
    }
    const headerWords = merged.keys.length * 2;
    const words = new Uint32Array(buffers.deltas);
    const floats = new Float32Array(buffers.deltas);
    let recordCount = 0;
    for (let key = 0; key < merged.keys.length; key += 1) {
        recordCount += words[key * 2 + 1]!;
    }
    if (words.length !== headerWords + recordCount * RECORD_WORDS) {
        throw new Error("Merged shape delta buffer does not match its key table.");
    }

    const computeBase = createGIMIShapePoseComputer(
        { ...deformer, shapePasses: [], pose: undefined },
        { base: buffers.base, shapeTargets: [] },
    );
    let base: GIMIShapePoseFrame | undefined;
    let destinations: Int32Array | undefined;

    return (options?: GIMIComputeOptions): GIMIShapePoseFrame => {
        const vertices = options?.vertices;
        base ??= computeBase(0, 0, { vertices });
        if (vertices && !destinations) {
            destinations = new Int32Array(deformer.vertexCount).fill(-1);
            vertices.forEach((vertex, dest) => {
                destinations![vertex] = dest;
            });
        }
        const { positions } = ensureGIMIShapePoseFrame(
            base.positions.length / 3,
            !!base.tangents,
            options?.out,
        );
        positions.set(base.positions);

        const groupWeights = options?.groupWeights ?? [];
        for (let key = 0; key < merged.keys.length; key += 1) {
            const weight = mergedDeltaKeyWeight(merged.keys[key]!, groupWeights);
            if (weight === 0) {
                continue;
            }
            const first = headerWords + words[key * 2]! * RECORD_WORDS;
            const end = first + words[key * 2 + 1]! * RECORD_WORDS;
            for (let record = first; record < end; record += RECORD_WORDS) {
                const vertex = words[record]!;
                const dest = destinations ? destinations[vertex]! : vertex;
                if (dest < 0) {
                    continue;
                }
                const position = dest * 3;
                positions[position] += floats[record + 1]! * weight;
                positions[position + 1] += floats[record + 2]! * weight;
                positions[position + 2] += floats[record + 3]! * weight;
            }
        }
        return { positions, normals: base.normals, tangents: base.tangents };
    };
}

/**
 * Plays the groups of a merged shape deformer as the mod's lists do, one step per
 * game frame. Each group runs at its own speed, so no clip length loops them all;
 * the clip clock only tells how much time has passed, and a clip restarting does
 * not rewind them. A seek moves them by its distance in either direction.
 */
export class MergedDeltaPlayback {
    private readonly frames: number[];
    private readonly directions: number[];
    private readonly seenWeights: Array<number | undefined>;
    private previousTime?: number;
    private pendingTicks = 0;

    constructor(private readonly groups: MergedDeltas["groups"]) {
        this.frames = groups.map(() => 0);
        this.directions = groups.map(() => 1);
        this.seenWeights = groups.map(() => undefined);
    }

    /** Takes every slider value, since a worker still busy skips the frames between them. */
    observe(state: Record<string, ViewerStateValue>): void {
        this.groups.forEach((group, index) => {
            const weight = Number(state[group.weightVar]);
            const changed = weight !== this.seenWeights[index];
            this.seenWeights[index] = weight;
            // Pausing keeps the pose until the slider sets another weight.
            if (changed && Number(state[group.autoPlayVar]) !== 1 && Number.isFinite(weight)) {
                this.frames[index] = clampUnit(weight) * group.frameEnd;
            }
        });
    }

    advance(time: number, duration: number, state: Record<string, ViewerStateValue>): number[] {
        this.observe(state);
        let elapsed = this.previousTime === undefined ? 0 : time - this.previousTime;
        if (elapsed < -duration / 2) {
            elapsed += duration;
        } else if (elapsed > duration / 2) {
            elapsed -= duration;
        }
        this.previousTime = time;
        this.pendingTicks += Math.abs(elapsed) * GAME_FRAMES_PER_SECOND;
        // A clip frame is one tick, which the frame times only reach within rounding.
        const ticks = Math.floor(this.pendingTicks + 1e-6);
        this.pendingTicks = Math.max(this.pendingTicks - ticks, 0);

        return this.groups.map((group, index) => {
            if (Number(state[group.autoPlayVar]) === 1) {
                const speed = Number(state[group.speedVar]) || 0;
                for (let tick = 0; tick < ticks; tick += 1) {
                    this.step(group, index, speed);
                }
            }
            return this.frames[index]! / group.frameEnd;
        });
    }

    private step(group: MergedDeltas["groups"][number], index: number, speed: number): void {
        const weight = this.frames[index]! / group.frameEnd;
        const step = group.steps.find((entry) => weight < entry.until)?.step ?? 1;
        const next =
            this.frames[index]! +
            this.directions[index]! * Math.min((step * speed) / 100, group.stepLimit);
        if (next >= group.frameEnd) {
            this.directions[index] = -1;
        } else if (next <= 0) {
            this.directions[index] = 1;
        }
        this.frames[index] = Math.min(Math.max(next, 0), group.frameEnd);
    }
}
