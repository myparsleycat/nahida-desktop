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
    tangents?: Float32Array;
};

export type GIMIComputeOptions = {
    vertices?: Uint32Array;
    out?: GIMIShapePoseFrame;
};

// Character shape/pose records pack position, normal, and a float4 tangent;
// object shape records keep position and normal and store a packed texcoord
// where the tangent would sit, so their tangent stream must stay untouched.
const MODEL_VIEWER_SHAPE_POSE_STRIDE = 40;
const MODEL_VIEWER_INLINE_OBJECT_STRIDE = 44;

export function validateGIMIShapePoseBuffers(
    deformer: ViewerComputeDeformer,
    buffers: GIMIShapePoseBuffers,
): void {
    validateSource("base", deformer.base.byteLength, deformer.base.stride, buffers.base);
    const baseStride = deformer.base.stride;
    if (
        (baseStride !== MODEL_VIEWER_SHAPE_POSE_STRIDE &&
            baseStride !== MODEL_VIEWER_INLINE_OBJECT_STRIDE) ||
        buffers.base.byteLength !== deformer.vertexCount * baseStride
    ) {
        throw new Error("GIMI shape/pose base buffer must use a 40-byte or 44-byte vertex stride.");
    }
    if (deformer.pose && baseStride !== MODEL_VIEWER_SHAPE_POSE_STRIDE) {
        throw new Error("GIMI shape/pose buffers with pose data must use a 40-byte vertex stride.");
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
        if (
            pass.target.stride !== baseStride ||
            pass.target.byteLength !== buffers.base.byteLength
        ) {
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
    validateBlendBoneIndices(buffers.blend, deformer.vertexCount, deformer.pose.boneCount);
}

export function computeGIMIShapePoseFrame(
    deformer: ViewerComputeDeformer,
    buffers: GIMIShapePoseBuffers,
    poseFrame: number,
    phaseSeconds: number,
    options?: GIMIComputeOptions,
): GIMIShapePoseFrame {
    return createGIMIShapePoseComputer(deformer, buffers)(poseFrame, phaseSeconds, options);
}

// The worker owns these immutable inputs for its lifetime. Validate all source
// vertices once, including those excluded from the rendered subset.
export function createGIMIShapePoseComputer(
    deformer: ViewerComputeDeformer,
    buffers: GIMIShapePoseBuffers,
) {
    validateGIMIShapePoseBuffers(deformer, buffers);
    return (poseFrame: number, phaseSeconds: number, options?: GIMIComputeOptions) =>
        computeValidatedGIMIShapePoseFrame(deformer, buffers, poseFrame, phaseSeconds, options);
}

function computeValidatedGIMIShapePoseFrame(
    deformer: ViewerComputeDeformer,
    buffers: GIMIShapePoseBuffers,
    poseFrame: number,
    phaseSeconds: number,
    options?: GIMIComputeOptions,
): GIMIShapePoseFrame {
    if (!Number.isInteger(poseFrame) || poseFrame < 0) {
        throw new Error(`Invalid GIMI shape/pose frame: ${poseFrame}`);
    }
    const vertices = options?.vertices;
    const outputCount = vertices?.length ?? deformer.vertexCount;
    const withTangents = deformer.base.stride === MODEL_VIEWER_SHAPE_POSE_STRIDE;
    const { positions, normals, tangents } = ensureGIMIShapePoseFrame(
        outputCount,
        withTangents,
        options?.out,
    );
    const base = new Float32Array(buffers.base);
    const strideFloats = deformer.base.stride / 4;
    forEachComputeVertex(deformer.vertexCount, vertices, (vertex, dest) => {
        const source = vertex * strideFloats;
        const position = dest * 3;
        positions[position] = base[source]!;
        positions[position + 1] = base[source + 1]!;
        positions[position + 2] = base[source + 2]!;
        normals[position] = base[source + 3]!;
        normals[position + 1] = base[source + 4]!;
        normals[position + 2] = base[source + 5]!;
        if (tangents) {
            const tangent = dest * 4;
            tangents[tangent] = base[source + 6]!;
            tangents[tangent + 1] = base[source + 7]!;
            tangents[tangent + 2] = base[source + 8]!;
            tangents[tangent + 3] = base[source + 9]!;
        }
    });

    for (let passIndex = 0; passIndex < deformer.shapePasses.length; passIndex += 1) {
        const pass = deformer.shapePasses[passIndex]!;
        const target = new Float32Array(buffers.shapeTargets[passIndex]!);
        const rawPhase = Math.max(phaseSeconds, 0) * pass.phaseRate;
        const phaseStart = pass.phaseStart ?? 0;
        const wrapAt = pass.wrapAt ?? 0;
        const phase =
            wrapAt !== 0 && wrapAt > phaseStart
                ? phaseStart + (rawPhase % (wrapAt - phaseStart))
                : phaseStart + rawPhase;
        const weight =
            pass.amplitude * Math.sin((phase + pass.phaseOffset) * pass.angularScale) + pass.bias;
        forEachComputeVertex(deformer.vertexCount, vertices, (vertex, dest) => {
            const source = vertex * strideFloats;
            const position = dest * 3;
            for (let axis = 0; axis < 3; axis += 1) {
                positions[position + axis] +=
                    (target[source + axis]! - base[source + axis]!) * weight;
                normals[position + axis] +=
                    (target[source + 3 + axis]! - base[source + 3 + axis]!) * weight;
            }
            if (tangents) {
                const tangent = dest * 4;
                for (let axis = 0; axis < 4; axis += 1) {
                    tangents[tangent + axis] +=
                        (target[source + 6 + axis]! - base[source + 6 + axis]!) * weight;
                }
            }
        });
    }

    if (!deformer.pose || !buffers.blend || !buffers.pose || !tangents) {
        normalizeVectors(normals);
        return { positions, normals, tangents };
    }
    const frame = Math.min(poseFrame, deformer.pose.frameCount - 1);
    applyGIMIShapePose(
        positions,
        normals,
        tangents,
        new DataView(buffers.blend),
        new Float32Array(buffers.pose),
        frame,
        deformer.pose.boneCount,
        vertices,
    );
    return { positions, normals, tangents };
}

export function compactGIMIShapePoseFrame(
    frame: GIMIShapePoseFrame,
    sourceIndices: Uint32Array,
    out?: GIMIShapePoseFrame,
): GIMIShapePoseFrame {
    const { positions, normals, tangents } = ensureGIMIShapePoseFrame(
        sourceIndices.length,
        !!frame.tangents,
        out,
    );
    for (let target = 0; target < sourceIndices.length; target += 1) {
        const source = sourceIndices[target]!;
        if (source * 3 + 2 >= frame.positions.length) {
            throw new Error(`GIMI shape/pose source index ${source} is outside the vertex buffer.`);
        }
        const from = source * 3;
        const to = target * 3;
        positions[to] = frame.positions[from]!;
        positions[to + 1] = frame.positions[from + 1]!;
        positions[to + 2] = frame.positions[from + 2]!;
        normals[to] = frame.normals[from]!;
        normals[to + 1] = frame.normals[from + 1]!;
        normals[to + 2] = frame.normals[from + 2]!;
        if (tangents && frame.tangents) {
            const fromTangent = source * 4;
            const toTangent = target * 4;
            tangents[toTangent] = frame.tangents[fromTangent]!;
            tangents[toTangent + 1] = frame.tangents[fromTangent + 1]!;
            tangents[toTangent + 2] = frame.tangents[fromTangent + 2]!;
            tangents[toTangent + 3] = frame.tangents[fromTangent + 3]!;
        }
    }
    return { positions, normals, tangents };
}

export function collectUsedVertices(
    sourceIndexLists: readonly Uint32Array[],
    vertexCount: number,
): Uint32Array {
    const used = new Uint8Array(vertexCount);
    for (const indices of sourceIndexLists) {
        for (let index = 0; index < indices.length; index += 1) {
            const source = indices[index]!;
            if (source >= vertexCount) {
                throw new Error(
                    `GIMI shape/pose source index ${source} is outside the vertex buffer.`,
                );
            }
            used[source] = 1;
        }
    }
    let count = 0;
    for (let vertex = 0; vertex < vertexCount; vertex += 1) {
        if (used[vertex]) {
            count += 1;
        }
    }
    const vertices = new Uint32Array(count);
    let offset = 0;
    for (let vertex = 0; vertex < vertexCount; vertex += 1) {
        if (used[vertex]) {
            vertices[offset] = vertex;
            offset += 1;
        }
    }
    return vertices;
}

export function remapSourceIndices(
    sourceIndexLists: readonly Uint32Array[],
    usedVertices: Uint32Array,
    vertexCount: number,
): Uint32Array[] {
    const map = new Int32Array(vertexCount).fill(-1);
    for (let index = 0; index < usedVertices.length; index += 1) {
        map[usedVertices[index]!] = index;
    }
    return sourceIndexLists.map((sourceIndices) => {
        const remapped = new Uint32Array(sourceIndices.length);
        for (let index = 0; index < sourceIndices.length; index += 1) {
            const compact = map[sourceIndices[index]!]!;
            if (compact === undefined || compact < 0) {
                throw new Error(
                    `GIMI shape/pose source index ${sourceIndices[index]} is outside the vertex buffer.`,
                );
            }
            remapped[index] = compact;
        }
        return remapped;
    });
}

export function ensureGIMIShapePoseFrame(
    vertexCount: number,
    withTangents: boolean,
    out?: GIMIShapePoseFrame,
): GIMIShapePoseFrame {
    const positions =
        out?.positions.length === vertexCount * 3
            ? out.positions
            : new Float32Array(vertexCount * 3);
    const normals =
        out?.normals.length === vertexCount * 3 ? out.normals : new Float32Array(vertexCount * 3);
    const tangents = withTangents
        ? out?.tangents?.length === vertexCount * 4
            ? out.tangents
            : new Float32Array(vertexCount * 4)
        : undefined;
    return { positions, normals, tangents };
}

export function forEachComputeVertex(
    vertexCount: number,
    vertices: Uint32Array | undefined,
    visit: (sourceVertex: number, dest: number) => void,
): void {
    if (!vertices) {
        for (let vertex = 0; vertex < vertexCount; vertex += 1) {
            visit(vertex, vertex);
        }
        return;
    }
    for (let dest = 0; dest < vertices.length; dest += 1) {
        const source = vertices[dest]!;
        if (source >= vertexCount) {
            throw new Error(`GIMI shape/pose source index ${source} is outside the vertex buffer.`);
        }
        visit(source, dest);
    }
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

function validateBlendBoneIndices(
    blend: ArrayBuffer,
    vertexCount: number,
    boneCount: number,
): void {
    const view = new DataView(blend);
    for (let vertex = 0; vertex < vertexCount; vertex += 1) {
        const blendOffset = vertex * 32;
        const bone0 = view.getInt32(blendOffset + 16, true);
        const bone1 = view.getInt32(blendOffset + 20, true);
        const bone2 = view.getInt32(blendOffset + 24, true);
        const bone3 = view.getInt32(blendOffset + 28, true);
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
    }
}

function applyGIMIShapePose(
    positions: Float32Array,
    normals: Float32Array,
    tangents: Float32Array,
    blend: DataView,
    pose: Float32Array,
    frame: number,
    boneCount: number,
    vertices?: Uint32Array,
): void {
    const count = positions.length / 3;
    for (let dest = 0; dest < count; dest += 1) {
        const vertex = vertices ? vertices[dest]! : dest;
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
        const offset = dest * 3;
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
        const tangentOffset = dest * 4;
        const tangentX = tangents[tangentOffset]!;
        const tangentY = tangents[tangentOffset + 1]!;
        const tangentZ = tangents[tangentOffset + 2]!;
        tangents[tangentOffset] = m00 * tangentX + m01 * tangentY + m02 * tangentZ;
        tangents[tangentOffset + 1] = m10 * tangentX + m11 * tangentY + m12 * tangentZ;
        tangents[tangentOffset + 2] = m20 * tangentX + m21 * tangentY + m22 * tangentZ;
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
