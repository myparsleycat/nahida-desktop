import type { ViewerComputeDeformer, ViewerComputeShapeStage } from "@shared/mod-viewer/types";

import type { GIMIShapePoseFrame } from "./model-viewer-compute-kernel";

import {
    normalizePackedVectors,
    preparedPackedVertices,
    validatePackedVertexSource,
} from "./model-viewer-packed-vertex";

export type PackedShapeStageBuffers = Array<{ base: ArrayBuffer; target: ArrayBuffer }>;

export function validatePackedShapeBuffers(
    deformer: ViewerComputeDeformer,
    stages: PackedShapeStageBuffers,
): void {
    if (deformer.kind !== "gimi_cyclic_packed_shape_v1") {
        throw new Error("Packed shape deformer kind is invalid.");
    }
    if (deformer.pose) {
        throw new Error("Packed shape deformer must not include pose buffers.");
    }
    if (deformer.shapeStages.length === 0) {
        throw new Error("Packed shape deformer has no stages.");
    }
    if (stages.length !== deformer.shapeStages.length) {
        throw new Error("Packed shape stage count does not match the descriptor.");
    }
    deformer.shapeStages.forEach((stage, index) => {
        const buffers = stages[index]!;
        validatePackedVertexSource(
            `Packed shape stage ${index} base`,
            stage.base,
            buffers.base,
            deformer.vertexCount,
        );
        validatePackedVertexSource(
            `Packed shape stage ${index} target`,
            stage.target,
            buffers.target,
            deformer.vertexCount,
        );
        if (!(stage.duration > 0)) {
            throw new Error(`Packed shape stage ${index} duration is invalid.`);
        }
    });
}

export function computePackedShapeFrame(
    deformer: ViewerComputeDeformer,
    stages: PackedShapeStageBuffers,
    phaseSeconds: number,
): GIMIShapePoseFrame {
    validatePackedShapeBuffers(deformer, stages);
    const { stage, index, localTime } = packedShapeStageAt(deformer, phaseSeconds);
    const weight =
        stage.amplitude *
            Math.sin(
                (stage.phaseStart + localTime * stage.phaseRate + stage.phaseOffset) *
                    stage.angularScale,
            ) +
        stage.bias;
    const base = preparedPackedVertices(stage.base, stages[index]!.base);
    const target = preparedPackedVertices(stage.target, stages[index]!.target);
    const positions = new Float32Array(deformer.vertexCount * 3);
    const normals = new Float32Array(deformer.vertexCount * 3);
    for (let vertex = 0; vertex < deformer.vertexCount; vertex += 1) {
        const source = vertex * 7;
        const dest = vertex * 3;
        for (let axis = 0; axis < 3; axis += 1) {
            const from = base[source + axis]!;
            const to = target[source + axis]!;
            positions[dest + axis] = from + (to - from) * weight;
        }
        for (let axis = 0; axis < 3; axis += 1) {
            const from = base[source + 4 + axis]!;
            const to = target[source + 4 + axis]!;
            normals[dest + axis] = from + (to - from) * weight;
        }
    }
    normalizePackedVectors(normals);
    return { positions, normals };
}

export function packedShapeStageAt(
    deformer: ViewerComputeDeformer,
    phaseSeconds: number,
): { stage: ViewerComputeShapeStage; index: number; localTime: number } {
    let remaining = Math.max(phaseSeconds, 0);
    const last = deformer.shapeStages.length - 1;
    for (let index = 0; index < deformer.shapeStages.length; index += 1) {
        const stage = deformer.shapeStages[index]!;
        if (index === last || remaining < stage.duration) {
            return { stage, index, localTime: Math.min(remaining, stage.duration) };
        }
        remaining -= stage.duration;
    }
    const stage = deformer.shapeStages[last]!;
    return { stage, index: last, localTime: stage.duration };
}
