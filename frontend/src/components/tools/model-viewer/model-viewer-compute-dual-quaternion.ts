import type { ViewerComputeDeformer } from "@shared/mod-viewer/types";

import {
    computeShapePassWeight,
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

export function validatePackedDualQuaternionBuffers(
    deformer: ViewerComputeDeformer,
    buffers: GIMIShapePoseBuffers,
): void {
    const pose = deformer.pose;
    const shapePassesAllowed =
        pose?.dualQuaternionVariant === "efmi_typed" ||
        pose?.dualQuaternionVariant === "efmi_compact";
    if (
        deformer.kind !== "gimi_packed_dual_quaternion_v1" ||
        !pose ||
        (pose.dualQuaternionVariant !== undefined &&
            pose.dualQuaternionVariant !== "object" &&
            pose.dualQuaternionVariant !== "object_direct" &&
            !shapePassesAllowed) ||
        !buffers.pose ||
        !buffers.blend ||
        !Number.isSafeInteger(deformer.vertexCount) ||
        deformer.vertexCount <= 0 ||
        !Number.isSafeInteger(pose.boneCount) ||
        pose.boneCount <= 0 ||
        !Number.isSafeInteger(pose.frameCount) ||
        pose.frameCount < 2 ||
        (!shapePassesAllowed && deformer.shapePasses.length !== 0) ||
        deformer.shapeStages.length !== 0 ||
        buffers.shapeTargets.length !== deformer.shapePasses.length
    ) {
        throw new Error("Packed dual-quaternion descriptor is invalid.");
    }
    validatePackedVertexSource(
        "Packed dual-quaternion base",
        deformer.base,
        buffers.base,
        deformer.vertexCount,
    );
    deformer.shapePasses.forEach((pass, index) => {
        validatePackedVertexSource(
            `Packed dual-quaternion shape target ${index}`,
            pass.target,
            buffers.shapeTargets[index]!,
            deformer.vertexCount,
        );
    });
    for (const [label, source, buffer, stride, count] of [
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
    validatePackedDualQuaternionInfluences(buffers.blend, deformer.vertexCount, pose.boneCount);
}

export function computePackedDualQuaternionFrame(
    deformer: ViewerComputeDeformer,
    buffers: GIMIShapePoseBuffers,
    poseFrame: number,
    options?: GIMIComputeOptions,
): GIMIShapePoseFrame {
    return createPackedDualQuaternionComputer(deformer, buffers)(poseFrame, options);
}

// Inputs remain immutable while this computer is in use.
export function createPackedDualQuaternionComputer(
    deformer: ViewerComputeDeformer,
    buffers: GIMIShapePoseBuffers,
) {
    validatePackedDualQuaternionBuffers(deformer, buffers);
    return (poseFrame: number, options?: GIMIComputeOptions, phaseSeconds = 0) =>
        computeValidatedPackedDualQuaternionFrame(
            deformer,
            buffers,
            poseFrame,
            options,
            phaseSeconds,
        );
}

function computeValidatedPackedDualQuaternionFrame(
    deformer: ViewerComputeDeformer,
    buffers: GIMIShapePoseBuffers,
    poseFrame: number,
    options: GIMIComputeOptions | undefined,
    phaseSeconds: number,
): GIMIShapePoseFrame {
    if (!Number.isFinite(poseFrame) || poseFrame < 0) {
        throw new Error(`Invalid packed dual-quaternion frame: ${poseFrame}`);
    }
    const pose = deformer.pose!;
    const directPositionAxes = pose.dualQuaternionVariant === "object_direct";
    // EFMI stores float positions and unit normals, so nothing is quantized to
    // the 16-bit and 8-bit records the other variants write back.
    const efmiCompact = pose.dualQuaternionVariant === "efmi_compact";
    const efmiTyped = pose.dualQuaternionVariant === "efmi_typed" || efmiCompact;
    const unsignedSeed = directPositionAxes || efmiTyped;
    const outputPosition = efmiTyped ? (value: number) => value : roundPackedPosition;
    const outputNormal = efmiTyped
        ? (value: number) => value
        : (value: number) => Math.max(-128, Math.min(127, Math.trunc(value)));
    const shapes = deformer.shapePasses.map((pass, index) => ({
        target: new Float32Array(buffers.shapeTargets[index]!),
        weight: computeShapePassWeight(pass, phaseSeconds),
    }));
    const frame = Math.min(poseFrame, pose.frameCount - 1);
    const frame0 = Math.floor(frame);
    const frame1 = Math.min(frame0 + 1, pose.frameCount - 1);
    const inter = frame - frame0;
    const base = preparedPackedVertices(deformer.base, buffers.base);
    const blendWeights = new Float32Array(buffers.blend!);
    const blendBones = new Int32Array(buffers.blend!);
    const palette = new Float32Array(buffers.pose!);
    const vertices = options?.vertices;
    const { positions, normals } = ensureGIMIShapePoseFrame(
        vertices?.length ?? deformer.vertexCount,
        false,
        options?.out,
    );
    const accumulated = new Float64Array(14);
    forEachComputeVertex(deformer.vertexCount, vertices, (vertex, destIndex) => {
        accumulated.fill(0);
        const source = vertex * 7;
        // Each blend record is four float32 weights followed by four int32 bones.
        const blendOffset = vertex * 8;
        const referenceBone = blendBones[blendOffset + 4]!;
        const reference = (frame0 * pose.boneCount + referenceBone) * 14 + 6;
        for (let influence = 0; influence < 4; influence += 1) {
            const bone = blendBones[blendOffset + 4 + influence]!;
            const weight = blendWeights[blendOffset + influence]!;
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
                // Legacy exports keep orthogonal contributions; object exports
                // multiply by sign(dot), dropping them when the dot is zero.
                // The direct shader seeds its first contribution without a sign.
                const signedFactor =
                    pose.dualQuaternionVariant === "object" ||
                    (unsignedSeed && (influence !== 0 || sample !== 0))
                        ? factor * Math.sign(dot)
                        : dot < 0
                          ? -factor
                          : factor;
                for (let component = 0; component < 14; component += 1) {
                    accumulated[component] +=
                        palette[offset + component]! * (component < 6 ? factor : signedFactor);
                }
            }
        }
        const quaternionLength = Math.hypot(
            accumulated[6]!,
            accumulated[7]!,
            accumulated[8]!,
            accumulated[9]!,
        );
        if ((!unsignedSeed && quaternionLength < 1e-8) || !accumulated.every(Number.isFinite)) {
            throw new Error(`Invalid packed dual-quaternion pose at vertex ${vertex}`);
        }
        const length = unsignedSeed ? Math.max(1e-6, quaternionLength) : quaternionLength;
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
        let baseX = base[source]!;
        let baseY = base[source + 1]!;
        let baseZ = base[source + 2]!;
        for (const shape of shapes) {
            baseX += (shape.target[source]! - base[source]!) * shape.weight;
            baseY += (shape.target[source + 1]! - base[source + 1]!) * shape.weight;
            baseZ += (shape.target[source + 2]! - base[source + 2]!) * shape.weight;
        }
        // Direct exports keep game position axes but still convert input normals.
        // EFMI rotates positions the opposite way around X from the legacy shaders.
        const x = baseX * accumulated[0]! + accumulated[3]!;
        const y =
            (directPositionAxes ? baseY : efmiTyped ? baseZ : -baseZ) * accumulated[1]! +
            accumulated[4]!;
        const z =
            (directPositionAxes ? baseZ : efmiTyped ? -baseY : baseY) * accumulated[2]! +
            accumulated[5]!;
        // Compact EFMI shaders rotate normals with the positions; the older ones do not.
        const nx = base[source + 4]!;
        const ny = efmiCompact ? base[source + 6]! : -base[source + 6]!;
        const nz = efmiCompact ? -base[source + 5]! : base[source + 5]!;
        const destination = destIndex * 3;
        positions[destination] = outputPosition(
            m00 * x + m01 * y + m02 * z + 2 * (-dw * qx + dx * qw - dy * qz + dz * qy),
        );
        const posedY = m10 * x + m11 * y + m12 * z + 2 * (-dw * qy + dx * qz + dy * qw - dz * qx);
        const posedZ = m20 * x + m21 * y + m22 * z + 2 * (-dw * qz - dx * qy + dy * qx + dz * qw);
        positions[destination + 1] = outputPosition(
            directPositionAxes ? posedY : efmiTyped ? -posedZ : posedZ,
        );
        positions[destination + 2] = outputPosition(
            directPositionAxes ? posedZ : efmiTyped ? posedY : -posedY,
        );
        normals[destination] = outputNormal(m00 * nx + m01 * ny + m02 * nz);
        const normalY = m10 * nx + m11 * ny + m12 * nz;
        const normalZ = m20 * nx + m21 * ny + m22 * nz;
        normals[destination + 1] = outputNormal(
            directPositionAxes ? normalY : efmiCompact ? -normalZ : normalZ,
        );
        normals[destination + 2] = outputNormal(
            directPositionAxes ? normalZ : efmiCompact ? normalY : -normalY,
        );
    });
    normalizePackedVectors(normals);
    return { positions, normals };
}

function validatePackedDualQuaternionInfluences(
    blend: ArrayBuffer,
    vertexCount: number,
    boneCount: number,
): void {
    const weights = new Float32Array(blend);
    const bones = new Int32Array(blend);
    for (let vertex = 0; vertex < vertexCount; vertex += 1) {
        const blendOffset = vertex * 8;
        for (let influence = 0; influence < 4; influence += 1) {
            const bone = bones[blendOffset + 4 + influence]!;
            const weight = weights[blendOffset + influence]!;
            if (bone < 0 || bone >= boneCount || !Number.isFinite(weight)) {
                throw new Error(
                    `Invalid packed dual-quaternion influence at vertex ${vertex}: bone=${bone}`,
                );
            }
        }
    }
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
