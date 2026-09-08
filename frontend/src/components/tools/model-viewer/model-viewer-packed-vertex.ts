export const PACKED_VERTEX_STRIDE = 20;

export function packedHalfToFloat(value: number): number {
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

export function normalizePackedVectors(vectors: Float32Array): void {
    for (let offset = 0; offset < vectors.length; offset += 3) {
        const length = Math.hypot(vectors[offset]!, vectors[offset + 1]!, vectors[offset + 2]!);
        if (length > 1e-8) {
            vectors[offset] /= length;
            vectors[offset + 1] /= length;
            vectors[offset + 2] /= length;
        }
    }
}

export function validatePackedBuffer(
    label: string,
    byteLength: number,
    stride: number,
    buffer: ArrayBuffer,
): void {
    if (stride <= 0 || byteLength <= 0 || byteLength % stride !== 0) {
        throw new Error(`${label} descriptor is invalid.`);
    }
    if (buffer.byteLength !== byteLength) {
        throw new Error(
            `${label} size changed: expected ${byteLength}, received ${buffer.byteLength}.`,
        );
    }
}
