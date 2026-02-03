import fse from "fs-extra";
import ky from "ky";
import { retry } from "es-toolkit";
import { pipeline } from "node:stream/promises";
import { Readable } from "node:stream";
import { appVersion } from "@main/const";
import { getAgent, getHeaders } from "@main/internal/fetcher";

export interface ParallelDownloadOptions {
    url: string;
    savePath: string;
    fileSize: number;
    token?: string;
    headers?: Record<string, string>;
    signal?: AbortSignal;
    onProgress?: (bytes: number) => void;
    chunkSize?: number;
    maxChunks?: number;
}

export class ParallelDownloader {
    constructor(
        private options: {
            logger?: {
                info: (msg: string, ...args: any[]) => void;
                warn: (msg: string, ...args: any[]) => void;
            };
        },
    ) {}

    public async checkRangeSupport(url: string): Promise<boolean> {
        try {
            const response = await ky.head(url, {
                headers: await getHeaders(url),
                timeout: 10000,
                throwHttpErrors: false,
                // @ts-expect-error
                dispatcher: await getAgent(),
            });

            const acceptRanges = response.headers.get("Accept-Ranges");
            return acceptRanges === "bytes";
        } catch {
            return false;
        }
    }

    private calculateChunkCount(sizeInBytes: number, maxChunks?: number): number {
        const sizeInMB = sizeInBytes / (1024 * 1024);

        if (sizeInMB < 1) return 1;

        const log10 = Math.floor(Math.log10(sizeInMB));
        const firstDigit = Math.floor(sizeInMB / Math.pow(10, log10));

        let count = Math.max(2, firstDigit);

        if (maxChunks && maxChunks > 0) {
            count = Math.min(count, maxChunks);
        }

        return count;
    }

    private async downloadChunk({
        url,
        headers,
        start,
        end,
        chunkPath,
        signal,
        onProgress,
    }: {
        url: string;
        headers?: Record<string, string>;
        start: number;
        end: number;
        chunkPath: string;
        signal?: AbortSignal;
        onProgress?: (bytes: number) => void;
    }): Promise<void> {
        let lastTransferredBytes = 0;

        const requestHeaders: Record<string, string> = {
            Range: `bytes=${start}-${end}`,
            ...headers,
        };

        const response = await ky(url, {
            headers: {
                ...(await getHeaders(url)),
                ...requestHeaders,
            },
            signal,
            throwHttpErrors: false,
            timeout: 100000,
            onDownloadProgress: (progress) => {
                if (onProgress) {
                    const incremental = progress.transferredBytes - lastTransferredBytes;
                    lastTransferredBytes = progress.transferredBytes;
                    if (incremental > 0) {
                        onProgress(incremental);
                    }
                }
            },
            // @ts-expect-error
            dispatcher: await getAgent(),
        });

        if (!response.ok && response.status !== 206) {
            throw new Error(`Chunk download failed: ${response.statusText} (${response.status})`);
        }

        if (!response.body) throw new Error("No response body");

        const fileStream = fse.createWriteStream(chunkPath);
        try {
            await pipeline(Readable.fromWeb(response.body as any), fileStream, { signal });
        } catch (pipeErr) {
            fileStream.destroy();
            await fse.remove(chunkPath).catch(() => {});
            throw pipeErr;
        }
    }

    private async combineChunks({
        chunkPaths,
        targetPath,
        signal,
    }: {
        chunkPaths: string[];
        targetPath: string;
        signal?: AbortSignal;
    }): Promise<void> {
        const fileStream = fse.createWriteStream(targetPath);

        try {
            for (const chunkPath of chunkPaths) {
                if (signal?.aborted) throw new Error("Aborted during chunk combination");

                const chunkStream = fse.createReadStream(chunkPath);
                await new Promise<void>((resolve, reject) => {
                    chunkStream.pipe(fileStream, { end: false });
                    chunkStream.on("end", resolve);
                    chunkStream.on("error", reject);
                });
            }

            fileStream.end();
            await new Promise<void>((resolve, reject) => {
                fileStream.on("finish", resolve);
                fileStream.on("error", reject);
            });

            await Promise.all(chunkPaths.map((p) => fse.remove(p).catch(() => {})));
        } catch (err) {
            fileStream.destroy();
            await Promise.all(chunkPaths.map((p) => fse.remove(p).catch(() => {})));
            throw err;
        }
    }

    public async download(options: ParallelDownloadOptions): Promise<void> {
        const { url, savePath, fileSize, headers, signal, onProgress, maxChunks } = options;
        const targetPath = `${savePath}.ntmp`;

        let chunkCount: number;
        let chunkSize: number;

        if (options.chunkSize && options.chunkSize > 0) {
            chunkSize = options.chunkSize;
            chunkCount = Math.ceil(fileSize / chunkSize);
            if (maxChunks && chunkCount > maxChunks) {
                chunkCount = maxChunks;
                chunkSize = Math.ceil(fileSize / chunkCount);
            }
        } else {
            chunkCount = this.calculateChunkCount(fileSize, maxChunks);
            chunkSize = Math.ceil(fileSize / chunkCount);
        }

        this.options.logger?.info(
            `Parallel download started: ${(fileSize / 1024 / 1024).toFixed(2)}MB into ${chunkCount} chunks (Max: ${maxChunks || "Auto"})`,
            "ParallelDownloader",
        );

        const chunkPaths: string[] = [];
        const downloadPromises: Promise<void>[] = [];

        for (let i = 0; i < chunkCount; i++) {
            const start = i * chunkSize;
            const end = Math.min(start + chunkSize - 1, fileSize - 1);

            if (start >= fileSize) break;

            const chunkPath = `${savePath}.chunk${i}`;
            chunkPaths.push(chunkPath);

            const downloadPromise = retry(
                () =>
                    this.downloadChunk({
                        url,
                        headers,
                        start,
                        end,
                        chunkPath,
                        signal,
                        onProgress,
                    }),
                {
                    retries: 2,
                    delay: (attempt) => Math.pow(2, attempt) * 1000,
                    shouldRetry: (err: any) => !(err.name === "AbortError" || signal?.aborted),
                    signal,
                },
            );

            downloadPromises.push(downloadPromise);
        }

        await Promise.all(downloadPromises);

        if (signal?.aborted) {
            await Promise.all(chunkPaths.map((p) => fse.remove(p).catch(() => {})));
            return;
        }

        await this.combineChunks({ chunkPaths, targetPath, signal });

        if (signal?.aborted) {
            await fse.remove(targetPath).catch(() => {});
            return;
        }

        await fse.rename(targetPath, savePath);
    }
}
