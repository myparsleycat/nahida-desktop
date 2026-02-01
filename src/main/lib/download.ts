import { NahidaDesktop } from "..";
import { eden2url } from "@main/client";
import { nanoid } from "nanoid";
import createSseWorker from "@main/worker/drive/sse.worker?nodeWorker";
import { TransferData } from "@shared/types";
import path from "node:path";
import fse from "fs-extra";
import { pipeline } from "node:stream/promises";
import { Readable } from "node:stream";
import { retry, throttle } from "es-toolkit";
import { createZstdDecompress, createGunzip } from "node:zlib";
import PQueue from "p-queue";
import ky from "ky";
import { appVersion } from "@main/const";
import { ParallelDownloader } from "./parallel-downloader";
import { getAgent } from "@main/internal/fetcher";

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

class DownloadStreamer {
    constructor(private readonly desktop: NahidaDesktop) {}

    public async fetchMetadata(uuid: string, signal: AbortSignal): Promise<DownloadMetadata> {
        const url = eden2url.akasha.dir.download.url({ query: { uuid } });
        const token = await this.desktop.service.auth.getToken();
        const worker = createSseWorker({
            workerData: {
                url: url.toString(),
                token,
            },
        });

        return new Promise<DownloadMetadata>((resolve, reject) => {
            const downloadData: Omit<DownloadMetadata, "root"> = {
                totalBytes: 0,
                files: [],
                dirs: [],
            };
            let rootDir: DownloadMetadata["root"] | null = null;

            const cleanup = () => {
                worker.terminate();
                signal.removeEventListener("abort", onAbort);
            };

            const onAbort = () => {
                cleanup();
                reject(new Error("download aborted"));
            };

            if (signal.aborted) return onAbort();
            signal.addEventListener("abort", onAbort, { once: true });

            worker.on("message", (event) => {
                const { type, payload } = event;
                switch (type) {
                    case "dirs":
                        downloadData.dirs = downloadData.dirs.concat(payload);
                        break;
                    case "files":
                        downloadData.files = downloadData.files.concat(payload);
                        break;
                    case "metadata":
                        downloadData.totalBytes = payload.totalBytes;
                        rootDir = payload.root;
                        break;
                    case "complete":
                        cleanup();
                        if (!rootDir) {
                            return reject(
                                new Error("Root directory information was not received."),
                            );
                        }
                        resolve({ root: rootDir, ...downloadData });
                        break;
                    case "error":
                        cleanup();
                        reject(new Error(payload || "An unknown worker error occurred"));
                        break;
                }
            });

            worker.on("error", (error) => {
                cleanup();
                reject(error);
            });

            worker.postMessage("start");
        });
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
            const parentId = stack.pop()!;
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

        const token = await this.desktop.service.auth.getToken();
        if (!token) return false;

        const supportsRange = await this.parallelDownloader.checkRangeSupport(file.url, token);
        if (!supportsRange) return false;

        try {
            await this.parallelDownloader.download({
                url: file.url,
                savePath: filePath,
                fileSize: file.size,
                token,
                signal,
                maxChunks: 8,
                onProgress,
                agent: await getAgent(),
            });
            return true;
        } catch (err: any) {
            if (signal.aborted || err.name === "AbortError") throw err;
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
                const token = await this.desktop.service.auth.getToken();
                await this.performDownload(file, targetPath, token, signal, onProgress);

                if (!isSmallFile && !signal.aborted) {
                    await this.desktop.lib.fs.rename(targetPath, filePath);
                }
            },
            {
                retries: 2,
                delay: (attempt) => Math.pow(2, attempt) * 1000,
                shouldRetry: (err: any) => !(err.name === "AbortError" || signal.aborted),
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
        token: string | null,
        signal: AbortSignal,
        onProgress?: (bytes: number) => void,
    ): Promise<void> {
        let lastTransferredBytes = 0;
        const response = await ky(file.url, {
            headers: {
                Authorization: `Bearer ${token}`,
                "User-Agent": `Nahida Desktop/${appVersion}`,
            },
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
            // @ts-expect-error
            dispatcher: await getAgent(),
        });

        if (!response.ok) throw new Error(`Download failed: ${response.statusText}`);
        if (!response.body) throw new Error("No response body");

        const fileStream = fse.createWriteStream(targetPath);
        const streams: any[] = [Readable.fromWeb(response.body as any)];

        if (file.compAlg === "gzip") streams.push(createGunzip());
        else if (file.compAlg === "zstd") streams.push(createZstdDecompress());

        streams.push(fileStream);

        try {
            await (pipeline as any)(...streams, { signal });
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
            } catch (err: any) {
                if (signal.aborted || err.name === "AbortError") {
                    if (err.message !== "Slow speed retry") {
                        await fse.remove(targetPath).catch(() => {});
                        throw err;
                    }
                }

                if (retryCount >= MAX_RETRY_ATTEMPTS) {
                    await fse.remove(targetPath).catch(() => {});
                    throw err;
                }

                await fse.remove(targetPath).catch(() => {});
                await new Promise((resolve) =>
                    setTimeout(resolve, Math.pow(2, retryCount + 1) * 1000),
                );
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

        let speedCheckTimeout: NodeJS.Timeout | null = null;
        let currentBytes = 0;

        try {
            const token = await this.desktop.service.auth.getToken();

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

            await this.performDownload(file, targetPath, token, combinedSignal, (bytes) => {
                currentBytes += bytes;
                onProgress?.(bytes);
            });

            if (speedCheckTimeout) clearTimeout(speedCheckTimeout);

            if (!isSmallFile && !signal.aborted) {
                await this.desktop.lib.fs.rename(targetPath, filePath);
            }
        } catch (err: any) {
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
    ): NodeJS.Timeout {
        const SLOW_SPEED_THRESHOLD = 500 * 1024; // 500KB/s
        const SPEED_CHECK_DELAY = 3000;
        const startTime = Date.now();
        const startBytes = getCurrentBytes();

        return setTimeout(() => {
            const elapsed = (Date.now() - startTime) / 1000;
            const transferred = getCurrentBytes() - startBytes;
            const speed = transferred / elapsed;

            if (speed < SLOW_SPEED_THRESHOLD && retryCount < maxRetries) {
                this.desktop.logger.warn(
                    `Slow download detected for ${fileName}: ${Math.round(speed / 1024)}KB/s. Retrying... (${retryCount + 1}/${maxRetries})`,
                    "FileDownloadTask:slowSpeed",
                );
                onSlow();
            }
        }, SPEED_CHECK_DELAY);
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

    public async startStreamingDownload(uuid: string, signal: AbortSignal) {
        return this.streamer.fetchMetadata(uuid, signal);
    }

    public async getDownloadUrl(id: string, signal: AbortSignal): Promise<DownloadMetadata> {
        const data = await this.startStreamingDownload(id, signal);
        return {
            root: data.root,
            files: data.files,
            dirs: [data.root, ...data.dirs],
            totalBytes: data.totalBytes,
        };
    }

    public async prepareDownload(id: string, name: string) {
        const pid = nanoid();
        const abort = new AbortController();
        const getDownloadUrlsPromise = this.getDownloadUrl(id, abort.signal);

        const data = await getDownloadUrlsPromise;
        return { pid, abort, data };
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
        try {
            if (!data.root) throw new Error("Root directory information was not received.");

            this.desktop.service.transfer.updateTransfer(pid, { status: "progress" });

            const pathMap = this.fs.resolveDirectoryPaths(data.root, data.dirs, params.savePath);
            await this.prepareDirectories(pathMap);

            let downloadedBytes = initialTransferedSize ?? 0;
            let downloadedCount = initialTransferedFiles ?? 0;

            const throttledUpdate = throttle((bytes: number, count: number) => {
                this.desktop.service.transfer.updateTransfer(pid, {
                    transferedSize: bytes,
                    transferedFiles: count,
                });
            }, 100);

            const redistributedFiles = this.fs.redistributeFilesBySize(data.files);

            for (const file of redistributedFiles) {
                if (abort.signal.aborted) break;

                await this.waitForQueueBackpressure();
                if (abort.signal.aborted) break;

                const parentPath = pathMap.get(file.parentId ?? "");
                if (!parentPath) continue;

                const filePath = path.join(parentPath, file.name);

                this.fileQueue.add(async () => {
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

            if (!abort.signal.aborted) await this.fileQueue.onIdle();
            throttledUpdate.flush();

            if (abort.signal.aborted) return;

            this.finalizeDownload(pid, params.savePath, data.root.name);
        } catch (err) {
            if (abort.signal.aborted) return;
            this.desktop.service.transfer.updateTransfer(pid, { status: "error" });
            throw err;
        }
    }

    private async prepareDirectories(pathMap: Map<string, string>) {
        for (const dirPath of pathMap.values()) {
            await this.desktop.lib.fs.ensureDir(dirPath);
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
            this.desktop.logger.error(err, `DownloadLib:executeDownload:${file.name}`);
        }
    }

    private finalizeDownload(pid: string, savePath: string, rootName: string) {
        this.desktop.service.transfer.updateTransfer(pid, {
            status: "completed",
            progress: 100,
        });

        const mainWindow = this.desktop.window.main.window;
        if (mainWindow) {
            this.desktop.ipc.postMessageToWindow(mainWindow, "download:completed", {
                path: savePath,
                name: rootName,
            });
        }
    }
}

export default DownloadLib;
