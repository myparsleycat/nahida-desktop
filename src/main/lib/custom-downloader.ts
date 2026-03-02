import path from "node:path";
import { pipeline } from "node:stream/promises";
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

    public async GBDownloader(props: {
        fileUrl: string;
        title: string;
        previewUrl?: string | null;
    }) {
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
        if (!result.path) return;

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
    }

    public async HuiDownloader(props: { fileUrl: string; title: string }) {
        const { title: _title, fileUrl } = props;
        const title = this.desktop.lib.fs.sanitizeWindowsFilename(_title);
        const result = await this.desktop.lib.pathSelector.getSelectedPathWithModeModal(title);

        if (!result.path) {
            return;
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
    }
}

export default CustomDownloader;
