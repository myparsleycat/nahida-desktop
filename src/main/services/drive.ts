import path from "node:path";
import { promisify } from "node:util";
import { gunzip, gzip, zstdCompress, zstdDecompress } from "node:zlib";

import type { Treaty } from "@elysiajs/eden";
import { eden } from "@main/client";
import Download, {
    BATCH_ROOT_ID,
    type DownloadMetadata,
    type DownloadParams,
} from "@main/lib/download";
import Upload, {
    type DirectoriesComponent,
    type FilesComponent,
    type UploadConflictStrategy,
    type UploadParams,
} from "@main/lib/upload";
import type { LinkData } from "@main/server";
import { BACKEND_URL } from "@shared/const";
import type { DownloadSource } from "@shared/mod";
import { toErrorMessage } from "@shared/utils";
import { dialog } from "electron";
import { retry } from "es-toolkit";
import fse from "fs-extra";
import { HTTPError } from "ky";
import Heap from "mnemonist/heap";
import { nanoid } from "nanoid";

import type { NahidaDesktop } from "..";
import type { LocalTransfer, TransferParams } from "./transfer";

import { createDriveApiError, DriveApiError } from "./drive-errors";
import { parseDriveSourceUrl } from "./drive-url";
import { processChunked } from "./util";

const Fn = eden.akasha.content({ id: "" }).get;
type DriveItem = Treaty.Data<typeof Fn>;

const DIRECTORY_CONFLICT_MESSAGES = {
    ko: {
        title: "폴더가 이미 존재합니다",
        message: (name: string) => `"${name}" 폴더가 이미 존재합니다.`,
        detail: "기존 폴더에 파일을 덮어쓰시겠습니까?",
        buttons: ["덮어쓰기", "새 이름으로 다운로드", "취소"],
    },
    en: {
        title: "Folder already exists",
        message: (name: string) => `The folder "${name}" already exists.`,
        detail: "Do you want to overwrite files in the existing folder?",
        buttons: ["Overwrite", "Download with a new name", "Cancel"],
    },
    ja: {
        title: "フォルダーはすでに存在します",
        message: (name: string) => `「${name}」フォルダーはすでに存在します。`,
        detail: "既存のフォルダーにファイルを上書きしますか？",
        buttons: ["上書き", "新しい名前でダウンロード", "キャンセル"],
    },
    zh: {
        title: "文件夹已存在",
        message: (name: string) => `文件夹“${name}”已存在。`,
        detail: "要覆盖现有文件夹中的文件吗？",
        buttons: ["覆盖", "使用新名称下载", "取消"],
    },
} as const;

export type DriveCopyFromUrlParams = {
    url: string;
    destinationId: string;
    password?: string;
    collectionId?: string;
    itemId?: string;
};

export type DriveCopyFromUrlResult = {
    source: "link" | "mod";
    copied: number;
    destinationId: string;
};

type SharedLinkAccess = {
    token: string;
    parent: {
        id: string;
        name: string;
    };
};

type ModCollection = {
    id: string;
    name: string;
    rootId: string;
    private?: boolean;
};

type ModOverview = {
    collections: ModCollection[];
};

export const gzipAsync = promisify(gzip);
export const gunzipAsync = promisify(gunzip);
export const zstdCompressAsync = promisify(zstdCompress);
export const zstdDecompressAsync = promisify(zstdDecompress);

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
        const chunks = Array.from({ length: chunkCount }, (): Chunk => ({
            currentSize: 0,
            files: [],
        }));
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
                        throw new Error(toErrorMessage(error.value));
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
                void this.desktop.service.transfer.updateTransfer(pid, {
                    status: "error",
                    error: toErrorMessage(err),
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
            items,
            targetPath,
            link,
            data,
            source = "nahidaLive",
        }: {
            items: Array<{ id: string; isDir: boolean; name: string }>;
            targetPath?: string;
            link?: LinkData;
            data?: DownloadMetadata;
            source?: Extract<DownloadSource, "drive" | "nahidaLive">;
        }): Promise<"started" | "canceled"> => {
            if (items.length === 0) return "canceled";

            for (const item of items) {
                item.name = this.desktop.lib.fs.sanitizeWindowsFilename(item.name);
            }

            const isSingle = items.length === 1;
            const single = items[0];
            let savePath = targetPath;
            let suggestedName = isSingle ? single.name : undefined;

            if (!savePath) {
                const result = await this.desktop.lib.pathSelector.getSelectedPathWithModeModal(
                    suggestedName,
                    undefined,
                    undefined,
                    source,
                    items.map((item) => item.name),
                    isSingle && !single.isDir,
                );
                if (!result.path) {
                    this.desktop.logger.info(
                        "Download cancelled by user selection",
                        "Drive:Download",
                    );
                    return "canceled";
                }
                savePath = result.path;
                suggestedName = isSingle ? (result.fileName ?? suggestedName) : undefined;
            }

            savePath = this.desktop.lib.fs.sanitizePath(savePath);

            const isWritable = await this.desktop.lib.fs.isPathWritable(savePath);
            if (!isWritable) {
                throw new Error(`Path is not writable: ${savePath}`);
            }

            try {
                const { pid, restartParams, abortController } =
                    await this.createDownloadTransferEntry({
                        id: isSingle ? single.id : items[0].id,
                        currentId: data?.root.id || "",
                        savePath,
                        suggestedName: isSingle ? suggestedName : undefined,
                    });

                this.processDownloadAsync({
                    items,
                    isSingle,
                    pid,
                    restartParams,
                    abortController,
                    data,
                    link,
                    suggestedName,
                    savePath,
                }).catch((err) => {
                    if (abortController.signal.aborted) return;
                    this.desktop.logger.error(err, "Drive:Download:Preprocessing");
                    void this.desktop.service.transfer.updateTransfer(pid, {
                        status: "error",
                        error: toErrorMessage(err),
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

            const params = transfer.restartParams;

            this.desktop.service.transfer.registerRunner(pid, async () => {
                await this.executeResumeRunner({
                    currentId: transfer.currentId,
                    pid,
                    params,
                    currentTransfer: transfer,
                });
            });

            void this.desktop.service.transfer.manualStart(pid);
        },

        retryTransfer: async (pid: string) => {
            return this.fn.resumeTransfer(pid);
        },

        moveMany: async ({ ids, destId }: { ids: string[]; destId: string }) => {
            const { data, error } = await eden.akasha.content.move_many.post({
                uuids: ids,
                target: destId,
            });
            if (error) throw error.value;
            return data;
        },

        copyMany: async ({ ids, destId }: { ids: string[]; destId: string }) => {
            const { data, error } = await eden.akasha.content.copy_many.post({
                uuids: ids,
                target: destId,
            });
            if (error) throw error.value;
            return data;
        },

        copyFromUrl: async ({
            url,
            destinationId,
            password = "",
            collectionId,
            itemId,
        }: DriveCopyFromUrlParams): Promise<DriveCopyFromUrlResult> => {
            const source = parseDriveSourceUrl(url);
            if (source.type === "link") {
                const access = await this.requestSharedLinkAccess(source.id, password);
                await this.copyRemoteItems(
                    [itemId ?? access.parent.id],
                    destinationId,
                    { "nhd-link-token": access.token },
                    "shared link",
                );
                return { source: "link", copied: 1, destinationId };
            }

            const overview = await this.requestJson<ModOverview>(
                `${BACKEND_URL}/akasha/mod/${encodeURIComponent(source.id)}`,
                undefined,
                "mod overview",
            );
            if (!isModOverview(overview)) {
                throw new DriveApiError(
                    "DRIVE_MOD_INVALID_RESPONSE",
                    "The collection response was invalid.",
                );
            }
            const publicCollections = overview.collections.filter(
                (collection) => !collection.private,
            );
            const selectedCollections = collectionId
                ? publicCollections.filter((collection) => collection.id === collectionId)
                : publicCollections;

            if (collectionId && selectedCollections.length === 0) {
                throw new DriveApiError(
                    "DRIVE_COLLECTION_NOT_FOUND",
                    "The requested collection was not found.",
                );
            }

            const sourceIds = itemId
                ? [itemId]
                : [...new Set(selectedCollections.map((collection) => collection.rootId))];
            if (sourceIds.length === 0) {
                throw new DriveApiError(
                    "DRIVE_COLLECTION_EMPTY",
                    "No public collections were found.",
                );
            }

            await this.copyRemoteItems(sourceIds, destinationId, undefined, "mod collection");
            return { source: "mod", copied: sourceIds.length, destinationId };
        },
    };

    private async requestSharedLinkAccess(linkId: string, password: string) {
        try {
            const data = await this.requestJson<unknown>(
                `${BACKEND_URL}/akasha/link/${encodeURIComponent(linkId)}`,
                {
                    method: "POST",
                    headers: { "content-type": "application/json" },
                    body: JSON.stringify({ password, cftoken: "" }),
                },
                "shared link access",
            );

            if (!isSharedLinkAccess(data)) {
                throw new DriveApiError(
                    "DRIVE_LINK_INVALID_RESPONSE",
                    "The shared link response was invalid.",
                );
            }
            return data;
        } catch (error) {
            const code = error instanceof DriveApiError ? error.code.toLowerCase() : "";
            if (code === "drive_link_password_required" || code === "missing_password") {
                throw new DriveApiError(
                    "DRIVE_LINK_PASSWORD_REQUIRED",
                    "This shared link requires a password.",
                    undefined,
                    error,
                );
            }
            if (code === "drive_link_invalid_password" || code === "invalid_password") {
                throw new DriveApiError(
                    "DRIVE_LINK_INVALID_PASSWORD",
                    "The shared link password is incorrect.",
                    undefined,
                    error,
                );
            }
            throw error;
        }
    }

    private async copyRemoteItems(
        ids: string[],
        destinationId: string,
        headers: Record<string, string> | undefined,
        source: string,
    ) {
        if (ids.length === 0) {
            throw new DriveApiError("DRIVE_COPY_EMPTY", "No items were selected for copying.");
        }

        await this.requestJson(
            `${BACKEND_URL}/akasha/content/copy_many`,
            {
                method: "POST",
                headers: {
                    "content-type": "application/json",
                    ...headers,
                },
                body: JSON.stringify({ uuids: ids, target: destinationId }),
            },
            `copy ${source}`,
        );
    }

    private async requestJson<T>(url: string, options: RequestInit | undefined, operation: string) {
        try {
            const response = await this.desktop.httpService.fetcher(url, options);
            const body = await readJsonResponse(response);
            if (!response.ok) {
                throw createDriveApiError(body, operation, response.status);
            }
            return body as T;
        } catch (error) {
            if (error instanceof DriveApiError) throw error;

            if (error instanceof HTTPError) {
                const body = await readJsonResponse(error.response);
                throw createDriveApiError(body, operation, error.response.status);
            }

            throw createDriveApiError(error, operation);
        }
    }

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
            void this.desktop.service.transfer.updateTransfer(pid, {
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

        void this.desktop.service.transfer.updateTransfer(pid, { status: "pending" });
        void this.desktop.service.transfer.processQueue();
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
        items,
        isSingle,
        pid,
        restartParams,
        abortController,
        data,
        link,
        suggestedName,
        savePath,
    }: {
        items: Array<{ id: string; isDir: boolean; name: string }>;
        isSingle: boolean;
        pid: string;
        restartParams: DownloadParams;
        abortController: AbortController;
        data?: DownloadMetadata;
        link?: LinkData;
        suggestedName?: string;
        savePath: string;
    }) {
        const single = items[0];
        const isDir = isSingle && single.isDir;

        if (!data) {
            data = isSingle
                ? isDir
                    ? await this.download.getDownloadUrl({
                          id: single.id,
                          link,
                          signal: abortController.signal,
                      })
                    : await this.download.getFileDownloadMetadata({
                          id: single.id,
                          link,
                      })
                : await this.download.fetchMergedMetadata({
                      items,
                      link,
                      signal: abortController.signal,
                  });
        }

        if (data.root) {
            if (suggestedName && isSingle) {
                data.root.name = suggestedName;

                if (!isDir && data.files?.length === 1) {
                    data.files[0].name = suggestedName;
                }
            }

            if (isSingle && isDir) {
                const resolvedName = await this.resolveDirectoryDownloadName({
                    name: data.root.name,
                    savePath,
                    pid,
                });
                if (!resolvedName) return;
                data.root.name = resolvedName;
            } else if (!isSingle) {
                const usedNames = new Set(await fse.readdir(savePath));
                for (const dir of data.dirs) {
                    if (dir.parentId === BATCH_ROOT_ID) {
                        dir.name = this.claimUniqueName(dir.name, usedNames);
                    }
                }
                for (const file of data.files) {
                    if (file.parentId === BATCH_ROOT_ID) {
                        file.name = this.claimUniqueName(file.name, usedNames);
                    }
                }
            }
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

        const name = isSingle ? data.root?.name || "Download" : `${items.length} items`;

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

        void this.desktop.service.transfer.updateTransfer(pid, {
            status: "pending",
            name,
        });
        void this.desktop.service.transfer.processQueue();
    }

    private async resolveDirectoryDownloadName({
        name,
        savePath,
        pid,
    }: {
        name: string;
        savePath: string;
        pid: string;
    }) {
        const sanitized = this.desktop.lib.fs.sanitizeWindowsFilename(name);
        const entries = await fse.readdir(savePath);
        const existingName = entries.find(
            (entry) => entry.toLowerCase() === sanitized.toLowerCase(),
        );
        if (!existingName) return sanitized;

        const existingPath = path.join(savePath, existingName);
        const isDirectory = await fse
            .stat(existingPath)
            .then((stat) => stat.isDirectory())
            .catch(() => false);
        if (!isDirectory) return this.desktop.lib.fs.getUniqueName(sanitized, entries);

        const language = await this.desktop.setting.general.getLanguage();
        const messages =
            DIRECTORY_CONFLICT_MESSAGES[language as keyof typeof DIRECTORY_CONFLICT_MESSAGES] ??
            DIRECTORY_CONFLICT_MESSAGES.en;
        const options = {
            type: "question" as const,
            title: messages.title,
            message: messages.message(existingName),
            detail: messages.detail,
            buttons: [...messages.buttons],
            defaultId: 1,
            cancelId: 2,
        };
        const mainWindow = this.desktop.window.main.window;
        const result = mainWindow
            ? await dialog.showMessageBox(mainWindow, options)
            : await dialog.showMessageBox(options);

        if (result.response === 2) {
            await this.desktop.service.transfer.cancelTransfer(pid);
            return null;
        }

        if (result.response === 0) return existingName;
        return this.desktop.lib.fs.getUniqueName(sanitized, entries);
    }

    private claimUniqueName(name: string, used: Set<string>) {
        const sanitized = this.desktop.lib.fs.sanitizeWindowsFilename(name);
        const unique = this.desktop.lib.fs.getUniqueName(sanitized, [...used]);
        used.add(unique);
        return unique;
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

async function readJsonResponse(response: Response): Promise<unknown> {
    const text = await response.text();
    if (!text.trim()) return undefined;

    try {
        return JSON.parse(text) as unknown;
    } catch {
        return text;
    }
}

function isSharedLinkAccess(value: unknown): value is SharedLinkAccess {
    if (typeof value !== "object" || value === null) return false;
    const record = value as Record<string, unknown>;
    const parent = record.parent;
    if (typeof record.token !== "string" || typeof parent !== "object" || parent === null) {
        return false;
    }

    const parentRecord = parent as Record<string, unknown>;
    return typeof parentRecord.id === "string" && typeof parentRecord.name === "string";
}

function isModOverview(value: unknown): value is ModOverview {
    if (typeof value !== "object" || value === null) return false;
    const collections = (value as Record<string, unknown>).collections;
    if (!Array.isArray(collections)) return false;

    return collections.every((collection) => {
        if (typeof collection !== "object" || collection === null) return false;
        const record = collection as Record<string, unknown>;
        return (
            typeof record.id === "string" &&
            typeof record.name === "string" &&
            typeof record.rootId === "string"
        );
    });
}
