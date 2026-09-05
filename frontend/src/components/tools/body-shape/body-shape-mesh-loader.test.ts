import { describe, expect, it, vi } from "vitest";

import type {
    BodyShapeMeshProcessInput,
    BodyShapeMeshProcessResult,
} from "./body-shape-mesh-worker";

import { BodyShapeMeshWorkerClient } from "./body-shape-mesh-loader";

class FakeWorker {
    onmessage: Worker["onmessage"] = null;
    onerror: Worker["onerror"] = null;
    messages: unknown[] = [];
    terminate = vi.fn();

    postMessage(message: unknown) {
        this.messages.push(message);
    }
}

const input: BodyShapeMeshProcessInput = {
    sessionId: "session",
    meshId: "mesh",
    vertexCount: 1,
    positionsUrl: "/positions",
    positionsCount: 3,
    indexCount: 0,
    blendBytes: 0,
    blendStride: 32,
};

function result(): BodyShapeMeshProcessResult {
    return {
        originalPositions: new Float32Array([1, 2, 3]).buffer,
        symmetryMap: new Int32Array([0]).buffer,
        boundingCenter: [1, 2, 3],
    };
}

describe("BodyShapeMeshWorkerClient", () => {
    it("resolves only the request with the matching id", async () => {
        const worker = new FakeWorker();
        const client = new BodyShapeMeshWorkerClient(worker);
        const first = client.process(input);
        const second = client.process({ ...input, meshId: "mesh-2" });
        const firstMessage = worker.messages[0] as { id: number };
        const secondMessage = worker.messages[1] as { id: number };

        worker.onmessage?.(
            new MessageEvent("message", {
                data: { type: "processed", id: secondMessage.id, result: result() },
            }),
        );
        await expect(second).resolves.toMatchObject({ boundingCenter: [1, 2, 3] });

        worker.onmessage?.(
            new MessageEvent("message", {
                data: { type: "processed", id: firstMessage.id, result: result() },
            }),
        );
        const loaded = await first;
        expect([...new Float32Array(loaded.originalPositions)]).toEqual([1, 2, 3]);
        client.dispose();
    });

    it("rejects an aborted request and ignores its late result", async () => {
        const worker = new FakeWorker();
        const client = new BodyShapeMeshWorkerClient(worker);
        const controller = new AbortController();
        const pending = client.process(input, controller.signal);
        controller.abort();

        await expect(pending).rejects.toMatchObject({ name: "AbortError" });
        expect(worker.messages).toEqual([
            expect.objectContaining({ type: "process" }),
            expect.objectContaining({ type: "cancel" }),
        ]);
        worker.onmessage?.(
            new MessageEvent("message", {
                data: { type: "processed", id: 1, result: result() },
            }),
        );
        client.dispose();
    });

    it("rejects pending requests on dispose", async () => {
        const worker = new FakeWorker();
        const client = new BodyShapeMeshWorkerClient(worker);
        const pending = client.process(input);
        client.dispose();

        await expect(pending).rejects.toMatchObject({ name: "AbortError" });
        expect(worker.terminate).toHaveBeenCalledOnce();
    });

    it("propagates worker failures and becomes terminal", async () => {
        const worker = new FakeWorker();
        const client = new BodyShapeMeshWorkerClient(worker);
        const pending = client.process(input);
        worker.onerror?.({ message: "mesh worker failed" } as ErrorEvent);

        await expect(pending).rejects.toThrow("mesh worker failed");
        await expect(client.process(input)).rejects.toThrow("disposed");
        expect(worker.terminate).toHaveBeenCalledOnce();
    });
    it("retains worker diagnostics without changing its display message", async () => {
        const worker = new FakeWorker();
        const client = new BodyShapeMeshWorkerClient(worker);
        const pending = client.process(input);
        const diagnostic = {
            stage: "decode",
            error: { name: "TypeError", message: "invalid buffer", stack: "worker stack" },
        };
        worker.onmessage?.(
            new MessageEvent("message", {
                data: { type: "error", id: 1, message: "decode failed", diagnostic },
            }),
        );
        await expect(pending).rejects.toMatchObject({
            message: "decode failed",
            cause: diagnostic,
        });
        client.dispose();
    });
});
