import { Transform } from "node:stream";
import { pipeline } from "node:stream/promises";

import fse from "fs-extra";
import ky from "ky";

import type { BandwidthLimiter } from "../bandwidth-limiter";
import type { ParallelDownloader } from "../parallel-downloader";

import { createBandwidthLimitTransform } from "../bandwidth-limit-stream";
import {
    isAbortError,
    SLOW_CHUNK_MAX_RECONNECTS,
    sleepWithAbort,
    slowReconnectDelayMs,
    type SlowChunkMonitor,
} from "../slow-chunk-monitor";
import { drainWebStream, webStreamToNodeReadable } from "../web-stream-to-readable";

interface HttpServiceLike {
    getHeaders: (url: string) => Promise<Record<string, string>>;
}

export async function downloadFile(props: {
    url: string;
    savePath: string;
    fileSize?: number;
    signal?: AbortSignal;
    onProgress?: (bytes: number) => void;
    downloader: ParallelDownloader;
    httpService: HttpServiceLike;
    bandwidthLimiter?: BandwidthLimiter;
    slowChunkMonitor?: SlowChunkMonitor;
    fileId?: string;
    cohortKey?: string;
}) {
    const {
        url,
        savePath,
        fileSize,
        signal,
        onProgress,
        downloader,
        httpService,
        bandwidthLimiter,
        slowChunkMonitor,
        fileId = savePath,
        cohortKey = "custom",
    } = props;
    const supportsRange = await downloader.checkRangeSupport(url);

    if (supportsRange && fileSize) {
        await downloader.download({
            url,
            savePath,
            fileSize,
            signal,
            onProgress(bytes) {
                onProgress?.(bytes);
            },
            maxChunks: 8,
            bandwidthLimiter,
            slowChunkMonitor,
            fileId,
            cohortKey,
        });
        return;
    }

    const MAX_ERROR_RETRIES = 2;
    let slowReconnects = 0;
    let errorRetries = 0;

    while (true) {
        if (signal?.aborted) {
            throw new DOMException("The operation was aborted.", "AbortError");
        }

        const attemptController = new AbortController();
        const combinedSignal = signal
            ? AbortSignal.any([signal, attemptController.signal])
            : attemptController.signal;
        const inFlight = slowChunkMonitor?.register({
            fileId,
            chunkIndex: 0,
            chunkSize: fileSize && fileSize > 0 ? fileSize : Number.MAX_SAFE_INTEGER,
            cohortKey,
            attemptController,
            slowReconnects,
        });

        let attemptBytes = 0;
        const fileStream = fse.createWriteStream(savePath);

        try {
            const resp = await ky.get(url, {
                signal: combinedSignal,
                headers: await httpService.getHeaders(url),
                throwHttpErrors: false,
            });
            if (!resp.ok) {
                await drainWebStream(resp.body).catch(() => {});
                throw new Error(
                    `Failed to download file: ${resp.statusText || "UNKNOWN"} (${resp.status})`,
                );
            }
            if (!resp.body) {
                throw new Error("No response body");
            }

            const source = webStreamToNodeReadable(resp.body, combinedSignal);
            const progressStream = new Transform({
                transform(chunk: Buffer, _encoding, callback) {
                    attemptBytes += chunk.byteLength;
                    if (inFlight) {
                        slowChunkMonitor?.recordSample(inFlight.key, attemptBytes);
                    }
                    onProgress?.(chunk.byteLength);
                    callback(null, chunk);
                },
            });

            if (bandwidthLimiter) {
                await pipeline(
                    source,
                    createBandwidthLimitTransform(bandwidthLimiter, {
                        signal: combinedSignal,
                        onPhaseChange: (phase) => {
                            if (inFlight) {
                                slowChunkMonitor?.setPhase(inFlight.key, phase);
                            }
                        },
                    }),
                    progressStream,
                    fileStream,
                    { signal: combinedSignal },
                );
            } else {
                await pipeline(source, progressStream, fileStream, { signal: combinedSignal });
            }
            return;
        } catch (err) {
            fileStream.destroy();
            await fse.remove(savePath).catch(() => {});

            if (attemptBytes > 0) {
                onProgress?.(-attemptBytes);
            }

            if (signal?.aborted) {
                throw err;
            }

            if (
                inFlight?.abortReason === "slow-chunk" &&
                slowReconnects < SLOW_CHUNK_MAX_RECONNECTS
            ) {
                slowReconnects += 1;
                if (signal) {
                    await sleepWithAbort(slowReconnectDelayMs(), signal);
                } else {
                    await new Promise((resolve) => setTimeout(resolve, slowReconnectDelayMs()));
                }
                continue;
            }

            if (!isAbortError(err) && errorRetries < MAX_ERROR_RETRIES) {
                errorRetries += 1;
                if (signal) {
                    await sleepWithAbort(2 ** errorRetries * 1000, signal);
                } else {
                    await new Promise((resolve) => setTimeout(resolve, 2 ** errorRetries * 1000));
                }
                continue;
            }

            throw err;
        } finally {
            if (inFlight) {
                slowChunkMonitor?.unregister(inFlight.key);
            }
        }
    }
}
