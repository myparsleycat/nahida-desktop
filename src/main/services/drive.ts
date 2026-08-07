import path from "node:path";

import type { Treaty } from "@elysiajs/eden";
import { eden } from "@main/client";
import { networkFetch } from "@main/internal/network-fetch";
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
import type { DriveCopyProgress } from "@shared/types";
import { toErrorMessage } from "@shared/utils";
import { dialog } from "electron";
import { retry } from "es-toolkit";
import fse from "fs-extra";
import Heap from "mnemonist/heap";
import { nanoid } from "nanoid";
import { parseServerSentEvents } from "parse-sse";

import type { NahidaDesktop } from "..";
import type { LocalTransfer, TransferParams } from "./transfer";

import { createDriveApiError, DriveApiError, isBackendUnavailableStatus } from "./drive-errors";
import { parseDriveSourceUrl } from "./drive-url";
import { processChunked } from "./util";

const Fn = eden.akasha.content({ id: "" }).get;
type DriveItem = Treaty.Data<typeof Fn>;

export type DriveCopyFromUrlParams = {
    url: string;
    destinationId: string;
    password?: string;
    collectionId?: string;
    itemId?: string;
    operationId?: string;
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

type CopyOperation = {
    controller: AbortController;
    source: DriveCopyProgress["source"];
    lastProgress?: Omit<DriveCopyProgress, "operationId">;
    cancelRequested: boolean;
};

type RemoteImportMode = "link" | "mod";

export class DriveService {
    private readonly desktop: NahidaDesktop;
    private readonly download: Download;
    private readonly upload: Upload;
    private readonly copyOperations = new Map<string, CopyOperation>();

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
            items: Array<{ id: string; isDir: boolean; name: string; size?: number | null }>;
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
            operationId: requestedOperationId,
        }: DriveCopyFromUrlParams): Promise<DriveCopyFromUrlResult> => {
            const source = parseDriveSourceUrl(url);
            const operationId = requestedOperationId?.trim() || nanoid();
            const operation: CopyOperation = {
                controller: new AbortController(),
                source: source.type,
                cancelRequested: false,
            };
            this.copyOperations.set(operationId, operation);

            try {
                if (source.type === "link") {
                    this.emitCopyProgress(operationId, {
                        source: "link",
                        phase: "preparing",
                        current: 0,
                        total: 1,
                        copiedFiles: 0,
                    });
                    const access = await this.requestSharedLinkAccess(
                        source.id,
                        password,
                        operation.controller.signal,
                    );
                    const copied = await this.copyRemoteImport({
                        mode: "link",
                        sourceId: itemId ?? access.parent.id,
                        sourceName: access.parent.name,
                        destinationId,
                        linkId: source.id,
                        linkToken: access.token,
                        operationId,
                        itemIndex: 0,
                        totalItems: 1,
                        signal: operation.controller.signal,
                    });
                    this.emitCopyProgress(operationId, {
                        source: "link",
                        phase: "completed",
                        current: 1,
                        total: 1,
                        itemName: access.parent.name,
                        copiedFiles: copied,
                    });
                    return { source: "link", copied, destinationId };
                }

                this.emitCopyProgress(operationId, {
                    source: "mod",
                    phase: "preparing",
                    current: 0,
                    total: 1,
                    copiedFiles: 0,
                });
                const modAccess = await this.requestModOverview(
                    source.id,
                    operation.controller.signal,
                );
                const overview = modAccess.data;
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

                const sources = itemId
                    ? [{ id: itemId, name: itemId }]
                    : selectedCollections.map((collection) => ({
                          id: collection.rootId,
                          name: collection.name,
                      }));
                if (sources.length === 0) {
                    throw new DriveApiError(
                        "DRIVE_COLLECTION_EMPTY",
                        "No public collections were found.",
                    );
                }

                let copied = 0;
                for (const [itemIndex, item] of sources.entries()) {
                    copied += await this.copyRemoteImport({
                        mode: "mod",
                        sourceId: item.id,
                        sourceName: item.name,
                        destinationId,
                        modToken: modAccess.token,
                        modSig: modAccess.sig,
                        operationId,
                        itemIndex,
                        totalItems: sources.length,
                        signal: operation.controller.signal,
                    });
                }
                this.emitCopyProgress(operationId, {
                    source: "mod",
                    phase: "completed",
                    current: sources.length,
                    total: sources.length,
                    copiedFiles: copied,
                });
                return { source: "mod", copied, destinationId };
            } catch (error) {
                if (operation.controller.signal.aborted) {
                    this.emitCopyCanceledProgress(operationId, operation);
                    throw this.createCopyCanceledError();
                }
                this.emitCopyProgress(operationId, {
                    source: operation.source,
                    phase: "error",
                    current: operation.lastProgress?.current ?? 0,
                    total: operation.lastProgress?.total ?? 1,
                    itemName: operation.lastProgress?.itemName,
                    copiedFiles: operation.lastProgress?.copiedFiles ?? 0,
                    message: toErrorMessage(error),
                });
                throw error;
            } finally {
                this.copyOperations.delete(operationId);
            }
        },

        cancelCopyFromUrl: async (operationId: string) => {
            const operation = this.copyOperations.get(operationId);
            if (!operation) {
                this.desktop.logger.warn(
                    { operationId },
                    "Drive:CopyFromUrl:CancelOperationNotFound",
                );
                return false;
            }

            this.desktop.logger.info(
                {
                    operationId,
                    source: operation.source,
                },
                "Drive:CopyFromUrl:CancelRequested",
            );
            operation.controller.abort();
            this.emitCopyCanceledProgress(operationId, operation);
            return true;
        },
    };

    private emitCopyProgress(
        operationId: string,
        progress: Omit<DriveCopyProgress, "operationId">,
    ) {
        const operation = this.copyOperations.get(operationId);
        if (operation) operation.lastProgress = progress;
        this.desktop.logger.info({ operationId, ...progress }, "Drive:CopyFromUrl:Progress");
        this.desktop.window.main.window?.webContents.send("drive:copy-progress", {
            operationId,
            ...progress,
        });
    }

    private emitCopyCanceledProgress(operationId: string, operation: CopyOperation) {
        if (operation.cancelRequested) return;
        operation.cancelRequested = true;
        const lastProgress = operation.lastProgress;
        this.emitCopyProgress(operationId, {
            source: operation.source,
            phase: "canceled",
            current: lastProgress?.current ?? 0,
            total: lastProgress?.total ?? 1,
            itemName: lastProgress?.itemName,
            copiedFiles: lastProgress?.copiedFiles ?? 0,
        });
    }

    private async requestSharedLinkAccess(linkId: string, password: string, signal: AbortSignal) {
        try {
            const data = await this.requestJson<unknown>(
                `${BACKEND_URL}/akasha/link/${encodeURIComponent(linkId)}`,
                {
                    method: "POST",
                    headers: { "content-type": "application/json" },
                    body: JSON.stringify({ password, cftoken: "" }),
                    signal,
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
            const message = toErrorMessage(error).toLowerCase();
            if (code === "drive_link_password_required" || message.includes("missing_password")) {
                throw new DriveApiError(
                    "DRIVE_LINK_PASSWORD_REQUIRED",
                    "This shared link requires a password.",
                    undefined,
                    error,
                );
            }
            if (code === "drive_link_invalid_password" || message.includes("invalid_password")) {
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

    private async requestModOverview(modId: string, signal: AbortSignal) {
        const response = await this.requestJsonWithHeaders<unknown>(
            `${BACKEND_URL}/akasha/mod/${encodeURIComponent(modId)}`,
            { signal },
            "mod overview",
        );
        if (!isModOverview(response.data)) {
            throw new DriveApiError(
                "DRIVE_MOD_INVALID_RESPONSE",
                "The collection response was invalid.",
            );
        }

        return {
            data: response.data,
            token: response.headers.get("x-token") ?? undefined,
            sig: response.headers.get("x-sig") ?? undefined,
        };
    }

    private async copyRemoteImport({
        mode,
        sourceId,
        sourceName,
        destinationId,
        linkId,
        linkToken,
        modToken,
        modSig,
        operationId,
        itemIndex,
        totalItems,
        signal,
    }: {
        mode: RemoteImportMode;
        sourceId: string;
        sourceName: string;
        destinationId: string;
        linkId?: string;
        linkToken?: string;
        modToken?: string;
        modSig?: string;
        operationId: string;
        itemIndex: number;
        totalItems: number;
        signal: AbortSignal;
    }) {
        if (signal.aborted) throw this.createCopyCanceledError();

        const requestUrl = new URL(`${BACKEND_URL}/akasha/common/sse/import`);
        requestUrl.searchParams.set("mode", mode);
        requestUrl.searchParams.set("src", sourceId);
        requestUrl.searchParams.set("dest", destinationId);
        if (mode === "link" && linkId && linkToken) {
            requestUrl.searchParams.set("linkId", linkId);
            requestUrl.searchParams.set("linkToken", linkToken);
        }

        const url = requestUrl.toString();
        const headers = await this.desktop.httpService.getHeaders(url);
        if (mode === "mod") {
            if (modSig) headers["x-sig"] = modSig;
            if (modToken) headers["x-token"] = modToken;
        }

        this.desktop.logger.info(
            {
                operationId,
                mode,
                sourceId,
                destinationId,
                itemIndex,
                totalItems,
                stage: "server-copy",
            },
            "Drive:CopyFromUrl:ServerImport",
        );
        this.emitCopyProgress(operationId, {
            source: mode,
            phase: "copying",
            current: itemIndex,
            total: totalItems,
            itemName: sourceName,
            copiedFiles: itemIndex,
        });

        const response = await networkFetch(requestUrl, { headers, signal });
        if (!response.ok) {
            if (isBackendUnavailableStatus(response.status)) {
                this.desktop.service.backendConnectivity.setOffline();
            }
            const body = await response.text().catch(() => response.statusText);
            throw createDriveApiError(
                parseRemoteImportData(body),
                `import ${mode} source ${sourceId}`,
                response.status,
            );
        }
        if (!response.body) {
            throw new DriveApiError(
                "DRIVE_IMPORT_INVALID_RESPONSE",
                `The server import stream for ${sourceId} was empty.`,
            );
        }

        let completed = false;
        for await (const event of parseServerSentEvents(response)) {
            if (signal.aborted) throw this.createCopyCanceledError();

            const data = parseRemoteImportData(event.data);
            this.desktop.logger.info(
                { operationId, mode, sourceId, event: event.type, data },
                "Drive:CopyFromUrl:ServerImportEvent",
            );

            if (event.type === "error") {
                throw createDriveApiError(
                    remoteImportErrorMessage(data),
                    `import ${mode} source ${sourceId}`,
                );
            }
            if (event.type === "complete") {
                completed = true;
                this.emitCopyProgress(operationId, {
                    source: mode,
                    phase: "copying",
                    current: itemIndex + 1,
                    total: totalItems,
                    itemName: sourceName,
                    copiedFiles: itemIndex + 1,
                });
                continue;
            }

            this.emitCopyProgress(operationId, {
                source: mode,
                phase: "copying",
                current: itemIndex,
                total: totalItems,
                itemName: sourceName,
                copiedFiles: getRemoteImportProcessedFiles(data) ?? itemIndex,
                message: event.type === "status" ? remoteImportStatusMessage(data) : undefined,
            });
        }

        if (!completed) {
            throw new DriveApiError(
                "DRIVE_IMPORT_INVALID_RESPONSE",
                `The server import for ${sourceId} ended before completion.`,
            );
        }

        return 1;
    }

    private createCopyCanceledError() {
        return new DriveApiError("DRIVE_COPY_CANCELED", "The copy operation was canceled.");
    }

    private async requestJson<T>(url: string, options: RequestInit | undefined, operation: string) {
        const response = await this.requestJsonWithHeaders<T>(url, options, operation);
        return response.data;
    }

    private async requestJsonWithHeaders<T>(
        url: string,
        options: RequestInit | undefined,
        operation: string,
    ) {
        try {
            const response = await this.desktop.httpService.fetcher(url, {
                ...options,
                throwHttpErrors: false,
            });
            const body = await readJsonResponse(response);
            if (!response.ok) {
                throw createDriveApiError(body, operation, response.status);
            }
            return { data: body as T, headers: response.headers };
        } catch (error) {
            if (error instanceof DriveApiError) throw error;
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
        abortController: providedAbortController,
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
        abortController?: AbortController;
    }) {
        const { pid, files, directories, processName } = preparation;
        const restartParams: UploadParams = { type: "upload", destId, paths, conflictStrategy };
        const abortController = providedAbortController ?? new AbortController();

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
        items: Array<{ id: string; isDir: boolean; name: string; size?: number | null }>;
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

        const metadataTotalBytes = data.totalBytes;
        const manifestTotalBytes = data.files.reduce((total, file) => total + file.size, 0);
        const effectiveTotalBytes =
            manifestTotalBytes > 0 ? manifestTotalBytes : metadataTotalBytes;
        const listedContentBytes = items.every((item) => typeof item.size === "number")
            ? items.reduce((total, item) => total + (item.size ?? 0), 0)
            : undefined;
        if (
            effectiveTotalBytes !== metadataTotalBytes ||
            (listedContentBytes !== undefined && listedContentBytes !== effectiveTotalBytes)
        ) {
            this.desktop.logger.warn(
                {
                    itemIds: items.map((item) => item.id),
                    itemNames: items.map((item) => item.name),
                    listedContentBytes,
                    downloadMetadataBytes: metadataTotalBytes,
                    manifestTotalBytes,
                    effectiveTotalBytes,
                    metadataDeltaBytes: manifestTotalBytes - metadataTotalBytes,
                    listedContentDeltaBytes:
                        listedContentBytes === undefined
                            ? undefined
                            : listedContentBytes - manifestTotalBytes,
                    fileCount: data.files.length,
                    directoryCount: data.dirs.length,
                    savePath,
                    linkId: link?.linkId,
                },
                "Drive:Download:ContentSizeMismatch",
            );
        }
        data.totalBytes = effectiveTotalBytes;

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

        const options = {
            type: "question" as const,
            title: "폴더가 이미 존재합니다",
            message: `"${existingName}" 폴더가 이미 존재합니다.`,
            detail: "기존 폴더에 파일을 덮어쓰시겠습니까?",
            buttons: ["덮어쓰기", "새 이름으로 다운로드", "취소"],
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

function parseRemoteImportData(value: string): unknown {
    if (!value.trim()) return undefined;
    try {
        return JSON.parse(value) as unknown;
    } catch {
        return value;
    }
}

function getRemoteImportProcessedFiles(value: unknown) {
    if (typeof value !== "object" || value === null) return undefined;
    const processedFiles = (value as Record<string, unknown>).processedFiles;
    return typeof processedFiles === "number" ? processedFiles : undefined;
}

function remoteImportStatusMessage(value: unknown) {
    if (typeof value === "string") return value;
    if (typeof value !== "object" || value === null) return undefined;
    const status = (value as Record<string, unknown>).status;
    return typeof status === "string" ? status : undefined;
}

function remoteImportErrorMessage(value: unknown) {
    if (typeof value === "string") return value;
    if (typeof value !== "object" || value === null) return "The server import failed.";
    const record = value as Record<string, unknown>;
    for (const key of ["message", "error", "code"]) {
        const message = record[key];
        if (typeof message === "string" && message.trim()) return message;
    }
    return "The server import failed.";
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
