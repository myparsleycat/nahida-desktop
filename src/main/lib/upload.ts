import { createHash, randomUUID } from "node:crypto";
import path from "node:path";

import sha256PiscinaWorker from "@main/worker/drive/sha256-piscina.worker?modulePath";
import { collectFiles } from "@native/fs";
import type { Content, PlanPhase } from "@shared/types";
import { toErrorMessage } from "@shared/utils";
import { orderBy, sumBy } from "es-toolkit";
import fse from "fs-extra";
import { nanoid } from "nanoid";
import PQueue from "p-queue";
import Piscina from "piscina";

import type { NahidaDesktop } from "..";

import { prepareDirectUploadFile } from "./upload-compress";
import { uploadDriveFilesV2, uploadErrorCode } from "./upload-v2";

const SYSTEM_FILE_PATTERNS = [
    /^\.DS_Store$/,
    /^\._/,
    /^\.AppleDouble$/,
    /^\.Spotlight-V100$/,
    /^\.Trashes$/,
    /^\.fseventsd$/,
    /^\.TemporaryItems$/,
    /^\.apdisk$/,
    /^__MACOSX$/,
    /^Thumbs\.db$/i,
    /^ehthumbs.*\.db$/i,
    /^desktop\.ini$/i,
    /^~$/,
];

export function isSystemFile(name: string) {
    return SYSTEM_FILE_PATTERNS.some((pattern) => pattern.test(name));
}

export function hasSystemFileSegment(path: string) {
    return path.split("/").some(isSystemFile);
}

export function assignStableUploadFileIds<T extends Omit<FilesComponent, "FID">>(files: T[]) {
    const occurrences = new Map<string, number>();
    return files.map((file) => {
        const relativePath = file.path.replaceAll("\\", "/").toLowerCase();
        const occurrence = occurrences.get(relativePath) ?? 0;
        occurrences.set(relativePath, occurrence + 1);
        return {
            ...file,
            FID: createHash("sha256").update(`${relativePath}\0${occurrence}`).digest("hex"),
        };
    });
}

export type FilesComponent = {
    FID: string;
    path: string;
    name: string;
    size: number;
    parentPath: string;
    fullPath: string;
};

export type DirectoriesComponent = {
    path: string;
    name: string;
    parentPath: string;
};

export interface ParentIdFiles extends FilesComponent {
    parentId: string;
}

export interface FinalFile extends ParentIdFiles {
    sha256: string;
}

export interface UploadProgress {
    bytes: number;
    fileId?: string;
    isServerDeduplicated?: boolean;
}

export type UploadConflictStrategy = "suffix" | "skip";

export interface UploadRootConflict {
    name: string;
    type: "file" | "directory";
}

export type UploadParams = {
    type: "upload";
    destId: string;
    paths: string[];
    processedFiles?: FinalFile[];
    fileHashes?: Record<string, string>;
    conflictStrategy?: UploadConflictStrategy;
    requestId?: string;
};

export class UploadLib {
    private readonly desktop: NahidaDesktop;

    public constructor(desktop: NahidaDesktop) {
        this.desktop = desktop;
    }

    private async collect(
        paths: string[],
        additionalExt: string[] = [],
        allowAllFiles = false,
    ): Promise<{ files: FilesComponent[]; directories: DirectoriesComponent[] }> {
        const defaultAllowedExt = [
            ".buf",
            ".ib",
            ".vb",
            ".dds",
            ".ini",
            ".jpeg",
            ".jpg",
            ".png",
            ".webp",
            ".gif",
            ".avif",
            ".avifs",
            ".bmp",
            ".hlsl",
            ".py",
            ".json",
            ".txt",
            ".pmx",
            ".tga",
            ".spa",
            ".assets",
            ".wem",
            ".mp4",
            ".webm",
            ".blend",
            ".pck",
            ".bin",
            ".pak",
            ".utoc",
            ".ucas",
        ];
        const allowedExt = [...defaultAllowedExt, ...additionalExt].map((ext) =>
            ext.startsWith(".") ? ext.toLowerCase() : `.${ext.toLowerCase()}`,
        );
        const rootFiles: Array<Omit<FilesComponent, "FID">> = [];
        const directoryPaths: string[] = [];

        for (const rawPath of paths) {
            try {
                const absolutePath = await fse.realpath(rawPath);
                const stat = await fse.stat(absolutePath);

                if (isSystemFile(path.basename(absolutePath))) {
                    continue;
                }

                if (stat.isDirectory()) {
                    directoryPaths.push(absolutePath);
                    continue;
                }

                if (!stat.isFile()) {
                    continue;
                }

                const normalizedFullPath = absolutePath.replaceAll("\\", "/");
                const name = path.basename(normalizedFullPath);
                const loweredName = name.toLowerCase();
                const isAllowed =
                    allowAllFiles ||
                    allowedExt.length === 0 ||
                    allowedExt.some((ext) => loweredName.endsWith(ext));

                if (!isAllowed) {
                    continue;
                }

                rootFiles.push({
                    path: name,
                    name,
                    size: stat.size,
                    parentPath: "",
                    fullPath: normalizedFullPath,
                });
            } catch {
                continue;
            }
        }

        const collected =
            directoryPaths.length > 0
                ? await collectFiles(directoryPaths, allowAllFiles ? [] : allowedExt)
                : null;
        const filteredFiles = collected?.files.filter((file) => !hasSystemFileSegment(file.path));
        const filteredDirectories = collected?.directories.filter(
            (dir) => !hasSystemFileSegment(dir.path),
        );
        const files = assignStableUploadFileIds([...(filteredFiles ?? []), ...rootFiles]);

        return { files, directories: filteredDirectories ?? [] };
    }

    public async calculateHashes(
        files: ParentIdFiles[],
        onProgress?: (count: number) => void,
        signal?: AbortSignal,
    ) {
        let processedCount = 0;
        let lastUpdate = 0;

        const piscina = new Piscina({
            filename: sha256PiscinaWorker,
            minThreads: 2,
            idleTimeout: 3000,
        });

        const promises = files.map(async (file) => {
            return piscina
                .run({ path: file.fullPath }, { signal })
                .then((hash: string) => {
                    signal?.throwIfAborted();
                    processedCount++;
                    const now = Date.now();

                    if (now - lastUpdate >= 100 || processedCount === files.length) {
                        lastUpdate = now;
                        onProgress?.(processedCount);
                    }

                    return { ...file, sha256: hash };
                })
                .catch((err) => {
                    if (signal?.aborted) throw signal.reason;
                    throw new Error(`Failed to hash ${file.name}: ${err}`);
                });
        });

        try {
            return await Promise.all(promises);
        } finally {
            await piscina.destroy();
        }
    }

    public mapFilesToParentIds(
        files: FilesComponent[],
        createdDirs: { id: string; path: string }[],
        defaultParentId: string,
    ): ParentIdFiles[] {
        if (createdDirs.length > 0) {
            const sortedDirs = orderBy(createdDirs, [(dir) => dir.path.length], ["desc"]);
            return files.map((file) => {
                const parentDir = sortedDirs.find(
                    (dir) => file.path.substring(0, file.path.lastIndexOf("/")) === dir.path,
                );
                return { ...file, parentId: parentDir ? parentDir.id : defaultParentId };
            });
        }
        return files.map((file) => ({ ...file, parentId: defaultParentId }));
    }

    private redistributeFilesBySize<T extends { size: number }>(files: T[]): T[] {
        const LARGE_FILE_THRESHOLD = 50 * 1024 * 1024; // 50MB

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

    public async filesUpload({
        currentId,
        requestId,
        files,
        onProgress,
        onPlanProgress,
        onPlanComplete,
        signal,
        pid,
    }: {
        currentId: string;
        requestId: string;
        files: FinalFile[];
        totalSize: number;
        onProgress?: (progress: UploadProgress) => void;
        onPlanProgress?: (progress: { phase: PlanPhase; processed: number; total: number }) => void;
        onPlanComplete?: () => void;
        signal?: AbortSignal;
        pid?: string;
    }) {
        const filesToProcess = pid
            ? files.filter((f) => !this.desktop.service.transfer.isFileCompleted(pid, f.FID))
            : files;
        if (filesToProcess.length === 0) return;
        const queue = new PQueue({
            concurrency: await this.desktop.setting.transfer.getUploadConcurrency(),
        });
        const clearQueue = () => queue.clear();
        signal?.addEventListener("abort", clearQueue, { once: true });

        try {
            await uploadDriveFilesV2({
                desktop: this.desktop,
                currentId,
                requestId,
                files: this.redistributeFilesBySize(filesToProcess),
                queue,
                signal,
                onProgress,
                onPlanProgress,
                onPlanComplete,
                prepareDirectFile: (file) =>
                    prepareDirectUploadFile(file, (data) =>
                        this.desktop.lib.compressor.zstd.compress(data),
                    ),
            });
        } finally {
            signal?.removeEventListener("abort", clearQueue);
        }
    }

    public async executeUpload({
        currentId,
        pid,
        params,
        files,
        directories,
        totalSize,
        abortController,
        processedFiles,
        initialTransferedSize,
    }: {
        currentId: string;
        pid: string;
        params: UploadParams;
        files: FilesComponent[];
        directories: DirectoriesComponent[];
        totalSize: number;
        processName: string;
        abortController: AbortController;
        processedFiles?: FinalFile[];
        initialTransferedSize?: number;
    }) {
        const operation = { stage: "create-directories" };

        try {
            void this.desktop.service.transfer.updateTransfer(pid, {
                status: "preparing",
                transferedFiles: 0,
            });

            const createdDirs =
                directories.length > 0
                    ? await this.desktop.service.drive.post.dirs(
                          params.destId,
                          directories,
                          abortController.signal,
                      )
                    : [];
            const parentIdProcessedFiles = this.mapFilesToParentIds(
                files,
                createdDirs,
                params.destId,
            );

            let finalFiles: FinalFile[] = [];
            operation.stage = "resolve-file-hashes";
            if (params.fileHashes) {
                finalFiles = parentIdProcessedFiles.map((f) => {
                    const hash = params.fileHashes?.[f.FID];
                    if (!hash) {
                        throw new Error(`Hash missing for file ${f.name}`);
                    }
                    return { ...f, sha256: hash };
                });
            } else if (processedFiles && processedFiles.length === parentIdProcessedFiles.length) {
                const hashes = new Map(processedFiles.map((file) => [file.FID, file.sha256]));
                finalFiles = parentIdProcessedFiles.map((file) => {
                    const hash = hashes.get(file.FID);
                    if (!hash) throw new Error(`Hash missing for file ${file.name}`);
                    return { ...file, sha256: hash };
                });
            } else {
                finalFiles = await this.calculateHashes(
                    parentIdProcessedFiles,
                    (count) => {
                        void this.desktop.service.transfer.updateTransfer(pid, {
                            transferedFiles: count,
                        });
                    },
                    abortController.signal,
                );
                const transfer = this.desktop.service.transfer.getTransferByPID(pid);
                if (transfer?.restartParams) {
                    transfer.restartParams = {
                        ...(transfer.restartParams as UploadParams),
                        processedFiles: finalFiles,
                    };
                }
            }

            let alreadyUploadedBytes = 0;
            let alreadyUploadedCount = 0;
            for (const file of finalFiles) {
                if (this.desktop.service.transfer.isFileCompleted(pid, file.FID)) {
                    alreadyUploadedBytes += file.size;
                    alreadyUploadedCount++;
                }
            }

            let currentUploadedBytes = initialTransferedSize ?? alreadyUploadedBytes;
            let currentUploadedCount = alreadyUploadedCount;

            const updateUI = () => {
                void this.desktop.service.transfer.updateTransfer(pid, {
                    status: "progress",
                    transferedSize: currentUploadedBytes,
                    transferedFiles: currentUploadedCount,
                });
            };

            updateUI();

            const heartbeat = setInterval(updateUI, 500);
            const requestId = params.requestId ?? randomUUID();
            params.requestId = requestId;

            try {
                operation.stage = "plan-and-upload-v2";
                await this.filesUpload({
                    currentId,
                    requestId,
                    files: finalFiles,
                    totalSize,
                    onProgress: (progress: UploadProgress) => {
                        if (abortController.signal.aborted) return;
                        if (progress.fileId) {
                            this.desktop.service.transfer.markFileCompleted(pid, progress.fileId);
                            currentUploadedCount++;
                        }
                        currentUploadedBytes += progress.bytes;
                    },
                    onPlanProgress: (progress) => {
                        if (abortController.signal.aborted) return;
                        void this.desktop.service.transfer.updateTransfer(pid, {
                            planPhase: progress.phase,
                            planProgress:
                                progress.total > 0
                                    ? (progress.processed / progress.total) * 100
                                    : null,
                        });
                    },
                    onPlanComplete: () => {
                        if (abortController.signal.aborted) return;
                        void this.desktop.service.transfer.updateTransfer(pid, {
                            status: "progress",
                            transferedSize: currentUploadedBytes,
                            transferedFiles: currentUploadedCount,
                            planPhase: undefined,
                            planProgress: undefined,
                        });
                    },
                    signal: abortController.signal,
                    pid,
                });
            } finally {
                clearInterval(heartbeat);
                void this.desktop.service.transfer.updateTransfer(pid, {
                    planPhase: undefined,
                    planProgress: undefined,
                });
            }

            if (abortController.signal.aborted) return;

            void this.desktop.service.transfer.updateTransfer(pid, {
                status: "completed",
                transferedSize: totalSize,
                progress: 100,
            });
        } catch (err) {
            if (abortController.signal.aborted) return;
            this.desktop.logger.error(
                err,
                `UploadLib:executeUpload:pid=${pid}:destId=${params.destId}:stage=${operation.stage}:pathCount=${params.paths.length}`,
            );
            void this.desktop.service.transfer.updateTransfer(pid, {
                status: "error",
                error: toErrorMessage(err),
                errorCode: uploadErrorCode(err),
            });
            throw err;
        }
    }

    public async prepareUpload(paths: string[], children: Content[]) {
        const { files, directories } = await this.collect(paths);
        const conflictStrategy: UploadConflictStrategy = "suffix";

        return this.prepareUploadWithStrategy(
            files,
            directories,
            children,
            conflictStrategy,
            paths,
        );
    }

    public async getRootNameConflicts(
        paths: string[],
        children: Content[],
    ): Promise<UploadRootConflict[]> {
        const { files, directories } = await this.collect(paths);
        const existingNames = new Set(children.map((child) => child.name));
        const seenRootNames = new Set<string>();
        const conflicts: UploadRootConflict[] = [];

        const rootDirectories = directories.filter((dir) => dir.parentPath === "");
        const rootFiles = files.filter((file) => file.parentPath === "");

        for (const rootDir of rootDirectories) {
            if (existingNames.has(rootDir.name) || seenRootNames.has(rootDir.name)) {
                conflicts.push({ name: rootDir.name, type: "directory" });
            }
            seenRootNames.add(rootDir.name);
        }

        for (const rootFile of rootFiles) {
            if (existingNames.has(rootFile.name) || seenRootNames.has(rootFile.name)) {
                conflicts.push({ name: rootFile.name, type: "file" });
            }
            seenRootNames.add(rootFile.name);
        }

        return conflicts;
    }

    public async prepareUploadWithConflictStrategy(
        paths: string[],
        children: Content[],
        conflictStrategy: UploadConflictStrategy,
        options?: { additionalExt?: string[]; allowAllFiles?: boolean },
    ) {
        const { files, directories } = await this.collect(
            paths,
            options?.additionalExt,
            options?.allowAllFiles,
        );
        return this.prepareUploadWithStrategy(
            files,
            directories,
            children,
            conflictStrategy,
            paths,
        );
    }

    private prepareUploadWithStrategy(
        collectedFiles: FilesComponent[],
        collectedDirectories: DirectoriesComponent[],
        children: Content[],
        conflictStrategy: UploadConflictStrategy,
        paths: string[],
    ) {
        let files = [...collectedFiles];
        let directories = [...collectedDirectories];
        const existingNames = new Set(children.map((child) => child.name));
        const skippedRootDirPaths = new Set<string>();
        const skippedRootFilePaths = new Set<string>();

        const getUniqueName = (baseName: string) => {
            let newName = baseName;
            let counter = 2;

            while (existingNames.has(newName)) {
                newName = `${baseName} (${counter})`;
                counter++;
            }

            return newName;
        };

        const rootDirectories = directories.filter((dir) => dir.parentPath === "");
        for (const rootDir of rootDirectories) {
            const baseName = rootDir.name;
            if (!existingNames.has(baseName)) {
                existingNames.add(baseName);
                continue;
            }

            if (conflictStrategy === "skip") {
                skippedRootDirPaths.add(rootDir.path);
                continue;
            }

            const newName = getUniqueName(baseName);
            rootDir.name = newName;
            existingNames.add(newName);
        }

        const rootFiles = files.filter((file) => file.parentPath === "");
        for (const rootFile of rootFiles) {
            const baseName = rootFile.name;
            if (!existingNames.has(baseName)) {
                existingNames.add(baseName);
                continue;
            }

            if (conflictStrategy === "skip") {
                skippedRootFilePaths.add(rootFile.path);
                continue;
            }

            const newName = getUniqueName(baseName);
            rootFile.name = newName;
            existingNames.add(newName);
        }

        if (skippedRootDirPaths.size > 0) {
            const shouldSkipByRootDir = (targetPath: string) =>
                Array.from(skippedRootDirPaths).some(
                    (rootPath) => targetPath === rootPath || targetPath.startsWith(`${rootPath}/`),
                );

            directories = directories.filter((dir) => !shouldSkipByRootDir(dir.path));
            files = files.filter((file) => !shouldSkipByRootDir(file.path));
        }

        if (skippedRootFilePaths.size > 0) {
            files = files.filter((file) => !skippedRootFilePaths.has(file.path));
        }

        let processName = paths.length === 1 ? path.basename(paths[0]) : "";
        if (!processName) {
            const folderNames = orderBy(directories, [(dir) => dir.name], ["desc"]).map(
                (dir) => dir.name,
            );
            processName =
                folderNames.length > 0
                    ? `${folderNames[0]} 외 ${paths.length - 1}개`
                    : `${path.basename(paths[0])} 외 ${paths.length - 1}개`;
        }

        const pid = nanoid();
        const totalSize = sumBy(files, (fileInfo) => fileInfo.size);
        return { pid, files, directories, totalSize, processName };
    }
}

export default UploadLib;
