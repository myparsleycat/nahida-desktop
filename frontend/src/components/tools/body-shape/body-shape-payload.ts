export function toBodyShapeBytes(value: unknown): Uint8Array | undefined {
    if (value == null) return undefined;
    if (typeof value === "string") return decodeBase64Bytes(value);
    if (value instanceof Uint8Array) return value;
    if (ArrayBuffer.isView(value)) {
        return new Uint8Array(value.buffer, value.byteOffset, value.byteLength);
    }
    if (value instanceof ArrayBuffer) return new Uint8Array(value);
    if (Array.isArray(value)) return Uint8Array.from(value as number[]);
    return undefined;
}

export function toFloat32Array(value: unknown): Float32Array {
    if (value instanceof Float32Array) return value;
    if (ArrayBuffer.isView(value)) {
        return new Float32Array(value.buffer, value.byteOffset, value.byteLength / 4);
    }
    if (value instanceof ArrayBuffer) return new Float32Array(value);
    if (Array.isArray(value)) return Float32Array.from(value as number[]);
    throw new Error("Invalid float array payload from backend");
}

export function toUint32Array(value: unknown): Uint32Array | undefined {
    if (value == null) return undefined;
    if (value instanceof Uint32Array) return value;
    if (ArrayBuffer.isView(value)) {
        return new Uint32Array(value.buffer, value.byteOffset, value.byteLength / 4);
    }
    if (value instanceof ArrayBuffer) return new Uint32Array(value);
    if (Array.isArray(value)) return Uint32Array.from(value as number[]);
    return undefined;
}

function decodeBase64Bytes(value: string): Uint8Array | undefined {
    try {
        const decoded = atob(value);
        const bytes = new Uint8Array(decoded.length);
        for (let index = 0; index < decoded.length; index++) {
            bytes[index] = decoded.charCodeAt(index);
        }
        return bytes;
    } catch {
        return undefined;
    }
}
