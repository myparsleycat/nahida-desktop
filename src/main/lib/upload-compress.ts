import { randomUUID } from "node:crypto";
import { createReadStream, createWriteStream } from "node:fs";
import { open } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { pipeline } from "node:stream/promises";
import { constants, createZstdCompress } from "node:zlib";

import { fileTypeFromBuffer } from "file-type";
import fse from "fs-extra";

import type { FinalFile } from "./upload";

import { DIRECT_UPLOAD_THRESHOLD } from "./upload-pack";

export const SKIP_COMPRESSION_MAX_BYTES = 100;
const FILE_TYPE_PROBE_BYTES = 4100;

export type PreparedDirectFile = {
    data?: Buffer;
    path?: string;
    byteLength?: number;
    compAlg?: "zstd";
    cleanupPath?: boolean;
};

function isMediaByMagicNumbers(slicedBuffer: Buffer) {
    const startsWith = (signature: number[]) => {
        if (slicedBuffer.length < signature.length) return false;
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

export async function isPreviewFile(file: Buffer, name?: string) {
    if (file.length === 0) return false;

    const fileSlice = file.subarray(0, 4100);
    const fileType = await fileTypeFromBuffer(fileSlice).catch(() => undefined);
    const isByMimeType = Boolean(
        fileType && (fileType.mime.startsWith("image/") || fileType.mime.startsWith("video/")),
    );
    if (isByMimeType || isMediaByMagicNumbers(fileSlice)) return true;

    if (!name) return false;
    return (
        /\.(gif|jpe?g|tiff?|png|webp|bmp|ico)$/i.test(name) ||
        /\.(mp4|webm|ogg|mov|avi|flv|mkv)$/i.test(name)
    );
}

export async function shouldSkipUploadCompression(data: Buffer, name?: string, size = data.byteLength) {
    if (size <= SKIP_COMPRESSION_MAX_BYTES) return true;
    return isPreviewFile(data, name);
}

export async function prepareDirectUploadFile(
    file: FinalFile,
    compress: (data: Buffer) => Promise<Buffer>,
    options?: { onDisk?: boolean },
): Promise<PreparedDirectFile> {
    const stats = await fse.stat(file.fullPath);
    if (stats.size !== file.size) {
        throw new Error(`Unexpected EOF while reading ${file.name}`);
    }
    const onDisk = options?.onDisk ?? file.size >= DIRECT_UPLOAD_THRESHOLD;
    if (onDisk) {
        const probe = await readFilePrefix(
            file.fullPath,
            Math.min(FILE_TYPE_PROBE_BYTES, file.size),
        );
        if (await shouldSkipUploadCompression(probe, file.name, file.size)) {
            return { path: file.fullPath, byteLength: file.size };
        }
        const tempPath = path.join(os.tmpdir(), `nahida-upload-zstd-${randomUUID()}`);
        try {
            await compressPathWithZstd(file.fullPath, tempPath);
        } catch (error) {
            await fse.remove(tempPath).catch(() => {});
            throw error;
        }
        return {
            path: tempPath,
            byteLength: (await fse.stat(tempPath)).size,
            compAlg: "zstd",
            cleanupPath: true,
        };
    }
    const data = await fse.readFile(file.fullPath);
    if (data.byteLength !== file.size) {
        throw new Error(`Unexpected EOF while reading ${file.name}`);
    }
    if (await shouldSkipUploadCompression(data, file.name, file.size)) {
        return { data, byteLength: data.byteLength };
    }
    const compressed = await compress(data);
    return { data: compressed, byteLength: compressed.byteLength, compAlg: "zstd" };
}

async function readFilePrefix(filePath: string, size: number) {
    const handle = await open(filePath, "r");
    try {
        const buffer = Buffer.alloc(size);
        const { bytesRead } = await handle.read(buffer, 0, size, 0);
        return bytesRead === size ? buffer : buffer.subarray(0, bytesRead);
    } finally {
        await handle.close();
    }
}

async function compressPathWithZstd(inputPath: string, outputPath: string) {
    await pipeline(
        createReadStream(inputPath),
        createZstdCompress({
            chunkSize: 16 * 1024,
            params: {
                [constants.ZSTD_c_compressionLevel]: 3,
            },
        }),
        createWriteStream(outputPath),
    );
}
