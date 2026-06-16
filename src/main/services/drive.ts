import { promisify } from "node:util";
import { gunzip, gzip, zstdCompress, zstdDecompress } from "node:zlib";
import type { Treaty } from "@elysiajs/eden";
import { eden } from "@main/client";
import Download, { type DownloadMetadata, type DownloadParams } from "@main/lib/download";
import Upload, {
    type DirectoriesComponent,
    type FilesComponent,
    type UploadConflictStrategy,
    type UploadParams,
} from "@main/lib/upload";
import type { LinkData } from "@main/server";
import { dialog } from "electron";
import { retry } from "es-toolkit";
import fse from "fs-extra";
import Heap from "mnemonist/heap";
import { nanoid } from "nanoid";
import type { NahidaDesktop } from "..";
import type { LocalTransfer } from "./transfer";
import { processChunked } from "./util";

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

    private balanceAndInterleaveFiles<T extends { size: number }>(
        files: T[],
        maxPerChunk: number = 100,
    ): T[] {
        if (!files || files.length === 0) return [];
        if (maxPerChunk <= 0) throw new Error("maxPerChunk must be greater than 0");

        const sortedFiles = [...files].sort((a, b) => b.size - a.size);

        const chunkCount = Math.ceil(sortedFiles.length / maxPerChunk);

        type Chunk = { currentSize: number; files: T[] };
        const chunks = Array.from(
            { length: chunkCount },
            (): Chunk => ({ currentSize: 0, files: [] }),
        );
        const heap = Heap.from(chunks, (a, b) => a.currentSize - b.currentSize);

        for (const file of sortedFiles) {
            const targetChunk = heap.peek();
            if (!targetChunk) {
                throw new Error("balanceAndInterleaveFiles: heap exhausted while assigning files");
            }
            targetChunk.files.push(file);
            targetChunk.currentSize += file.size;
            heap.replace(targetChunk);
        }

        return chunks
            .filter((chunk) => chunk.files.length > 0)
            .flatMap((chunk) => {
                const interleaved: T[] = [];
                let left = 0;
                let right = chunk.files.length - 1;

                while (left <= right) {
                    interleaved.push(chunk.files[left]);
                    if (left !== right) {
                        interleaved.push(chunk.files[right]);
                    }
                    left++;
                    right--;
                }

                return interleaved;
            });
    }

    get = {
        item: async (itemId: string): Promise<DriveItem> => {
            const { data, error } = await eden.akasha.content({ id: itemId }).get();
            if (error) throw error;
            return data;
        },
    };

    post = {
        dir: async (parentId: string, name: string, signal?: AbortSignal) => {
            this.desktop.lib.fs.assertValidWindowsFilename(name);

            return await retry(
                async () => {
                    const { data, error } = await eden.akasha.dir.create_many.post({
                        parentId,
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
            this.desktop.lib.fs.assertValidWindowsFilename(name);

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
        items: async (ids: string[], action: "trash" | "delete") => {
            if (action === "trash") {
                const { error } = await eden.akasha.content.trash.trash_many.post({
                    uuids: ids,
                });
                if (error) throw error.value;
            } else if (action === "delete") {
                const { error } = await eden.akasha.content.delete_many.post({
                    uuids: ids,
                });
                if (error) throw error.value;
            } else {
                throw new Error("INVALID_ACTION");
            }
        },
    };

    fn = {
        startUpload: async ({
            destId,
            paths,
            conflictStrategy = "suffix",
        }: {
            destId: string;
            paths?: string[];
            conflictStrategy?: UploadConflictStrategy;
        }) => {
            const selectedPaths = await this.selectUploadPaths(paths);
            if (!selectedPaths) return;

            const existing = await this.get.item(destId);
            const preparation = await this.upload.prepareUploadWithConflictStrategy(
                selectedPaths,
                existing.children ?? [],
                conflictStrategy,
            );
            if (preparation.files.length < 1) {
                throw new Error("NO_UPLOADABLE_FILES");
            }

            const { pid, restartParams, abortController } = await this.createUploadTransferEntry({
                destId,
                paths: selectedPaths,
                conflictStrategy,
                preparation,
            });

            this.processUploadAsync({
                pid,
                currentId: destId,
                restartParams,
                preparation,
                abortController,
            }).catch((err) => {
                this.desktop.logger.error(err, "Drive:Upload:Preprocessing");
                this.desktop.service.transfer.updateTransfer(pid, {
                    status: "error",
                    error: err instanceof Error ? err.message : String(err),
                });
            });
        },

        getUploadConflicts: async ({ destId, paths }: { destId: string; paths?: string[] }) => {
            const selectedPaths = await this.selectUploadPaths(paths);
            if (!selectedPaths) {
                return { selectedPaths: null, conflicts: [] as string[] };
            }

            const existing = await this.get.item(destId);
            const conflicts = await this.upload.getRootNameConflicts(
                selectedPaths,
                existing.children ?? [],
            );

            return {
                selectedPaths,
                conflicts: conflicts.map((conflict) => conflict.name),
            };
        },

        startDownload: async ({
            id,
            data,
            link,
            suggestedName,
            targetPath,
        }: {
            id: string;
            data?: DownloadMetadata;
            link?: LinkData;
            suggestedName?: string;
            targetPath?: string;
        }): Promise<"started" | "canceled"> => {
            if (suggestedName) {
                suggestedName = this.desktop.lib.fs.sanitizeWindowsFilename(suggestedName);
            }

            let savePath = targetPath;

            if (!savePath) {
                const result =
                    await this.desktop.lib.pathSelector.getSelectedPathWithModeModal(suggestedName);
                if (!result.path) {
                    this.desktop.logger.info(
                        "Download cancelled by user selection",
                        "Drive:Download",
                    );
                    return "canceled";
                }
                savePath = this.desktop.lib.fs.sanitizePath(result.path);
            }

            const isWritable = await this.desktop.lib.fs.isPathWritable(savePath);
            if (!isWritable) {
                throw new Error(`Path is not writable: ${savePath}`);
            }

            try {
                const { pid, restartParams, abortController } =
                    await this.createDownloadTransferEntry({
                        id,
                        currentId: data?.root.id || "",
                        savePath,
                        suggestedName,
                    });

                this.processDownloadAsync({
                    id,
                    pid,
                    restartParams,
                    abortController,
                    data,
                    link,
                    suggestedName,
                    savePath,
                }).catch((err) => {
                    this.desktop.logger.error(err, "Drive:Download:Preprocessing");
                    this.desktop.service.transfer.updateTransfer(pid, {
                        status: "error",
                        error: err instanceof Error ? err.message : String(err),
                    });
                });

                return "started";
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
                await this.executeResumeRunner({
                    currentId: transfer.currentId,
                    pid,
                    params,
                    currentTransfer: transfer,
                });
            });

            this.desktop.service.transfer.manualStart(pid);
        },

        retryTransfer: async (pid: string) => {
            return this.fn.resumeTransfer(pid);
        },

        moveMany: async ({ ids, destId }: { ids: string[]; destId: string }) => {
            const { data, error } = await eden.akasha.content.move_many.post({
                uuids: ids,
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
        conflictStrategy,
        preparation,
    }: {
        destId: string;
        paths: string[];
        conflictStrategy: UploadConflictStrategy;
        preparation: {
            pid: string;
            files: FilesComponent[];
            directories: DirectoriesComponent[];
            totalSize: number;
            processName: string;
        };
    }) {
        const { pid, files, directories, processName } = preparation;
        const restartParams: UploadParams = { type: "upload", destId, paths, conflictStrategy };
        const abortController = new AbortController();

        await this.desktop.service.transfer.createTransfer({
            pid,
            type: "upload",
            currentId: destId,
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
        currentId,
        pid,
        restartParams,
        preparation,
    }: {
        currentId: string;
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

        const dummyFiles = this.balanceAndInterleaveFiles(
            files.map((f) => ({
                ...f,
                parentId: "",
                fullPath: f.fullPath,
            })),
        );

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
                currentId,
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
        currentId,
        pid,
        preparation,
        totalSize,
        processName,
    }: {
        currentId: string;
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
            currentId,
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
        currentId,
        savePath,
        suggestedName,
    }: {
        id: string;
        currentId: string;
        savePath: string;
        suggestedName?: string;
    }) {
        const pid = nanoid();
        const abortController = new AbortController();
        const restartParams: DownloadParams = { type: "download", id, savePath, suggestedName };

        await this.desktop.service.transfer.createTransfer({
            pid,
            type: "download",
            currentId,
            data: { root: { id: "", parentId: null, name: "Loading" }, files: [], dirs: [] },
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
        link,
        suggestedName,
        savePath,
    }: {
        id: string;
        pid: string;
        restartParams: DownloadParams;
        abortController: AbortController;
        data?: DownloadMetadata;
        link?: LinkData;
        suggestedName?: string;
        savePath: string;
    }) {
        if (!data) {
            data = await this.download.getDownloadUrl({ id, link, signal: abortController.signal });
        }

        if (data.root) {
            if (suggestedName) {
                data.root.name = suggestedName;
            }

            const sanitized = this.desktop.lib.fs.sanitizeWindowsFilename(data.root.name);

            const entries = await fse.readdir(savePath);
            data.root.name = this.desktop.lib.fs.getUniqueName(sanitized, entries);
        }

        if (data.files) {
            await processChunked(
                data.files,
                (file) => {
                    file.name = this.desktop.lib.fs.sanitizeWindowsFilename(file.name);
                },
                2000,
                abortController.signal,
            );
        }

        if (data.dirs) {
            await processChunked(
                data.dirs,
                (dir) => {
                    dir.name = this.desktop.lib.fs.sanitizeWindowsFilename(dir.name);
                },
                2000,
                abortController.signal,
            );
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
        currentId,
        pid,
        params,
        currentTransfer,
    }: {
        currentId?: string;
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
            if (!currentId) {
                throw new Error("currentId is required for upload");
            }

            const existing = await this.get.item(currentId);
            const { files, directories } = await this.upload.prepareUploadWithConflictStrategy(
                params.paths,
                existing.children ?? [],
                params.conflictStrategy ?? "suffix",
            );

            await this.upload.executeUpload({
                currentId,
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
