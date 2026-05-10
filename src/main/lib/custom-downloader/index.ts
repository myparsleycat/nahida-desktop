import path from "node:path";
import type { ResolvedArchiveExtractPathMode } from "@shared/mod";
import type { TransferData } from "@shared/types";
import { throttle } from "es-toolkit";
import fse from "fs-extra";
import ky from "ky";
import { nanoid } from "nanoid";
import type { NahidaDesktop } from "../..";
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
            getAgent: () => this.desktop.httpService.getAgent(),
            getHeaders: (url: string) => this.desktop.httpService.getHeaders(url),
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
                this.desktop.window.main.createMainWindow().then((window) => {
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
            headers: await this.desktop.httpService.getHeaders(trimmedUrl),
            // @ts-expect-error - dispatcher is not in the type definition, but it's passed through to fetch.
            dispatcher: await this.desktop.httpService.getAgent(),
        });

        const realFileUrl = resp.ok ? resp.url : trimmedUrl;
        const fileSize = parseContentLength(resp.headers.get("Content-Length"));
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
                this.desktop.service.transfer.updateTransfer(pid, { status: "progress" });

                let downloadedBytes = 0;
                const throttledUpdate = throttle((bytes: number) => {
                    this.desktop.service.transfer.updateTransfer(pid, {
                        transferedSize: bytes,
                    });
                }, 100);

                await downloadFile({
                    url: realFileUrl,
                    savePath,
                    fileSize,
                    signal: abortController.signal,
                    onProgress: (bytes) => {
                        downloadedBytes += bytes;
                        throttledUpdate(downloadedBytes);
                    },
                    downloader: this.downloader,
                    httpService: this.desktop.httpService,
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

                await finalizeStagedDownload(stagingPath, groupPath);

                this.desktop.service.transfer.markFileCompleted(pid, pid);
                this.desktop.service.transfer.updateTransfer(pid, {
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
                    this.desktop.service.transfer.updateTransfer(pid, { status: "canceled" });
                } else {
                    this.desktop.logger.error(err, "CustomDownloader:downloadToGroup");
                    this.desktop.service.transfer.updateTransfer(pid, { status: "error" });
                }
            } finally {
                await fse.remove(savePath).catch(() => {});
                await fse.remove(stagingPath).catch(() => {});
            }
        });

        return "started";
    }

    public async GBDownloader(props: {
        fileUrl: string;
        title: string;
        previewUrl?: string | null;
    }): Promise<"started" | "canceled"> {
        const { title: _title, fileUrl, previewUrl } = props;

        const respPromise = ky.head(fileUrl, {
            redirect: "follow",
            throwHttpErrors: false,
            headers: await this.desktop.httpService.getHeaders(fileUrl),
            // @ts-expect-error - dispatcher is not in the type definition, but it's passed through to fetch.
            dispatcher: await this.desktop.httpService.getAgent(),
        });

        const resp = await respPromise;

        if (!resp.ok) {
            throw new Error(`Failed to get real file URL: ${resp.statusText}`);
        }

        const realFileUrl = resp.url;
        const fileSize = parseContentLength(resp.headers.get("Content-Length"));
        const suggestedFileName = parseDownloadFileName(
            realFileUrl,
            this.sanitize.bind(this),
            resp.headers.get("Content-Disposition"),
        );

        const result =
            await this.desktop.lib.pathSelector.getSelectedPathWithModeModal(suggestedFileName);
        if (!result.path) return "canceled";
        const destinationPath = result.path;

        const finalFileName = result.fileName || suggestedFileName;
        const { stagingPath, stagedDownloadPath } = getStagingPaths(
            finalFileName,
            this.sanitize.bind(this),
        );

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
            try {
                this.desktop.service.transfer.updateTransfer(pid, { status: "progress" });
                await fse.ensureDir(stagingPath);

                let downloadedBytes = 0;
                const throttledUpdate = throttle((bytes: number) => {
                    this.desktop.service.transfer.updateTransfer(pid, {
                        transferedSize: bytes,
                    });
                }, 100);

                await downloadFile({
                    url: realFileUrl,
                    savePath: stagedDownloadPath,
                    fileSize,
                    signal: abortController.signal,
                    onProgress: (bytes) => {
                        downloadedBytes += bytes;
                        throttledUpdate(downloadedBytes);
                    },
                    downloader: this.downloader,
                    httpService: this.desktop.httpService,
                });

                throttledUpdate.flush();

                if (abortController.signal.aborted) throw new Error("Aborted");

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
                    const previewSavePath = path.join(
                        getPreviewTargetDir(finalStagedPath),
                        "preview.jpg",
                    );
                    await downloadFile({
                        url: previewUrl,
                        savePath: previewSavePath,
                        downloader: this.downloader,
                        httpService: this.desktop.httpService,
                    });
                }

                await finalizeStagedDownload(stagingPath, destinationPath);

                this.desktop.service.transfer.markFileCompleted(pid, pid);

                this.desktop.service.transfer.updateTransfer(pid, {
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
                    this.desktop.service.transfer.updateTransfer(pid, { status: "canceled" });
                } else {
                    this.desktop.logger.error(err, "GameBanana:downloadFromGB");
                    this.desktop.service.transfer.updateTransfer(pid, { status: "error" });
                }
            } finally {
                await fse.remove(stagingPath).catch(() => {});
            }
        });

        return "started";
    }

    public async HuiDownloader(props: {
        fileUrl: string;
        title: string;
    }): Promise<"started" | "canceled"> {
        const { title: _title, fileUrl } = props;
        const title = this.sanitize(_title);
        const result = await this.desktop.lib.pathSelector.getSelectedPathWithModeModal(title);

        if (!result.path) {
            return "canceled";
        }
        const destinationPath = result.path;

        const resp = await ky.head(fileUrl, {
            redirect: "follow",
            throwHttpErrors: false,
            headers: await this.desktop.httpService.getHeaders(fileUrl),
            // @ts-expect-error - dispatcher is not in the type definition, but it's passed through to fetch.
            dispatcher: await this.desktop.httpService.getAgent(),
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
                this.desktop.service.transfer.updateTransfer(pid, { status: "progress" });
                await fse.ensureDir(stagingPath);

                let downloadedBytes = 0;
                const throttledUpdate = throttle((bytes: number) => {
                    this.desktop.service.transfer.updateTransfer(pid, {
                        transferedSize: bytes,
                    });
                }, 100);

                await downloadFile({
                    url: fileUrl,
                    savePath: stagedDownloadPath,
                    fileSize,
                    signal: abortController.signal,
                    onProgress: (bytes) => {
                        downloadedBytes += bytes;
                        throttledUpdate(downloadedBytes);
                    },
                    downloader: this.downloader,
                    httpService: this.desktop.httpService,
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

                await finalizeStagedDownload(stagingPath, destinationPath);

                this.desktop.service.transfer.markFileCompleted(pid, pid);

                this.desktop.service.transfer.updateTransfer(pid, {
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
                    this.desktop.service.transfer.updateTransfer(pid, { status: "canceled" });
                } else {
                    this.desktop.logger.error(err, "GameBanana:downloadFromGB");
                    this.desktop.service.transfer.updateTransfer(pid, { status: "error" });
                }
            } finally {
                await fse.remove(stagingPath).catch(() => {});
            }
        });

        return "started";
    }
}

export default CustomDownloader;
