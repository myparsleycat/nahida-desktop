import { createWriteStream } from "node:fs";
import path from "node:path";
import { Readable } from "node:stream";
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
import ms from "ms";
import PQueue from "p-queue";

import type { NahidaDesktop } from "..";

import { zstdDecompressAsync } from "./compressor";
import { ParallelDownloader } from "./parallel-downloader";

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
    constructor(private readonly desktop: NahidaDesktop) {}

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

    public async checkFileCompleted(filePath: string, expectedSize: number): Promise<boolean> {
        try {
            if (await this.desktop.lib.fs.pathExists(filePath)) {
                const stats = await this.desktop.lib.fs.stat(filePath);
                return stats.size === expectedSize;
            }
        } catch {
            return false;
        }
        return false;
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
                await this.performDownload(file, targetPath, signal, onProgress);

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
        onProgress?: (bytes: number) => void,
    ): Promise<void> {
        let lastTransferredBytes = 0;
        const response = await ky(file.url, {
            headers: await this.desktop.httpService.getHeaders(file.url),
            signal,
            throwHttpErrors: false,
            timeout: 100000,
            onDownloadProgress: (progress) => {
                if (onProgress) {
                    const incremental = progress.transferredBytes - lastTransferredBytes;
                    lastTransferredBytes = progress.transferredBytes;
                    if (incremental > 0) onProgress(incremental);
                }
            },
        });

        if (!response.ok) throw new Error(`Download failed: ${response.statusText}`);
        if (!response.body) throw new Error("No response body");

        const fileStream = createWriteStream(targetPath);
        const source = Readable.fromWeb(response.body as unknown as ReadableStream);

        try {
            if (file.compAlg === "gzip") {
                await pipeline(source, createGunzip(), fileStream, { signal });
            } else if (file.compAlg === "zstd") {
                await pipeline(source, createZstdDecompress(), fileStream, { signal });
            } else {
                await pipeline(source, fileStream, { signal });
            }
        } catch (pipeErr) {
            fileStream.destroy();
            await fse.remove(targetPath).catch(() => {});
            throw pipeErr;
        }
    }

    public async executeWithSlowRetry({
        file,
        filePath,
        signal,
        onComplete,
        onProgress,
        currentConcurrency,
    }: {
        file: DownloadMetadata["files"][0];
        filePath: string;
        signal: AbortSignal;
        onComplete: () => void;
        onProgress?: (bytes: number) => void;
        currentConcurrency?: () => number;
    }): Promise<void> {
        if (file.size === 0) {
            await fse.writeFile(filePath, "");
            onComplete();
            return;
        }

        const SMALL_FILE_THRESHOLD = 5 * 1024 * 1024;
        const isSmallFile = file.size < SMALL_FILE_THRESHOLD;
        const targetPath = isSmallFile ? filePath : `${filePath}.ntmp`;
        const MAX_RETRY_ATTEMPTS = 2;

        for (let retryCount = 0; retryCount <= MAX_RETRY_ATTEMPTS; retryCount++) {
            if (signal.aborted) return;

            try {
                await this.attemptDownloadWithSlowSpeedCheck({
                    file,
                    targetPath,
                    filePath,
                    isSmallFile,
                    signal,
                    onProgress,
                    currentConcurrency,
                    retryCount,
                    maxRetries: MAX_RETRY_ATTEMPTS,
                });
                onComplete();
                return;
            } catch (err) {
                if (signal.aborted || (err as Error).name === "AbortError") {
                    if ((err as Error).message !== "Slow speed retry") {
                        await fse.remove(targetPath).catch(() => {});
                        throw err;
                    }
                }

                if (retryCount >= MAX_RETRY_ATTEMPTS) {
                    await fse.remove(targetPath).catch(() => {});
                    throw err;
                }

                await fse.remove(targetPath).catch(() => {});
                await new Promise((resolve) => setTimeout(resolve, 2 ** (retryCount + 1) * 1000));
            }
        }
    }

    private async attemptDownloadWithSlowSpeedCheck({
        file,
        targetPath,
        filePath,
        isSmallFile,
        signal,
        onProgress,
        currentConcurrency,
        retryCount,
        maxRetries,
    }: {
        file: DownloadMetadata["files"][0];
        targetPath: string;
        filePath: string;
        isSmallFile: boolean;
        signal: AbortSignal;
        onProgress?: (bytes: number) => void;
        currentConcurrency?: () => number;
        retryCount: number;
        maxRetries: number;
    }): Promise<void> {
        const abortController = new AbortController();
        const combinedSignal = AbortSignal.any([signal, abortController.signal]);

        let speedCheckTimeout: ReturnType<typeof setInterval> | null = null;
        let currentBytes = 0;

        try {
            if (currentConcurrency && currentConcurrency() < 6) {
                speedCheckTimeout = this.startSpeedMonitor(
                    file.name,
                    () => currentBytes,
                    () => {
                        abortController.abort();
                    },
                    retryCount,
                    maxRetries,
                );
            }

            await this.performDownload(file, targetPath, combinedSignal, (bytes) => {
                currentBytes += bytes;
                onProgress?.(bytes);
            });

            if (speedCheckTimeout) clearTimeout(speedCheckTimeout);

            if (!isSmallFile && !signal.aborted) {
                await this.desktop.lib.fs.rename(targetPath, filePath);
            }
        } catch (err) {
            if (speedCheckTimeout) clearTimeout(speedCheckTimeout);
            if (combinedSignal.aborted && abortController.signal.aborted) {
                throw new Error("Slow speed retry");
            }
            throw err;
        }
    }

    private startSpeedMonitor(
        fileName: string,
        getCurrentBytes: () => number,
        onSlow: () => void,
        retryCount: number,
        maxRetries: number,
    ): ReturnType<typeof setInterval> {
        const CHECK_INTERVAL = ms("1s");
        const SLOW_SPEED_THRESHOLD = 500 * 1024; // 500KB/s

        let lastBytes = getCurrentBytes();

        return setInterval(() => {
            const currentBytes = getCurrentBytes();
            const diff = currentBytes - lastBytes;
            const speed = diff / (CHECK_INTERVAL / 1000);

            lastBytes = currentBytes;

            if (speed < SLOW_SPEED_THRESHOLD && retryCount < maxRetries) {
                this.desktop.logger.warn(
                    `Slow download detected for ${fileName}: ${Math.round(speed / 1024)}KB/s. Retrying... (${retryCount + 1}/${maxRetries})`,
                    "FileDownloadTask:slowSpeed",
                );
                onSlow();
            }
        }, CHECK_INTERVAL);
    }
}

export class DownloadLib {
    private readonly streamer: DownloadStreamer;
    private readonly fs: DownloadFileSystem;
    private readonly task: FileDownloadTask;
    private readonly fileQueue: PQueue = new PQueue({ concurrency: 32 });

    public constructor(private readonly desktop: NahidaDesktop) {
        this.streamer = new DownloadStreamer(desktop);
        this.fs = new DownloadFileSystem(desktop);
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
                    if (!isSingleFile && parentPath && !ensuredDirs.has(parentPath)) {
                        await this.desktop.lib.fs.ensureDir(parentPath);
                        ensuredDirs.add(parentPath);
                    }

                    await this.processFileDownloadTask({
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
                });
            }

            if (!isSingleFile) {
                for (const dirPath of pathMap.values()) {
                    if (abort.signal.aborted) break;
                    if (!ensuredDirs.has(dirPath)) {
                        void this.fileQueue.add(async () => {
                            if (abort.signal.aborted) return;
                            await this.desktop.lib.fs.ensureDir(dirPath);
                            ensuredDirs.add(dirPath);
                        });
                    }
                }
            }

            if (!abort.signal.aborted) await this.fileQueue.onIdle();
            throttledUpdate.flush();

            if (abort.signal.aborted) return;

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
    }) {
        if (abort.signal.aborted) return;

        let isCompleted = this.desktop.service.transfer.isFileCompleted(pid, file.id);
        if (!isCompleted) {
            try {
                isCompleted = await this.fs.checkFileCompleted(filePath, file.size);
            } catch {}
        }

        if (isCompleted) {
            if (!this.desktop.service.transfer.isFileCompleted(pid, file.id)) {
                this.desktop.service.transfer.markFileCompleted(pid, file.id);
            }
            onProgress(file.size);
            onComplete();
            return;
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
                currentConcurrency: () => this.fileQueue.pending,
            });
        } catch (err) {
            if (abort.signal.aborted || (err as Error).name === "AbortError") {
                return;
            }
            this.desktop.service.transfer.markFileFailed(pid);
            this.desktop.logger.error(err, `DownloadLib:executeDownload:${file.name}`);
        }
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
