import { Transform } from "node:stream";
import { pipeline } from "node:stream/promises";

// oxlint-disable typescript/no-explicit-any
import fse from "fs-extra";
import ky from "ky";

import type { BandwidthLimiter } from "./bandwidth-limiter";

import { networkFetch } from "../internal/network-fetch";
import { createBandwidthLimitTransform } from "./bandwidth-limit-stream";
import {
    downloadRequestLimiter,
    rangeProbeLimiter,
    type DownloadRequestLimiter,
} from "./download-request-limiter";
import {
    isAbortError,
    SLOW_CHUNK_MAX_RECONNECTS,
    sleepWithAbort,
    slowReconnectDelayMs,
    type SlowChunkMonitor,
} from "./slow-chunk-monitor";
import { drainWebStream, webStreamToNodeReadable } from "./web-stream-to-readable";

export interface ParallelDownloadOptions {
    url: string;
    savePath: string;
    fileSize: number;
    headers?: Record<string, string>;
    signal?: AbortSignal;
    onProgress?: (bytes: number) => void;
    chunkSize?: number;
    maxChunks?: number;
    adaptive?: boolean;
    bandwidthLimiter?: BandwidthLimiter;
    slowChunkMonitor?: SlowChunkMonitor;
    fileId?: string;
    cohortKey?: string;
}

const RANGE_SUPPORT_CACHE_TTL_MS = 5 * 60 * 1000;

type SegmentStatus = "pending" | "running" | "completed";

type Segment = {
    id: number;
    start: number;
    end: number;
    chunkPath: string;
    status: SegmentStatus;
    transferredBytes: number;
    reportedBytes: number;
    splitRequested: boolean;
    controller?: AbortController;
};

const createAbortError = (message = "Aborted") => {
    const error = new Error(message);
    error.name = "AbortError";
    return error;
};

export class ParallelDownloader {
    private readonly minSegmentSize = 4 * 1024 * 1024;
    private readonly requestLimiter: DownloadRequestLimiter;
    private readonly rangeProbeLimiter: DownloadRequestLimiter;
    private readonly rangeSupportCache = new Map<string, { expiresAt: number; value: boolean }>();

    constructor(
        private options: {
            logger?: {
                info: (msg: string, ...args: any[]) => void;
                warn: (msg: string, ...args: any[]) => void;
            };
            getHeaders: (url: string) => Promise<Record<string, string>>;
            requestLimiter?: DownloadRequestLimiter;
            rangeProbeLimiter?: DownloadRequestLimiter;
        },
    ) {
        this.requestLimiter = options.requestLimiter ?? downloadRequestLimiter;
        this.rangeProbeLimiter = options.rangeProbeLimiter ?? rangeProbeLimiter;
    }

    public async checkRangeSupport(
        url: string,
        headers?: Record<string, string>,
        signal?: AbortSignal,
    ): Promise<boolean> {
        const cacheKey = getRangeSupportCacheKey(url);
        const cached = this.rangeSupportCache.get(cacheKey);
        if (cached && cached.expiresAt > Date.now()) return cached.value;

        const rangeSupported = await this.requestRangeSupport(url, headers, signal);
        if (rangeSupported) {
            this.rangeSupportCache.set(cacheKey, {
                expiresAt: Date.now() + RANGE_SUPPORT_CACHE_TTL_MS,
                value: true,
            });
        }
        return rangeSupported;
    }

    private async requestRangeSupport(
        url: string,
        headers?: Record<string, string>,
        signal?: AbortSignal,
    ) {
        try {
            const response = await this.rangeProbeLimiter.run(
                async () =>
                    ky.head(url, {
                        headers: {
                            ...(await this.options.getHeaders(url)),
                            ...headers,
                        },
                        fetch: networkFetch,
                        signal,
                        timeout: 10000,
                        throwHttpErrors: false,
                        retry: 0,
                    }),
                signal,
            );

            const acceptRanges = response.headers.get("Accept-Ranges");
            return acceptRanges === "bytes";
        } catch (error) {
            if (signal?.aborted) throw error;
            return false;
        }
    }

    private calculateChunkCount(sizeInBytes: number, maxChunks?: number): number {
        const sizeInMB = sizeInBytes / (1024 * 1024);

        if (sizeInMB < 1) return 1;

        const log10 = Math.floor(Math.log10(sizeInMB));
        const firstDigit = Math.floor(sizeInMB / 10 ** log10);

        let count = Math.max(2, firstDigit);

        if (maxChunks && maxChunks > 0) {
            count = Math.min(count, maxChunks);
        }

        return count;
    }

    private calculateSegmentSize(fileSize: number, concurrency: number): number {
        const targetSegmentCount = Math.max(concurrency, concurrency * 4);
        return Math.max(this.minSegmentSize, Math.ceil(fileSize / targetSegmentCount));
    }

    private async downloadChunk({
        url,
        headers,
        start,
        end,
        chunkPath,
        signal,
        onProgress,
        preservePartialOnAbort,
        bandwidthLimiter,
        onPhaseChange,
    }: {
        url: string;
        headers?: Record<string, string>;
        start: number;
        end: number;
        chunkPath: string;
        signal?: AbortSignal;
        onProgress?: (transferredBytes: number, incrementalBytes: number) => void;
        preservePartialOnAbort?: () => boolean;
        bandwidthLimiter?: BandwidthLimiter;
        onPhaseChange?: (phase: "network" | "bandwidth-wait") => void;
    }): Promise<void> {
        const requestHeaders: Record<string, string> = {
            ...(await this.options.getHeaders(url)),
            ...headers,
            Range: `bytes=${start}-${end}`,
        };

        await this.requestLimiter.run(
            async () => {
                const response = await ky(url, {
                    headers: requestHeaders,
                    fetch: networkFetch,
                    signal,
                    throwHttpErrors: false,
                    timeout: 100000,
                    retry: 0,
                });

                if (response.status !== 206) {
                    await drainWebStream(response.body, signal).catch(() => {});
                    throw new Error(
                        `Chunk download failed: expected 206 Partial Content, got ${response.statusText} (${response.status})`,
                    );
                }

                if (!response.body) throw new Error("No response body");

                const fileStream = fse.createWriteStream(chunkPath);
                let transferredBytes = 0;
                const source = webStreamToNodeReadable(response.body, signal);
                const progressStream = new Transform({
                    transform(chunk: Buffer, _encoding, callback) {
                        transferredBytes += chunk.byteLength;
                        onProgress?.(transferredBytes, chunk.byteLength);
                        callback(null, chunk);
                    },
                });

                try {
                    if (bandwidthLimiter) {
                        await pipeline(
                            source,
                            createBandwidthLimitTransform(bandwidthLimiter, {
                                signal,
                                onPhaseChange,
                            }),
                            progressStream,
                            fileStream,
                            { signal },
                        );
                    } else {
                        await pipeline(source, progressStream, fileStream, { signal });
                    }
                } catch (pipeErr) {
                    fileStream.destroy();
                    if (!preservePartialOnAbort?.()) {
                        await fse.remove(chunkPath).catch(() => {});
                    }
                    throw pipeErr;
                }
            },
            signal,
        );
    }

    private async combineChunks({
        segments,
        targetPath,
        signal,
    }: {
        segments: Segment[];
        targetPath: string;
        signal?: AbortSignal;
    }): Promise<void> {
        const fileStream = fse.createWriteStream(targetPath);
        const orderedSegments = [...segments].sort((a, b) => a.start - b.start);

        try {
            for (const segment of orderedSegments) {
                if (signal?.aborted) throw new Error("Aborted during chunk combination");

                const chunkStream = fse.createReadStream(segment.chunkPath);
                await new Promise<void>((resolve, reject) => {
                    let settled = false;
                    const settle = (error?: Error) => {
                        if (settled) return;
                        settled = true;
                        chunkStream.off("end", onEnd);
                        chunkStream.off("error", onError);
                        fileStream.off("error", onError);

                        if (error) {
                            reject(error);
                            return;
                        }
                        resolve();
                    };
                    const onEnd = () => settle();
                    const onError = (error: Error) => settle(error);

                    chunkStream.on("end", onEnd);
                    chunkStream.on("error", onError);
                    fileStream.on("error", onError);
                    chunkStream.pipe(fileStream, { end: false });
                });
            }

            fileStream.end();
            await new Promise<void>((resolve, reject) => {
                fileStream.on("finish", resolve);
                fileStream.on("error", reject);
            });

            await Promise.all(
                orderedSegments.map((segment) => fse.remove(segment.chunkPath).catch(() => {})),
            );
        } catch (err) {
            fileStream.destroy();
            await Promise.all(
                orderedSegments.map((segment) => fse.remove(segment.chunkPath).catch(() => {})),
            );
            throw err;
        }
    }

    private createSchedulerSignal() {
        const waiters = new Set<() => void>();
        let version = 0;

        return {
            notify() {
                version++;
                for (const resolve of waiters) {
                    resolve();
                }
                waiters.clear();
            },
            getVersion() {
                return version;
            },
            wait(lastSeenVersion: number, signal?: AbortSignal) {
                if (signal?.aborted) {
                    return Promise.resolve();
                }
                if (version !== lastSeenVersion) {
                    return Promise.resolve();
                }

                return new Promise<void>((resolve) => {
                    const onAbort = () => {
                        waiters.delete(onReady);
                        resolve();
                    };
                    const onReady = () => {
                        signal?.removeEventListener("abort", onAbort);
                        resolve();
                    };

                    waiters.add(onReady);
                    signal?.addEventListener("abort", onAbort, { once: true });
                });
            },
        };
    }

    private async getCompletedBytes(segment: Segment): Promise<number> {
        try {
            const stat = await fse.stat(segment.chunkPath);
            return Math.min(stat.size, segment.end - segment.start + 1);
        } catch {
            return Math.min(segment.transferredBytes, segment.end - segment.start + 1);
        }
    }

    private async splitSegmentForRebalance({
        segment,
        segments,
        tempChunkPaths,
        savePath,
        nextSegmentId,
        onProgressAdjustment,
    }: {
        segment: Segment;
        segments: Segment[];
        tempChunkPaths: Set<string>;
        savePath: string;
        nextSegmentId: () => number;
        onProgressAdjustment?: (bytes: number) => void;
    }): Promise<void> {
        const completedBytes = await this.getCompletedBytes(segment);
        const originalEnd = segment.end;
        const remainingStart = segment.start + completedBytes;
        const remainingBytes = originalEnd - remainingStart + 1;
        const progressCorrection = completedBytes - segment.reportedBytes;

        segment.controller = undefined;
        segment.splitRequested = false;
        segment.transferredBytes = completedBytes;
        segment.reportedBytes = completedBytes;

        if (progressCorrection !== 0) {
            onProgressAdjustment?.(progressCorrection);
        }

        if (completedBytes > 0) {
            segment.end = remainingStart - 1;
            segment.status = "completed";
        } else {
            segment.status = "completed";
            segment.end = segment.start - 1;
            await fse.remove(segment.chunkPath).catch(() => {});
            tempChunkPaths.delete(segment.chunkPath);
        }

        if (remainingBytes <= 0) {
            return;
        }

        const midpoint =
            remainingBytes >= this.minSegmentSize * 2
                ? remainingStart + Math.ceil(remainingBytes / 2) - 1
                : originalEnd;

        const newRanges =
            midpoint < originalEnd
                ? [
                      { start: remainingStart, end: midpoint },
                      { start: midpoint + 1, end: originalEnd },
                  ]
                : [{ start: remainingStart, end: originalEnd }];

        for (const range of newRanges) {
            const newId = nextSegmentId();
            const chunkPath = `${savePath}.chunk${newId}`;
            tempChunkPaths.add(chunkPath);
            segments.push({
                id: newId,
                start: range.start,
                end: range.end,
                chunkPath,
                status: "pending",
                transferredBytes: 0,
                reportedBytes: 0,
                splitRequested: false,
            });
        }
    }

    public async download(options: ParallelDownloadOptions): Promise<void> {
        const {
            url,
            savePath,
            fileSize,
            headers,
            signal,
            onProgress,
            maxChunks,
            adaptive = true,
            bandwidthLimiter,
            slowChunkMonitor,
            fileId = savePath,
            cohortKey = "parallel",
        } = options;
        const targetPath = `${savePath}.ntmp`;

        let concurrency =
            maxChunks && maxChunks > 0 ? maxChunks : this.calculateChunkCount(fileSize, maxChunks);
        const chunkSize =
            options.chunkSize && options.chunkSize > 0
                ? options.chunkSize
                : this.calculateSegmentSize(fileSize, concurrency);
        const chunkCount = Math.ceil(fileSize / chunkSize);
        concurrency = Math.min(concurrency, chunkCount);

        this.options.logger?.info(
            `Parallel download started: ${(fileSize / 1024 / 1024).toFixed(2)}MB as ${chunkCount} segments with concurrency ${concurrency} (Max: ${maxChunks || "Auto"})`,
            "ParallelDownloader",
        );

        const segments: Segment[] = [];
        const tempChunkPaths = new Set<string>();
        let segmentId = 0;
        const nextSegmentId = () => segmentId++;

        for (let i = 0; i < chunkCount; i++) {
            const start = i * chunkSize;
            const end = Math.min(start + chunkSize - 1, fileSize - 1);

            if (start >= fileSize) break;

            const segmentIdValue = nextSegmentId();
            const chunkPath = `${savePath}.chunk${segmentIdValue}`;
            tempChunkPaths.add(chunkPath);
            segments.push({
                id: segmentIdValue,
                start,
                end,
                chunkPath,
                status: "pending",
                transferredBytes: 0,
                reportedBytes: 0,
                splitRequested: false,
            });
        }

        const schedulerSignal = this.createSchedulerSignal();

        const cleanupChunks = async () => {
            await Promise.all([...tempChunkPaths].map((p) => fse.remove(p).catch(() => {})));
        };

        const getPendingSegment = () =>
            segments
                .filter((segment) => segment.status === "pending")
                .sort((a, b) => a.start - b.start)[0];

        const getRunningSegments = () => segments.filter((segment) => segment.status === "running");

        const requestRebalance = () => {
            if (!adaptive) return false;

            const candidate = getRunningSegments()
                .filter((segment) => {
                    const remainingBytes =
                        segment.end - (segment.start + segment.transferredBytes) + 1;
                    return remainingBytes >= this.minSegmentSize * 2 && !segment.splitRequested;
                })
                .sort((a, b) => {
                    const remainingA = a.end - (a.start + a.transferredBytes) + 1;
                    const remainingB = b.end - (b.start + b.transferredBytes) + 1;
                    return remainingB - remainingA;
                })[0];

            if (!candidate?.controller) {
                return false;
            }

            candidate.splitRequested = true;
            candidate.controller.abort();
            return true;
        };

        const acquireSegment = async (): Promise<Segment | null> => {
            while (true) {
                if (signal?.aborted) return null;

                const pending = getPendingSegment();
                if (pending) {
                    pending.status = "running";
                    pending.controller = new AbortController();
                    pending.splitRequested = false;
                    return pending;
                }

                const running = getRunningSegments();
                if (running.length === 0) {
                    return null;
                }

                const schedulerVersion = schedulerSignal.getVersion();
                requestRebalance();
                await schedulerSignal.wait(schedulerVersion, signal);
            }
        };

        const runWorker = async () => {
            while (true) {
                const segment = await acquireSegment();
                if (!segment) return;

                let slowReconnects = 0;
                let errorRetries = 0;
                const MAX_ERROR_RETRIES = 2;
                let completed = false;

                while (!completed) {
                    if (signal?.aborted) {
                        return;
                    }

                    const attemptController = new AbortController();
                    segment.controller = attemptController;
                    segment.transferredBytes = 0;
                    const previousReported = segment.reportedBytes;
                    const combinedSignal = signal
                        ? AbortSignal.any([signal, attemptController.signal])
                        : attemptController.signal;

                    const chunkSize = segment.end - segment.start + 1;
                    const inFlight = slowChunkMonitor?.register({
                        fileId,
                        chunkIndex: segment.id,
                        chunkSize,
                        cohortKey,
                        attemptController,
                        slowReconnects,
                    });

                    try {
                        await this.downloadChunk({
                            url,
                            headers,
                            start: segment.start,
                            end: segment.end,
                            chunkPath: segment.chunkPath,
                            signal: combinedSignal,
                            onProgress: (transferredBytes) => {
                                segment.transferredBytes = transferredBytes;
                                if (inFlight) {
                                    slowChunkMonitor?.recordSample(inFlight.key, transferredBytes);
                                }
                                const nextReported = Math.max(
                                    segment.reportedBytes,
                                    transferredBytes,
                                );
                                const incremental = nextReported - segment.reportedBytes;
                                segment.reportedBytes = nextReported;
                                if (incremental > 0) {
                                    onProgress?.(incremental);
                                }
                            },
                            preservePartialOnAbort: () => segment.splitRequested,
                            bandwidthLimiter,
                            onPhaseChange: (phase) => {
                                if (inFlight) {
                                    slowChunkMonitor?.setPhase(inFlight.key, phase);
                                }
                            },
                        });

                        segment.status = "completed";
                        segment.controller = undefined;
                        completed = true;
                    } catch (err) {
                        const reportedDelta = segment.reportedBytes - previousReported;
                        if (reportedDelta > 0) {
                            onProgress?.(-reportedDelta);
                            segment.reportedBytes = previousReported;
                        }

                        if (signal?.aborted) {
                            return;
                        }

                        if (isAbortError(err) && segment.splitRequested) {
                            await this.splitSegmentForRebalance({
                                segment,
                                segments,
                                tempChunkPaths,
                                savePath,
                                nextSegmentId,
                                onProgressAdjustment: onProgress,
                            });
                            schedulerSignal.notify();
                            completed = true;
                            continue;
                        }

                        if (
                            inFlight?.abortReason === "slow-chunk" &&
                            slowReconnects < SLOW_CHUNK_MAX_RECONNECTS
                        ) {
                            slowReconnects += 1;
                            this.options.logger?.warn(
                                `Slow chunk reconnect for ${fileId} segment ${segment.id} (${inFlight.detect}, reconnect ${slowReconnects})`,
                                "ParallelDownloader:slowChunk",
                            );
                            if (signal) {
                                await sleepWithAbort(slowReconnectDelayMs(), signal);
                            } else {
                                await new Promise((resolve) =>
                                    setTimeout(resolve, slowReconnectDelayMs()),
                                );
                            }
                            continue;
                        }

                        if (!isAbortError(err) && errorRetries < MAX_ERROR_RETRIES) {
                            errorRetries += 1;
                            if (signal) {
                                await sleepWithAbort(2 ** errorRetries * 1000, signal);
                            } else {
                                await new Promise((resolve) =>
                                    setTimeout(resolve, 2 ** errorRetries * 1000),
                                );
                            }
                            continue;
                        }

                        segment.controller = undefined;
                        throw err;
                    } finally {
                        if (inFlight) {
                            slowChunkMonitor?.unregister(inFlight.key);
                        }
                    }
                }

                schedulerSignal.notify();
            }
        };

        try {
            await Promise.all(Array.from({ length: concurrency }, () => runWorker()));

            if (signal?.aborted) {
                await cleanupChunks();
                throw createAbortError();
            }

            const completedSegments = segments.filter(
                (segment) => segment.status === "completed" && segment.start <= segment.end,
            );
            await this.combineChunks({ segments: completedSegments, targetPath, signal });

            if (signal?.aborted) {
                await fse.remove(targetPath).catch(() => {});
                throw createAbortError();
            }

            await fse.rename(targetPath, savePath);
        } catch (err) {
            await cleanupChunks();
            await fse.remove(targetPath).catch(() => {});
            throw err;
        }
    }
}

function getRangeSupportCacheKey(url: string) {
    try {
        const parsed = new URL(url);
        return `${parsed.origin}${parsed.pathname}`;
    } catch {
        return url;
    }
}
