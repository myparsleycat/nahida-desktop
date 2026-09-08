import type { ViewerComputeDeformer } from "@shared/mod-viewer/types";

import type { GIMIShapePoseBuffers, GIMIShapePoseFrame } from "./model-viewer-compute-kernel";

const PACKED_STRIDE = 20;
const BLEND_STRIDE = 32;
const POSE_STRIDE = 48;

export function validateCyclicPackedBuffers(
    deformer: ViewerComputeDeformer,
    buffers: GIMIShapePoseBuffers,
): void {
    if (deformer.kind !== "gimi_cyclic_packed_v1") {
        throw new Error("Cyclic packed deformer kind is invalid.");
    }
    validateSource("base", deformer.base.byteLength, deformer.base.stride, buffers.base);
    if (
        deformer.base.stride !== PACKED_STRIDE ||
        buffers.base.byteLength !== deformer.vertexCount * PACKED_STRIDE
    ) {
        throw new Error("Cyclic packed base buffer must use a 20-byte vertex stride.");
    }
    if (!deformer.pose || !buffers.blend || !buffers.pose) {
        throw new Error("Cyclic packed pose buffers are missing.");
    }
    validateSource(
        "pose blend",
        deformer.pose.blend.byteLength,
        deformer.pose.blend.stride,
        buffers.blend,
    );
    validateSource(
        "pose frames",
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
}

export function computeCyclicPackedFrame(
    deformer: ViewerComputeDeformer,
    buffers: GIMIShapePoseBuffers,
    poseFrame: number,
): GIMIShapePoseFrame {
    validateCyclicPackedBuffers(deformer, buffers);
    const pose = deformer.pose!;
    const frameCount = pose.frameCount;
    const boneCount = pose.boneCount;
    const frame0 = Math.min(Math.max(Math.floor(poseFrame), 0), frameCount - 1);
    const frame1 = Math.min(frame0 + 1, frameCount - 1);
    const inter = frame0 === frame1 ? 0 : poseFrame - frame0;
    const base = new DataView(buffers.base);
    const blend = new DataView(buffers.blend!);
    const poseValues = new Float32Array(buffers.pose!);
    const positions = new Float32Array(deformer.vertexCount * 3);
    const normals = new Float32Array(deformer.vertexCount * 3);
    for (let vertex = 0; vertex < deformer.vertexCount; vertex += 1) {
        const source = vertex * PACKED_STRIDE;
        const gameX = halfToFloat(base.getUint16(source, true));
        const gameY = halfToFloat(base.getUint16(source + 2, true));
        const gameZ = halfToFloat(base.getUint16(source + 4, true));
        const gameW = halfToFloat(base.getUint16(source + 6, true));
        const blenderX = gameX;
        const blenderY = gameZ * -1;
        const blenderZ = gameY;
        const blenderW = gameW;
        const normalX = i8(base.getUint8(source + 8));
        const normalY = i8(base.getUint8(source + 10)) * -1;
        const normalZ = i8(base.getUint8(source + 9));
        const blendOffset = vertex * BLEND_STRIDE;
        const weights = [
            blend.getFloat32(blendOffset, true),
            blend.getFloat32(blendOffset + 4, true),
            blend.getFloat32(blendOffset + 8, true),
            blend.getFloat32(blendOffset + 12, true),
        ];
        const bones = [
            blend.getInt32(blendOffset + 16, true),
            blend.getInt32(blendOffset + 20, true),
            blend.getInt32(blendOffset + 24, true),
            blend.getInt32(blendOffset + 28, true),
        ];
        const rows = [
            [0, 0, 0, 0],
            [0, 0, 0, 0],
            [0, 0, 0, 0],
        ];
        for (let influence = 0; influence < 4; influence += 1) {
            const bone = bones[influence]!;
            const weight = weights[influence]!;
            if (weight === 0) {
                continue;
            }
            if (bone < 0 || bone >= boneCount) {
                throw new Error(`Cyclic packed bone index ${bone} is outside the pose buffer.`);
            }
            accumulatePoseRows(rows, poseValues, frame0, bone, boneCount, weight * (1 - inter));
            accumulatePoseRows(rows, poseValues, frame1, bone, boneCount, weight * inter);
        }
        const pos = [blenderX, blenderY, blenderZ, blenderW];
        const nrm = [normalX, normalY, normalZ, 0];
        const skinnedPos = [dot4(rows[0]!, pos), dot4(rows[1]!, pos), dot4(rows[2]!, pos)];
        const skinnedNrm = [dot4(rows[0]!, nrm), dot4(rows[1]!, nrm), dot4(rows[2]!, nrm)];
        const dest = vertex * 3;
        positions[dest] = skinnedPos[0]!;
        positions[dest + 1] = skinnedPos[2]!;
        positions[dest + 2] = skinnedPos[1]! * -1;
        normals[dest] = skinnedNrm[0]!;
        normals[dest + 1] = skinnedNrm[2]!;
        normals[dest + 2] = skinnedNrm[1]! * -1;
    }
    normalizeVectors(normals);
    return { positions, normals };
}

function accumulatePoseRows(
    rows: number[][],
    pose: Float32Array,
    frame: number,
    bone: number,
    boneCount: number,
    weight: number,
): void {
    if (weight === 0) {
        return;
    }
    const offset = (frame * boneCount + bone) * 12;
    for (let row = 0; row < 3; row += 1) {
        const dest = rows[row]!;
        const source = offset + row * 4;
        dest[0] += pose[source]! * weight;
        dest[1] += pose[source + 1]! * weight;
        dest[2] += pose[source + 2]! * weight;
        dest[3] += pose[source + 3]! * weight;
    }
}

function dot4(row: number[], value: number[]): number {
    return row[0]! * value[0]! + row[1]! * value[1]! + row[2]! * value[2]! + row[3]! * value[3]!;
}

function validateSource(
    name: string,
    byteLength: number,
    stride: number,
    buffer: ArrayBuffer,
): void {
    if (stride <= 0 || byteLength <= 0 || byteLength % stride !== 0) {
        throw new Error(`Cyclic packed ${name} descriptor is invalid.`);
    }
    if (buffer.byteLength !== byteLength) {
        throw new Error(
            `Cyclic packed ${name} size changed: expected ${byteLength}, received ${buffer.byteLength}.`,
        );
    }
}

function halfToFloat(value: number): number {
    const sign = value & 0x8000 ? -1 : 1;
    const exponent = (value >> 10) & 0x1f;
    const fraction = value & 0x03ff;
    if (exponent === 0) {
        return sign * 2 ** -14 * (fraction / 1024);
    }
    if (exponent === 31) {
        return fraction === 0 ? sign * Infinity : Number.NaN;
    }
    return sign * 2 ** (exponent - 15) * (1 + fraction / 1024);
}

function i8(value: number): number {
    return value > 127 ? value - 256 : value;
}

function normalizeVectors(vectors: Float32Array): void {
    for (let offset = 0; offset < vectors.length; offset += 3) {
        const length = Math.hypot(vectors[offset]!, vectors[offset + 1]!, vectors[offset + 2]!);
        if (length > 1e-8) {
            vectors[offset] /= length;
            vectors[offset + 1] /= length;
            vectors[offset + 2] /= length;
        }
    }
}
