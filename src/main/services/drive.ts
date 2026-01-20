import { NahidaDesktop } from "..";
import { eden } from "@main/client";
import Download, { DownloadMetadata, DownloadParams } from "@main/lib/download";
import { gunzip, gzip, zstdCompress, zstdDecompress } from "node:zlib";
import { promisify } from "node:util";
import { dialog } from "electron";
import Upload, { DirectoriesComponent, UploadParams } from "@main/lib/upload";
import { nanoid } from "nanoid";
import { windowsReservedNameRegex } from "filename-reserved-regex";

export const gzipAsync = promisify(gzip);
export const gunzipAsync = promisify(gunzip);
export const zstdCompressAsync = promisify(zstdCompress);
export const zstdDecompressAsync = promisify(zstdDecompress);

export type TransferParams = UploadParams | DownloadParams;

export interface PendingDownload {
    id: string;
    data: DownloadMetadata;
    suggestedName?: string;
}

export class DriveService {
    private readonly desktop: NahidaDesktop;
    private readonly download: Download;
    private readonly upload: Upload;
    private readonly pendingDownloads: Map<string, PendingDownload> = new Map();

    public constructor(desktop: NahidaDesktop) {
        this.desktop = desktop;
        this.download = new Download(this.desktop);
        this.upload = new Upload(this.desktop);
    }

    private async makeMyDrive() {
        const { data, error } = await eden.akasha.drive.my.post();
        if (error) {
            throw String(error);
        }
        return data;
    }

    get = {
        item: async (itemId: string): Promise<any> => {
            const { data, error } = await eden.akasha.content({ id: itemId }).get();

            if (error) {
                if (error.status === 404 && error.value === "user_drive_not_generated") {
                    await this.makeMyDrive();
                    return this.desktop.service.drive.get.item(itemId);
                }

                throw error;
            }

            return data;
        },
    };

    post = {
        dir: async (parentId: string, name: string) => {
            const { data, error } = await eden.akasha.dir.create_many.post({
                current: parentId,
                parentId: parentId,
                dirs: [{ path: parentId, name }],
            });
            if (error) {
                throw error;
            }
            return data;
        },

        dirs: async (parentId: string, dirs: DirectoriesComponent[]) => {
            const { data, error } = await eden.akasha["create-dirs"].post({
                parentId,
                dirs,
            });
            if (error) {
                throw new Error(error.value.toString());
            }
            return data;
        },
    };

    patch = {
        rename: async (itemId: string, name: string) => {
            const { data, error } = await eden.akasha.content
                .rename({ id: itemId })
                .post({ rename: name });
            if (error) {
                throw error;
            }
            return data;
        },
    };

    delete = {
        items: async (ids: string[]) => {
            const { error } = await eden.akasha.content.trash.trash_many.post({
                uuids: ids,
            });
            if (error) {
                throw error;
            }
        },
    };

    fn = {
        startUpload: async ({ destId, paths }: { destId: string; paths?: string[] }) => {
            const selectedPaths = await this.selectUploadPaths(paths);
            if (!selectedPaths) return;

            const preparation = await this.upload.prepareUpload(selectedPaths, []);
            if (preparation.files.length < 1) {
                throw new Error("업로드 가능한 파일이 없습니다");
            }

            const { pid, restartParams, abortController } = await this.createUploadTransferEntry({
                destId,
                paths: selectedPaths,
                preparation,
            });

            this.processUploadAsync({ pid, restartParams, preparation, abortController }).catch(
                (err) => {
                    this.desktop.logger.error(err, "Drive:Upload:Preprocessing");
                    this.desktop.service.transfer.updateTransfer(pid, { status: "error" });
                },
            );
        },

        startDownload: async ({
            id,
            data,
            suggestedName,
            targetPath,
        }: {
            id: string;
            data?: DownloadMetadata;
            suggestedName?: string;
            targetPath?: string;
        }) => {
            if (!targetPath) {
                const downloadId = nanoid();

                if (!data) {
                    const abortController = new AbortController();
                    data = await this.download.getDownloadUrl(id, abortController.signal);
                }

                this.pendingDownloads.set(downloadId, { id, data, suggestedName });

                const mainWindow = this.desktop.window.main;
                if (mainWindow) {
                    this.desktop.ipc.postMessageToWindow(mainWindow, "download:modeSelect", {
                        downloadId,
                        suggestedName,
                    });
                }
                return;
            }

            const savePath = targetPath;
            const isWritable = this.desktop.lib.fs.isPathWritable(savePath);
            if (!isWritable) {
                throw new Error("Path is not writable");
            }

            const { pid, restartParams, abortController } = await this.createDownloadTransferEntry({
                id,
                savePath,
                suggestedName,
            });

            this.processDownloadAsync({
                id,
                pid,
                restartParams,
                abortController,
                data,
                suggestedName,
            }).catch((err) => {
                this.desktop.logger.error(err, "Drive:Download:Preprocessing");
                this.desktop.service.transfer.updateTransfer(pid, { status: "error" });
            });
        },

        startDownloadWithPath: async (downloadId: string, targetPath: string) => {
            const pending = this.pendingDownloads.get(downloadId);
            if (!pending) {
                throw new Error("Pending download not found");
            }

            this.pendingDownloads.delete(downloadId);

            await this.fn.startDownload({
                id: pending.id,
                data: pending.data,
                suggestedName: pending.suggestedName,
                targetPath,
            });
        },

        cancelPendingDownload: async (downloadId: string) => {
            this.pendingDownloads.delete(downloadId);
        },

        selectPathForDownload: async (downloadId: string) => {
            const pending = this.pendingDownloads.get(downloadId);
            if (!pending) {
                throw new Error("Pending download not found");
            }

            const savePath = await this.selectDownloadPath();
            if (!savePath) return;

            await this.fn.startDownloadWithPath(downloadId, savePath);
        },

        resumeTransfer: async (pid: string) => {
            const transfer = this.desktop.service.transfer.getTransferByPID(pid);
            if (!transfer || !transfer.restartParams) return;

            const params = transfer.restartParams as TransferParams;

            this.desktop.service.transfer.registerRunner(pid, async () => {
                await this.executeResumeRunner({ pid, params, currentTransfer: transfer });
            });

            this.desktop.service.transfer.manualStart(pid);
        },

        retryTransfer: async (pid: string) => {
            return this.fn.resumeTransfer(pid);
        },
    };

    private async selectUploadPaths(paths?: string[]): Promise<string[] | null> {
        const window = this.desktop.window.main;
        if (!window) {
            throw new Error("main window not found");
        }

        let selectedPaths: string[] = [];
        if (paths) {
            selectedPaths = paths;
        } else {
            const dialogResult = await dialog.showOpenDialog(window, {
                properties: ["openDirectory", "multiSelections"],
            });
            if (!dialogResult || dialogResult.canceled) {
                return null;
            }
            selectedPaths = dialogResult.filePaths;
        }

        const isWritable = this.desktop.lib.fs.isPathWritable(selectedPaths[0]);
        if (!isWritable) {
            throw new Error("Path is not writable");
        }

        return selectedPaths;
    }

    private async createUploadTransferEntry({
        destId,
        paths,
        preparation,
    }: {
        destId: string;
        paths: string[];
        preparation: any;
    }) {
        const { pid, files, directories, processName } = preparation;
        const restartParams: UploadParams = { type: "upload", destId, paths };
        const abortController = new AbortController();

        await this.desktop.service.transfer.createTransfer(
            pid,
            "upload",
            {
                files: files.map((f: any) => ({
                    uuid: f.FID,
                    fileId: "",
                    parentId: f.parentPath || null,
                    name: f.name,
                    size: f.size,
                    compAlg: null,
                    url: "",
                })),
                dirs: directories.map((d: any) => ({
                    uuid: "",
                    parentId: d.parentPath || null,
                    name: d.name,
                })),
            },
            abortController,
            processName,
            restartParams,
            "preparing",
            paths[0],
        );

        return { pid, restartParams, abortController };
    }

    private async processUploadAsync({
        pid,
        restartParams,
        preparation,
        abortController,
    }: {
        pid: string;
        restartParams: UploadParams;
        preparation: any;
        abortController: AbortController;
    }) {
        const { files, directories, totalSize, processName } = preparation;

        const dummyFiles = files.map((f: any) => ({
            ...f,
            parentId: "",
            fullPath: f.fullPath,
        }));

        const hashedFiles = await this.upload.calculateHashes(dummyFiles as any);
        const fileHashes: Record<string, string> = {};
        hashedFiles.forEach((f) => {
            fileHashes[f.FID] = f.sha256;
        });

        const transfer = this.desktop.service.transfer.getTransferByPID(pid);
        if (transfer && transfer.restartParams) {
            transfer.restartParams = {
                ...(transfer.restartParams as UploadParams),
                fileHashes,
            };
        }

        this.desktop.service.transfer.registerRunner(pid, async () => {
            await this.executeUploadRunner({
                pid,
                restartParams,
                preparation,
                totalSize,
                processName,
            });
        });

        this.desktop.service.transfer.updateTransfer(pid, { status: "pending" });
        this.desktop.service.transfer.processQueue();
    }

    private async executeUploadRunner({
        pid,
        restartParams,
        preparation,
        totalSize,
        processName,
    }: {
        pid: string;
        restartParams: UploadParams;
        preparation: any;
        totalSize: number;
        processName: string;
    }) {
        const currentTransfer = this.desktop.service.transfer.getTransferByPID(pid);
        if (!currentTransfer) return;

        const currentParams = currentTransfer.restartParams as UploadParams;
        const newAbort = new AbortController();
        this.desktop.service.transfer.updateAbortController(pid, newAbort);

        await this.upload.executeUpload({
            pid,
            params: currentParams,
            files: preparation.files,
            directories: preparation.directories,
            totalSize,
            processName,
            abortController: newAbort,
        });
    }

    private async selectDownloadPath(): Promise<string | null> {
        const window = this.desktop.window.main;
        if (!window) {
            throw new Error("main window not found");
        }

        const dialogResult = await dialog.showOpenDialog(window, {
            properties: ["openDirectory"],
        });
        if (!dialogResult || dialogResult.canceled) {
            return null;
        }
        const savePath = dialogResult.filePaths[0];

        const isWritable = this.desktop.lib.fs.isPathWritable(savePath);
        if (!isWritable) {
            throw new Error("Path is not writable");
        }

        return savePath;
    }

    private async createDownloadTransferEntry({
        id,
        savePath,
        suggestedName,
    }: {
        id: string;
        savePath: string;
        suggestedName?: string;
    }) {
        const pid = nanoid();
        const abortController = new AbortController();
        const restartParams: DownloadParams = { type: "download", id, savePath, suggestedName };

        await this.desktop.service.transfer.createTransfer(
            pid,
            "download",
            { root: { id: "", parentId: null, name: "Loading..." }, files: [], dirs: [] },
            abortController,
            "Preparing Download...",
            restartParams,
            "preparing",
            savePath,
        );

        return { pid, restartParams, abortController };
    }

    private async processDownloadAsync({
        id,
        pid,
        restartParams,
        abortController,
        data,
        suggestedName,
    }: {
        id: string;
        pid: string;
        restartParams: DownloadParams;
        abortController: AbortController;
        data?: DownloadMetadata;
        suggestedName?: string;
    }) {
        if (!data) {
            data = await this.download.getDownloadUrl(id, abortController.signal);
        }

        if (suggestedName && data.root) {
            data.root.name = suggestedName.replace(windowsReservedNameRegex(), " ");
        }

        const name = data.root?.name || "Download";

        const transfer = this.desktop.service.transfer.getTransferByPID(pid);
        if (transfer) {
            transfer.data = data;
            transfer.totalSize = data.totalBytes;
            transfer.totalFiles = data.files.length;
            transfer.name = name;
        }

        this.desktop.service.transfer.registerRunner(pid, async () => {
            await this.executeDownloadRunner({ pid, restartParams, data });
        });

        this.desktop.service.transfer.updateTransfer(pid, {
            status: "pending",
            name,
        });
        this.desktop.service.transfer.processQueue();
    }

    private async executeDownloadRunner({
        pid,
        restartParams,
        data,
    }: {
        pid: string;
        restartParams: DownloadParams;
        data: any;
    }) {
        const currentTransfer = this.desktop.service.transfer.getTransferByPID(pid);
        if (!currentTransfer) return;

        const newAbort = new AbortController();
        this.desktop.service.transfer.updateAbortController(pid, newAbort);

        await this.download.executeDownload({
            pid,
            params: restartParams,
            data,
            abort: newAbort,
        });
    }

    private async executeResumeRunner({
        pid,
        params,
        currentTransfer,
    }: {
        pid: string;
        params: TransferParams;
        currentTransfer: any;
    }) {
        const transferNow = this.desktop.service.transfer.getTransferByPID(pid);
        if (!transferNow) return;

        const newAbort = new AbortController();
        this.desktop.service.transfer.updateAbortController(pid, newAbort);
        this.desktop.service.transfer.resetStartTime(pid);

        if (params.type === "upload") {
            const { files, directories } = await this.upload.prepareUpload(params.paths, []);

            await this.upload.executeUpload({
                pid,
                params,
                files,
                directories,
                totalSize: currentTransfer.totalSize,
                processName: currentTransfer.name,
                abortController: newAbort,
                initialTransferedSize: currentTransfer.transferedSize,
            });
        } else {
            await this.download.executeDownload({
                pid,
                params,
                data: currentTransfer.data,
                abort: newAbort,
                initialTransferedSize: currentTransfer.transferedSize,
                initialTransferedFiles: currentTransfer.transferedFiles,
            });
        }
    }
}
