import { createWriteStream } from "node:fs";
import path from "node:path";
import { Readable, Transform } from "node:stream";
import { pipeline } from "node:stream/promises";
import type { ReadableStream } from "node:stream/web";
import { createGunzip, createZstdDecompress } from "node:zlib";

import { eden } from "@main/client";
import type { LinkData } from "@main/server";
import type { TransferData } from "@shared/types";
import { toErrorMessage } from "@shared/utils";
import { decode } from "cbor-x";
import { chunk, retry, throttle } from "es-toolkit";
import fse from "fs-extra";
import ky from "ky";
import PQueue from "p-queue";

import type { NahidaDesktop } from "..";

import { createBandwidthLimitTransform } from "./bandwidth-limit-stream";
import { zstdDecompressAsync } from "./compressor";
import { ParallelDownloader } from "./parallel-downloader";
import {
    isAbortError,
    SLOW_CHUNK_MAX_RECONNECTS,
    sleepWithAbort,
    slowReconnectDelayMs,
    type SlowChunkTransferPhase,
} from "./slow-chunk-monitor";

export type DownloadParams = {
    type: "download";
    id: string;
    savePath: string;
    suggestedName?: string;
};

export type DownloadMetadata = {
    root: { id: string; parentId: string | null; name: string };
    totalBytes: number;
    files: Array<{
        id: string;
        fileId: string;
        parentId: string | null;
        name: string;
        size: number;
        compAlg: "gzip" | "zstd" | null;
        url: string;
    }>;
    dirs: Array<{
        id: string;
        parentId: string | null;
        name: string;
    }>;
};

export const BATCH_ROOT_ID = "batch-root";
const FILE_ID_BATCH_LIMIT = 100;

async function drainResponseBody(response: Response, signal?: AbortSignal) {
    const reader = response.body?.getReader();
    if (!reader) return;

    try {
        while (true) {
            if (signal?.aborted) return;
            const { done } = await reader.read();
            if (done) return;
        }
    } finally {
        reader.releaseLock();
    }
}

function isExpectedContentRange(value: string | null, resumeFrom: number, fileSize: number) {
    const match = /^bytes\s+(\d+)-(\d+)\/(\d+)$/i.exec(value ?? "");
    if (!match) return false;

    const start = Number(match[1]);
    const end = Number(match[2]);
    const total = Number(match[3]);
    return (
        Number.isSafeInteger(start) &&
        Number.isSafeInteger(end) &&
        Number.isSafeInteger(total) &&
        start === resumeFrom &&
        end === fileSize - 1 &&
        total === fileSize
    );
}

class DownloadStreamer {
    constructor(private readonly desktop: NahidaDesktop) {}

    private async decompressData(str: string) {
        const compressedData = Buffer.from(str, "base64");
        return zstdDecompressAsync(compressedData);
    }

    private async parseStreamedData(data) {
        if (data.compressed) {
            const decompressed = await this.decompressData(data.data);
            if (data.type === "cbor") {
                return decode(decompressed);
            }
            const decoder = new TextDecoder();
            return decoder.decode(decompressed);
        }

        return JSON.parse(data.data);
    }

    public async fetchMetadata({
        id,
        link,
        signal,
    }: {
        id: string;
        link?: LinkData;
        signal: AbortSignal;
    }): Promise<DownloadMetadata> {
        const { data: stream, error } = await eden.akasha.dir.download.get({
            query: {
                uuid: id,
                ...(link && { linkId: link.linkId }),
            },
            headers: {
                ...(link && { "nhd-link-token": link.token }),
            },
        });
        if (error) throw error;

        const downloadData: Omit<DownloadMetadata, "root"> = {
            totalBytes: 0,
            files: [],
            dirs: [],
        };
        let rootDir: DownloadMetadata["root"] | null = null;

        if (!stream || typeof stream !== "object" || !(Symbol.asyncIterator in stream)) {
            throw new Error("Invalid stream");
        }

        for await (const chunk of stream) {
            if (signal.aborted) {
                throw new Error("Download cancelled");
            }

            switch (chunk.event) {
                case "dirs": {
                    const dirsChunk = await this.parseStreamedData(chunk.data);
                    downloadData.dirs.push(...dirsChunk);
                    break;
                }
                case "files": {
                    const filesChunk = await this.parseStreamedData(chunk.data);
                    downloadData.files.push(...filesChunk);
                    break;
                }
                case "metadata": {
                    const metadata = chunk.data as unknown as DownloadMetadata;
                    downloadData.totalBytes = metadata.totalBytes;
                    rootDir = metadata.root;
                    break;
                }
            }
        }

        if (!rootDir) {
            throw new Error("Root directory information was not received.");
        }

        return {
            root: rootDir,
            ...downloadData,
        };
    }

    public async fetchFileDownloads({
        id,
        link,
    }: {
        id: string;
        link?: LinkData;
    }): Promise<DownloadMetadata> {
        const [file] = await this.fetchFileDownloadsBatch({ ids: [id], link });

        return {
            root: { id: file.id, parentId: null, name: file.name },
            totalBytes: file.size,
            files: [file],
            dirs: [],
        };
    }

    public async fetchFileDownloadsBatch({
        ids,
        link,
    }: {
        ids: string[];
        link?: LinkData;
    }): Promise<DownloadMetadata["files"]> {
        const { data, error } = await eden.akasha.file.downloads.post(
            { ids },
            {
                ...(link && {
                    query: { linkId: link.linkId },
                    headers: { "nhd-link-token": link.token },
                }),
            },
        );
        if (error) throw error;
        if (!data || data.length === 0) {
            throw new Error("File download URL not received.");
        }

        return data.map((file) => ({
            id: file.id,
            fileId: file.id,
            parentId: null,
            name: file.name,
            size: file.size,
            compAlg: (file.compAlg as DownloadMetadata["files"][0]["compAlg"]) ?? null,
            url: file.url,
        }));
    }
}

class DownloadFileSystem {
    public resolveDirectoryPaths(
        root: DownloadMetadata["root"],
        dirs: DownloadMetadata["dirs"],
        savePath: string,
    ): Map<string, string> {
        const pathMap = new Map<string, string>();
        const rootPath = path.join(savePath, root.name);
        pathMap.set(root.id, rootPath);

        const childrenMap = new Map<string, DownloadMetadata["dirs"]>();
        for (const dir of dirs) {
            if (!dir.parentId || dir.id === root.id) continue;
            const list = childrenMap.get(dir.parentId) ?? [];
            list.push(dir);
            childrenMap.set(dir.parentId, list);
        }

        const stack = [root.id];
        while (stack.length > 0) {
            const parentId = stack.pop();
            if (!parentId) continue;
            const parentPath = pathMap.get(parentId);
            if (!parentPath) continue;

            const children = childrenMap.get(parentId) ?? [];
            for (const child of children) {
                const childPath = path.join(parentPath, child.name);
                pathMap.set(child.id, childPath);
                stack.push(child.id);
            }
        }

        return pathMap;
    }

    public redistributeFilesBySize<T extends { size: number }>(files: T[]): T[] {
        const LARGE_FILE_THRESHOLD = 50 * 1024 * 1024;

        const largeFiles: T[] = [];
        const smallFiles: T[] = [];

        for (const file of files) {
            if (file.size >= LARGE_FILE_THRESHOLD) {
                largeFiles.push(file);
            } else {
                smallFiles.push(file);
            }
        }

        if (largeFiles.length === 0 || smallFiles.length === 0) {
            return files;
        }

        const interval = Math.floor(smallFiles.length / largeFiles.length) || 1;
        const result: T[] = [];

        let largeFileIndex = 0;
        let smallFileIndex = 0;

        while (smallFileIndex < smallFiles.length || largeFileIndex < largeFiles.length) {
            for (let i = 0; i < interval && smallFileIndex < smallFiles.length; i++) {
                result.push(smallFiles[smallFileIndex++]);
            }

            if (largeFileIndex < largeFiles.length) {
                result.push(largeFiles[largeFileIndex++]);
            }
        }

        return result;
    }
}

class FileDownloadTask {
    private parallelDownloader: ParallelDownloader;

    constructor(private readonly desktop: NahidaDesktop) {
        this.parallelDownloader = new ParallelDownloader({
            logger: this.desktop.logger,
            getHeaders: (url: string) => this.desktop.httpService.getHeaders(url),
        });
    }

    public async execute({
        file,
        filePath,
        signal,
        onComplete,
        onProgress,
    }: {
        file: DownloadMetadata["files"][0];
        filePath: string;
        signal: AbortSignal;
        onComplete: () => void;
        onProgress?: (bytes: number) => void;
    }): Promise<void> {
        if (file.size === 0) {
            await fse.writeFile(filePath, "");
            onComplete();
            return;
        }

        const isSmallFile = file.size < 1024 * 1024;
        const targetPath = isSmallFile ? filePath : `${filePath}.ntmp`;

        const parallelResult = await this.tryParallelDownload(file, filePath, signal, onProgress);
        if (parallelResult) {
            onComplete();
            return;
        }

        await this.downloadWithRetry(file, targetPath, filePath, isSmallFile, signal, onProgress);
        onComplete();
    }

    private async tryParallelDownload(
        file: DownloadMetadata["files"][0],
        filePath: string,
        signal: AbortSignal,
        onProgress?: (bytes: number) => void,
    ): Promise<boolean> {
        const PARALLEL_DOWNLOAD_THRESHOLD = 20 * 1024 * 1024; // 20MB
        if (file.size < PARALLEL_DOWNLOAD_THRESHOLD || !!file.compAlg) {
            return false;
        }

        const supportsRange = await this.parallelDownloader.checkRangeSupport(file.url);
        if (!supportsRange) return false;

        try {
            await this.parallelDownloader.download({
                url: file.url,
                savePath: filePath,
                fileSize: file.size,
                signal,
                maxChunks: 8,
                onProgress,
                bandwidthLimiter: this.desktop.service.transfer.downloadBandwidth,
                slowChunkMonitor: this.desktop.service.transfer.slowChunkMonitor,
                fileId: file.id,
                cohortKey: "drive-parallel",
            });
            return true;
        } catch (err) {
            if (signal.aborted || (err as Error).name === "AbortError") throw err;
            this.desktop.logger.warn(
                `Parallel download failed for ${file.name}, falling back to regular download`,
                "FileDownloadTask:fallback",
            );
            return false;
        }
    }

    private async downloadWithRetry(
        file: DownloadMetadata["files"][0],
        targetPath: string,
        filePath: string,
        isSmallFile: boolean,
        signal: AbortSignal,
        onProgress?: (bytes: number) => void,
    ): Promise<void> {
        await retry(
            async () => {
                if (signal.aborted) return;
                await this.performDownload(file, targetPath, signal, {
                    onProgress,
                    resumeFrom: isSmallFile ? 0 : await this.getResumeOffset(file, targetPath),
                });

                if (!isSmallFile && !signal.aborted) {
                    await this.desktop.lib.fs.rename(targetPath, filePath);
                }
            },
            {
                retries: 2,
                delay: (attempt) => 2 ** attempt * 1000,
                shouldRetry: (err) => !((err as Error).name === "AbortError" || signal.aborted),
                signal,
            },
        ).catch(async (err) => {
            await fse.remove(targetPath).catch(() => {});
            if (!signal.aborted) throw err;
        });
    }

    private async performDownload(
        file: DownloadMetadata["files"][0],
        targetPath: string,
        signal: AbortSignal,
        options?: {
            onProgress?: (bytes: number) => void;
            onPhaseChange?: (
                phase: Extract<SlowChunkTransferPhase, "network" | "bandwidth-wait">,
            ) => void;
            onResumeReset?: () => void;
            resumeFrom?: number;
        },
    ): Promise<void> {
        const headers = await this.desktop.httpService.getHeaders(file.url);
        const resumeFrom = options?.resumeFrom ?? 0;
        const request = async (requestHeaders: Record<string, string>) =>
            await ky(file.url, {
                headers: requestHeaders,
                signal,
                throwHttpErrors: false,
                timeout: 100000,
            });

        let response = await request({
            ...headers,
            ...(resumeFrom > 0 && !file.compAlg ? { Range: `bytes=${resumeFrom}-` } : {}),
        });
        let append = resumeFrom > 0 && !file.compAlg;

        if (append && response.status === 416) {
            await drainResponseBody(response, signal).catch(() => {});
            if (resumeFrom === file.size) return;
            await fse.remove(targetPath).catch(() => {});
            options?.onResumeReset?.();
            response = await request(headers);
            append = false;
        }

        if (!response.ok) {
            await drainResponseBody(response, signal).catch(() => {});
            throw new Error(`Download failed: ${response.statusText}`);
        }

        if (append && response.status !== 206) {
            await fse.remove(targetPath).catch(() => {});
            options?.onResumeReset?.();
            append = false;
        }

        if (
            append &&
            !isExpectedContentRange(response.headers.get("Content-Range"), resumeFrom, file.size)
        ) {
            await drainResponseBody(response, signal).catch(() => {});
            await fse.remove(targetPath).catch(() => {});
            options?.onResumeReset?.();
            response = await request(headers);
            append = false;

            if (!response.ok) {
                await drainResponseBody(response, signal).catch(() => {});
                throw new Error(`Download failed: ${response.statusText}`);
            }
        }

        if (!response.body) throw new Error("No response body");

        const fileStream = createWriteStream(targetPath, append ? { flags: "a" } : undefined);
        const source = Readable.fromWeb(response.body as unknown as ReadableStream);
        const bandwidth = createBandwidthLimitTransform(
            this.desktop.service.transfer.downloadBandwidth,
            {
                signal,
                onPhaseChange: options?.onPhaseChange,
            },
        );
        const progress = new Transform({
            transform(chunk: Buffer, _encoding, callback) {
                options?.onProgress?.(chunk.byteLength);
                callback(null, chunk);
            },
        });

        try {
            if (file.compAlg === "gzip") {
                await pipeline(source, bandwidth, progress, createGunzip(), fileStream, { signal });
            } else if (file.compAlg === "zstd") {
                await pipeline(source, bandwidth, progress, createZstdDecompress(), fileStream, {
                    signal,
                });
            } else {
                await pipeline(source, bandwidth, progress, fileStream, { signal });
            }
        } catch (pipeErr) {
            fileStream.destroy();
            throw pipeErr;
        }
    }

    private async getResumeOffset(
        file: DownloadMetadata["files"][0],
        targetPath: string,
    ): Promise<number> {
        if (file.compAlg) {
            await fse.remove(targetPath).catch(() => {});
            return 0;
        }

        return await fse
            .stat(targetPath)
            .then(({ size }) => size)
            .catch(() => 0);
    }

    public async executeWithSlowRetry({
        file,
        filePath,
        signal,
        onComplete,
        onProgress,
    }: {
        file: DownloadMetadata["files"][0];
        filePath: string;
        signal: AbortSignal;
        onComplete: () => void;
        onProgress?: (bytes: number) => void;
    }): Promise<void> {
        if (file.size === 0) {
            await fse.writeFile(filePath, "");
            onComplete();
            return;
        }

        const targetPath = `${filePath}.ntmp`;
        const MAX_ERROR_RETRIES = 3;
        const monitor = this.desktop.service.transfer.slowChunkMonitor;

        let slowReconnects = 0;
        let errorRetries = 0;
        let reportedResumeBytes = await this.getResumeOffset(file, targetPath);

        if (reportedResumeBytes > 0) onProgress?.(reportedResumeBytes);

        while (true) {
            if (signal.aborted) return;

            const attemptController = new AbortController();
            const combinedSignal = AbortSignal.any([signal, attemptController.signal]);
            const transfer = monitor.register({
                fileId: file.id,
                chunkIndex: 0,
                chunkSize: file.size,
                cohortKey: "drive",
                attemptController,
                slowReconnects,
                initialTransferredBytes: reportedResumeBytes,
            });

            let attemptBytes = 0;

            try {
                const resumeFrom = await this.getResumeOffset(file, targetPath);
                const resumeDelta = resumeFrom - reportedResumeBytes;
                if (resumeDelta !== 0) onProgress?.(resumeDelta);
                reportedResumeBytes = resumeFrom;

                await this.performDownload(file, targetPath, combinedSignal, {
                    resumeFrom,
                    onProgress: (bytes) => {
                        attemptBytes += bytes;
                        reportedResumeBytes += bytes;
                        monitor.recordSample(transfer.key, attemptBytes);
                        onProgress?.(bytes);
                    },
                    onPhaseChange: (phase) => monitor.setPhase(transfer.key, phase),
                    onResumeReset: () => {
                        if (reportedResumeBytes > 0) {
                            onProgress?.(-reportedResumeBytes);
                            reportedResumeBytes = 0;
                        }
                    },
                });

                if (!signal.aborted) {
                    await this.desktop.lib.fs.rename(targetPath, filePath);
                }

                onComplete();
                return;
            } catch (err) {
                if (attemptBytes > 0) {
                    onProgress?.(-attemptBytes);
                    reportedResumeBytes = Math.max(0, reportedResumeBytes - attemptBytes);
                }

                if (file.compAlg) await fse.remove(targetPath).catch(() => {});

                if (signal.aborted) {
                    throw err;
                }

                if (
                    transfer.abortReason === "slow-chunk" &&
                    slowReconnects < SLOW_CHUNK_MAX_RECONNECTS
                ) {
                    slowReconnects += 1;
                    this.desktop.logger.warn(
                        `Slow chunk reconnect for ${file.name} (${transfer.detect}, speed=${Math.round(transfer.chunkSpeedBps / 1024)}KB/s, peerMedian=${Math.round(transfer.peerMedianBps / 1024)}KB/s, reconnect ${slowReconnects})`,
                        "FileDownloadTask:slowChunk",
                    );
                    await sleepWithAbort(slowReconnectDelayMs(), signal);
                    continue;
                }

                if (!isAbortError(err) && errorRetries < MAX_ERROR_RETRIES) {
                    errorRetries += 1;
                    const retryDelayMs = 2 ** errorRetries * 1000;
                    this.desktop.logger.warn(
                        `Retrying download for ${file.name} (${errorRetries}/${MAX_ERROR_RETRIES}) after ${toErrorMessage(err)}; waiting ${retryDelayMs}ms`,
                        "FileDownloadTask:retry",
                    );
                    await sleepWithAbort(retryDelayMs, signal);
                    continue;
                }

                throw err;
            } finally {
                monitor.unregister(transfer.key);
            }
        }
    }
}

export class DownloadLib {
    private readonly streamer: DownloadStreamer;
    private readonly fs: DownloadFileSystem;
    private readonly task: FileDownloadTask;
    private readonly fileQueue: PQueue = new PQueue({ concurrency: 32 });

    public constructor(private readonly desktop: NahidaDesktop) {
        this.streamer = new DownloadStreamer(desktop);
        this.fs = new DownloadFileSystem();
        this.task = new FileDownloadTask(desktop);
    }

    private async syncQueueConcurrency() {
        this.fileQueue.concurrency = await this.desktop.setting.transfer.getDownloadConcurrency();
    }

    private clearPendingFileQueue() {
        this.fileQueue.clear();
    }

    public async startStreamingDownload({
        id,
        link,
        signal,
    }: {
        id: string;
        link?: LinkData;
        signal: AbortSignal;
    }) {
        return this.streamer.fetchMetadata({ id, link, signal });
    }

    public async getDownloadUrl({
        id,
        link,
        signal,
    }: {
        id: string;
        link?: LinkData;
        signal: AbortSignal;
    }): Promise<DownloadMetadata> {
        const data = await this.startStreamingDownload({ id, link, signal });
        return {
            root: data.root,
            files: data.files,
            dirs: [data.root, ...data.dirs],
            totalBytes: data.totalBytes,
        };
    }

    public async getFileDownloadMetadata({
        id,
        link,
    }: {
        id: string;
        link?: LinkData;
    }): Promise<DownloadMetadata> {
        return this.streamer.fetchFileDownloads({ id, link });
    }

    public async fetchMergedMetadata({
        items,
        link,
        signal,
    }: {
        items: Array<{ id: string; isDir: boolean }>;
        link?: LinkData;
        signal: AbortSignal;
    }): Promise<DownloadMetadata> {
        const folders = items.filter((item) => item.isDir);
        const files = items.filter((item) => !item.isDir);

        const mergedDirs: DownloadMetadata["dirs"] = [];
        const mergedFiles: DownloadMetadata["files"] = [];
        let totalBytes = 0;

        for (const fileChunk of chunk(files, FILE_ID_BATCH_LIMIT)) {
            const fileEntries = await this.streamer.fetchFileDownloadsBatch({
                ids: fileChunk.map((file) => file.id),
                link,
            });
            if (signal.aborted) throw new Error("Download cancelled");

            const returnedIds = new Set(fileEntries.map((file) => file.id));
            if (fileChunk.some((file) => !returnedIds.has(file.id))) {
                throw new Error("Some selected files could not be fetched");
            }

            for (const file of fileEntries) {
                mergedFiles.push({ ...file, parentId: BATCH_ROOT_ID });
                totalBytes += file.size;
            }
        }

        const folderQueue = new PQueue({ concurrency: 4 });
        await folderQueue.addAll(
            folders.map((folder) => async () => {
                if (signal.aborted) throw new Error("Download cancelled");

                const meta = await this.streamer.fetchMetadata({
                    id: folder.id,
                    link,
                    signal,
                });

                mergedDirs.push({ ...meta.root, parentId: BATCH_ROOT_ID });
                mergedDirs.push(...meta.dirs);
                mergedFiles.push(...meta.files);
                totalBytes += meta.totalBytes;
            }),
        );

        return {
            root: { id: BATCH_ROOT_ID, parentId: null, name: "" },
            totalBytes,
            files: mergedFiles,
            dirs: mergedDirs,
        };
    }

    public async executeDownload({
        pid,
        params,
        data,
        abort,
        initialTransferedSize,
        initialTransferedFiles,
    }: {
        pid: string;
        params: DownloadParams;
        data: TransferData;
        abort: AbortController;
        initialTransferedSize?: number;
        initialTransferedFiles?: number;
    }) {
        const handleAbort = () => {
            this.clearPendingFileQueue();
        };

        abort.signal.addEventListener("abort", handleAbort, { once: true });

        try {
            await this.syncQueueConcurrency();

            if (!data.root) throw new Error("Root directory information was not received.");

            void this.desktop.service.transfer.updateTransfer(pid, { status: "progress" });

            const isSingleFile = data.dirs.length === 0 && data.files.length === 1;
            const pathMap = isSingleFile
                ? new Map<string, string>([[data.root.id, params.savePath]])
                : this.fs.resolveDirectoryPaths(data.root, data.dirs, params.savePath);
            const singleFileParentKey = isSingleFile ? data.root.id : null;
            const ensuredDirs = new Set<string>();

            let downloadedBytes = initialTransferedSize ?? 0;
            let downloadedCount = initialTransferedFiles ?? 0;
            let hasFailedOperation = false;

            const throttledUpdate = throttle((bytes: number, count: number) => {
                void this.desktop.service.transfer.updateTransfer(pid, {
                    transferedSize: bytes,
                    transferedFiles: count,
                });
            }, 100);

            const redistributedFiles = this.fs.redistributeFilesBySize(data.files);

            for (const file of redistributedFiles) {
                if (abort.signal.aborted) break;

                await this.waitForQueueBackpressure();
                if (abort.signal.aborted) break;

                const parentPath = pathMap.get(singleFileParentKey ?? file.parentId ?? "");
                if (!parentPath) continue;

                const filePath = path.join(parentPath, file.name);

                void this.fileQueue.add(async () => {
                    if (abort.signal.aborted) return;

                    try {
                        if (!isSingleFile && parentPath && !ensuredDirs.has(parentPath)) {
                            await this.desktop.lib.fs.ensureDir(parentPath);
                            ensuredDirs.add(parentPath);
                        }

                        const completed = await this.processFileDownloadTask({
                            pid,
                            file,
                            filePath,
                            abort,
                            onProgress: (bytes) => {
                                downloadedBytes += bytes;
                                throttledUpdate(downloadedBytes, downloadedCount);
                            },
                            onComplete: () => {
                                downloadedCount++;
                                throttledUpdate(downloadedBytes, downloadedCount);
                            },
                        });

                        if (!completed) hasFailedOperation = true;
                    } catch (err) {
                        if (abort.signal.aborted || isAbortError(err)) return;

                        hasFailedOperation = true;
                        this.desktop.service.transfer.markFileFailed(
                            pid,
                            `${file.name}: ${toErrorMessage(err)}`,
                        );
                        this.desktop.logger.error(
                            err,
                            `DownloadLib:executeDownload:${file.name}:prepare`,
                        );
                    }
                });
            }

            if (!isSingleFile) {
                for (const dirPath of pathMap.values()) {
                    if (abort.signal.aborted) break;
                    if (!ensuredDirs.has(dirPath)) {
                        void this.fileQueue.add(async () => {
                            if (abort.signal.aborted) return;

                            try {
                                await this.desktop.lib.fs.ensureDir(dirPath);
                                ensuredDirs.add(dirPath);
                            } catch (err) {
                                if (abort.signal.aborted || isAbortError(err)) return;

                                hasFailedOperation = true;
                                const errorMessage = `Directory preparation failed for ${dirPath}: ${toErrorMessage(err)}`;
                                this.desktop.logger.error(
                                    err,
                                    `DownloadLib:executeDownload:directory:${dirPath}`,
                                );
                                await this.desktop.service.transfer.updateTransfer(pid, {
                                    error:
                                        this.desktop.service.transfer.getTransferByPID(pid)
                                            ?.error ?? errorMessage,
                                });
                            }
                        });
                    }
                }
            }

            if (!abort.signal.aborted) await this.fileQueue.onIdle();
            throttledUpdate.flush();

            if (abort.signal.aborted) return;

            if (hasFailedOperation) {
                await this.desktop.service.transfer.updateTransfer(pid, {
                    status: "error",
                    error:
                        this.desktop.service.transfer.getTransferByPID(pid)?.error ??
                        "One or more download operations failed.",
                });
                return;
            }

            await this.finalizeDownload(pid, params.savePath);
        } catch (err) {
            if (abort.signal.aborted) return;
            void this.desktop.service.transfer.updateTransfer(pid, {
                status: "error",
                error: toErrorMessage(err),
            });
            throw err;
        } finally {
            abort.signal.removeEventListener("abort", handleAbort);
        }
    }

    private async waitForQueueBackpressure() {
        const BACKPRESSURE_LIMIT = 200;
        if (this.fileQueue.size >= BACKPRESSURE_LIMIT) {
            await new Promise<void>((resolve) => this.fileQueue.once("next", () => resolve()));
        }
    }

    private async processFileDownloadTask({
        pid,
        file,
        filePath,
        abort,
        onProgress,
        onComplete,
    }: {
        pid: string;
        file: DownloadMetadata["files"][0];
        filePath: string;
        abort: AbortController;
        onProgress: (bytes: number) => void;
        onComplete: () => void;
    }): Promise<boolean> {
        if (abort.signal.aborted) return true;

        const isCompleted = this.desktop.service.transfer.isFileCompleted(pid, file.id);

        if (isCompleted) {
            if (!this.desktop.service.transfer.isFileCompleted(pid, file.id)) {
                this.desktop.service.transfer.markFileCompleted(pid, file.id);
            }
            onProgress(file.size);
            onComplete();
            return true;
        }

        try {
            await this.task.executeWithSlowRetry({
                file,
                filePath,
                signal: abort.signal,
                onComplete: () => {
                    this.desktop.service.transfer.markFileCompleted(pid, file.id);
                    onComplete();
                },
                onProgress,
            });
        } catch (err) {
            if (abort.signal.aborted || (err as Error).name === "AbortError") {
                return true;
            }
            this.desktop.service.transfer.markFileFailed(
                pid,
                `${file.name}: ${toErrorMessage(err)}`,
            );
            this.desktop.logger.error(err, `DownloadLib:executeDownload:${file.name}`);
            return false;
        }

        return true;
    }

    private async finalizeDownload(pid: string, savePath: string) {
        void this.desktop.service.transfer.updateTransfer(pid, {
            status: "completed",
            progress: 100,
        });

        const transfer = this.desktop.service.transfer.getTransferByPID(pid);
        const name = transfer?.name ?? "Download";

        const mainWindow = this.desktop.window.main.window;
        if (mainWindow) {
            this.desktop.ipc.postMessageToWindow(mainWindow, "download:completed", {
                path: savePath,
                name,
            });
        }
    }
}

export default DownloadLib;
