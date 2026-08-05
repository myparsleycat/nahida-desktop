import { Readable } from "node:stream";

type WebReadableStream = {
    getReader(): {
        read(): Promise<{ done: boolean; value?: Uint8Array<ArrayBuffer> }>;
        cancel(reason?: unknown): Promise<void>;
    };
};

export function webStreamToNodeReadable(
    webStream: WebReadableStream,
    signal?: AbortSignal,
): Readable {
    const reader = webStream.getReader();

    const readable = new Readable({
        async read() {
            try {
                const { done, value } = await reader.read();
                if (done) {
                    this.push(null);
                    return;
                }
                if (value) {
                    this.push(Buffer.from(value));
                }
            } catch (err) {
                this.destroy(err instanceof Error ? err : new Error(String(err)));
            }
        },
    });

    const cleanup = () => {
        void reader.cancel().catch(() => {});
    };

    if (signal) {
        const onAbort = () => {
            cleanup();
            readable.destroy(new DOMException("The operation was aborted.", "AbortError"));
        };
        signal.addEventListener("abort", onAbort, { once: true });
        readable.once("close", () => {
            signal.removeEventListener("abort", onAbort);
            cleanup();
        });
    } else {
        readable.once("close", cleanup);
    }

    return readable;
}
