import type { ViewerComputeDeformer } from "@shared/mod-viewer/types";

export type GIMIShapePoseBuffers = {
    base: ArrayBuffer;
    shapeTargets: ArrayBuffer[];
    blend?: ArrayBuffer;
    pose?: ArrayBuffer;
};

export type GIMIShapePoseFrame = {
    positions: Float32Array;
    normals: Float32Array;
    tangents: Float32Array;
};

export function validateGIMIShapePoseBuffers(
    deformer: ViewerComputeDeformer,
    buffers: GIMIShapePoseBuffers,
): void {
    validateSource("base", deformer.base.byteLength, deformer.base.stride, buffers.base);
    if (deformer.base.stride !== 40 || buffers.base.byteLength !== deformer.vertexCount * 40) {
        throw new Error("GIMI shape/pose base buffer must use a 40-byte vertex stride.");
    }
    if (buffers.shapeTargets.length !== deformer.shapePasses.length) {
        throw new Error("GIMI shape/pose target count does not match the descriptor.");
    }
    deformer.shapePasses.forEach((pass, index) => {
        validateSource(
            `shape target ${index}`,
            pass.target.byteLength,
            pass.target.stride,
            buffers.shapeTargets[index]!,
        );
        if (pass.target.stride !== 40 || pass.target.byteLength !== buffers.base.byteLength) {
            throw new Error(
                `GIMI shape/pose target ${index} is incompatible with the base buffer.`,
            );
        }
    });
    if (!deformer.pose) {
        return;
    }
    if (!buffers.blend || !buffers.pose) {
        throw new Error("GIMI shape/pose buffers are missing.");
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
        deformer.pose.blend.stride !== 32 ||
        buffers.blend.byteLength !== deformer.vertexCount * 32
    ) {
        throw new Error("GIMI shape/pose blend buffer must use a 32-byte vertex stride.");
    }
    if (
        deformer.pose.frames.stride !== 56 ||
        buffers.pose.byteLength !== deformer.pose.frameCount * deformer.pose.boneCount * 56
    ) {
        throw new Error("GIMI shape/pose frame buffer dimensions are invalid.");
    }
}

export function computeGIMIShapePoseFrame(
    deformer: ViewerComputeDeformer,
    buffers: GIMIShapePoseBuffers,
    poseFrame: number,
    phaseSeconds: number,
): GIMIShapePoseFrame {
    validateGIMIShapePoseBuffers(deformer, buffers);
    if (!Number.isInteger(poseFrame) || poseFrame < 0) {
        throw new Error(`Invalid GIMI shape/pose frame: ${poseFrame}`);
    }
    const base = new Float32Array(buffers.base);
    const positions = new Float32Array(deformer.vertexCount * 3);
    const normals = new Float32Array(deformer.vertexCount * 3);
    const tangents = new Float32Array(deformer.vertexCount * 4);
    for (let vertex = 0; vertex < deformer.vertexCount; vertex += 1) {
        const source = vertex * 10;
        positions.set(base.subarray(source, source + 3), vertex * 3);
        normals.set(base.subarray(source + 3, source + 6), vertex * 3);
        tangents.set(base.subarray(source + 6, source + 10), vertex * 4);
    }

    for (let passIndex = 0; passIndex < deformer.shapePasses.length; passIndex += 1) {
        const pass = deformer.shapePasses[passIndex]!;
        const target = new Float32Array(buffers.shapeTargets[passIndex]!);
        const rawPhase = Math.max(phaseSeconds, 0) * pass.phaseRate;
        const phase = pass.wrapAt && pass.wrapAt > 0 ? rawPhase % pass.wrapAt : rawPhase;
        const weight =
            pass.amplitude * Math.sin((phase + pass.phaseOffset) * pass.angularScale) + pass.bias;
        for (let vertex = 0; vertex < deformer.vertexCount; vertex += 1) {
            const source = vertex * 10;
            const position = vertex * 3;
            const tangent = vertex * 4;
            for (let axis = 0; axis < 3; axis += 1) {
                positions[position + axis] +=
                    (target[source + axis]! - base[source + axis]!) * weight;
                normals[position + axis] +=
                    (target[source + 3 + axis]! - base[source + 3 + axis]!) * weight;
            }
            for (let axis = 0; axis < 4; axis += 1) {
                tangents[tangent + axis] +=
                    (target[source + 6 + axis]! - base[source + 6 + axis]!) * weight;
            }
        }
    }

    if (!deformer.pose || !buffers.blend || !buffers.pose) {
        normalizeVectors(normals);
        return { positions, normals, tangents };
    }
    const frame = Math.min(poseFrame, deformer.pose.frameCount - 1);
    applyGIMIShapePose(
        positions,
        normals,
        new DataView(buffers.blend),
        new Float32Array(buffers.pose),
        frame,
        deformer.pose.boneCount,
    );
    return { positions, normals, tangents };
}

export function compactGIMIShapePoseFrame(
    frame: GIMIShapePoseFrame,
    sourceIndices: Uint32Array,
): GIMIShapePoseFrame {
    const positions = new Float32Array(sourceIndices.length * 3);
    const normals = new Float32Array(sourceIndices.length * 3);
    const tangents = new Float32Array(sourceIndices.length * 4);
    sourceIndices.forEach((source, target) => {
        if (source * 3 + 2 >= frame.positions.length) {
            throw new Error(`GIMI shape/pose source index ${source} is outside the vertex buffer.`);
        }
        positions.set(frame.positions.subarray(source * 3, source * 3 + 3), target * 3);
        normals.set(frame.normals.subarray(source * 3, source * 3 + 3), target * 3);
        tangents.set(frame.tangents.subarray(source * 4, source * 4 + 4), target * 4);
    });
    return { positions, normals, tangents };
}

function validateSource(
    name: string,
    byteLength: number,
    stride: number,
    buffer: ArrayBuffer,
): void {
    if (stride <= 0 || byteLength <= 0 || byteLength % stride !== 0) {
        throw new Error(`GIMI shape/pose ${name} descriptor is invalid.`);
    }
    if (buffer.byteLength !== byteLength) {
        throw new Error(
            `GIMI shape/pose ${name} size changed: expected ${byteLength}, received ${buffer.byteLength}.`,
        );
    }
}

function applyGIMIShapePose(
    positions: Float32Array,
    normals: Float32Array,
    blend: DataView,
    pose: Float32Array,
    frame: number,
    boneCount: number,
): void {
    for (let vertex = 0; vertex < positions.length / 3; vertex += 1) {
        const blendOffset = vertex * 32;
        const weights0 = blend.getFloat32(blendOffset, true);
        const weights1 = blend.getFloat32(blendOffset + 4, true);
        const weights2 = blend.getFloat32(blendOffset + 8, true);
        const weights3 = blend.getFloat32(blendOffset + 12, true);
        const bone0 = blend.getInt32(blendOffset + 16, true);
        const bone1 = blend.getInt32(blendOffset + 20, true);
        const bone2 = blend.getInt32(blendOffset + 24, true);
        const bone3 = blend.getInt32(blendOffset + 28, true);
        if (
            bone0 < 0 ||
            bone0 >= boneCount ||
            bone1 < 0 ||
            bone1 >= boneCount ||
            bone2 < 0 ||
            bone2 >= boneCount ||
            bone3 < 0 ||
            bone3 >= boneCount
        ) {
            const invalid = [bone0, bone1, bone2, bone3].find(
                (bone) => bone < 0 || bone >= boneCount,
            );
            throw new Error(`GIMI shape/pose bone index ${invalid} is outside the pose buffer.`);
        }
        const frameOffset = frame * boneCount;
        const pose0 = (frameOffset + bone0) * 14;
        const referenceX = pose[pose0 + 6]!;
        const referenceY = pose[pose0 + 7]!;
        const referenceZ = pose[pose0 + 8]!;
        const referenceW = pose[pose0 + 9]!;
        let scaleX = 0;
        let scaleY = 0;
        let scaleZ = 0;
        let biasX = 0;
        let biasY = 0;
        let biasZ = 0;
        let qx = 0;
        let qy = 0;
        let qz = 0;
        let qw = 0;
        let qdx = 0;
        let qdy = 0;
        let qdz = 0;
        let qdw = 0;
        for (let influence = 0; influence < 4; influence += 1) {
            const bone =
                influence === 0 ? bone0 : influence === 1 ? bone1 : influence === 2 ? bone2 : bone3;
            const weight =
                influence === 0
                    ? weights0
                    : influence === 1
                      ? weights1
                      : influence === 2
                        ? weights2
                        : weights3;
            const poseOffset = (frameOffset + bone) * 14;
            scaleX += pose[poseOffset]! * weight;
            scaleY += pose[poseOffset + 1]! * weight;
            scaleZ += pose[poseOffset + 2]! * weight;
            biasX += pose[poseOffset + 3]! * weight;
            biasY += pose[poseOffset + 4]! * weight;
            biasZ += pose[poseOffset + 5]! * weight;
            const sign =
                influence === 0
                    ? 1
                    : Math.sign(
                          referenceX * pose[poseOffset + 6]! +
                              referenceY * pose[poseOffset + 7]! +
                              referenceZ * pose[poseOffset + 8]! +
                              referenceW * pose[poseOffset + 9]!,
                      );
            const signedWeight = weight * sign;
            qx += pose[poseOffset + 6]! * signedWeight;
            qy += pose[poseOffset + 7]! * signedWeight;
            qz += pose[poseOffset + 8]! * signedWeight;
            qw += pose[poseOffset + 9]! * signedWeight;
            qdx += pose[poseOffset + 10]! * signedWeight;
            qdy += pose[poseOffset + 11]! * signedWeight;
            qdz += pose[poseOffset + 12]! * signedWeight;
            qdw += pose[poseOffset + 13]! * signedWeight;
        }
        const qrLength = Math.max(Math.hypot(qx, qy, qz, qw), 1e-6);
        qx /= qrLength;
        qy /= qrLength;
        qz /= qrLength;
        qw /= qrLength;
        qdx /= qrLength;
        qdy /= qrLength;
        qdz /= qrLength;
        qdw /= qrLength;
        const m00 = 1 - 2 * qy * qy - 2 * qz * qz;
        const m01 = 2 * (qx * qy - qw * qz);
        const m02 = 2 * (qx * qz + qw * qy);
        const m10 = 2 * (qx * qy + qw * qz);
        const m11 = 1 - 2 * qx * qx - 2 * qz * qz;
        const m12 = 2 * (qy * qz - qw * qx);
        const m20 = 2 * (qx * qz - qw * qy);
        const m21 = 2 * (qy * qz + qw * qx);
        const m22 = 1 - 2 * qx * qx - 2 * qy * qy;
        const translationX = 2 * (-qdw * qx + qdx * qw - qdy * qz + qdz * qy);
        const translationY = 2 * (-qdw * qy + qdx * qz + qdy * qw - qdz * qx);
        const translationZ = 2 * (-qdw * qz - qdx * qy + qdy * qx + qdz * qw);
        const offset = vertex * 3;
        const positionX = positions[offset]! * scaleX + biasX;
        const positionY = positions[offset + 1]! * scaleY + biasY;
        const positionZ = positions[offset + 2]! * scaleZ + biasZ;
        const normalX = normals[offset]!;
        const normalY = normals[offset + 1]!;
        const normalZ = normals[offset + 2]!;
        positions[offset] = m00 * positionX + m01 * positionY + m02 * positionZ + translationX;
        positions[offset + 1] = m10 * positionX + m11 * positionY + m12 * positionZ + translationY;
        positions[offset + 2] = m20 * positionX + m21 * positionY + m22 * positionZ + translationZ;
        const transformedNormalX = m00 * normalX + m01 * normalY + m02 * normalZ;
        const transformedNormalY = m10 * normalX + m11 * normalY + m12 * normalZ;
        const transformedNormalZ = m20 * normalX + m21 * normalY + m22 * normalZ;
        const normalLength = Math.hypot(transformedNormalX, transformedNormalY, transformedNormalZ);
        const divisor = normalLength > 1e-8 ? normalLength : 1;
        normals[offset] = transformedNormalX / divisor;
        normals[offset + 1] = transformedNormalY / divisor;
        normals[offset + 2] = transformedNormalZ / divisor;
    }
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
