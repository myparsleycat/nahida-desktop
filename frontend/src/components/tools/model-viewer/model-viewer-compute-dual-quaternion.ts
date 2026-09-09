import type { ViewerComputeDeformer } from "@shared/mod-viewer/types";

import type { GIMIShapePoseBuffers, GIMIShapePoseFrame } from "./model-viewer-compute-kernel";

import {
    normalizePackedVectors,
    packedHalfToFloat,
    validatePackedBuffer,
} from "./model-viewer-packed-vertex";

export function validatePackedDualQuaternionBuffers(
    deformer: ViewerComputeDeformer,
    buffers: GIMIShapePoseBuffers,
): void {
    const pose = deformer.pose;
    if (
        deformer.kind !== "gimi_packed_dual_quaternion_v1" ||
        !pose ||
        !buffers.pose ||
        !buffers.blend ||
        !Number.isSafeInteger(deformer.vertexCount) ||
        deformer.vertexCount <= 0 ||
        !Number.isSafeInteger(pose.boneCount) ||
        pose.boneCount <= 0 ||
        !Number.isSafeInteger(pose.frameCount) ||
        pose.frameCount < 2 ||
        deformer.shapePasses.length !== 0 ||
        deformer.shapeStages.length !== 0 ||
        buffers.shapeTargets.length !== 0
    ) {
        throw new Error("Packed dual-quaternion descriptor is invalid.");
    }
    for (const [label, source, buffer, stride, count] of [
        ["base", deformer.base, buffers.base, 20, deformer.vertexCount],
        ["blend", pose.blend, buffers.blend, 32, deformer.vertexCount],
        ["pose", pose.frames, buffers.pose, 56, pose.boneCount * pose.frameCount],
    ] as const) {
        validatePackedBuffer(
            `Packed dual-quaternion ${label}`,
            source.byteLength,
            source.stride,
            buffer,
        );
        if (source.stride !== stride || buffer.byteLength !== count * stride) {
            throw new Error(`Packed dual-quaternion ${label} dimensions are invalid.`);
        }
    }
}

export function computePackedDualQuaternionFrame(
    deformer: ViewerComputeDeformer,
    buffers: GIMIShapePoseBuffers,
    poseFrame: number,
): GIMIShapePoseFrame {
    validatePackedDualQuaternionBuffers(deformer, buffers);
    if (!Number.isFinite(poseFrame) || poseFrame < 0) {
        throw new Error(`Invalid packed dual-quaternion frame: ${poseFrame}`);
    }
    const pose = deformer.pose!;
    const frame = Math.min(poseFrame, pose.frameCount - 1);
    const frame0 = Math.floor(frame);
    const frame1 = Math.min(frame0 + 1, pose.frameCount - 1);
    const inter = frame - frame0;
    const base = new DataView(buffers.base);
    const blend = new DataView(buffers.blend!);
    const palette = new Float32Array(buffers.pose!);
    const positions = new Float32Array(deformer.vertexCount * 3);
    const normals = new Float32Array(deformer.vertexCount * 3);
    const accumulated = new Float64Array(14);
    for (let vertex = 0; vertex < deformer.vertexCount; vertex += 1) {
        accumulated.fill(0);
        const source = vertex * 20;
        const blendOffset = vertex * 32;
        const referenceBone = blend.getInt32(blendOffset + 16, true);
        const reference = (frame0 * pose.boneCount + referenceBone) * 14 + 6;
        for (let influence = 0; influence < 4; influence += 1) {
            const bone = blend.getInt32(blendOffset + 16 + influence * 4, true);
            const weight = blend.getFloat32(blendOffset + influence * 4, true);
            // The shader fetches all four indices, including zero-weight influences.
            if (bone < 0 || bone >= pose.boneCount || !Number.isFinite(weight)) {
                throw new Error(
                    `Invalid packed dual-quaternion influence at vertex ${vertex}: bone=${bone}`,
                );
            }
            for (let sample = 0; sample < 2; sample += 1) {
                const offset = ((sample === 0 ? frame0 : frame1) * pose.boneCount + bone) * 14;
                const factor = weight * (sample === 0 ? 1 - inter : inter);
                const dot =
                    palette[reference]! * palette[offset + 6]! +
                    palette[reference + 1]! * palette[offset + 7]! +
                    palette[reference + 2]! * palette[offset + 8]! +
                    palette[reference + 3]! * palette[offset + 9]!;
                // Unlike Math.sign(dot), the shader preserves contributions at dot == 0.
                const signedFactor = dot < 0 ? -factor : factor;
                for (let component = 0; component < 14; component += 1) {
                    accumulated[component] +=
                        palette[offset + component]! * (component < 6 ? factor : signedFactor);
                }
            }
        }
        const length = Math.hypot(
            accumulated[6]!,
            accumulated[7]!,
            accumulated[8]!,
            accumulated[9]!,
        );
        if (length < 1e-8 || !accumulated.every(Number.isFinite)) {
            throw new Error(`Invalid packed dual-quaternion pose at vertex ${vertex}`);
        }
        const qx = accumulated[6]! / length;
        const qy = accumulated[7]! / length;
        const qz = accumulated[8]! / length;
        const qw = accumulated[9]! / length;
        const dx = accumulated[10]! / length;
        const dy = accumulated[11]! / length;
        const dz = accumulated[12]! / length;
        const dw = accumulated[13]! / length;
        const m00 = 1 - 2 * qy * qy - 2 * qz * qz;
        const m01 = 2 * (qx * qy - qw * qz);
        const m02 = 2 * (qx * qz + qw * qy);
        const m10 = 2 * (qx * qy + qw * qz);
        const m11 = 1 - 2 * qx * qx - 2 * qz * qz;
        const m12 = 2 * (qy * qz - qw * qx);
        const m20 = 2 * (qx * qz - qw * qy);
        const m21 = 2 * (qy * qz + qw * qx);
        const m22 = 1 - 2 * qx * qx - 2 * qy * qy;
        // Packed game axes -> Blender axes; this shader forces position.w = 1.
        const x =
            packedHalfToFloat(base.getUint16(source, true)) * accumulated[0]! + accumulated[3]!;
        const y =
            -packedHalfToFloat(base.getUint16(source + 4, true)) * accumulated[1]! +
            accumulated[4]!;
        const z =
            packedHalfToFloat(base.getUint16(source + 2, true)) * accumulated[2]! + accumulated[5]!;
        const nx = base.getInt8(source + 8);
        const ny = -base.getInt8(source + 10);
        const nz = base.getInt8(source + 9);
        const destination = vertex * 3;
        positions[destination] = roundPackedPosition(
            m00 * x + m01 * y + m02 * z + 2 * (-dw * qx + dx * qw - dy * qz + dz * qy),
        );
        positions[destination + 1] = roundPackedPosition(
            m20 * x + m21 * y + m22 * z + 2 * (-dw * qz - dx * qy + dy * qx + dz * qw),
        );
        positions[destination + 2] = roundPackedPosition(
            -(m10 * x + m11 * y + m12 * z + 2 * (-dw * qy + dx * qz + dy * qw - dz * qx)),
        );
        normals[destination] = Math.max(
            -128,
            Math.min(127, Math.trunc(m00 * nx + m01 * ny + m02 * nz)),
        );
        normals[destination + 1] = Math.max(
            -128,
            Math.min(127, Math.trunc(m20 * nx + m21 * ny + m22 * nz)),
        );
        normals[destination + 2] = Math.max(
            -128,
            Math.min(127, Math.trunc(-(m10 * nx + m11 * ny + m12 * nz))),
        );
    }
    normalizePackedVectors(normals);
    return { positions, normals };
}

// f32tof16 output is rounded to nearest, ties to even, then decoded for Three.js.
// Keeping the quantization prevents the preview from inventing extra precision.
function roundPackedPosition(value: number): number {
    const magnitude = Math.abs(Math.fround(value));
    if (!Number.isFinite(magnitude) || magnitude >= 65520) {
        throw new Error("Packed dual-quaternion position exceeds finite float16 range.");
    }
    if (magnitude === 0) {
        return value;
    }
    const step = 2 ** Math.max(-24, Math.floor(Math.log2(magnitude)) - 10);
    const scaled = magnitude / step;
    const lower = Math.floor(scaled);
    const rounded = scaled - lower === 0.5 ? lower + (lower % 2) : Math.round(scaled);
    return Math.sign(value) * rounded * step;
}
