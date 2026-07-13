import path from "node:path";

import { eden, eden2url } from "@main/client";
import sha256PiscinaWorker from "@main/worker/drive/sha256-piscina.worker?modulePath";
import { collectFiles } from "@native/fs";
import type { Content } from "@shared/types";
import { toErrorMessage } from "@shared/utils";
import { chunk, groupBy, orderBy, retry, sumBy } from "es-toolkit";
import { fileTypeFromBuffer } from "file-type";
import fse from "fs-extra";
import ky from "ky";
import { nanoid } from "nanoid";
import PQueue from "p-queue";
import Piscina from "piscina";

import type { NahidaDesktop } from "..";

import { AkashaBundleUploader } from "./bundle-upload";

const CHUNK_SIZE = 100;
const UPLOAD_STREAM_CHUNK_SIZE = 64 * 1024;

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
    mode?: "drive" | "mod-v2";
    collectionId?: string;
    sig?: string;
    sessionId?: string;
    createdDirectories?: Array<{ id: string; path: string }>;
};

export class UploadLib {
    private readonly desktop: NahidaDesktop;
    private readonly fileQueue: PQueue = new PQueue({ concurrency: 8 });
    private readonly textEncoder = new TextEncoder();
    private readonly bundleUploader: AkashaBundleUploader;

    public constructor(desktop: NahidaDesktop) {
        this.desktop = desktop;
        this.bundleUploader = new AkashaBundleUploader(desktop);
    }

    public async executeModBundleUpload({
        pid,
        params,
        files,
        directories,
        abortController,
    }: {
        pid: string;
        params: UploadParams;
        files: FilesComponent[];
        directories: DirectoriesComponent[];
        abortController: AbortController;
    }) {
        if (!params.collectionId || !params.sessionId) {
            throw new Error("Mod bundle upload parameters are incomplete");
        }
        const hashes = params.fileHashes;
        if (!hashes) throw new Error("Mod bundle upload hashes are missing");
        const hashedFiles = files.map((file) => {
            const sha256 = hashes[file.FID];
            if (!sha256) throw new Error(`Hash missing for file ${file.name}`);
            return { ...file, sha256 };
        });
        let uploadedBytes = 0;
        let uploadedFiles = 0;
        let plannedTotalSize = hashedFiles.reduce((sum, file) => sum + file.size, 0);
        let plannedTotalFiles = hashedFiles.length;
        const heartbeat = setInterval(() => {
            void this.desktop.service.transfer.updateTransfer(pid, {
                status: "progress",
                transferedSize: uploadedBytes,
                transferedFiles: uploadedFiles,
            });
        }, 500);

        try {
            await this.bundleUploader.execute({
                collectionId: params.collectionId,
                currentId: params.destId,
                sig: params.sig,
                sessionId: params.sessionId,
                files: hashedFiles,
                directories,
                createdDirectories: params.createdDirectories,
                signal: abortController.signal,
                onDirectoriesCreated: (createdDirectories) => {
                    params.createdDirectories = createdDirectories;
                },
                onDiagnostics: (diagnostics) => {
                    for (const diagnostic of diagnostics) {
                        this.desktop.logger[diagnostic.severity === "error" ? "error" : "warn"](
                            diagnostic.message,
                            "UploadLib:bundle-plan",
                        );
                    }
                },
                onInventoryPlanned: (plannedFiles) => {
                    plannedTotalSize = plannedFiles.reduce((sum, file) => sum + file.size, 0);
                    plannedTotalFiles = plannedFiles.length;
                    void this.desktop.service.transfer.updateTransfer(pid, {
                        totalSize: plannedTotalSize,
                        totalFiles: plannedTotalFiles,
                    });
                },
                onFileComplete: (fileId, size) => {
                    if (this.desktop.service.transfer.isFileCompleted(pid, fileId)) return;
                    this.desktop.service.transfer.markFileCompleted(pid, fileId);
                    uploadedBytes += size;
                    uploadedFiles++;
                },
            });
            if (abortController.signal.aborted) return;
            void this.desktop.service.transfer.updateTransfer(pid, {
                status: "completed",
                transferedSize: plannedTotalSize,
                transferedFiles: plannedTotalFiles,
                progress: 100,
            });
        } catch (error) {
            if (abortController.signal.aborted) return;
            this.desktop.logger.error(error, "UploadLib:executeModBundleUpload");
            void this.desktop.service.transfer.updateTransfer(pid, {
                status: "error",
                error: toErrorMessage(error),
            });
            throw error;
        } finally {
            clearInterval(heartbeat);
        }
    }

    private async syncQueueConcurrency() {
        this.fileQueue.concurrency = await this.desktop.setting.transfer.getUploadConcurrency();
    }

    private clearPendingFileQueue() {
        this.fileQueue.clear();
    }

    private async getCreateManyConcurrency() {
        return await this.desktop.setting.transfer.getUploadCreateManyConcurrency();
    }

    private async collect(
        paths: string[],
        additionalExt: string[] = [],
        allowAll = false,
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
        ];
        const allowedExt = allowAll
            ? []
            : [...defaultAllowedExt, ...additionalExt].map((ext) =>
                  ext.startsWith(".") ? ext.toLowerCase() : `.${ext.toLowerCase()}`,
              );
        const rootFiles: Array<Omit<FilesComponent, "FID">> = [];
        const directoryPaths: string[] = [];

        for (const rawPath of paths) {
            try {
                const absolutePath = await fse.realpath(rawPath);
                const stat = await fse.stat(absolutePath);

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
                    allowedExt.length === 0 || allowedExt.some((ext) => loweredName.endsWith(ext));

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
            directoryPaths.length > 0 ? await collectFiles(directoryPaths, allowedExt) : null;
        const files: FilesComponent[] = [...(collected?.files ?? []), ...rootFiles].map((f) => ({
            ...f,
            FID: nanoid(),
        }));

        return { files, directories: collected?.directories ?? [] };
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
        const uploadUrl = eden2url.akasha.file.upload.url();

        const boundary = `----nahida-desktop-${nanoid()}`;
        const fields: Array<[string, string]> = [
            ["sha256", sha256],
            ["key", key],
            ["name", name],
        ];
        if (part) fields.push(["part", part]);
        if (compAlg) fields.push(["comp-alg", compAlg]);
        if (parentId) fields.push(["parent", parentId]);

        let lastTransferredBytes = 0;
        const normalizedFile =
            fileToUpload instanceof Blob
                ? new Uint8Array(await fileToUpload.arrayBuffer())
                : new Uint8Array(fileToUpload);
        const multipart = this.createMultipartUploadBody({
            boundary,
            fields,
            file: normalizedFile,
            filename: name,
            onProgress: (bytes) => {
                lastTransferredBytes += bytes;
                onProgress?.(bytes);
            },
        });

        try {
            const response = await ky.post(uploadUrl, {
                body: multipart.body,
                headers: {
                    ...(await this.desktop.httpService.getHeaders(uploadUrl)),
                    "Content-Type": `multipart/form-data; boundary=${boundary}`,
                    "Content-Length": String(multipart.contentLength),
                },
                signal,
                throwHttpErrors: false,
                timeout: 100000,
                // @ts-expect-error - duplex is required by Node/undici for streaming request bodies.
                duplex: "half",
                hooks: {
                    beforeRequest: [
                        () => {
                            lastTransferredBytes = 0;
                        },
                    ],
                },
            });

            if (!response.ok && onProgress && lastTransferredBytes > 0) {
                onProgress(-lastTransferredBytes);
            }

            return response;
        } catch (error) {
            if (onProgress && lastTransferredBytes > 0) {
                onProgress(-lastTransferredBytes);
            }
            throw error;
        }
    }

    private createMultipartUploadBody({
        boundary,
        fields,
        file,
        filename,
        onProgress,
    }: {
        boundary: string;
        fields: Array<[string, string]>;
        file: Uint8Array;
        filename: string;
        onProgress?: (bytes: number) => void;
    }) {
        const encode = (value: string) => this.textEncoder.encode(value);
        const escapeHeaderValue = (value: string) =>
            value
                .replaceAll("\\", "\\\\")
                .replaceAll('"', '\\"')
                .replaceAll("\r", "")
                .replaceAll("\n", "");

        const fieldParts: Uint8Array[] = [];
        for (const [fieldName, fieldValue] of fields) {
            fieldParts.push(
                encode(
                    `\r\n--${boundary}\r\nContent-Disposition: form-data; name="${escapeHeaderValue(fieldName)}"\r\n\r\n${fieldValue}`,
                ),
            );
        }

        const fileHeader = encode(
            `--${boundary}\r\nContent-Disposition: form-data; name="file"; filename="${escapeHeaderValue(filename)}"\r\nContent-Type: application/octet-stream\r\n\r\n`,
        );
        const fileFooter = encode(`\r\n--${boundary}--\r\n`);
        const contentLength =
            fieldParts.reduce((total, part) => total + part.byteLength, 0) +
            fileHeader.byteLength +
            file.byteLength +
            fileFooter.byteLength;

        let fieldIndex = 0;
        let fileOffset = 0;
        let sentFileHeader = false;
        let sentFileFooter = false;

        const body = new ReadableStream<Uint8Array>({
            pull(controller) {
                if (!sentFileHeader) {
                    controller.enqueue(fileHeader);
                    sentFileHeader = true;
                    return;
                }

                if (fileOffset < file.byteLength) {
                    const end = Math.min(fileOffset + UPLOAD_STREAM_CHUNK_SIZE, file.byteLength);
                    const chunk = file.subarray(fileOffset, end);
                    fileOffset = end;
                    controller.enqueue(chunk);
                    onProgress?.(chunk.byteLength);
                    return;
                }

                if (fieldIndex < fieldParts.length) {
                    controller.enqueue(fieldParts[fieldIndex]);
                    fieldIndex++;
                    return;
                }

                if (!sentFileFooter) {
                    controller.enqueue(fileFooter);
                    sentFileFooter = true;
                    return;
                }

                controller.close();
            },
        });

        return { body, contentLength };
    }

    private async uploadLargeFile(
        file: FinalFile,
        signal?: AbortSignal,
        onProgress?: (bytes: number) => void,
    ) {
        const { name, sha256, fullPath, form } = file;

        if (!form) {
            throw new Error("form is not defined");
        }

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
                            key: form.key,
                            parentId: form.parentId,
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
                        delay: (attempt) => 2 ** attempt * 1000,
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

        if (!form) {
            throw new Error("form is not defined");
        }

        const fileBuffer = await fse.readFile(fullPath);
        const isPreview = await this.isPreviewFile(fileBuffer, name);

        let zstdFile: Buffer | undefined;
        if (!isPreview && file.size > 100) {
            zstdFile = (await this.desktop.lib.compressor.zstd.compress(fileBuffer)) ?? undefined;
            if (!zstdFile) throw new Error("Failed to compress file");
        }

        const compAlg = zstdFile ? "zstd" : undefined;
        let fileToUpload: Buffer = zstdFile || fileBuffer;
        let uploadedPayloadBytes = 0;
        let reportedOriginalBytes = 0;
        const reportOriginalFileProgress = (bytes: number) => {
            if (!onProgress) return;

            if (bytes < 0) {
                uploadedPayloadBytes = 0;
                if (reportedOriginalBytes > 0) {
                    onProgress(-reportedOriginalBytes);
                    reportedOriginalBytes = 0;
                }
                return;
            }

            uploadedPayloadBytes += bytes;
            const payloadSize = Math.max(1, fileToUpload.byteLength);
            const targetOriginalBytes = Math.min(
                file.size,
                Math.floor((uploadedPayloadBytes / payloadSize) * file.size),
            );
            const incremental = targetOriginalBytes - reportedOriginalBytes;

            if (incremental > 0) {
                reportedOriginalBytes = targetOriginalBytes;
                onProgress(incremental);
            }
        };

        await retry(
            async () => {
                if (signal?.aborted) throw new Error("Aborted");
                const response = await this.postFormData({
                    name,
                    sha256,
                    key: form.key,
                    parentId: form.parentId,
                    fileToUpload,
                    compAlg,
                    signal,
                    onProgress: reportOriginalFileProgress,
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
                delay: (attempt) => 2 ** attempt * 1000,
                shouldRetry: () => !signal?.aborted,
            },
        );
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
                    processedCount++;
                    const now = Date.now();

                    if (now - lastUpdate >= 100 || processedCount === files.length) {
                        lastUpdate = now;
                        onProgress?.(processedCount);
                    }

                    return { ...file, sha256: hash };
                })
                .catch((err) => {
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
        files,
        onProgress,
        signal,
        pid,
    }: {
        currentId: string;
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

        const queueUploads = async (filesToUpload: FinalFile[]) => {
            for (const file of filesToUpload) {
                if (signal?.aborted) break;

                if (this.fileQueue.size >= BACKPRESSURE_LIMIT) {
                    await new Promise<void>((resolve) => {
                        this.fileQueue.once("next", () => resolve());
                    });
                }

                if (signal?.aborted) break;

                void this.fileQueue.add(async () => {
                    if (signal?.aborted) return;
                    try {
                        await this.uploadFile(file, signal, (bytes) => {
                            if (onProgress) {
                                onProgress({
                                    bytes,
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

            const { data } = await retry(
                async () => {
                    const result = await eden.akasha.file.create_many.post({
                        current: currentId,
                        files: fileMetadatas,
                    });

                    if (result.error) {
                        throw new Error(
                            `[create_files chunk failed] ${toErrorMessage(result.error.value)}`,
                        );
                    }

                    return result;
                },
                {
                    retries: 3,
                    delay: (attempt) => 2 ** attempt * 1000,
                    shouldRetry: () => !signal?.aborted,
                },
            );

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

            await queueUploads(filesToUpload);
        };

        const processMetadataChunks = async (chunks: FinalFile[][], concurrency: number) => {
            const metadataQueue = new PQueue({ concurrency });

            await Promise.all(
                chunks.map((fileChunk) =>
                    metadataQueue.add(async () => {
                        if (signal?.aborted) return;
                        await processChunk(fileChunk);
                    }),
                ),
            );
        };

        const representativeChunks = chunk(representativeFiles, CHUNK_SIZE);
        await processMetadataChunks(representativeChunks, await this.getCreateManyConcurrency());

        if (!signal?.aborted) {
            await this.fileQueue.onIdle();
        }

        const remainingChunks = chunk(allRemainingFiles, CHUNK_SIZE);
        await processMetadataChunks(remainingChunks, 1);

        if (!signal?.aborted) {
            await this.fileQueue.onIdle();
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
        const handleAbort = () => {
            this.clearPendingFileQueue();
        };

        abortController.signal.addEventListener("abort", handleAbort, { once: true });

        try {
            await this.syncQueueConcurrency();

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
            if (params.fileHashes) {
                finalFiles = parentIdProcessedFiles.map((f) => {
                    const hash = params.fileHashes?.[f.FID];
                    if (!hash) {
                        throw new Error(`Hash missing for file ${f.name}`);
                    }
                    return { ...f, sha256: hash };
                });
            } else if (processedFiles && processedFiles.length === parentIdProcessedFiles.length) {
                finalFiles = processedFiles;
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

            try {
                await this.filesUpload({
                    currentId,
                    files: finalFiles,
                    totalSize,
                    onProgress: (progress: UploadProgress) => {
                        if (progress.fileId) {
                            this.desktop.service.transfer.markFileCompleted(pid, progress.fileId);
                            currentUploadedCount++;
                        }
                        currentUploadedBytes += progress.bytes;
                    },
                    signal: abortController.signal,
                    pid,
                });
            } finally {
                clearInterval(heartbeat);
            }

            if (abortController.signal.aborted) return;

            void this.desktop.service.transfer.updateTransfer(pid, {
                status: "completed",
                transferedSize: totalSize,
                progress: 100,
            });
        } catch (err) {
            if (abortController.signal.aborted) return;
            this.desktop.logger.error(err, "UploadLib:executeUpload");
            void this.desktop.service.transfer.updateTransfer(pid, {
                status: "error",
                error: toErrorMessage(err),
            });
            throw err;
        } finally {
            abortController.signal.removeEventListener("abort", handleAbort);
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

    public async prepareModBundleUpload(paths: string[]) {
        const { files, directories } = await this.collect(paths, [], true);
        return this.prepareUploadWithStrategy(files, directories, [], "suffix", paths);
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
    ) {
        const { files, directories } = await this.collect(paths);
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
