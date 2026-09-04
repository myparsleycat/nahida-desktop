import type {
    BodyShapeMeshProcessInput,
    BodyShapeMeshProcessResult,
} from "./body-shape-mesh-worker";

type MeshWorker = Pick<Worker, "onmessage" | "onerror" | "postMessage" | "terminate">;
type PendingRequest = {
    resolve: (result: BodyShapeMeshProcessResult) => void;
    reject: (error: Error) => void;
    removeAbortListener?: () => void;
};

export class BodyShapeMeshWorkerClient {
    private readonly worker: MeshWorker;
    private readonly pending = new Map<number, PendingRequest>();
    private nextId = 1;
    private disposed = false;

    constructor(
        worker: MeshWorker = new Worker(new URL("./body-shape-mesh.worker.ts", import.meta.url), {
            type: "module",
        }),
    ) {
        this.worker = worker;
        this.worker.onmessage = (event) => {
            const message = event.data as
                | { type: "processed"; id: number; result: BodyShapeMeshProcessResult }
                | { type: "error"; id: number; message: string };
            const request = this.pending.get(message.id);
            if (!request) return;
            this.pending.delete(message.id);
            request.removeAbortListener?.();
            if (message.type === "processed") request.resolve(message.result);
            else request.reject(new Error(message.message));
        };
        this.worker.onerror = (event) => {
            if (this.disposed) return;
            this.disposed = true;
            this.rejectAll(new Error(event.message || "Body shape mesh worker failed"));
            this.worker.terminate();
        };
    }

    process(input: BodyShapeMeshProcessInput, signal?: AbortSignal) {
        if (this.disposed) return Promise.reject(new Error("Body shape mesh worker is disposed"));
        if (signal?.aborted) return Promise.reject(abortError());
        const id = this.nextId++;
        return new Promise<BodyShapeMeshProcessResult>((resolve, reject) => {
            const abort = () => {
                if (!this.pending.delete(id)) return;
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
            this.worker.postMessage({ type: "process", id, input });
        });
    }

    dispose(): void {
        if (this.disposed) return;
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
    return new DOMException("Body shape mesh request was cancelled", "AbortError");
}
