import {
    ensureVec4 as ensureVec4Native,
    normalizeTangentArray as normalizeTangentArrayNative,
    normalizeVec3Array as normalizeVec3ArrayNative,
    removeDegenerateTriangles as removeDegenerateTrianglesNative,
} from "@native/static-glb";

export function bufferToUint32Array(buffer: Buffer): Uint32Array {
    return new Uint32Array(
        buffer.buffer.slice(buffer.byteOffset, buffer.byteOffset + buffer.byteLength),
    );
}

export function uint32ArrayToBuffer(values: Uint32Array): Buffer {
    return Buffer.from(values.buffer, values.byteOffset, values.byteLength);
}

export function bufferToFloat32Array(buffer: Buffer): Float32Array {
    return new Float32Array(
        buffer.buffer.slice(buffer.byteOffset, buffer.byteOffset + buffer.byteLength),
    );
}

export function float32ArrayToBuffer(values: Float32Array): Buffer {
    return Buffer.from(values.buffer, values.byteOffset, values.byteLength);
}

export function ensureVec4(
    data: Float32Array,
    vertexCount: number,
    width: number,
    fillW = 1,
): Float32Array {
    if (width === 4) return data;
    return bufferToFloat32Array(
        ensureVec4Native(float32ArrayToBuffer(data), vertexCount, width, fillW),
    );
}

export function normalizeVec3Array(data: Float32Array): Float32Array {
    return bufferToFloat32Array(normalizeVec3ArrayNative(float32ArrayToBuffer(data)));
}

export function normalizeTangentArray(data: Float32Array): Float32Array {
    return bufferToFloat32Array(normalizeTangentArrayNative(float32ArrayToBuffer(data)));
}

export function removeDegenerateTriangles(
    indices: Uint32Array,
    warn: (message: string) => void,
): Uint32Array {
    const result = removeDegenerateTrianglesNative(uint32ArrayToBuffer(indices));
    const removed = result.removed;
    if (removed > 0) {
        warn(`Removed ${removed} degenerate triangles`);
    }
    return removed === 0 ? indices : bufferToUint32Array(result.indices);
}

export function readDxgiValues(bytes: Buffer, offset: number, format: string): number[] {
    const upper = format.toUpperCase();
    const count = formatComponentCount(upper);

    if (upper === "DXGI_FORMAT_R10G10B10A2_UNORM") {
        const value = bytes.readUInt32LE(offset);
        return [
            (value & 0x3ff) / 1023,
            ((value >> 10) & 0x3ff) / 1023,
            ((value >> 20) & 0x3ff) / 1023,
            ((value >> 30) & 0x3) / 3,
        ];
    }

    if (upper.includes("_FLOAT")) {
        if (upper.includes("32")) return range(count).map((i) => bytes.readFloatLE(offset + i * 4));
        if (upper.includes("16"))
            return range(count).map((i) => halfToFloat(bytes.readUInt16LE(offset + i * 2)));
    }

    if (upper.includes("_UNORM")) {
        if (upper.includes("16"))
            return range(count).map((i) => bytes.readUInt16LE(offset + i * 2) / 65535);
        if (upper.includes("8")) return range(count).map((i) => bytes.readUInt8(offset + i) / 255);
    }

    if (upper.includes("_SNORM")) {
        if (upper.includes("16"))
            return range(count).map((i) => Math.max(-1, bytes.readInt16LE(offset + i * 2) / 32767));
        if (upper.includes("8"))
            return range(count).map((i) => Math.max(-1, bytes.readInt8(offset + i) / 127));
    }

    if (upper.includes("_UINT")) {
        if (upper.includes("32"))
            return range(count).map((i) => bytes.readUInt32LE(offset + i * 4));
        if (upper.includes("16"))
            return range(count).map((i) => bytes.readUInt16LE(offset + i * 2));
        if (upper.includes("8")) return range(count).map((i) => bytes.readUInt8(offset + i));
    }

    if (upper.includes("_SINT")) {
        if (upper.includes("32")) return range(count).map((i) => bytes.readInt32LE(offset + i * 4));
        if (upper.includes("16")) return range(count).map((i) => bytes.readInt16LE(offset + i * 2));
        if (upper.includes("8")) return range(count).map((i) => bytes.readInt8(offset + i));
    }

    throw new Error(`Unsupported DXGI format: ${format}`);
}

export function formatComponentCount(format: string): number {
    const normalized = format.toUpperCase().replace(/^DXGI_FORMAT_/, "");
    const channels = normalized.match(/[RGBA]\d+/g);
    return channels ? channels.length : 1;
}

export function formatByteSize(format: string): number {
    const upper = format.toUpperCase();
    if (upper === "DXGI_FORMAT_R10G10B10A2_UNORM") return 4;
    const count = formatComponentCount(upper);
    if (upper.includes("32")) return count * 4;
    if (upper.includes("16")) return count * 2;
    if (upper.includes("8")) return count;
    return 0;
}

function halfToFloat(h: number): number {
    const sign = h & 0x8000 ? -1 : 1;
    const exponent = (h >> 10) & 0x1f;
    const fraction = h & 0x03ff;
    if (exponent === 0) return sign * 2 ** -14 * (fraction / 1024);
    if (exponent === 31) return fraction ? Number.NaN : sign * Number.POSITIVE_INFINITY;
    return sign * 2 ** (exponent - 15) * (1 + fraction / 1024);
}

export function range(count: number): number[] {
    return Array.from({ length: count }, (_, i) => i);
}
