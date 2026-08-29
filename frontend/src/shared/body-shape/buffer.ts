/** Common float32 XYZ position layout (stride 12). */
export const POSITION_STRIDE_FLOAT3 = 12;

export type PositionLayoutValidation = {
    ok: true;
    vertexCount: number;
    stride: number;
    fileSize: number;
};

export type LayoutValidationError = {
    ok: false;
    reason: string;
};

/**
 * Validate a position buffer before reshape/write.
 * Only float3 with optional stride padding is accepted for write-back.
 */
export function validatePositionBuffer(
    fileSize: number,
    stride: number,
    expectedVertexCount?: number,
): PositionLayoutValidation | LayoutValidationError {
    if (!Number.isInteger(stride) || stride < 12) {
        return { ok: false, reason: `Unsupported position stride: ${stride}` };
    }
    if (fileSize <= 0 || fileSize % stride !== 0) {
        return {
            ok: false,
            reason: `Position file size ${fileSize} is not divisible by stride ${stride}`,
        };
    }
    const vertexCount = fileSize / stride;
    if (expectedVertexCount !== undefined && expectedVertexCount !== vertexCount) {
        return {
            ok: false,
            reason: `Vertex count mismatch: file has ${vertexCount}, expected ${expectedVertexCount}`,
        };
    }
    return { ok: true, vertexCount, stride, fileSize };
}

/**
 * Extract xyz floats from a raw position buffer (little-endian float32 at start of each stride).
 */
export function extractPositions(bytes: Uint8Array, stride: number): Float32Array {
    const validation = validatePositionBuffer(bytes.byteLength, stride);
    if (!validation.ok) {
        throw new Error(validation.reason);
    }

    const positions = new Float32Array(validation.vertexCount * 3);
    const view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);

    for (let i = 0; i < validation.vertexCount; i++) {
        const base = i * stride;
        const o = i * 3;
        positions[o] = view.getFloat32(base, true);
        positions[o + 1] = view.getFloat32(base + 4, true);
        positions[o + 2] = view.getFloat32(base + 8, true);
    }

    return positions;
}

/**
 * Write xyz floats back into a copy of the original buffer bytes (preserves padding / extra fields).
 * Enforces same byte length and finite values.
 */
export function writePositionsIntoBuffer(
    originalBytes: Uint8Array,
    stride: number,
    positions: Float32Array,
): Uint8Array {
    const validation = validatePositionBuffer(originalBytes.byteLength, stride);
    if (!validation.ok) {
        throw new Error(validation.reason);
    }
    if (positions.length !== validation.vertexCount * 3) {
        throw new Error(
            `Position count ${positions.length / 3} does not match vertex count ${validation.vertexCount}`,
        );
    }

    for (let i = 0; i < positions.length; i++) {
        if (!Number.isFinite(positions[i])) {
            throw new Error(`Non-finite position at float index ${i}`);
        }
    }

    const output = new Uint8Array(originalBytes);
    const view = new DataView(output.buffer, output.byteOffset, output.byteLength);

    for (let i = 0; i < validation.vertexCount; i++) {
        const base = i * stride;
        const o = i * 3;
        view.setFloat32(base, positions[o], true);
        view.setFloat32(base + 4, positions[o + 1], true);
        view.setFloat32(base + 8, positions[o + 2], true);
    }

    if (output.byteLength !== originalBytes.byteLength) {
        throw new Error("Position write changed file size");
    }

    return output;
}

/**
 * Detect whether a vector buffer is the common 8-byte SNORM8 tangent+normal layout.
 * Returns null when layout is unknown — callers must leave the buffer untouched.
 */
export function detectSnorm8VectorLayout(
    fileSize: number,
    vertexCount: number,
    stride?: number,
): "snorm8-tangent-normal" | null {
    const expectedStride = stride ?? 8;
    if (expectedStride !== 8) return null;
    if (fileSize !== vertexCount * 8) return null;
    return "snorm8-tangent-normal";
}

/**
 * Apply effective scales to a SNORM8 vector buffer for vertices with weight > 0.
 * Returns a new Int8Array of the same length; original handedness bytes (3, 7) preserved.
 */
export function applySnorm8VectorCorrection(options: {
    originalVectors: Int8Array;
    weights: Float32Array;
    amount: number;
    axisScale: readonly [number, number, number];
}): Int8Array {
    const { originalVectors, weights, amount, axisScale } = options;
    if (originalVectors.length % 8 !== 0) {
        throw new Error("Vector buffer length is not divisible by 8");
    }

    const vertexCount = Math.min(weights.length, originalVectors.length / 8);
    const output = new Int8Array(originalVectors);

    // Inline import-free copy of effective scale + correct to avoid circular deps in tests
    for (let i = 0; i < vertexCount; i++) {
        const w = weights[i];
        if (w <= 0 || amount === 0) continue;

        const sx = 1 + amount * axisScale[0] * w;
        const sy = 1 + amount * axisScale[1] * w;
        const sz = 1 + amount * axisScale[2] * w;
        if (Math.abs(sx - 1) < 1e-8 && Math.abs(sy - 1) < 1e-8 && Math.abs(sz - 1) < 1e-8) {
            continue;
        }

        correctVertexVectors(output, i, sx, sy, sz);
    }

    if (output.length !== originalVectors.length) {
        throw new Error("Vector write changed buffer length");
    }

    return output;
}

function correctVertexVectors(
    vectors: Int8Array,
    vertexIndex: number,
    sx: number,
    sy: number,
    sz: number,
): void {
    const base = vertexIndex * 8;
    let tx = vectors[base] / 127;
    let ty = vectors[base + 1] / 127;
    let tz = vectors[base + 2] / 127;
    let nx = vectors[base + 4] / 127;
    let ny = vectors[base + 5] / 127;
    let nz = vectors[base + 6] / 127;

    tx *= sx;
    ty *= sy;
    tz *= sz;
    const tLen = Math.hypot(tx, ty, tz);
    if (tLen > 1e-8) {
        tx /= tLen;
        ty /= tLen;
        tz /= tLen;
    }

    nx /= sx || 1;
    ny /= sy || 1;
    nz /= sz || 1;
    const nLen = Math.hypot(nx, ny, nz);
    if (nLen > 1e-8) {
        nx /= nLen;
        ny /= nLen;
        nz /= nLen;
    }

    const dot = tx * nx + ty * ny + tz * nz;
    tx -= nx * dot;
    ty -= ny * dot;
    tz -= nz * dot;
    const tLen2 = Math.hypot(tx, ty, tz);
    if (tLen2 > 1e-8) {
        tx /= tLen2;
        ty /= tLen2;
        tz /= tLen2;
    }

    vectors[base] = snorm8(tx);
    vectors[base + 1] = snorm8(ty);
    vectors[base + 2] = snorm8(tz);
    vectors[base + 4] = snorm8(nx);
    vectors[base + 5] = snorm8(ny);
    vectors[base + 6] = snorm8(nz);
}

function snorm8(value: number): number {
    const rounded = Math.round(value * 127);
    if (rounded < -127) return -127;
    if (rounded > 127) return 127;
    return rounded;
}

export function assertFinitePositions(positions: Float32Array): void {
    for (let i = 0; i < positions.length; i++) {
        if (!Number.isFinite(positions[i])) {
            throw new Error(`Non-finite position at index ${i}`);
        }
    }
}

export function encodeFloat32Le(positions: Float32Array): Uint8Array {
    assertFinitePositions(positions);
    return new Uint8Array(
        positions.buffer.slice(positions.byteOffset, positions.byteOffset + positions.byteLength),
    );
}
