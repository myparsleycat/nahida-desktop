import { NahidaDesktop } from "..";
import { eden2url } from "@main/client";
import { nanoid } from "nanoid";
import createSseWorker from "@main/worker/drive/sse.worker?nodeWorker";
import { TransferData } from "@shared/types";
import path from "node:path";
import fse from "fs-extra";
import { pipeline } from "node:stream/promises";
import { Readable } from "node:stream";
import { throttle } from "es-toolkit";
import { createZstdDecompress, createGunzip } from "node:zlib";
import ky from "ky";
import { appVersion } from "@main/const";
import { ParallelDownloader } from "./parallel-downloader";
import {
    Observable,
    from,
    mergeMap,
    retry,
    timer,
    defer,
    lastValueFrom,
    Subject,
    takeUntil,
    finalize,
} from "rxjs";

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

    public fetchMetadata(uuid: string, signal: AbortSignal): Promise<DownloadMetadata> {
        return lastValueFrom(
            new Observable<DownloadMetadata>((subscriber) => {
                const downloadData: Omit<DownloadMetadata, "root"> = {
                    totalBytes: 0,
                    files: [],
                    dirs: [],
                };
                let rootDir: DownloadMetadata["root"] | null = null;

                let worker: any;

                const start = async () => {
                    const token = await this.desktop.service.auth.getToken();
                    const url = eden2url.akasha.dir.download.url({ query: { uuid } });

                    worker = createSseWorker({
                        workerData: {
                            url: url.toString(),
                            token,
                        },
                    });

                    worker.on("message", (event: any) => {
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
                                if (!rootDir) {
                                    subscriber.error(
                                        new Error("Root directory information was not received."),
                                    );
                                } else {
                                    subscriber.next({ root: rootDir, ...downloadData });
                                    subscriber.complete();
                                }
                                break;
                            case "error":
                                subscriber.error(
                                    new Error(payload || "An unknown worker error occurred"),
                                );
                                break;
                        }
                    });

                    worker.on("error", (error: any) => {
                        subscriber.error(error);
                    });

                    worker.postMessage("start");
                };

                start();

                const onAbort = () => {
                    if (worker) worker.terminate();
                    subscriber.error(new Error("download aborted"));
                };

                if (signal.aborted) onAbort();
                signal.addEventListener("abort", onAbort, { once: true });

                return () => {
                    if (worker) worker.terminate();
                    signal.removeEventListener("abort", onAbort);
                };
            }),
        );
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

    public async executeWithSlowRetry({
        file,
        filePath,
        signal,
        onComplete,
        onProgress,
        getConcurrency,
    }: {
        file: DownloadMetadata["files"][0];
        filePath: string;
        signal: AbortSignal;
        onComplete: () => void;
        onProgress?: (bytes: number) => void;
        getConcurrency?: () => number;
    }): Promise<void> {
        const SMALL_FILE_THRESHOLD = 5 * 1024 * 1024;
        const LOW_CONCURRENCY_THRESHOLD = 6;
        const SLOW_SPEED_THRESHOLD = 500 * 1024;
        const SPEED_CHECK_DELAY = 3000;
        const MAX_RETRY_ATTEMPTS = 2;

        const isSmallFile = file.size < SMALL_FILE_THRESHOLD;
        const targetPath = isSmallFile ? filePath : `${filePath}.ntmp`;

        const downloadObservable = defer(() =>
            from(
                this.downloadCore({
                    file,
                    filePath: targetPath,
                    signal,
                    onProgress,
                    isSmallFile,
                    speedCheck: {
                        enabled:
                            isSmallFile &&
                            !!getConcurrency &&
                            getConcurrency() < LOW_CONCURRENCY_THRESHOLD,
                        delay: SPEED_CHECK_DELAY,
                        threshold: SLOW_SPEED_THRESHOLD,
                    },
                }),
            ),
        ).pipe(
            retry({
                count: MAX_RETRY_ATTEMPTS,
                delay: (error, retryCount) => {
                    this.desktop.logger.warn(
                        `Retrying download for ${file.name} (${retryCount}/${MAX_RETRY_ATTEMPTS}) due to: ${error.message}`,
                    );
                    return timer(Math.pow(2, retryCount) * 1000);
                },
            }),
            finalize(async () => {
                if (!onCompleteExecuted) {
                    await fse.remove(targetPath).catch(() => {});
                }
            }),
        );

        let onCompleteExecuted = false;
        await lastValueFrom(downloadObservable);

        if (!isSmallFile) {
            await this.desktop.lib.fs.rename(targetPath, filePath);
        }
        onCompleteExecuted = true;
        onComplete();
    }

    private async downloadCore({
        file,
        filePath,
        signal,
        onProgress,
        isSmallFile,
        speedCheck,
    }: {
        file: DownloadMetadata["files"][0];
        filePath: string;
        signal: AbortSignal;
        onProgress?: (bytes: number) => void;
        isSmallFile: boolean;
        speedCheck: { enabled: boolean; delay: number; threshold: number };
    }) {
        const abortController = new AbortController();
        const combinedSignal = AbortSignal.any([signal, abortController.signal]);

        let lastTransferredBytes = 0;
        let currentBytes = 0;
        let startTime = Date.now();

        let speedCheckTimeout: NodeJS.Timeout | null = null;

        if (speedCheck.enabled) {
            speedCheckTimeout = setTimeout(() => {
                const elapsed = (Date.now() - startTime) / 1000;
                const speed = currentBytes / elapsed;
                if (speed < speedCheck.threshold) {
                    abortController.abort(new Error("Slow speed detected"));
                }
            }, speedCheck.delay);
        }

        try {
            const token = await this.desktop.service.auth.getToken();
            const response = await ky(file.url, {
                headers: {
                    Authorization: `Bearer ${token}`,
                    "User-Agent": `Nahida Desktop/${appVersion}`,
                },
                signal: combinedSignal,
                throwHttpErrors: false,
                timeout: 100000,
                onDownloadProgress: (progress) => {
                    currentBytes = progress.transferredBytes;
                    if (onProgress) {
                        const incremental = progress.transferredBytes - lastTransferredBytes;
                        lastTransferredBytes = progress.transferredBytes;
                        if (incremental > 0) onProgress(incremental);
                    }
                },
            });

            if (speedCheckTimeout) clearTimeout(speedCheckTimeout);

            if (!response.ok) throw new Error(`Download failed: ${response.statusText}`);
            if (!response.body) throw new Error("No response body");

            const fileStream = fse.createWriteStream(filePath);
            const streams: any[] = [Readable.fromWeb(response.body as any)];

            if (file.compAlg === "gzip") streams.push(createGunzip());
            else if (file.compAlg === "zstd") streams.push(createZstdDecompress());

            streams.push(fileStream);

            try {
                await (pipeline as any)(...streams, { signal: combinedSignal });
            } catch (pipeErr) {
                fileStream.destroy();
                throw pipeErr;
            }
        } catch (err) {
            if (speedCheckTimeout) clearTimeout(speedCheckTimeout);
            throw err;
        }
    }
}

export class DownloadLib {
    private readonly streamer: DownloadStreamer;
    private readonly fs: DownloadFileSystem;
    private readonly task: FileDownloadTask;
    private readonly CONCURRENCY = 64;

    public constructor(private readonly desktop: NahidaDesktop) {
        this.streamer = new DownloadStreamer(desktop);
        this.fs = new DownloadFileSystem(desktop);
        this.task = new FileDownloadTask(desktop);
    }

    public async getDownloadUrl(id: string, signal: AbortSignal): Promise<DownloadMetadata> {
        const data = await this.streamer.fetchMetadata(id, signal);
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
        const data = await this.getDownloadUrl(id, abort.signal);
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
        const cancel$ = new Subject<void>();
        abort.signal.addEventListener("abort", () => cancel$.next(), { once: true });

        try {
            if (!data.root) throw new Error("Root directory information was not received.");

            this.desktop.service.transfer.updateTransfer(pid, { status: "progress" });

            const pathMap = this.fs.resolveDirectoryPaths(data.root, data.dirs, params.savePath);

            for (const dirPath of pathMap.values()) {
                await this.desktop.lib.fs.ensureDir(dirPath);
            }

            let downloadedBytes = initialTransferedSize ?? 0;
            let downloadedCount = initialTransferedFiles ?? 0;

            const throttledUpdate = throttle((bytes: number, count: number) => {
                this.desktop.service.transfer.updateTransfer(pid, {
                    transferedSize: bytes,
                    transferedFiles: count,
                });
            }, 100);

            const redistributedFiles = this.fs.redistributeFilesBySize(data.files);
            let activeCount = 0;

            const downloadStream$ = from(redistributedFiles).pipe(
                takeUntil(cancel$),
                mergeMap(async (file) => {
                    const parentPath = pathMap.get(file.parentId ?? "");
                    if (!parentPath) return;

                    const filePath = path.join(parentPath, file.name);

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
                        downloadedBytes += file.size;
                        downloadedCount++;
                        throttledUpdate(downloadedBytes, downloadedCount);
                        return;
                    }

                    activeCount++;
                    try {
                        await this.task.executeWithSlowRetry({
                            file,
                            filePath,
                            signal: abort.signal,
                            onComplete: () => {
                                this.desktop.service.transfer.markFileCompleted(pid, file.id);
                                downloadedCount++;
                                throttledUpdate(downloadedBytes, downloadedCount);
                            },
                            onProgress: (bytes) => {
                                downloadedBytes += bytes;
                                throttledUpdate(downloadedBytes, downloadedCount);
                            },
                            getConcurrency: () => activeCount,
                        });
                    } catch (err) {
                        if (!abort.signal.aborted) {
                            this.desktop.logger.error(
                                err,
                                `DownloadLib:executeDownload:${file.name}`,
                            );
                        }
                    } finally {
                        activeCount--;
                    }
                }, this.CONCURRENCY),
            );

            await lastValueFrom(downloadStream$, { defaultValue: undefined });
            throttledUpdate.flush();

            if (abort.signal.aborted) return;

            this.desktop.service.transfer.updateTransfer(pid, {
                status: "completed",
                progress: 100,
            });

            const mainWindow = this.desktop.window.main.window;
            if (mainWindow && data.root) {
                this.desktop.ipc.postMessageToWindow(mainWindow, "download:completed", {
                    path: params.savePath,
                    name: data.root.name,
                });
            }
        } catch (err) {
            if (abort.signal.aborted) return;
            this.desktop.service.transfer.updateTransfer(pid, { status: "error" });
            throw err;
        } finally {
            cancel$.complete();
        }
    }
}

export default DownloadLib;
