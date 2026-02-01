import { NahidaDesktop } from "..";
import { nanoid } from "nanoid";
import { Content } from "@shared/types";
import { chunk, compact, flatten, groupBy, orderBy, sumBy, retry } from "es-toolkit";
import path from "node:path";
import fg from "fast-glob";
import os from "node:os";
import { Worker } from "node:worker_threads";
import sha256Worker from "@main/worker/drive/sha256.worker?modulePath";
import { eden, eden2url } from "@main/client";
import fse from "fs-extra";
import { fileTypeFromBuffer } from "file-type/node";
import PQueue from "p-queue";
import ky from "ky";
import { appVersion } from "@main/const";

const CHUNK_SIZE = 500;

export type FilesComponent = {
    FID: string;
    path: string;
    name: string;
    size: number;
    parentPath: string;
    fullPath: string;
    form?: {
        parentId: string | null;
        sha256: string;
        name: string;
        key: string;
    };
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

export type UploadParams = {
    type: "upload";
    destId: string;
    paths: string[];
    processedFiles?: FinalFile[];
    fileHashes?: Record<string, string>;
};

export class UploadLib {
    private readonly desktop: NahidaDesktop;
    private readonly fileQueue: PQueue = new PQueue({ concurrency: 8 });

    public constructor(desktop: NahidaDesktop) {
        this.desktop = desktop;
    }

    private validateExt(name: string, additionalExt: string[] = []) {
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
        ];

        const allowedExt = defaultAllowedExt.concat(additionalExt);
        return allowedExt.some((ext) => name.toLowerCase().endsWith(ext.toLowerCase()));
    }

    private async collectFiles(
        paths: string[],
        additionalExt: string[] = [],
    ): Promise<FilesComponent[]> {
        const results = await Promise.all(
            paths.map(async (p) => {
                const absolutePath = path.resolve(p);
                const parentDir = path.dirname(absolutePath);

                const entries = await fg("**/*", {
                    cwd: absolutePath,
                    stats: true,
                    absolute: true,
                    onlyFiles: true,
                });

                return entries
                    .filter((entry) => this.validateExt(path.basename(entry.path), additionalExt))
                    .map((entry) => {
                        const fullPath = entry.path.replace(/\\/g, "/");
                        const relativeFromRoot = path
                            .relative(parentDir, fullPath)
                            .replace(/\\/g, "/");
                        const name = path.basename(fullPath);
                        const parentPath = path.dirname(relativeFromRoot).replace(/\\/g, "/");

                        return {
                            FID: nanoid(),
                            path: relativeFromRoot,
                            name: name,
                            size: entry.stats?.size ?? 0,
                            parentPath: parentPath === "." ? "" : parentPath,
                            fullPath: fullPath,
                        };
                    });
            }),
        );

        return flatten(results);
    }

    private async collectDirectories(paths: string[]): Promise<DirectoriesComponent[]> {
        const results = await Promise.all(
            paths.map(async (p) => {
                const absolutePath = path.resolve(p);
                const rootName = path.basename(absolutePath);
                const parentDir = path.dirname(absolutePath);

                const entries = await fg("**/*", {
                    cwd: absolutePath,
                    onlyDirectories: true,
                    absolute: true,
                });

                const rootEntry = {
                    path: rootName.replace(/\\/g, "/"),
                    name: rootName,
                    parentPath: "",
                };

                const subDirs = entries.map((dirPath) => {
                    const relativePath = path.relative(parentDir, dirPath).replace(/\\/g, "/");
                    const name = path.basename(dirPath);
                    const dirParentPath = path.dirname(relativePath).replace(/\\/g, "/");

                    return {
                        path: relativePath,
                        name: name,
                        parentPath: dirParentPath === "." ? "" : dirParentPath,
                    };
                });

                return [rootEntry, ...subDirs];
            }),
        );

        return flatten(results);
    }

    private createSha256WorkerPool(size: number) {
        const workers: Worker[] = [];
        for (let i = 0; i < size; i++) {
            const worker = new Worker(sha256Worker);
            workers.push(worker);
        }
        return workers;
    }

    private cleanupSha256Workers(workers: Worker[]) {
        workers.forEach((worker) => worker.terminate());
    }

    private async isMediaByMagicNumbers(file: Buffer) {
        const slicedBuffer = file.subarray(0, 8);

        const startsWith = (signature: number[]) => {
            if (slicedBuffer.length < signature.length) {
                return false;
            }
            return signature.every((byte, index) => byte === slicedBuffer[index]);
        };

        const mediaSignatures = [
            [0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a],
            [0x47, 0x49, 0x46, 0x38],
            [0xff, 0xd8, 0xff],
            [0x42, 0x4d],
            [0x49, 0x49, 0x2a, 0x00],
            [0x4d, 0x4d, 0x00, 0x2a],
            [0x00, 0x00, 0x01, 0x00],
            [0x1a, 0x45, 0xdf, 0xa3],
        ];

        const isMP4 =
            slicedBuffer.length >= 8 &&
            slicedBuffer[4] === 0x66 &&
            slicedBuffer[5] === 0x74 &&
            slicedBuffer[6] === 0x79 &&
            slicedBuffer[7] === 0x70;

        return mediaSignatures.some((sig) => startsWith(sig)) || isMP4;
    }

    private async isPreviewFile(file: Buffer, name?: string) {
        if (file.length === 0) return false;

        const fileSlice = file.subarray(0, 4100);

        try {
            const fileType = await fileTypeFromBuffer(fileSlice);
            if (fileType) {
                return fileType.mime.startsWith("image/") || fileType.mime.startsWith("video/");
            }
        } catch {}

        if (await this.isMediaByMagicNumbers(fileSlice)) {
            return true;
        }

        const fileType = await fileTypeFromBuffer(fileSlice);
        const isByMimeType =
            fileType && (fileType.mime.startsWith("image/") || fileType.mime.startsWith("video/"));

        let isByName = false;
        if (name) {
            isByName =
                /\.(gif|jpe?g|tiff?|png|webp|bmp|ico)$/i.test(name) ||
                /\.(mp4|webm|ogg|mov|avi|flv|mkv)$/i.test(name);
        }

        return isByMimeType || isByName;
    }

    private async reverseFile(buf: Buffer) {
        const uint8Array = new Uint8Array(buf);
        return uint8Array.slice().reverse();
    }

    private async uploadFile(
        file: FinalFile,
        signal?: AbortSignal,
        onProgress?: (bytes: number) => void,
    ) {
        if (signal?.aborted) throw new Error("Aborted");
        const { fullPath, form } = file;

        const readable = await this.desktop.lib.fs.isPathReadable(fullPath);
        if (!readable) throw new Error("path is not readable");
        if (!form) throw new Error("form is not defined");

        const CHUNK_THRESHOLD = 50 * 1024 * 1024; // 50MB
        if (file.size > CHUNK_THRESHOLD) {
            await this.uploadLargeFile(file, signal, onProgress);
        } else {
            await this.uploadSmallFile(file, signal, onProgress);
        }
    }

    private async postFormData({
        name,
        sha256,
        key,
        parentId,
        fileToUpload,
        part,
        compAlg,
        signal,
        onProgress,
    }: {
        name: string;
        sha256: string;
        key: string;
        parentId?: string | null;
        fileToUpload: Buffer | Blob;
        part?: string;
        compAlg?: string;
        signal?: AbortSignal;
        onProgress?: (bytes: number) => void;
    }) {
        const token = await this.desktop.service.auth.getToken();
        const uploadUrl = eden2url.akasha.file.upload.url();

        const formData = new FormData();
        formData.append(
            "file",
            fileToUpload instanceof Blob ? fileToUpload : new Blob([new Uint8Array(fileToUpload)]),
            name,
        );
        formData.append("sha256", sha256);
        formData.append("key", key);
        formData.append("name", name);
        if (part) formData.append("part", part);
        if (compAlg) formData.append("comp-alg", compAlg);
        if (parentId) formData.append("parent", parentId);

        let lastTransferredBytes = 0;

        const response = await ky.post(uploadUrl, {
            body: formData,
            headers: {
                Authorization: `Bearer ${token}`,
                "User-Agent": `Nahida Desktop/${appVersion}`,
            },
            signal,
            throwHttpErrors: false,
            timeout: 100000,
            onUploadProgress: (progress) => {
                if (onProgress) {
                    const incremental = progress.transferredBytes - lastTransferredBytes;
                    lastTransferredBytes = progress.transferredBytes;
                    if (incremental > 0) {
                        onProgress(incremental);
                    }
                }
            },
        });

        return response;
    }

    private async uploadLargeFile(
        file: FinalFile,
        signal?: AbortSignal,
        onProgress?: (bytes: number) => void,
    ) {
        const { name, sha256, fullPath, form } = file;
        const CHUNK_SIZE = 5 * 1024 * 1024; // 5MB
        const totalChunks = Math.ceil(file.size / CHUNK_SIZE);
        const maxIndex = totalChunks - 1;

        const fd = await fse.open(fullPath, "r");
        try {
            const chunkQueue = new PQueue({ concurrency: 4 });
            const uploadChunk = async (index: number) => {
                const start = index * CHUNK_SIZE;
                const end = Math.min(start + CHUNK_SIZE, file.size);
                const length = end - start;
                const buffer = Buffer.allocUnsafe(length);
                await fse.read(fd, buffer, 0, length, start);

                const partToken = `${index}-${maxIndex}`;
                let chunkToUpload: Buffer = buffer;

                await retry(
                    async () => {
                        if (signal?.aborted) throw new Error("Aborted");
                        const response = await this.postFormData({
                            name,
                            sha256,
                            key: form!.key,
                            parentId: form!.parentId,
                            fileToUpload: chunkToUpload,
                            part: partToken,
                            signal,
                            onProgress,
                        });

                        if (!response.ok) {
                            if (response.status === 403) {
                                const reversed = await this.reverseFile(chunkToUpload);
                                chunkToUpload = Buffer.from(reversed);
                            }
                            const errorText = await response.text().catch(() => "Unknown error");
                            throw new Error(`Chunk ${index} upload failed: ${errorText}`);
                        }
                    },
                    {
                        retries: 3,
                        delay: (attempt) => Math.pow(2, attempt) * 1000,
                        shouldRetry: () => !signal?.aborted,
                    },
                );
            };

            const promises: Promise<void>[] = [];
            for (let i = 0; i < maxIndex; i++) {
                promises.push(chunkQueue.add(() => uploadChunk(i)));
            }
            await Promise.all(promises);
            await uploadChunk(maxIndex);
        } finally {
            await fse.close(fd);
        }
    }

    private async uploadSmallFile(
        file: FinalFile,
        signal?: AbortSignal,
        onProgress?: (bytes: number) => void,
    ) {
        const { name, sha256, fullPath, form } = file;
        const fileBuffer = await fse.readFile(fullPath);
        const isPreview = await this.isPreviewFile(fileBuffer, name);

        let zstdFile: Buffer | undefined;
        if (!isPreview && file.size > 100) {
            zstdFile = (await this.desktop.lib.compressor.zstd.compress(fileBuffer)) ?? undefined;
            if (!zstdFile) throw new Error("Failed to compress file");
        }

        const compAlg = zstdFile ? "zstd" : undefined;
        let fileToUpload: Buffer = zstdFile || fileBuffer;

        await retry(
            async () => {
                if (signal?.aborted) throw new Error("Aborted");
                const response = await this.postFormData({
                    name,
                    sha256,
                    key: form!.key,
                    parentId: form!.parentId,
                    fileToUpload,
                    compAlg,
                    signal,
                    onProgress,
                });

                if (!response.ok) {
                    if (response.status === 403) {
                        const reversed = await this.reverseFile(fileToUpload);
                        fileToUpload = Buffer.from(reversed);
                    }
                    const errorText = await response.text().catch(() => "Unknown error");
                    throw new Error(`Upload failed: ${errorText}`);
                }
            },
            {
                retries: 3,
                delay: (attempt) => Math.pow(2, attempt) * 1000,
                shouldRetry: () => !signal?.aborted,
            },
        );
    }

    public async calculateHashes(files: ParentIdFiles[], onProgress?: (count: number) => void) {
        let processedCount = 0;
        let lastUpdate = 0;
        const optimalWorkerCount = Math.min(files.length, os.cpus().length);
        const workers = this.createSha256WorkerPool(optimalWorkerCount);
        const chunks: ParentIdFiles[][] = Array(optimalWorkerCount)
            .fill(null)
            .map(() => []);

        files.forEach((file, index) => {
            chunks[index % optimalWorkerCount].push(file);
        });

        const results = await Promise.all(
            chunks.map((chunk, workerIndex) => {
                return new Promise<Map<string, string>>((resolve, reject) => {
                    const worker = workers[workerIndex];
                    worker.on("message", (message: any) => {
                        if (message.type === "complete") {
                            resolve(new Map(message.hashes));
                        } else if (message.type === "progress") {
                            processedCount++;
                            const now = Date.now();
                            if (now - lastUpdate >= 100 || processedCount === files.length) {
                                lastUpdate = now;
                                onProgress?.(processedCount);
                            }
                        } else if (message.type === "error") {
                            reject(new Error(message.error));
                        }
                    });
                    worker.on("error", reject);
                    worker.postMessage({
                        files: chunk.map((f) => ({ FID: f.FID, path: f.fullPath })),
                    });
                });
            }),
        );

        this.cleanupSha256Workers(workers);

        const combinedHashes = new Map<string, string>();
        results.forEach((result) => {
            result.forEach((hash, fid) => combinedHashes.set(fid, hash));
        });

        return files.map((file) => {
            const hash = combinedHashes.get(file.FID);
            if (!hash) throw new Error("cannot get hash from FID");
            return { ...file, sha256: hash };
        });
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
        files,
        totalSize,
        onProgress,
        signal,
        pid,
    }: {
        files: FinalFile[];
        totalSize: number;
        onProgress?: (progress: UploadProgress) => void;
        signal?: AbortSignal;
        pid?: string;
    }) {
        const filesToProcess = pid
            ? files.filter((f) => !this.desktop.service.transfer.isFileCompleted(pid, f.FID))
            : files;

        const redistributedFiles = this.redistributeFilesBySize(filesToProcess);

        const hashGroups = groupBy(redistributedFiles, (file) => file.sha256 || "unknown");
        const representativeFiles: FinalFile[] = [];
        const allRemainingFiles: FinalFile[] = [];

        Object.values(hashGroups).forEach((group) => {
            if (group.length > 0) {
                representativeFiles.push(group[0]);
                if (group.length > 1) {
                    allRemainingFiles.push(...group.slice(1));
                }
            }
        });

        const BACKPRESSURE_LIMIT = (this.fileQueue.concurrency || 32) * 3;

        const processChunk = async (chunkItems: FinalFile[]) => {
            if (signal?.aborted || chunkItems.length === 0) return;

            const fileMetadatas = chunkItems.map((f) => ({
                FID: f.FID,
                parentId: f.parentId,
                name: f.name,
                path: f.path,
                size: f.size,
                sha256: f.sha256,
            }));

            const { data, error } = await eden.akasha.file.create_many.post({
                current: "",
                files: fileMetadatas,
            });

            if (error) {
                throw new Error(`[create_files chunk failed] ${error.value.toString()}`);
            }

            const serverDataMap = new Map(data.map((item) => [item.FID, item]));
            const filesToUpload: FinalFile[] = [];
            const serverDeduplicatedFiles: FinalFile[] = [];

            chunkItems.forEach((file) => {
                const serverInfo = serverDataMap.get(file.FID);
                if (serverInfo?.form) {
                    file.form = serverInfo.form;
                    filesToUpload.push(file);
                } else {
                    serverDeduplicatedFiles.push(file);
                }
            });

            if (onProgress && serverDeduplicatedFiles.length > 0) {
                for (const file of serverDeduplicatedFiles) {
                    onProgress({
                        bytes: file.size,
                        fileId: file.FID,
                        isServerDeduplicated: true,
                    });
                }
            }

            for (const file of filesToUpload) {
                if (signal?.aborted) break;

                if (this.fileQueue.size >= BACKPRESSURE_LIMIT) {
                    await new Promise<void>((resolve) => {
                        this.fileQueue.once("next", () => resolve());
                    });
                }

                if (signal?.aborted) break;

                this.fileQueue.add(async () => {
                    if (signal?.aborted) return;
                    try {
                        await this.uploadFile(file, signal, (bytes) => {
                            if (onProgress) {
                                onProgress({
                                    bytes,
                                    fileId: file.FID,
                                    isServerDeduplicated: false,
                                });
                            }
                        });
                        if (onProgress) {
                            onProgress({
                                bytes: 0,
                                fileId: file.FID,
                                isServerDeduplicated: false,
                            });
                        }
                    } catch (err) {
                        if (signal?.aborted) return;
                        this.desktop.logger.error(err, `UploadLib:filesUpload:${file.name}`);
                    }
                });
            }
        };

        const representativeChunks = chunk(representativeFiles, CHUNK_SIZE);
        for (const fileChunk of representativeChunks) {
            if (signal?.aborted) break;
            await processChunk(fileChunk);
        }

        const remainingChunks = chunk(allRemainingFiles, CHUNK_SIZE);
        for (const fileChunk of remainingChunks) {
            if (signal?.aborted) break;
            await processChunk(fileChunk);
        }

        if (!signal?.aborted) {
            await this.fileQueue.onIdle();
        }
    }

    public async executeUpload({
        pid,
        params,
        files,
        directories,
        totalSize,
        processName,
        abortController,
        processedFiles,
        initialTransferedSize,
    }: {
        pid: string;
        params: UploadParams;
        files: any[];
        directories: any[];
        totalSize: number;
        processName: string;
        abortController: AbortController;
        processedFiles?: FinalFile[];
        initialTransferedSize?: number;
    }) {
        try {
            this.desktop.service.transfer.updateTransfer(pid, {
                status: "preparing",
                transferedFiles: 0,
            });

            const createdDirs = await this.desktop.service.drive.post.dirs(
                params.destId,
                directories,
            );
            const parentIdProcessedFiles = this.mapFilesToParentIds(
                files,
                createdDirs,
                params.destId,
            );

            let finalFiles: FinalFile[] = [];
            if (params.fileHashes) {
                finalFiles = parentIdProcessedFiles.map((f) => {
                    const hash = params.fileHashes![f.FID];
                    if (!hash) {
                        throw new Error(`Hash missing for file ${f.name}`);
                    }
                    return { ...f, sha256: hash };
                });
            } else if (processedFiles && processedFiles.length === parentIdProcessedFiles.length) {
                finalFiles = processedFiles;
            } else {
                finalFiles = await this.calculateHashes(parentIdProcessedFiles, (count) => {
                    this.desktop.service.transfer.updateTransfer(pid, {
                        transferedFiles: count,
                    });
                });
                const transfer = this.desktop.service.transfer.getTransferByPID(pid);
                if (transfer && transfer.restartParams) {
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

            this.desktop.service.transfer.updateTransfer(pid, {
                status: "progress",
                transferedSize: currentUploadedBytes,
                transferedFiles: currentUploadedCount,
            });

            await this.filesUpload({
                files: finalFiles,
                totalSize,
                onProgress: (progress: UploadProgress) => {
                    if (progress.fileId) {
                        this.desktop.service.transfer.markFileCompleted(pid, progress.fileId);
                        currentUploadedCount++;
                    }
                    currentUploadedBytes += progress.bytes;
                    this.desktop.service.transfer.updateTransfer(pid, {
                        transferedSize: currentUploadedBytes,
                        transferedFiles: currentUploadedCount,
                    });
                },
                signal: abortController.signal,
                pid,
            });

            if (abortController.signal.aborted) return;

            this.desktop.service.transfer.updateTransfer(pid, {
                status: "completed",
                transferedSize: totalSize,
                progress: 100,
            });
        } catch (err) {
            if (abortController.signal.aborted) return;
            this.desktop.logger.error(err, "UploadLib:executeUpload");
            this.desktop.service.transfer.updateTransfer(pid, { status: "error" });
            throw err;
        }
    }

    public async prepareUpload(paths: string[], items: Content[]) {
        const files = await this.collectFiles(paths, [".blend"]);

        const directories = await this.collectDirectories(paths);
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

        if (items.some((child) => child.name === processName)) {
            throw new Error("업로드하려는 대상과 동일한 이름을 가진 폴더/파일이 있습니다");
        }

        const pid = nanoid();
        const totalSize = sumBy(files, (fileInfo) => fileInfo.size);
        return { pid, files, directories, totalSize, processName };
    }
}

export default UploadLib;
