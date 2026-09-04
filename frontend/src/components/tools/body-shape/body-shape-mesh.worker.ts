import {
    processBodyShapeMesh,
    type BodyShapeMeshProcessInput,
    type BodyShapeMeshProcessResult,
} from "./body-shape-mesh-worker";

type ProcessRequest = { type: "process"; id: number; input: BodyShapeMeshProcessInput };
type CancelRequest = { type: "cancel"; id: number };

const scope = self as unknown as {
    onmessage: ((event: MessageEvent<ProcessRequest | CancelRequest>) => void) | null;
    postMessage(message: unknown, transfer?: Transferable[]): void;
};
const cancelled = new Set<number>();
const queued = new Set<number>();
let active: { id: number; controller: AbortController } | undefined;
let queue = Promise.resolve();

scope.onmessage = (event) => {
    const message = event.data;
    if (message.type === "cancel") {
        if (active?.id === message.id) active.controller.abort();
        else if (queued.has(message.id)) cancelled.add(message.id);
        return;
    }
    queued.add(message.id);
    queue = queue.then(() => processRequest(message));
};

async function processRequest(request: ProcessRequest): Promise<void> {
    queued.delete(request.id);
    if (cancelled.delete(request.id)) return;
    const controller = new AbortController();
    active = { id: request.id, controller };
    try {
        const result = await processBodyShapeMesh(request.input, controller.signal);
        if (cancelled.delete(request.id)) return;
        scope.postMessage({ type: "processed", id: request.id, result }, transferResult(result));
    } catch (error) {
        if (!controller.signal.aborted && !cancelled.delete(request.id)) {
            scope.postMessage({
                type: "error",
                id: request.id,
                message: error instanceof Error ? error.message : String(error),
            });
        }
    } finally {
        if (active?.id === request.id) active = undefined;
        cancelled.delete(request.id);
    }
}

function transferResult(result: BodyShapeMeshProcessResult): Transferable[] {
    return [
        result.originalPositions,
        result.indices,
        result.blendBytes,
        result.adjacencyOffsets,
        result.adjacencyNeighbors,
        result.originalNormals,
        result.componentIds,
        result.symmetryMap,
    ].filter((value): value is ArrayBuffer => value instanceof ArrayBuffer);
}
