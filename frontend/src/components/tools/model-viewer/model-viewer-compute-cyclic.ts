import type { ViewerComputeDeformer } from "@shared/mod-viewer/types";

import {
    ensureGIMIShapePoseFrame,
    forEachComputeVertex,
    type GIMIComputeOptions,
    type GIMIShapePoseBuffers,
    type GIMIShapePoseFrame,
} from "./model-viewer-compute-kernel";
import {
    normalizePackedVectors,
    preparedPackedVertices,
    validatePackedVertexSource,
    validatePackedBuffer,
} from "./model-viewer-packed-vertex";

const BLEND_STRIDE = 32;
const POSE_STRIDE = 48;

export function validateCyclicPackedBuffers(
    deformer: ViewerComputeDeformer,
    buffers: GIMIShapePoseBuffers,
): void {
    if (deformer.kind !== "gimi_cyclic_packed_v1") {
        throw new Error("Cyclic packed deformer kind is invalid.");
    }
    validatePackedVertexSource(
        "Cyclic packed base",
        deformer.base,
        buffers.base,
        deformer.vertexCount,
    );
    if (!deformer.pose || !buffers.blend || !buffers.pose) {
        throw new Error("Cyclic packed pose buffers are missing.");
    }
    validatePackedBuffer(
        "Cyclic packed pose blend",
        deformer.pose.blend.byteLength,
        deformer.pose.blend.stride,
        buffers.blend,
    );
    validatePackedBuffer(
        "Cyclic packed pose frames",
        deformer.pose.frames.byteLength,
        deformer.pose.frames.stride,
        buffers.pose,
    );
    if (
        deformer.pose.blend.stride !== BLEND_STRIDE ||
        buffers.blend.byteLength !== deformer.vertexCount * BLEND_STRIDE
    ) {
        throw new Error("Cyclic packed blend buffer must use a 32-byte vertex stride.");
    }
    if (
        deformer.pose.frames.stride !== POSE_STRIDE ||
        buffers.pose.byteLength !== deformer.pose.frameCount * deformer.pose.boneCount * POSE_STRIDE
    ) {
        throw new Error("Cyclic packed frame buffer dimensions are invalid.");
    }
    validateCyclicBlendInfluences(buffers.blend, deformer.vertexCount, deformer.pose.boneCount);
}

export function computeCyclicPackedFrame(
    deformer: ViewerComputeDeformer,
    buffers: GIMIShapePoseBuffers,
    poseFrame: number,
    options?: GIMIComputeOptions,
): GIMIShapePoseFrame {
    return createCyclicPackedComputer(deformer, buffers)(poseFrame, options);
}

// Inputs remain immutable while this computer is in use.
export function createCyclicPackedComputer(
    deformer: ViewerComputeDeformer,
    buffers: GIMIShapePoseBuffers,
) {
    validateCyclicPackedBuffers(deformer, buffers);
    return (poseFrame: number, options?: GIMIComputeOptions) =>
        computeValidatedCyclicPackedFrame(deformer, buffers, poseFrame, options);
}

function computeValidatedCyclicPackedFrame(
    deformer: ViewerComputeDeformer,
    buffers: GIMIShapePoseBuffers,
    poseFrame: number,
    options?: GIMIComputeOptions,
): GIMIShapePoseFrame {
    const pose = deformer.pose!;
    const frameCount = pose.frameCount;
    const boneCount = pose.boneCount;
    const frame0 = Math.min(Math.max(Math.floor(poseFrame), 0), frameCount - 1);
    const frame1 = Math.min(frame0 + 1, frameCount - 1);
    const inter = frame0 === frame1 ? 0 : poseFrame - frame0;
    const base = preparedPackedVertices(deformer.base, buffers.base);
    const blend = new DataView(buffers.blend!);
    const poseValues = new Float32Array(buffers.pose!);
    const vertices = options?.vertices;
    const { positions, normals } = ensureGIMIShapePoseFrame(
        vertices?.length ?? deformer.vertexCount,
        false,
        options?.out,
    );
    forEachComputeVertex(deformer.vertexCount, vertices, (vertex, destIndex) => {
        const source = vertex * 7;
        const blenderX = base[source]!;
        const blenderY = base[source + 2]! * -1;
        const blenderZ = base[source + 1]!;
        const blenderW = base[source + 3]!;
        const normalX = base[source + 4]!;
        const normalY = base[source + 6]! * -1;
        const normalZ = base[source + 5]!;
        const blendOffset = vertex * BLEND_STRIDE;
        const weight0 = blend.getFloat32(blendOffset, true);
        const weight1 = blend.getFloat32(blendOffset + 4, true);
        const weight2 = blend.getFloat32(blendOffset + 8, true);
        const weight3 = blend.getFloat32(blendOffset + 12, true);
        const bone0 = blend.getInt32(blendOffset + 16, true);
        const bone1 = blend.getInt32(blendOffset + 20, true);
        const bone2 = blend.getInt32(blendOffset + 24, true);
        const bone3 = blend.getInt32(blendOffset + 28, true);
        let r00 = 0;
        let r01 = 0;
        let r02 = 0;
        let r03 = 0;
        let r10 = 0;
        let r11 = 0;
        let r12 = 0;
        let r13 = 0;
        let r20 = 0;
        let r21 = 0;
        let r22 = 0;
        let r23 = 0;
        for (let influence = 0; influence < 4; influence += 1) {
            const bone =
                influence === 0 ? bone0 : influence === 1 ? bone1 : influence === 2 ? bone2 : bone3;
            const weight =
                influence === 0
                    ? weight0
                    : influence === 1
                      ? weight1
                      : influence === 2
                        ? weight2
                        : weight3;
            if (weight === 0) {
                continue;
            }
            if (bone < 0 || bone >= boneCount) {
                throw new Error(`Cyclic packed bone index ${bone} is outside the pose buffer.`);
            }
            // Preserve the original sample order and skip zero contributions:
            // reading an inactive sample can otherwise turn NaN * 0 into NaN.
            for (let sample = 0; sample < 2; sample += 1) {
                const factor = weight * (sample === 0 ? 1 - inter : inter);
                if (factor === 0) continue;
                const offset = ((sample === 0 ? frame0 : frame1) * boneCount + bone) * 12;
                r00 += poseValues[offset]! * factor;
                r01 += poseValues[offset + 1]! * factor;
                r02 += poseValues[offset + 2]! * factor;
                r03 += poseValues[offset + 3]! * factor;
                r10 += poseValues[offset + 4]! * factor;
                r11 += poseValues[offset + 5]! * factor;
                r12 += poseValues[offset + 6]! * factor;
                r13 += poseValues[offset + 7]! * factor;
                r20 += poseValues[offset + 8]! * factor;
                r21 += poseValues[offset + 9]! * factor;
                r22 += poseValues[offset + 10]! * factor;
                r23 += poseValues[offset + 11]! * factor;
            }
        }
        const skinnedX = r00 * blenderX + r01 * blenderY + r02 * blenderZ + r03 * blenderW;
        const skinnedY = r10 * blenderX + r11 * blenderY + r12 * blenderZ + r13 * blenderW;
        const skinnedZ = r20 * blenderX + r21 * blenderY + r22 * blenderZ + r23 * blenderW;
        const skinnedNx = r00 * normalX + r01 * normalY + r02 * normalZ;
        const skinnedNy = r10 * normalX + r11 * normalY + r12 * normalZ;
        const skinnedNz = r20 * normalX + r21 * normalY + r22 * normalZ;
        const dest = destIndex * 3;
        positions[dest] = skinnedX;
        positions[dest + 1] = skinnedZ;
        positions[dest + 2] = skinnedY * -1;
        normals[dest] = skinnedNx;
        normals[dest + 1] = skinnedNz;
        normals[dest + 2] = skinnedNy * -1;
    });
    normalizePackedVectors(normals);
    return { positions, normals };
}

function validateCyclicBlendInfluences(
    blend: ArrayBuffer,
    vertexCount: number,
    boneCount: number,
): void {
    const view = new DataView(blend);
    for (let vertex = 0; vertex < vertexCount; vertex += 1) {
        const blendOffset = vertex * BLEND_STRIDE;
        for (let influence = 0; influence < 4; influence += 1) {
            const weight = view.getFloat32(blendOffset + influence * 4, true);
            if (weight === 0) {
                continue;
            }
            const bone = view.getInt32(blendOffset + 16 + influence * 4, true);
            if (bone < 0 || bone >= boneCount) {
                throw new Error(`Cyclic packed bone index ${bone} is outside the pose buffer.`);
            }
        }
    }
}
