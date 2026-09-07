export class BinaryTransportError extends Error {
    constructor(
        message: string,
        readonly status?: number,
    ) {
        super(message);
        this.name = "BinaryTransportError";
    }
}

export async function fetchBinaryBytes(
    url: string,
    expectedBytes?: number,
    signal?: AbortSignal,
): Promise<Uint8Array> {
    const response = await fetch(url, { signal, cache: "no-store" });
    if (!response.ok) {
        throw new BinaryTransportError(
            `Failed to load binary buffer: ${url} (${response.status})`,
            response.status,
        );
    }
    const buffer = await response.arrayBuffer();
    if (expectedBytes !== undefined && buffer.byteLength !== expectedBytes) {
        throw new BinaryTransportError(
            `Invalid binary buffer length for ${url}: expected ${expectedBytes}, received ${buffer.byteLength}`,
        );
    }
    return new Uint8Array(buffer);
}

export async function fetchFloat32(
    url: string,
    expectedElements?: number,
    signal?: AbortSignal,
): Promise<Float32Array> {
    const bytes = await fetchBinaryBytes(
        url,
        expectedElements === undefined
            ? undefined
            : expectedElements * Float32Array.BYTES_PER_ELEMENT,
        signal,
    );
    if (bytes.byteLength % Float32Array.BYTES_PER_ELEMENT !== 0) {
        throw new BinaryTransportError(
            `Invalid float32 buffer length for ${url}: ${bytes.byteLength} is not 4-byte aligned`,
        );
    }
    return new Float32Array(bytes.buffer, bytes.byteOffset, bytes.byteLength / 4);
}

export async function fetchUint32(
    url: string,
    expectedElements?: number,
    signal?: AbortSignal,
): Promise<Uint32Array> {
    const bytes = await fetchBinaryBytes(
        url,
        expectedElements === undefined
            ? undefined
            : expectedElements * Uint32Array.BYTES_PER_ELEMENT,
        signal,
    );
    if (bytes.byteLength % Uint32Array.BYTES_PER_ELEMENT !== 0) {
        throw new BinaryTransportError(
            `Invalid uint32 buffer length for ${url}: ${bytes.byteLength} is not 4-byte aligned`,
        );
    }
    return new Uint32Array(bytes.buffer, bytes.byteOffset, bytes.byteLength / 4);
}

// WebView2 delivers at most ~2MB per intercepted request body. Stay well under
// that cap and copy each slice so fetch cannot send the backing ArrayBuffer.
export const binaryUploadChunkBytes = 512 * 1024;

export async function uploadTypedArray(
    url: string,
    value: ArrayBufferView<ArrayBufferLike>,
    signal?: AbortSignal,
): Promise<void> {
    const bytes = new Uint8Array(value.buffer, value.byteOffset, value.byteLength);
    let offset = 0;
    do {
        const chunk = bytes.slice(offset, offset + binaryUploadChunkBytes);
        const response = await fetch(url, {
            method: "PUT",
            headers: { "Content-Type": "application/octet-stream" },
            body: chunk as unknown as BodyInit,
            signal,
        });
        if (!response.ok) {
            throw new BinaryTransportError(
                `Failed to upload binary buffer: ${url} (${response.status})`,
                response.status,
            );
        }
        offset += chunk.byteLength;
    } while (offset < bytes.byteLength);
}

export function isAbortError(error: unknown): boolean {
    return error instanceof DOMException && error.name === "AbortError";
}
