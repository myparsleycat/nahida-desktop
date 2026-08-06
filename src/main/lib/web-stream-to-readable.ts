import { Readable } from "node:stream";

type WebReadableStream = {
    getReader(): {
        read(): Promise<{ done: boolean; value?: Uint8Array<ArrayBufferLike> }>;
        cancel(reason?: unknown): Promise<void>;
        releaseLock?(): void;
    };
};

export async function drainWebStream(
    webStream: WebReadableStream | null | undefined,
    _signal?: AbortSignal,
) {
    const reader = webStream?.getReader();
    if (!reader) {
        return;
    }

    try {
        // Always consume the response before releasing the reader. Releasing a
        // locked reader early can leave Undici's HTTP parser paused.
        while (true) {
            const { done } = await reader.read();
            if (done) {
                return;
            }
        }
    } catch {
        // The request may already have been aborted while draining.
    } finally {
        reader.releaseLock?.();
    }
}

export function webStreamToNodeReadable(
    webStream: WebReadableStream,
    signal?: AbortSignal,
): Readable {
    const reader = webStream.getReader();
    let ended = false;
    let lockReleased = false;
    let cancelPromise: Promise<void> | undefined;

    const releaseReader = () => {
        if (lockReleased) {
            return;
        }
        lockReleased = true;
        reader.releaseLock?.();
    };

    const cancelReader = (reason?: unknown): Promise<void> => {
        if (ended) {
            releaseReader();
            return Promise.resolve();
        }
        if (!cancelPromise) {
            cancelPromise = reader
                .cancel(reason)
                .catch(() => {})
                .then(() => {
                    releaseReader();
                });
        }
        return cancelPromise;
    };

    const pull = async (target: Readable) => {
        try {
            while (!target.destroyed) {
                const { done, value } = await reader.read();
                if (done) {
                    ended = true;
                    releaseReader();
                    target.push(null);
                    return;
                }
                if (value && value.byteLength > 0) {
                    target.push(Buffer.from(value));
                    return;
                }
            }
        } catch (err) {
            target.destroy(err instanceof Error ? err : new Error(String(err)));
        }
    };

    const readable = new Readable({
        read() {
            void pull(this);
        },
        destroy(err, callback) {
            void cancelReader(err).then(
                () => callback(err),
                () => callback(err),
            );
        },
    });

    if (signal) {
        const onAbort = () => {
            readable.destroy(new DOMException("The operation was aborted.", "AbortError"));
        };
        signal.addEventListener("abort", onAbort, { once: true });
        readable.once("close", () => {
            signal.removeEventListener("abort", onAbort);
        });
    }

    return readable;
}
