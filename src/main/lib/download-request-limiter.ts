import PQueue from "p-queue";

const DOWNLOAD_REQUEST_CONCURRENCY_DEFAULT = 32;
const CUSTOM_DOWNLOAD_REQUEST_CONCURRENCY = 16;
const RANGE_PROBE_CONCURRENCY = 4;

/**
 * Limits concurrent HTTP download requests across files and parallel byte ranges.
 * File-level concurrency and range-level concurrency otherwise multiply.
 */
export class DownloadRequestLimiter {
    private readonly queue: PQueue;

    public constructor(concurrency = DOWNLOAD_REQUEST_CONCURRENCY_DEFAULT) {
        this.queue = new PQueue({ concurrency });
    }

    public setConcurrency(concurrency: number) {
        this.queue.concurrency = concurrency;
    }

    public run<T>(task: () => Promise<T>, signal?: AbortSignal) {
        return this.queue.add(task, signal ? { signal } : undefined) as Promise<T>;
    }
}

export const downloadRequestLimiter = new DownloadRequestLimiter();
export const customDownloadRequestLimiter = new DownloadRequestLimiter(
    CUSTOM_DOWNLOAD_REQUEST_CONCURRENCY,
);
export const rangeProbeLimiter = new DownloadRequestLimiter(RANGE_PROBE_CONCURRENCY);
