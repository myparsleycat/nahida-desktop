import { describe, expect, it, vi } from "vitest";

import { ModelViewerPositionLoader } from "./model-viewer-position-loader";

class FakeWorker {
    onmessage: Worker["onmessage"] = null;
    onerror: Worker["onerror"] = null;
    messages: unknown[] = [];
    terminate = vi.fn();

    postMessage(message: unknown) {
        this.messages.push(message);
    }
}

const descriptor = {
    conditions: [],
    sourceUrl: "/source.buf",
    stride: 40,
    sourceBytes: 80,
};

describe("ModelViewerPositionLoader", () => {
    it("resolves the request matching the decoded worker message id", async () => {
        const worker = new FakeWorker();
        const loader = new ModelViewerPositionLoader(worker);
        const request = loader.load(descriptor, undefined, 2);
        const message = worker.messages[0] as { id: number };
        const positions = new Float32Array([1, 2, 3, 4, 5, 6]);

        worker.onmessage?.(
            new MessageEvent("message", {
                data: { type: "decoded", id: message.id, positions: positions.buffer },
            }),
        );

        await expect(request).resolves.toEqual(positions);
        loader.dispose();
    });

    it("cancels pending work and ignores a late worker result", async () => {
        const worker = new FakeWorker();
        const loader = new ModelViewerPositionLoader(worker);
        const controller = new AbortController();
        const request = loader.load(descriptor, "/indices", 2, controller.signal);
        controller.abort();

        await expect(request).rejects.toMatchObject({ name: "AbortError" });
        expect(worker.messages).toEqual([
            expect.objectContaining({ type: "decode", sourceUrl: "/source.buf" }),
            expect.objectContaining({ type: "cancel" }),
        ]);
        worker.onmessage?.(
            new MessageEvent("message", {
                data: { type: "decoded", id: 1, positions: new Float32Array(6).buffer },
            }),
        );
        loader.dispose();
        expect(worker.terminate).toHaveBeenCalledOnce();
    });

    it("terminates the worker and rejects every pending request on dispose", async () => {
        const worker = new FakeWorker();
        const loader = new ModelViewerPositionLoader(worker);
        const request = loader.load(descriptor, undefined, 2);
        loader.dispose();

        await expect(request).rejects.toMatchObject({ name: "AbortError" });
        expect(worker.terminate).toHaveBeenCalledOnce();
    });

    it("becomes terminal when the worker itself fails", async () => {
        const worker = new FakeWorker();
        const loader = new ModelViewerPositionLoader(worker);
        const pending = loader.load(descriptor, undefined, 2);
        worker.onerror?.({ message: "worker initialization failed" } as ErrorEvent);

        await expect(pending).rejects.toThrow("worker initialization failed");
        await expect(loader.load(descriptor, undefined, 2)).rejects.toThrow(
            "Model viewer position loader is disposed",
        );
        expect(worker.terminate).toHaveBeenCalledOnce();
    });
});
