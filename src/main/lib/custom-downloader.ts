import path from "node:path";
import { pipeline } from "node:stream/promises";
import type { ArchiveExtractPathMode, ResolvedArchiveExtractPathMode } from "@shared/mod";
import type { TransferData } from "@shared/types.gen";
import { Notification } from "electron";
import { throttle } from "es-toolkit";
import fse from "fs-extra";
import ky from "ky";
import { nanoid } from "nanoid";
import type { NahidaDesktop } from "..";
import { ParallelDownloader } from "./parallel-downloader";

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

    private async downloadFile(props: {
        url: string;
        savePath: string;
        fileSize?: number;
        signal?: AbortSignal;
        onProgress?: (bytes: number) => void;
    }) {
        const { url, savePath, fileSize, signal, onProgress } = props;
        const supportsRange = await this.downloader.checkRangeSupport(url);

        if (supportsRange && fileSize) {
            await this.downloader.download({
                url,
                savePath,
                fileSize,
                signal,
                onProgress(bytes) {
                    onProgress?.(bytes);
                },
                maxChunks: 8,
            });
        } else {
            const fileStream = fse.createWriteStream(savePath);
            let lastTransferredBytes = 0;

            const resp = await ky.get(url, {
                signal,
                onDownloadProgress(progress, _chunk) {
                    if (onProgress) {
                        const incremental = progress.transferredBytes - lastTransferredBytes;
                        lastTransferredBytes = progress.transferredBytes;
                        if (incremental > 0) {
                            onProgress(incremental);
                        }
                    }
                },
                headers: await this.desktop.httpService.getHeaders(url),
                // @ts-expect-error - dispatcher is not in the type definition, but it's passed through to fetch.
                dispatcher: await this.desktop.httpService.getAgent(),
            });
            if (!resp.ok) {
                throw new Error(`Failed to download file: ${resp.statusText}`);
            }
            try {
                await pipeline(resp.body as ReadableStream, fileStream, { signal });
            } catch (err) {
                fileStream.destroy();
                await fse.remove(savePath).catch(() => {});
                throw err;
            }
        }
    }

    private parseDownloadFileName(url: string, contentDisposition?: string | null) {
        const fileNameFromDisposition = contentDisposition
            ?.match(/filename\*\s*=\s*(?:UTF-8''|')?([^;]+)/i)?.[1]
            ?.trim()
            ?.replace(/^"(.*)"$/, "$1");

        if (fileNameFromDisposition) {
            return this.desktop.lib.fs.sanitizeWindowsFilename(
                decodeURIComponent(fileNameFromDisposition),
            );
        }

        const fileNamePlain = contentDisposition
            ?.match(/filename\s*=\s*("?)([^";]+)\1/i)?.[2]
            ?.trim();

        if (fileNamePlain) {
            return this.desktop.lib.fs.sanitizeWindowsFilename(
                fileNamePlain,
            );
        }

        try {
            const pathname = new URL(url).pathname;
            const rawFileName = pathname.split("/").pop() || "download.zip";
            return this.desktop.lib.fs.sanitizeWindowsFilename(decodeURIComponent(rawFileName));
        } catch {
            return "download.zip";
        }
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
        const extractMode: ArchiveExtractPathMode =
            await this.desktop.setting.mod.getArchiveExtractPathMode();

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

    public async downloadToGroup(url: string, groupPath: string): Promise<"started"> {
        const trimmedUrl = url.trim();

        if (!trimmedUrl) {
            throw new Error("Download URL is required.");
        }

        let parsedUrl: URL;
        try {
            parsedUrl = new URL(trimmedUrl);
        } catch {
            throw new Error("Invalid download URL.");
        }

        if (!["http:", "https:"].includes(parsedUrl.protocol)) {
            throw new Error("Only HTTP(S) URLs are supported.");
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
        const fileSizeHeader = resp.headers.get("Content-Length");
        const fileSize = fileSizeHeader ? Number(fileSizeHeader) : undefined;
        const suggestedFileName = this.parseDownloadFileName(
            realFileUrl,
            resp.headers.get("Content-Disposition"),
        );

        const archiveExt = path.extname(suggestedFileName);
        const tempArchiveName = `${nanoid()}${archiveExt || ".zip"}`;
        const savePath = path.join(groupPath, tempArchiveName);

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

                await this.downloadFile({
                    url: realFileUrl,
                    savePath,
                    fileSize,
                    signal: abortController.signal,
                    onProgress: (bytes) => {
                        downloadedBytes += bytes;
                        throttledUpdate(downloadedBytes);
                    },
                });

                throttledUpdate.flush();

                if (abortController.signal.aborted) throw new Error("Aborted");

                await this.extractDownloadedArchive(savePath, groupPath);
                await fse.rm(savePath, { force: true });

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
                await fse.remove(savePath).catch(() => {});

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
        // const title = this.desktop.lib.fs.sanitizeWindowsFilename(_title);

        const respPromise = ky.head(fileUrl, {
            redirect: "follow",
            throwHttpErrors: false,
        });

        new Notification({
            title: "다운로드 준비중",
            body: "다운로드 URL을 가져오고 있습니다...",
        }).show();

        const resp = await respPromise;

        if (!resp.ok) {
            throw new Error(`Failed to get real file URL: ${resp.statusText}`);
        }

        const realFileUrl = resp.url;
        const fileSize = Number(resp.headers.get("Content-Length"));
        const fileName = realFileUrl.split("/").pop()?.split("?")[0] || "";
        const suggestedFileName = this.desktop.lib.fs.sanitizeWindowsFilename(fileName);

        const result =
            await this.desktop.lib.pathSelector.getSelectedPathWithModeModal(suggestedFileName);
        if (!result.path) return "canceled";

        const finalFileName = result.fileName || suggestedFileName;
        const savePath = path.join(result.path, finalFileName);

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
                    size: fileSize,
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
            path: result.path,
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

                await this.downloadFile({
                    url: realFileUrl,
                    savePath,
                    fileSize,
                    signal: abortController.signal,
                    onProgress: (bytes) => {
                        downloadedBytes += bytes;
                        throttledUpdate(downloadedBytes);
                    },
                });

                throttledUpdate.flush();

                if (abortController.signal.aborted) throw new Error("Aborted");

                const extractedPath = path.dirname(savePath);
                const finalPath = await this.desktop.service.archive.extract(
                    savePath,
                    extractedPath,
                );
                console.log("finalPath", finalPath);
                await fse.rm(savePath, { force: true });

                let previewPromise: Promise<void> | null = null;
                if (previewUrl) {
                    const previewSavePath = path.join(finalPath, "preview.jpg");
                    previewPromise = this.downloadFile({
                        url: previewUrl,
                        savePath: previewSavePath,
                    });
                }

                this.desktop.service.transfer.markFileCompleted(pid, pid);

                this.desktop.service.transfer.updateTransfer(pid, {
                    status: "completed",
                    progress: 100,
                    transferedSize: fileSize,
                    transferedFiles: 1,
                });

                const mainWindow = this.desktop.window.main.window;
                if (mainWindow && result.path) {
                    this.desktop.ipc.postMessageToWindow(mainWindow, "download:completed", {
                        path: result.path,
                        name: finalFileName,
                    });
                }

                if (previewPromise && mainWindow && result.path) {
                    await previewPromise;
                    this.desktop.ipc.postMessageToWindow(mainWindow, "download:completed", {
                        path: result.path,
                        name: finalFileName,
                        disableToast: true,
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
            }
        });

        return "started";
    }

    public async HuiDownloader(props: {
        fileUrl: string;
        title: string;
    }): Promise<"started" | "canceled"> {
        const { title: _title, fileUrl } = props;
        const title = this.desktop.lib.fs.sanitizeWindowsFilename(_title);
        const result = await this.desktop.lib.pathSelector.getSelectedPathWithModeModal(title);

        if (!result.path) {
            return "canceled";
        }

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
        const savePath = path.join(result.path, finalFileName);
        const fileSize = Number(resp.headers.get("Content-Length"));

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
                    size: fileSize,
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
            name: title,
            initialStatus: "pending",
            path: result.path,
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

                await this.downloadFile({
                    url: fileUrl,
                    savePath,
                    fileSize,
                    signal: abortController.signal,
                    onProgress: (bytes) => {
                        downloadedBytes += bytes;
                        throttledUpdate(downloadedBytes);
                    },
                });

                throttledUpdate.flush();

                if (abortController.signal.aborted) throw new Error("Aborted");

                await this.desktop.service.archive.extract(savePath, path.dirname(savePath));
                await fse.rm(savePath, { force: true });

                this.desktop.service.transfer.markFileCompleted(pid, pid);

                this.desktop.service.transfer.updateTransfer(pid, {
                    status: "completed",
                    progress: 100,
                    transferedSize: fileSize,
                    transferedFiles: 1,
                });

                const mainWindow = this.desktop.window.main.window;
                if (mainWindow && result.path) {
                    this.desktop.ipc.postMessageToWindow(mainWindow, "download:completed", {
                        path: result.path,
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
            }
        });

        return "started";
    }
}

export default CustomDownloader;
