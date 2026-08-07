import PQueue from "p-queue";

const DOWNLOAD_REQUEST_CONCURRENCY = 16;

/**
 * Limits active download streams across files and parallel byte ranges.
 * File-level concurrency and range-level concurrency otherwise multiply.
 */
export class DownloadRequestLimiter {
    private readonly queue = new PQueue({ concurrency: DOWNLOAD_REQUEST_CONCURRENCY });

    public run<T>(task: () => Promise<T>, signal?: AbortSignal) {
        return this.queue.add(task, signal ? { signal } : undefined) as Promise<T>;
    }
}

export const downloadRequestLimiter = new DownloadRequestLimiter();
