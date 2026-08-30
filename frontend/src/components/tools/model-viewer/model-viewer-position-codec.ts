export const MODEL_VIEWER_POSITION_CACHE_BYTES = 64 * 1024 * 1024;

export function decodeModelViewerPositions(
    source: ArrayBuffer,
    stride: number,
    sourceIndices: Uint32Array | undefined,
    vertexCount: number,
): Float32Array {
    if (!Number.isInteger(stride) || stride < 12) {
        throw new RangeError(`Invalid model viewer position stride: ${stride}`);
    }
    const count = sourceIndices?.length ?? vertexCount;
    if (!Number.isInteger(count) || count < 0) {
        throw new RangeError(`Invalid model viewer vertex count: ${count}`);
    }
    const view = new DataView(source);
    const positions = new Float32Array(count * 3);
    for (let outputIndex = 0; outputIndex < count; outputIndex++) {
        const sourceIndex = sourceIndices?.[outputIndex] ?? outputIndex;
        const offset = sourceIndex * stride;
        if (offset + 12 > source.byteLength) {
            continue;
        }
        positions[outputIndex * 3] = view.getFloat32(offset, true);
        positions[outputIndex * 3 + 1] = view.getFloat32(offset + 4, true);
        positions[outputIndex * 3 + 2] = view.getFloat32(offset + 8, true);
    }
    return positions;
}

export class ModelViewerByteLRU {
    readonly limit: number;
    private readonly entries = new Map<string, ArrayBuffer>();
    private bytes = 0;

    constructor(limit = MODEL_VIEWER_POSITION_CACHE_BYTES) {
        this.limit = limit;
    }

    get sizeBytes(): number {
        return this.bytes;
    }

    get(key: string): ArrayBuffer | undefined {
        const value = this.entries.get(key);
        if (!value) {
            return undefined;
        }
        this.entries.delete(key);
        this.entries.set(key, value);
        return value;
    }

    set(key: string, value: ArrayBuffer): void {
        const previous = this.entries.get(key);
        if (previous) {
            this.bytes -= previous.byteLength;
            this.entries.delete(key);
        }
        if (value.byteLength > this.limit) {
            return;
        }
        while (this.bytes + value.byteLength > this.limit) {
            const oldest = this.entries.keys().next().value as string | undefined;
            if (oldest === undefined) {
                break;
            }
            const removed = this.entries.get(oldest);
            this.entries.delete(oldest);
            this.bytes -= removed?.byteLength ?? 0;
        }
        this.entries.set(key, value);
        this.bytes += value.byteLength;
    }

    clear(): void {
        this.entries.clear();
        this.bytes = 0;
    }
}
