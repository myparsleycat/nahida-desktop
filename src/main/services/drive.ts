import { promisify } from "node:util";
import { gunzip, gzip, zstdCompress, zstdDecompress } from "node:zlib";
import type { Treaty } from "@elysiajs/eden";
import { eden } from "@main/client";
import Download, { type DownloadMetadata, type DownloadParams } from "@main/lib/download";
import Upload, {
    type DirectoriesComponent,
    type FilesComponent,
    type UploadParams,
} from "@main/lib/upload";
import { dialog } from "electron";
import { retry } from "es-toolkit";
import { windowsReservedNameRegex } from "filename-reserved-regex";
import { nanoid } from "nanoid";
import type { NahidaDesktop } from "..";
import type { LocalTransfer } from "./transfer";

const Fn = eden.akasha.content({ id: "" }).get;
type DriveItem = Treaty.Data<typeof Fn>;

export const gzipAsync = promisify(gzip);
export const gunzipAsync = promisify(gunzip);
export const zstdCompressAsync = promisify(zstdCompress);
export const zstdDecompressAsync = promisify(zstdDecompress);

export type TransferParams = UploadParams | DownloadParams;

export class DriveService {
    private readonly desktop: NahidaDesktop;
    private readonly download: Download;
    private readonly upload: Upload;

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
        item: async (itemId: string): Promise<DriveItem> => {
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
        dir: async (parentId: string, name: string, signal?: AbortSignal) => {
            return await retry(
                async () => {
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
                {
                    retries: 3,
                    delay: (attempt) => 2 ** attempt * 1000,
                    shouldRetry: () => !signal?.aborted,
                },
            );
        },

        dirs: async (parentId: string, dirs: DirectoriesComponent[], signal?: AbortSignal) => {
            return await retry(
                async () => {
                    const { data, error } = await eden.akasha["create-dirs"].post({
                        parentId,
                        dirs,
                    });
                    if (error) {
                        throw new Error(error.value.toString());
                    }
                    return data;
                },
                {
                    retries: 3,
                    delay: (attempt) => 2 ** attempt * 1000,
                    shouldRetry: () => !signal?.aborted,
                },
            );
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

            const existing = await this.get.item(destId);
            const preparation = await this.upload.prepareUpload(
                selectedPaths,
                existing.children ?? [],
            );
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
                    this.desktop.service.transfer.updateTransfer(pid, {
                        status: "error",
                        error: err instanceof Error ? err.message : String(err),
                    });
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
            try {
                let savePath = targetPath;

                if (!savePath) {
                    const result =
                        await this.desktop.lib.pathSelector.getSelectedPathWithModeModal(
                            suggestedName,
                        );
                    if (!result.path) {
                        this.desktop.logger.info(
                            "Download cancelled by user selection",
                            "Drive:Download",
                        );
                        return;
                    }
                    savePath = result.path;
                }

                const isWritable = this.desktop.lib.fs.isPathWritable(savePath);
                if (!isWritable) {
                    throw new Error(`Path is not writable: ${savePath}`);
                }

                const { pid, restartParams, abortController } =
                    await this.createDownloadTransferEntry({
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
                    this.desktop.service.transfer.updateTransfer(pid, {
                        status: "error",
                        error: err instanceof Error ? err.message : String(err),
                    });
                });
            } catch (error) {
                console.error("Drive:Download:Error", error);
                this.desktop.logger.error(error, "Drive:Download:Start");
                throw error;
            }
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

        moveMany: async ({ ids, destId }: { ids: string[]; destId: string }) => {
            const { data, error } = await eden.akasha.content.move_many.post({
                uuids: ids,
                current: destId,
                target: destId,
            });
            return { data, error };
        },
    };

    private async selectUploadPaths(paths?: string[]): Promise<string[] | null> {
        const window = this.desktop.window.main.window;
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
        preparation: {
            pid: string;
            files: FilesComponent[];
            directories: DirectoriesComponent[];
            totalSize: number;
            processName: string;
        };
    }) {
        const { pid, files, directories, processName } = preparation;
        const restartParams: UploadParams = { type: "upload", destId, paths };
        const abortController = new AbortController();

        await this.desktop.service.transfer.createTransfer({
            pid,
            type: "upload",
            data: {
                files: files.map((f) => ({
                    id: f.FID,
                    fileId: "",
                    parentId: f.parentPath || null,
                    name: f.name,
                    size: f.size,
                    compAlg: null,
                    url: "",
                })),
                dirs: directories.map((d) => ({
                    id: "",
                    parentId: d.parentPath || null,
                    name: d.name,
                })),
            },
            abortController,
            name: processName,
            restartParams,
            initialStatus: "preparing",
            path: paths[0],
        });

        return { pid, restartParams, abortController };
    }

    private async processUploadAsync({
        pid,
        restartParams,
        preparation,
    }: {
        pid: string;
        restartParams: UploadParams;
        preparation: {
            pid: string;
            files: FilesComponent[];
            directories: DirectoriesComponent[];
            totalSize: number;
            processName: string;
        };
        abortController: AbortController;
    }) {
        const { files, totalSize, processName } = preparation;

        const dummyFiles = files.map((f) => ({
            ...f,
            parentId: "",
            fullPath: f.fullPath,
        }));

        const hashedFiles = await this.upload.calculateHashes(dummyFiles, (count) => {
            this.desktop.service.transfer.updateTransfer(pid, {
                transferedFiles: count,
            });
        });
        const fileHashes: Record<string, string> = {};
        hashedFiles.forEach((f) => {
            fileHashes[f.FID] = f.sha256;
        });

        const transfer = this.desktop.service.transfer.getTransferByPID(pid);
        if (transfer?.restartParams) {
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
        preparation,
        totalSize,
        processName,
    }: {
        pid: string;
        restartParams: UploadParams;
        preparation: {
            pid: string;
            files: FilesComponent[];
            directories: DirectoriesComponent[];
            totalSize: number;
            processName: string;
        };
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

        await this.desktop.service.transfer.createTransfer({
            pid,
            type: "download",
            data: { root: { id: "", parentId: null, name: "Loading..." }, files: [], dirs: [] },
            abortController,
            name: "Preparing Download...",
            restartParams,
            initialStatus: "preparing",
            path: savePath,
        });

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
            data.root.name = suggestedName.replace(windowsReservedNameRegex(), " ").trim();
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
        data: DownloadMetadata;
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
        currentTransfer: LocalTransfer;
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
                initialTransferedSize: 0,
                initialTransferedFiles: 0,
            });
        }
    }
}
