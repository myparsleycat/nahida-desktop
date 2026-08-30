import { decodeModelViewerPositions, ModelViewerByteLRU } from "./model-viewer-position-codec";

type DecodeRequest = {
    type: "decode";
    id: number;
    sourceUrl: string;
    sourceBytes: number;
    sourceIndicesUrl?: string;
    stride: number;
    vertexCount: number;
};

type CancelRequest = { type: "cancel"; id: number };

type WorkerScope = {
    onmessage: ((event: MessageEvent<DecodeRequest | CancelRequest>) => void) | null;
    postMessage(message: unknown, transfer?: Transferable[]): void;
};

const scope = self as unknown as WorkerScope;
const cache = new ModelViewerByteLRU();
const cancelled = new Set<number>();
const queued = new Set<number>();
let active: { id: number; controller: AbortController } | undefined;
let queue = Promise.resolve();

scope.onmessage = (event) => {
    const message = event.data;
    if (message.type === "cancel") {
        if (active?.id === message.id) {
            active.controller.abort();
            return;
        }
        if (queued.has(message.id)) {
            cancelled.add(message.id);
        }
        return;
    }
    queued.add(message.id);
    queue = queue.then(() => decodeRequest(message));
};

async function decodeRequest(request: DecodeRequest): Promise<void> {
    queued.delete(request.id);
    if (cancelled.delete(request.id)) {
        return;
    }
    const controller = new AbortController();
    active = { id: request.id, controller };
    try {
        const source = await fetchCached(request.sourceUrl, controller.signal);
        if (source.byteLength !== request.sourceBytes) {
            throw new Error(
                `Position source size changed: expected ${request.sourceBytes}, received ${source.byteLength}`,
            );
        }
        const sourceIndices = request.sourceIndicesUrl
            ? new Uint32Array(await fetchCached(request.sourceIndicesUrl, controller.signal))
            : undefined;
        if (cancelled.delete(request.id)) {
            return;
        }
        const positions = decodeModelViewerPositions(
            source,
            request.stride,
            sourceIndices,
            request.vertexCount,
        );
        scope.postMessage({ type: "decoded", id: request.id, positions: positions.buffer }, [
            positions.buffer,
        ]);
    } catch (error) {
        if (!controller.signal.aborted && !cancelled.delete(request.id)) {
            scope.postMessage({
                type: "error",
                id: request.id,
                message: error instanceof Error ? error.message : String(error),
            });
        }
    } finally {
        if (active?.id === request.id) {
            active = undefined;
        }
        cancelled.delete(request.id);
    }
}

async function fetchCached(url: string, signal: AbortSignal): Promise<ArrayBuffer> {
    const cached = cache.get(url);
    if (cached) {
        return cached;
    }
    const response = await fetch(url, { cache: "no-store", signal });
    if (!response.ok) {
        throw new Error(`Failed to load model viewer position source (${response.status})`);
    }
    const buffer = await response.arrayBuffer();
    cache.set(url, buffer);
    return buffer;
}
