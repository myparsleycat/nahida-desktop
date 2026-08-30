import type { ViewerMeshTransport } from "@shared/mod-viewer/types";

type PositionDescriptor = ViewerMeshTransport["positionVariants"][number];

type PositionWorker = Pick<Worker, "onmessage" | "onerror" | "postMessage" | "terminate">;

type PendingRequest = {
    resolve: (positions: Float32Array) => void;
    reject: (error: Error) => void;
    removeAbortListener?: () => void;
};

export interface PositionVariantLoader {
    load(
        descriptor: PositionDescriptor,
        sourceIndicesUrl: string | undefined,
        vertexCount: number,
        signal?: AbortSignal,
    ): Promise<Float32Array>;
}

export class ModelViewerPositionLoader implements PositionVariantLoader {
    private readonly worker: PositionWorker;
    private readonly pending = new Map<number, PendingRequest>();
    private nextId = 1;
    private disposed = false;

    constructor(
        worker: PositionWorker = new Worker(
            new URL("./model-viewer-position.worker.ts", import.meta.url),
            { type: "module" },
        ),
    ) {
        this.worker = worker;
        this.worker.onmessage = (event: MessageEvent) => {
            const message = event.data as
                | { type: "decoded"; id: number; positions: ArrayBuffer }
                | { type: "error"; id: number; message: string };
            const request = this.pending.get(message.id);
            if (!request) {
                return;
            }
            this.pending.delete(message.id);
            request.removeAbortListener?.();
            if (message.type === "decoded") {
                request.resolve(new Float32Array(message.positions));
            } else {
                request.reject(new Error(message.message));
            }
        };
        this.worker.onerror = (event) => {
            if (this.disposed) {
                return;
            }
            this.disposed = true;
            this.rejectAll(new Error(event.message || "Model viewer position worker failed"));
            this.worker.terminate();
        };
    }

    load(
        descriptor: PositionDescriptor,
        sourceIndicesUrl: string | undefined,
        vertexCount: number,
        signal?: AbortSignal,
    ): Promise<Float32Array> {
        if (this.disposed) {
            return Promise.reject(new Error("Model viewer position loader is disposed"));
        }
        if (signal?.aborted) {
            return Promise.reject(abortError());
        }
        const id = this.nextId++;
        return new Promise<Float32Array>((resolve, reject) => {
            const abort = () => {
                if (!this.pending.delete(id)) {
                    return;
                }
                this.worker.postMessage({ type: "cancel", id });
                reject(abortError());
            };
            this.pending.set(id, {
                resolve,
                reject,
                removeAbortListener: signal
                    ? () => signal.removeEventListener("abort", abort)
                    : undefined,
            });
            signal?.addEventListener("abort", abort, { once: true });
            this.worker.postMessage({
                type: "decode",
                id,
                sourceUrl: descriptor.sourceUrl,
                sourceBytes: descriptor.sourceBytes,
                sourceIndicesUrl,
                stride: descriptor.stride,
                vertexCount,
            });
        });
    }

    dispose(): void {
        if (this.disposed) {
            return;
        }
        this.disposed = true;
        this.rejectAll(abortError());
        this.worker.terminate();
    }

    private rejectAll(error: Error): void {
        for (const request of this.pending.values()) {
            request.removeAbortListener?.();
            request.reject(error);
        }
        this.pending.clear();
    }
}

function abortError(): Error {
    return new DOMException("Model viewer position request was cancelled", "AbortError");
}
