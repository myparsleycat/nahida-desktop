import { randomUUID } from "node:crypto";
import { promisify } from "node:util";
import { zstdCompress, zstdDecompress } from "node:zlib";
import { rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";

import fse from "fs-extra";
import { afterEach, describe, expect, it } from "vitest";

import { DIRECT_UPLOAD_THRESHOLD } from "./upload-pack";

import {
    prepareDirectUploadFile,
    shouldSkipUploadCompression,
    SKIP_COMPRESSION_MAX_BYTES,
} from "./upload-compress";

const zstdCompressAsync = promisify(zstdCompress);
const zstdDecompressAsync = promisify(zstdDecompress);

const tempFiles: string[] = [];

async function writeTemp(name: string, data: Buffer) {
    const filePath = path.join(os.tmpdir(), `nahida-upload-compress-${randomUUID()}-${name}`);
    await writeFile(filePath, data);
    tempFiles.push(filePath);
    return filePath;
}

function file(name: string, fullPath: string, size: number) {
    return {
        FID: name,
        path: name,
        name,
        size,
        parentPath: "",
        parentId: "parent",
        fullPath,
        sha256: "a".repeat(64),
    };
}

afterEach(async () => {
    await Promise.all(tempFiles.splice(0).map((filePath) => rm(filePath, { force: true })));
});

describe("prepareDirectUploadFile", () => {
    it("compresses non-media files above the skip floor", async () => {
        const logical = Buffer.from("ini-payload-".repeat(20));
        const fullPath = await writeTemp("note.ini", logical);
        const prepared = await prepareDirectUploadFile(
            file("note.ini", fullPath, logical.byteLength),
            (data) => zstdCompressAsync(data),
        );
        expect(prepared.compAlg).toBe("zstd");
        expect(prepared.data.byteLength).not.toBe(logical.byteLength);
        expect(Buffer.from(await zstdDecompressAsync(prepared.data))).toEqual(logical);
    });

    it("does not compress images or small files", async () => {
        const png = Buffer.alloc(200);
        png.set([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]);
        const pngPath = await writeTemp("cover.png", png);
        const image = await prepareDirectUploadFile(
            file("cover.png", pngPath, png.byteLength),
            async () => {
                throw new Error("should not compress media");
            },
        );
        expect(image.compAlg).toBeUndefined();
        expect(image.data).toEqual(png);

        const small = Buffer.from("tiny");
        const smallPath = await writeTemp("tiny.ini", small);
        const prepared = await prepareDirectUploadFile(
            file("tiny.ini", smallPath, small.byteLength),
            async () => {
                throw new Error("should not compress small files");
            },
        );
        expect(prepared.compAlg).toBeUndefined();
        expect(prepared.data.byteLength).toBeLessThanOrEqual(SKIP_COMPRESSION_MAX_BYTES);
    });

    it("compresses nte-named payloads and files at the multipart threshold", async () => {
        const pak = Buffer.from("pak-payload-".repeat(20));
        const pakPath = await writeTemp("Character.pak", pak);
        const nte = await prepareDirectUploadFile(
            file("Character.pak", pakPath, pak.byteLength),
            (data) => zstdCompressAsync(data),
        );
        expect(nte.compAlg).toBe("zstd");
        expect(nte.data.byteLength).not.toBe(pak.byteLength);

        const large = Buffer.from("huge-payload-".repeat(20));
        const largePath = await writeTemp("huge.ini", large);
        expect(await shouldSkipUploadCompression(large, "huge.ini", DIRECT_UPLOAD_THRESHOLD)).toBe(
            false,
        );
        const prepared = await prepareDirectUploadFile(
            file("huge.ini", largePath, large.byteLength),
            (data) => zstdCompressAsync(data),
        );
        expect(prepared.compAlg).toBe("zstd");
        expect(prepared.data?.byteLength).not.toBe(large.byteLength);
    });

    it("keeps large payloads on disk instead of a full-file buffer", async () => {
        const logical = Buffer.from("ini-payload-".repeat(20));
        const fullPath = await writeTemp("stream.ini", logical);
        const prepared = await prepareDirectUploadFile(
            file("stream.ini", fullPath, logical.byteLength),
            async () => {
                throw new Error("in-memory compress should not run for on-disk prepare");
            },
            { onDisk: true },
        );
        expect(prepared.data).toBeUndefined();
        expect(prepared.path).toBeDefined();
        expect(prepared.cleanupPath).toBe(true);
        expect(prepared.compAlg).toBe("zstd");
        expect(prepared.byteLength).not.toBe(logical.byteLength);
        if (prepared.path) tempFiles.push(prepared.path);
        expect(Buffer.from(await zstdDecompressAsync(await fse.readFile(prepared.path!)))).toEqual(
            logical,
        );
    });

    it("streams skipped media from the original path", async () => {
        const png = Buffer.alloc(200);
        png.set([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]);
        const pngPath = await writeTemp("cover.png", png);
        const prepared = await prepareDirectUploadFile(
            file("cover.png", pngPath, png.byteLength),
            async () => {
                throw new Error("should not compress media");
            },
            { onDisk: true },
        );
        expect(prepared.data).toBeUndefined();
        expect(prepared.path).toBe(pngPath);
        expect(prepared.cleanupPath).toBeUndefined();
        expect(prepared.compAlg).toBeUndefined();
        expect(prepared.byteLength).toBe(png.byteLength);
    });
});
