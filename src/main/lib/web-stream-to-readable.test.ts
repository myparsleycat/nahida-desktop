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

describe("webStreamToNodeReadable", () => {
    it("drains an unsuccessful response without cancelling it", async () => {
        const source = createWebStream([new TextEncoder().encode("error")]);

        await drainWebStream(source.stream);

        expect(source.cancel).not.toHaveBeenCalled();
        expect(source.releaseLock).toHaveBeenCalledOnce();
    });

    it("drains an aborted response before releasing its reader", async () => {
        const source = createWebStream([new TextEncoder().encode("error")]);
        const signal = AbortSignal.abort();

        await drainWebStream(source.stream, signal);

        expect(source.cancel).not.toHaveBeenCalled();
        expect(source.releaseLock).toHaveBeenCalledOnce();
    });

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

    it("cancels an incomplete response when the node stream is destroyed", async () => {
        const source = createWebStream([]);
        const readable = webStreamToNodeReadable(source.stream);

        readable.destroy();
        await once(readable, "close");

        expect(source.cancel).toHaveBeenCalledOnce();
    });
});
