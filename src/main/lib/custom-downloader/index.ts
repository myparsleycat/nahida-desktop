import path from "node:path";

import type { ResolvedArchiveExtractPathMode } from "@shared/mod";
import type { TransferData } from "@shared/types";
import { toErrorMessage } from "@shared/utils";
import { throttle } from "es-toolkit";
import fse from "fs-extra";
import ky from "ky";
import { nanoid } from "nanoid";

import type { NahidaDesktop } from "../..";

import { customDownloadRequestLimiter } from "../download-request-limiter";
import {
    type ModDownloadMetadataInput,
    writeModDownloadMetadataToDirectories,
} from "../mod-download-metadata";
import { ParallelDownloader } from "../parallel-downloader";
import { downloadFile } from "./download-file";
import {
    isArchiveByResponseOrContent,
    isHtmlContentType,
    isHtmlResponseOrContent,
} from "./file-detection";
import { applySelectedExtractedName, finalizeStagedDownload } from "./file-operations";
import {
    createSiblingTempPath,
    getDownloadTempExtension,
    getPreviewTargetDir,
    getStagingPaths,
    parseContentLength,
    parseDownloadFileName,
} from "./utils";

export type GBDownloaderFailureContext = {
    operation: string;
    stage: string;
    itemId: number;
    fileId: number;
    modelName?: string;
    downloadUrl?: string;
    previewUrl?: string;
    destinationPath?: string;
    stagingPath?: string;
    rollback: {
        stagingPathRemoved: boolean;
        finalized: boolean;
        finalizedPaths: string[];
        cleanupError?: string;
    };
};

export type GBDownloaderError = Error & { context: GBDownloaderFailureContext };

export function isGBDownloaderError(error: unknown): error is GBDownloaderError {
    return (
        error instanceof Error &&
        typeof (error as GBDownloaderError).context === "object" &&
        (error as GBDownloaderError).context !== null &&
        typeof (error as GBDownloaderError).context.stage === "string"
    );
}

function attachGBDownloaderContext(error: unknown, context: GBDownloaderFailureContext): Error {
    const normalized =
        error instanceof Error ? error : new Error(toErrorMessage(error), { cause: error });
    (normalized as GBDownloaderError).context = {
        ...context,
        rollback: {
            ...context.rollback,
            finalizedPaths: [...context.rollback.finalizedPaths],
        },
    };
    return normalized;
}

export class CustomDownloader {
    public desktop: NahidaDesktop;
    private readonly downloader: ParallelDownloader;
    private readonly pendingArchiveExtractPrompts = new Map<
        string,
        {
            resolve: (mode: ResolvedArchiveExtractPathMode) => void;
            reject: (error: Error) => void;
        }
    >();

    public constructor(desktop: NahidaDesktop) {
        this.desktop = desktop;
        this.downloader = new ParallelDownloader({
            logger: desktop.logger,
            getHeaders: (url: string) => this.desktop.httpService.getHeaders(url),
            requestLimiter: customDownloadRequestLimiter,
        });
    }

    private sanitize(name: string) {
        return this.desktop.lib.fs.sanitizeWindowsFilename(name);
    }

    private async promptForArchiveExtractMode(
        archivePath: string,
    ): Promise<ResolvedArchiveExtractPathMode> {
        return new Promise((resolve, reject) => {
            const requestId = nanoid();
            const fileName = path.basename(archivePath);

            this.pendingArchiveExtractPrompts.set(requestId, {
                resolve,
                reject,
            });

            const sendPrompt = () => {
                const mainWindow = this.desktop.window.main.window;
                if (!mainWindow) {
                    const pending = this.pendingArchiveExtractPrompts.get(requestId);
                    if (pending) {
                        pending.reject(new Error("Main window not found"));
                        this.pendingArchiveExtractPrompts.delete(requestId);
                    }
                    return;
                }

                this.desktop.ipc.postMessageToWindow(mainWindow, "mod:archiveExtractPrompt", {
                    requestId,
                    fileName,
                });
                this.desktop.window.main.focus();
            };

            const mainWindow = this.desktop.window.main.window;
            if (!mainWindow) {
                void this.desktop.window.main.createMainWindow().then((window) => {
                    if (window?.webContents.isLoading()) {
                        window.webContents.once("did-finish-load", () => {
                            setTimeout(sendPrompt, 500);
                        });
                    } else {
                        sendPrompt();
                    }
                });
                return;
            }

            sendPrompt();
        });
    }

    private async resolveArchiveExtractMode(
        archivePath: string,
    ): Promise<ResolvedArchiveExtractPathMode> {
        const extractMode = await this.desktop.setting.mod.getArchiveExtractPathMode();

        if (extractMode !== "ask_every_time") {
            return extractMode;
        }

        const hasSingleTopLevelDirectory =
            await this.desktop.service.archive.hasSingleTopLevelDirectory(archivePath);

        if (!hasSingleTopLevelDirectory) {
            return "flatten_single_root";
        }

        return await this.promptForArchiveExtractMode(archivePath);
    }

    public resolveArchiveExtractPrompt(
        requestId: string,
        mode: ResolvedArchiveExtractPathMode | null,
    ): void {
        const pending = this.pendingArchiveExtractPrompts.get(requestId);
        if (!pending) {
            throw new Error("Pending archive extract prompt not found");
        }

        this.pendingArchiveExtractPrompts.delete(requestId);

        if (!mode) {
            pending.reject(new Error("Aborted"));
            return;
        }

        pending.resolve(mode);
    }

    private async extractDownloadedArchive(archivePath: string, groupPath: string) {
        const extractMode = await this.resolveArchiveExtractMode(archivePath);
        const flattenSingleRoot = extractMode === "flatten_single_root";

        return await this.desktop.service.archive.extract(archivePath, groupPath, {
            flattenSingleRoot,
        });
    }

    private async extractGBArchive(archivePath: string) {
        const targetPath = path.dirname(archivePath);
        const extractedPath = await this.desktop.service.archive.extract(archivePath, targetPath);
        await fse.rm(archivePath, { force: true });
        return extractedPath;
    }

    public async downloadToGroup(url: string, groupPath: string): Promise<"started"> {
        const trimmedUrl = url.trim();

        if (!trimmedUrl) {
            throw new Error("DOWNLOAD_URL_REQUIRED");
        }

        let parsedUrl: URL;
        try {
            parsedUrl = new URL(trimmedUrl);
        } catch {
            throw new Error("INVALID_DOWNLOAD_URL");
        }

        if (!["http:", "https:"].includes(parsedUrl.protocol)) {
            throw new Error("UNSUPPORTED_DOWNLOAD_URL_PROTOCOL");
        }

        await fse.ensureDir(groupPath);

        const resp = await ky.head(trimmedUrl, {
            redirect: "follow",
            throwHttpErrors: false,
            retry: { limit: 2 },
            headers: await this.desktop.httpService.getHeaders(trimmedUrl),
        });

        const realFileUrl = resp.ok ? resp.url : trimmedUrl;
        const fileSize = parseContentLength(resp.headers.get("Content-Length"));
        const supportsRange = resp.ok
            ? resp.headers.get("Accept-Ranges")?.toLowerCase() === "bytes"
            : undefined;
        const suggestedFileName = parseDownloadFileName(
            realFileUrl,
            this.sanitize.bind(this),
            resp.headers.get("Content-Disposition"),
        );

        if (isHtmlContentType(resp.headers)) {
            throw new Error("DOWNLOAD_URL_HTML_PAGE");
        }

        const savePath = createSiblingTempPath(
            groupPath,
            `download${getDownloadTempExtension(suggestedFileName)}`,
        );
        const stagingPath = createSiblingTempPath(groupPath, "staging");

        const pid = nanoid();
        const abortController = new AbortController();

        const transferData: TransferData = {
            root: {
                id: pid,
                parentId: null,
                name: suggestedFileName,
            },
            files: [
                {
                    id: pid,
                    fileId: pid,
                    parentId: pid,
                    name: suggestedFileName,
                    size: fileSize ?? 0,
                    compAlg: null,
                    url: realFileUrl,
                },
            ],
            dirs: [],
        };

        await this.desktop.service.transfer.createTransfer({
            pid,
            type: "download",
            data: transferData,
            abortController,
            name: suggestedFileName,
            initialStatus: "pending",
            path: groupPath,
        });

        this.desktop.service.transfer.registerRunner(pid, async () => {
            try {
                void this.desktop.service.transfer.updateTransfer(pid, { status: "progress" });

                let downloadedBytes = 0;
                const throttledUpdate = throttle((bytes: number) => {
                    void this.desktop.service.transfer.updateTransfer(pid, {
                        transferedSize: bytes,
                    });
                }, 100);

                await downloadFile({
                    url: realFileUrl,
                    savePath,
                    fileSize,
                    supportsRange,
                    signal: abortController.signal,
                    onProgress: (bytes) => {
                        downloadedBytes += bytes;
                        throttledUpdate(downloadedBytes);
                    },
                    downloader: this.downloader,
                    httpService: this.desktop.httpService,
                    bandwidthLimiter: this.desktop.service.transfer.downloadBandwidth,
                    slowChunkMonitor: this.desktop.service.transfer.slowChunkMonitor,
                    fileId: pid,
                    cohortKey: "custom",
                });

                throttledUpdate.flush();

                if (abortController.signal.aborted) throw new Error("Aborted");
                if (
                    await isHtmlResponseOrContent({
                        headers: resp.headers,
                        filePath: savePath,
                    })
                ) {
                    throw new Error("DOWNLOAD_URL_HTML_PAGE");
                }

                await fse.ensureDir(stagingPath);
                const shouldExtract = await isArchiveByResponseOrContent({
                    headers: resp.headers,
                    originalFileName: suggestedFileName,
                    filePath: savePath,
                });
                const extractedPath = shouldExtract
                    ? await this.extractDownloadedArchive(savePath, stagingPath)
                    : path.join(stagingPath, suggestedFileName);

                if (!shouldExtract) {
                    await fse.move(savePath, extractedPath, { overwrite: true });
                }

                const stagedEntries = await fse.readdir(stagingPath);

                if (!(await fse.pathExists(extractedPath)) || stagedEntries.length === 0) {
                    throw new Error("Downloaded file did not produce staged content.");
                }

                const finalized = await finalizeStagedDownload(stagingPath, groupPath);
                try {
                    await writeModDownloadMetadataToDirectories(finalized.destinationPaths, {
                        source: "mod",
                        downloadedAt: new Date().toISOString(),
                    });
                    await finalized.commit();
                } catch (error) {
                    await finalized.restore().catch(() => {});
                    throw error;
                }

                this.desktop.service.transfer.markFileCompleted(pid, pid);
                void this.desktop.service.transfer.updateTransfer(pid, {
                    status: "completed",
                    progress: 100,
                    transferedSize: fileSize ?? downloadedBytes,
                    transferedFiles: 1,
                });

                const mainWindow = this.desktop.window.main.window;
                if (mainWindow) {
                    this.desktop.ipc.postMessageToWindow(mainWindow, "download:completed", {
                        path: groupPath,
                        name: suggestedFileName,
                    });
                }
            } catch (err) {
                if (
                    abortController.signal.aborted ||
                    (err as Error).name === "AbortError" ||
                    (err as Error).message === "Aborted"
                ) {
                    void this.desktop.service.transfer.updateTransfer(pid, { status: "canceled" });
                } else {
                    this.desktop.logger.error(err, "CustomDownloader:downloadToGroup");
                    void this.desktop.service.transfer.updateTransfer(pid, { status: "error" });
                }
            } finally {
                await fse.remove(savePath).catch(() => {});
                await fse.remove(stagingPath).catch(() => {});
            }
        });

        return "started";
    }

    public async GBDownloader(props: {
        itemId: number;
        fileId: number;
        modelName?: string;
    }): Promise<"started" | "canceled"> {
        const context: GBDownloaderFailureContext = {
            operation: "mod:downloadGameBananaFile",
            stage: "resolve-download-url",
            itemId: props.itemId,
            fileId: props.fileId,
            modelName: props.modelName,
            downloadUrl: undefined,
            previewUrl: undefined,
            destinationPath: undefined,
            stagingPath: undefined,
            rollback: {
                stagingPathRemoved: false,
                finalized: false,
                finalizedPaths: [],
                cleanupError: undefined,
            },
        };
        const logPreTransferFailure = (error: unknown): Error => {
            const enriched = attachGBDownloaderContext(error, context);
            this.desktop.logger.error(enriched, "GameBanana:downloadFromGB");
            this.desktop.logger.error(
                {
                    ...context,
                    error: toErrorMessage(enriched),
                },
                "GameBanana:downloadFromGB:context",
            );
            return enriched;
        };
        const downloadFilePayload = await this.desktop.service.gamebanana
            .getDownloadFilePayload({
                itemId: props.itemId,
                fileId: props.fileId,
                modelName: props.modelName,
            })
            .catch((error) => {
                throw logPreTransferFailure(error);
            });
        try {
            const { title: _title, fileUrl, previewUrl } = downloadFilePayload;
            context.downloadUrl = fileUrl;
            context.previewUrl = previewUrl ?? undefined;
            context.stage = "head-request";

            const resp = await (async () => {
                try {
                    const response = await ky.head(fileUrl, {
                        redirect: "follow",
                        throwHttpErrors: false,
                        retry: { limit: 2 },
                        headers: await this.desktop.httpService.getHeaders(fileUrl),
                    });

                    if (!response.ok) {
                        throw new Error(
                            `GAMEBANANA_DOWNLOAD_HEAD_FAILED:${response.status}:${response.statusText || "UNKNOWN"}`,
                        );
                    }

                    return response;
                } catch (error) {
                    const failure =
                        error instanceof Error &&
                        error.message.startsWith("GAMEBANANA_DOWNLOAD_HEAD_FAILED:")
                            ? error
                            : new Error(
                                  `GAMEBANANA_DOWNLOAD_HEAD_FAILED:${toErrorMessage(error)}`,
                                  {
                                      cause: error,
                                  },
                              );
                    throw logPreTransferFailure(failure);
                }
            })();

            const realFileUrl = resp.url;
            const fileSize = parseContentLength(resp.headers.get("Content-Length"));
            const supportsRange = resp.headers.get("Accept-Ranges")?.toLowerCase() === "bytes";
            const suggestedFileName = parseDownloadFileName(
                realFileUrl,
                this.sanitize.bind(this),
                resp.headers.get("Content-Disposition"),
            );

            const result = await this.desktop.lib.pathSelector.getSelectedPathWithModeModal(
                suggestedFileName,
                downloadFilePayload.categoryName,
                downloadFilePayload.importerKey ?? undefined,
                "gamebanana",
            );
            if (!result.path) return "canceled";
            const destinationPath = result.path;
            context.destinationPath = destinationPath;
            context.stage = "prepare-staging";

            const finalFileName = result.fileName || suggestedFileName;
            const { stagingPath, stagedDownloadPath } = getStagingPaths(
                finalFileName,
                this.sanitize.bind(this),
            );
            context.stagingPath = stagingPath;
            context.stage = "queue-transfer";

            const pid = nanoid();
            const abortController = new AbortController();

            const transferData: TransferData = {
                root: {
                    id: pid,
                    parentId: null,
                    name: finalFileName,
                },
                files: [
                    {
                        id: pid,
                        fileId: pid,
                        parentId: pid,
                        name: finalFileName,
                        size: fileSize ?? 0,
                        compAlg: null,
                        url: realFileUrl,
                    },
                ],
                dirs: [],
            };

            await this.desktop.service.transfer.createTransfer({
                pid,
                type: "download",
                data: transferData,
                abortController,
                name: finalFileName,
                initialStatus: "pending",
                path: destinationPath,
            });

            this.desktop.service.transfer.registerRunner(pid, async () => {
                let cleanupAttempted = false;
                const cleanupStaging = async () => {
                    cleanupAttempted = true;
                    try {
                        await fse.remove(stagingPath);
                        context.rollback.stagingPathRemoved = true;
                    } catch (error) {
                        context.rollback.cleanupError = toErrorMessage(error);
                        this.desktop.logger.error(error, "GameBanana:downloadFromGB:cleanup");
                    }
                };
                const cleanupDestinationEntries = async () => {
                    const entries = context.rollback.finalizedPaths;
                    if (entries.length === 0) return;
                    for (const entry of entries) {
                        try {
                            await fse.remove(entry);
                        } catch (error) {
                            context.rollback.cleanupError = toErrorMessage(error);
                            this.desktop.logger.error(
                                error,
                                "GameBanana:downloadFromGB:cleanup:destination",
                            );
                        }
                    }
                };

                try {
                    context.stage = "download-file";
                    void this.desktop.service.transfer.updateTransfer(pid, { status: "progress" });
                    await fse.ensureDir(stagingPath);

                    let downloadedBytes = 0;
                    const throttledUpdate = throttle((bytes: number) => {
                        void this.desktop.service.transfer.updateTransfer(pid, {
                            transferedSize: bytes,
                        });
                    }, 100);

                    await downloadFile({
                        url: realFileUrl,
                        savePath: stagedDownloadPath,
                        fileSize,
                        supportsRange,
                        signal: abortController.signal,
                        onProgress: (bytes) => {
                            downloadedBytes += bytes;
                            throttledUpdate(downloadedBytes);
                        },
                        downloader: this.downloader,
                        httpService: this.desktop.httpService,
                        bandwidthLimiter: this.desktop.service.transfer.downloadBandwidth,
                        slowChunkMonitor: this.desktop.service.transfer.slowChunkMonitor,
                        fileId: pid,
                        cohortKey: "gamebanana",
                    });

                    throttledUpdate.flush();

                    if (abortController.signal.aborted) throw new Error("Aborted");

                    context.stage = "extract-archive";
                    const shouldExtract = await isArchiveByResponseOrContent({
                        headers: resp.headers,
                        originalFileName: suggestedFileName,
                        filePath: stagedDownloadPath,
                    });
                    const stagedPath = shouldExtract
                        ? await this.extractGBArchive(stagedDownloadPath)
                        : stagedDownloadPath;
                    const finalStagedPath = shouldExtract
                        ? await applySelectedExtractedName({
                              extractedPath: stagedPath,
                              stagingPath,
                              requestedFileName: finalFileName,
                              originalSuggestedFileName: suggestedFileName,
                              sanitizeWindowsFilename: this.sanitize.bind(this),
                          })
                        : stagedPath;

                    if (previewUrl) {
                        context.stage = "download-preview";
                        const previewSavePath = path.join(
                            getPreviewTargetDir(finalStagedPath),
                            "preview.jpg",
                        );
                        await downloadFile({
                            url: previewUrl,
                            savePath: previewSavePath,
                            downloader: this.downloader,
                            httpService: this.desktop.httpService,
                            bandwidthLimiter: this.desktop.service.transfer.downloadBandwidth,
                            slowChunkMonitor: this.desktop.service.transfer.slowChunkMonitor,
                            fileId: `${pid}:preview`,
                            cohortKey: "gamebanana-preview",
                        });
                    }

                    context.stage = "finalize-files";
                    let finalized: Awaited<ReturnType<typeof finalizeStagedDownload>> | null = null;
                    try {
                        finalized = await finalizeStagedDownload(stagingPath, destinationPath);
                        context.rollback.finalized = true;
                        context.rollback.finalizedPaths = [...finalized.destinationPaths];
                    } catch (error) {
                        const partial = (error as Error & { partialDestinationPaths?: string[] })
                            .partialDestinationPaths;
                        if (partial?.length) context.rollback.finalizedPaths = [...partial];
                        throw error;
                    }
                    const metadata: ModDownloadMetadataInput = {
                        source: "gamebanana",
                        downloadedAt: new Date().toISOString(),
                        mod: {
                            id: downloadFilePayload.modId,
                            pageUrl: downloadFilePayload.modPageUrl,
                            version: downloadFilePayload.version,
                        },
                        author: {
                            name: downloadFilePayload.authorName,
                            url: downloadFilePayload.authorUrl,
                        },
                        file: {
                            downloadUrl: downloadFilePayload.fileUrl,
                            md5: downloadFilePayload.fileMd5,
                        },
                    };
                    try {
                        await writeModDownloadMetadataToDirectories(
                            finalized.destinationPaths,
                            metadata,
                        );
                        await finalized.commit();
                    } catch (error) {
                        const written = (error as Error & { writtenDirectories?: string[] })
                            .writtenDirectories;
                        if (written?.length) {
                            this.desktop.logger.error(
                                { writtenDirectories: written, error: toErrorMessage(error) },
                                "GameBanana:downloadFromGB:metadataCleanup",
                            );
                        }
                        // restore owns destination rollback — skip raw cleanup even on partial failure
                        (error as Error & { restoreCompleted?: boolean }).restoreCompleted = true;
                        try {
                            await finalized.restore();
                        } catch (restoreError) {
                            context.rollback.cleanupError = toErrorMessage(restoreError);
                            this.desktop.logger.error(
                                {
                                    restoreError: toErrorMessage(restoreError),
                                    finalizedPaths: [...context.rollback.finalizedPaths],
                                    incompleteRestoration: true,
                                },
                                "GameBanana:downloadFromGB:cleanup:destination",
                            );
                        }
                        throw error;
                    }

                    this.desktop.service.transfer.markFileCompleted(pid, pid);

                    void this.desktop.service.transfer.updateTransfer(pid, {
                        status: "completed",
                        progress: 100,
                        transferedSize: fileSize ?? downloadedBytes,
                        transferedFiles: 1,
                    });

                    const mainWindow = this.desktop.window.main.window;
                    if (mainWindow) {
                        this.desktop.ipc.postMessageToWindow(mainWindow, "download:completed", {
                            path: destinationPath,
                            name: finalFileName,
                        });
                    }
                } catch (err) {
                    await cleanupStaging();
                    if (!(err as Error & { restoreCompleted?: boolean }).restoreCompleted) {
                        await cleanupDestinationEntries();
                    }
                    if (
                        abortController.signal.aborted ||
                        (err as Error).name === "AbortError" ||
                        (err as Error).message === "Aborted"
                    ) {
                        void this.desktop.service.transfer.updateTransfer(pid, {
                            status: "canceled",
                        });
                    } else {
                        this.desktop.logger.error(
                            {
                                ...context,
                                rollback: {
                                    ...context.rollback,
                                    finalizedPaths: [...context.rollback.finalizedPaths],
                                },
                                error: toErrorMessage(err),
                            },
                            "GameBanana:downloadFromGB:context",
                        );
                        void this.desktop.service.transfer.updateTransfer(pid, {
                            status: "error",
                            error: toErrorMessage(err),
                        });
                    }
                } finally {
                    if (!cleanupAttempted) await cleanupStaging();
                }
            });

            return "started";
        } catch (error) {
            if (isGBDownloaderError(error)) throw error;
            throw logPreTransferFailure(error);
        }
    }

    public async HuiDownloader(props: {
        fileUrl: string;
        title: string;
    }): Promise<"started" | "canceled"> {
        const { title: _title, fileUrl } = props;
        const title = this.sanitize(_title);
        const result = await this.desktop.lib.pathSelector.getSelectedPathWithModeModal(
            title,
            undefined,
            undefined,
            "hui",
        );

        if (!result.path) {
            return "canceled";
        }
        const destinationPath = result.path;

        const resp = await ky.head(fileUrl, {
            redirect: "follow",
            throwHttpErrors: false,
            retry: { limit: 2 },
            headers: await this.desktop.httpService.getHeaders(fileUrl),
        });
        if (!resp.ok) {
            throw new Error(`Failed to get real file URL: ${resp.statusText}`);
        }

        const finalFileName = result.fileName || title;
        const { stagingPath, stagedDownloadPath } = getStagingPaths(
            finalFileName,
            this.sanitize.bind(this),
        );
        const fileSize = parseContentLength(resp.headers.get("Content-Length"));
        const supportsRange = resp.headers.get("Accept-Ranges")?.toLowerCase() === "bytes";

        const pid = nanoid();
        const abortController = new AbortController();

        const transferData: TransferData = {
            root: {
                id: pid,
                parentId: null,
                name: finalFileName,
            },
            files: [
                {
                    id: pid,
                    fileId: pid,
                    parentId: pid,
                    name: finalFileName,
                    size: fileSize ?? 0,
                    compAlg: null,
                    url: fileUrl,
                },
            ],
            dirs: [],
        };

        await this.desktop.service.transfer.createTransfer({
            pid,
            type: "download",
            data: transferData,
            abortController,
            name: finalFileName,
            initialStatus: "pending",
            path: destinationPath,
        });

        this.desktop.service.transfer.registerRunner(pid, async () => {
            try {
                void this.desktop.service.transfer.updateTransfer(pid, { status: "progress" });
                await fse.ensureDir(stagingPath);

                let downloadedBytes = 0;
                const throttledUpdate = throttle((bytes: number) => {
                    void this.desktop.service.transfer.updateTransfer(pid, {
                        transferedSize: bytes,
                    });
                }, 100);

                await downloadFile({
                    url: fileUrl,
                    savePath: stagedDownloadPath,
                    fileSize,
                    supportsRange,
                    signal: abortController.signal,
                    onProgress: (bytes) => {
                        downloadedBytes += bytes;
                        throttledUpdate(downloadedBytes);
                    },
                    downloader: this.downloader,
                    httpService: this.desktop.httpService,
                    bandwidthLimiter: this.desktop.service.transfer.downloadBandwidth,
                    slowChunkMonitor: this.desktop.service.transfer.slowChunkMonitor,
                    fileId: pid,
                    cohortKey: "hui",
                });

                throttledUpdate.flush();

                if (abortController.signal.aborted) throw new Error("Aborted");

                const shouldExtract = await isArchiveByResponseOrContent({
                    headers: resp.headers,
                    originalFileName: parseDownloadFileName(
                        resp.url || fileUrl,
                        this.sanitize.bind(this),
                        resp.headers.get("Content-Disposition"),
                    ),
                    filePath: stagedDownloadPath,
                });

                if (shouldExtract) {
                    const extractedPath = await this.desktop.service.archive.extract(
                        stagedDownloadPath,
                        stagingPath,
                    );
                    await applySelectedExtractedName({
                        extractedPath,
                        stagingPath,
                        requestedFileName: finalFileName,
                        originalSuggestedFileName: title,
                        sanitizeWindowsFilename: this.sanitize.bind(this),
                    });
                    await fse.rm(stagedDownloadPath, { force: true });
                }

                const finalized = await finalizeStagedDownload(stagingPath, destinationPath);
                await finalized.commit();

                this.desktop.service.transfer.markFileCompleted(pid, pid);

                void this.desktop.service.transfer.updateTransfer(pid, {
                    status: "completed",
                    progress: 100,
                    transferedSize: fileSize ?? downloadedBytes,
                    transferedFiles: 1,
                });

                const mainWindow = this.desktop.window.main.window;
                if (mainWindow) {
                    this.desktop.ipc.postMessageToWindow(mainWindow, "download:completed", {
                        path: destinationPath,
                        name: finalFileName,
                    });
                }
            } catch (err) {
                if (
                    abortController.signal.aborted ||
                    (err as Error).name === "AbortError" ||
                    (err as Error).message === "Aborted"
                ) {
                    void this.desktop.service.transfer.updateTransfer(pid, { status: "canceled" });
                } else {
                    this.desktop.logger.error(err, "GameBanana:downloadFromGB");
                    void this.desktop.service.transfer.updateTransfer(pid, { status: "error" });
                }
            } finally {
                await fse.remove(stagingPath).catch(() => {});
            }
        });

        return "started";
    }
}

export default CustomDownloader;
