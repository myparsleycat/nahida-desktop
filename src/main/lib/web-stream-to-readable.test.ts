import { once } from "node:events";

import { describe, expect, it, vi } from "vitest";

import { drainWebStream, webStreamToNodeReadable } from "./web-stream-to-readable";

function createWebStream(chunks: Uint8Array[]) {
    let index = 0;
    const cancel = vi.fn(async () => {});
    const releaseLock = vi.fn();

    return {
        stream: {
            getReader: () => ({
                read: async () => ({
                    done: index >= chunks.length,
                    value: chunks[index++],
                }),
                cancel,
                releaseLock,
            }),
        },
        cancel,
        releaseLock,
    };
}

describe("drainWebStream", () => {
    it("drains an unsuccessful response without cancelling it", async () => {
        const source = createWebStream([new TextEncoder().encode("error")]);

        await drainWebStream(source.stream);

        expect(source.cancel).not.toHaveBeenCalled();
        expect(source.releaseLock).toHaveBeenCalledOnce();
    });

    it("cancels an oversized response body while draining", async () => {
        const source = createWebStream([new Uint8Array(1024 * 1024 + 1)]);

        await drainWebStream(source.stream);

        expect(source.cancel).toHaveBeenCalledOnce();
        expect(source.releaseLock).toHaveBeenCalledOnce();
    });
});

describe("webStreamToNodeReadable", () => {
    it("releases a completed response without cancelling it", async () => {
        const source = createWebStream([new TextEncoder().encode("hello")]);
        const readable = webStreamToNodeReadable(source.stream);
        const chunks: Buffer[] = [];

        readable.on("data", (chunk: Buffer) => chunks.push(chunk));
        await once(readable, "end");

        expect(Buffer.concat(chunks).toString()).toBe("hello");
        expect(source.cancel).not.toHaveBeenCalled();
        expect(source.releaseLock).toHaveBeenCalledOnce();
    });

    it("serializes reads while forwarding every chunk", async () => {
        const source = createWebStream([
            new TextEncoder().encode("one"),
            new TextEncoder().encode("two"),
            new TextEncoder().encode("three"),
        ]);
        const readable = webStreamToNodeReadable(source.stream);
        const chunks: Buffer[] = [];

        readable.on("data", (chunk: Buffer) => chunks.push(chunk));
        await once(readable, "end");

        expect(Buffer.concat(chunks).toString()).toBe("onetwothree");
    });

    it("cancels an incomplete response when the node stream is destroyed", async () => {
        const source = createWebStream([]);
        const readable = webStreamToNodeReadable(source.stream);

        readable.destroy();
        await once(readable, "close");

        expect(source.cancel).toHaveBeenCalledOnce();
    });

    it("propagates an abort signal to the node stream", async () => {
        const cancel = vi.fn(async () => {});
        const releaseLock = vi.fn();
        const controller = new AbortController();
        const readable = webStreamToNodeReadable(
            {
                getReader: () => ({
                    read: () => new Promise<never>(() => {}),
                    cancel,
                    releaseLock,
                }),
            },
            controller.signal,
        );
        const errorPromise = once(readable, "error");

        readable.resume();
        controller.abort();

        const [error] = await errorPromise;
        expect((error as Error).name).toBe("AbortError");
        expect(cancel).not.toHaveBeenCalled();
    });

    it("propagates reader errors to the node stream", async () => {
        const readable = webStreamToNodeReadable({
            getReader: () => ({
                read: async () => {
                    throw new Error("boom");
                },
                cancel: async () => {},
                releaseLock: () => {},
            }),
        });
        const errorPromise = once(readable, "error");

        readable.resume();

        const [error] = await errorPromise;
        expect((error as Error).message).toBe("boom");
    });
});
