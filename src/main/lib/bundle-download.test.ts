import { createHash } from "node:crypto";
import os from "node:os";
import path from "node:path";

import { deflateSync } from "fflate";
import fse from "fs-extra";
import { afterEach, describe, expect, it } from "vitest";

import {
    type BundleDownload,
    MAX_BUNDLE_RANGE_BYTES,
    downloadBundleEntry,
} from "./bundle-download";

const temporaryDirectories: string[] = [];

afterEach(async () => {
    await Promise.all(temporaryDirectories.splice(0).map((dir) => fse.remove(dir)));
});

describe("downloadBundleEntry", () => {
    it("extracts a raw DEFLATE member using validated Range requests", async () => {
        const source = Buffer.from("Nahida bundle v2 ".repeat(20_000));
        const compressed = Buffer.from(deflateSync(source));
        const prefix = Buffer.alloc(137, 0x42);
        const archive = Buffer.concat([prefix, compressed, Buffer.alloc(29)]);
        const bundle = createBundle({
            archiveSize: archive.length,
            dataOffset: prefix.length,
            compressedSize: compressed.length,
            size: source.length,
            sha256: sha256(source),
            method: 8,
        });
        const ranges: string[] = [];
        const destination = await createDestination();

        await downloadBundleEntry({
            bundle,
            entry: bundle.entries[0],
            filePath: destination,
            signal: new AbortController().signal,
            fetcher: createRangeFetcher(archive, bundle, ranges),
        });

        expect(sha256(await fse.readFile(destination))).toBe(sha256(source));
        expect(ranges).toEqual([`bytes=${prefix.length}-${prefix.length + compressed.length - 1}`]);
    });

    it("keeps a partial STORE member and resumes at a 25 MiB boundary", async () => {
        const source = Buffer.alloc(MAX_BUNDLE_RANGE_BYTES + 1024);
        for (let index = 0; index < source.length; index += 4096) source[index] = index % 251;
        const prefix = Buffer.alloc(11);
        const archive = Buffer.concat([prefix, source]);
        const bundle = createBundle({
            archiveSize: archive.length,
            dataOffset: prefix.length,
            compressedSize: source.length,
            size: source.length,
            sha256: sha256(source),
            method: 0,
        });
        const destination = await createDestination();
        const firstController = new AbortController();

        await expect(
            downloadBundleEntry({
                bundle,
                entry: bundle.entries[0],
                filePath: destination,
                signal: firstController.signal,
                fetcher: createRangeFetcher(archive, bundle, []),
                onProgress: () => firstController.abort(),
            }),
        ).rejects.toMatchObject({ name: "AbortError" });
        expect((await fse.stat(`${destination}.ntmp`)).size).toBe(MAX_BUNDLE_RANGE_BYTES);

        const resumedRanges: string[] = [];
        await downloadBundleEntry({
            bundle,
            entry: bundle.entries[0],
            filePath: destination,
            signal: new AbortController().signal,
            fetcher: createRangeFetcher(archive, bundle, resumedRanges),
        });

        expect(resumedRanges).toEqual([
            `bytes=${prefix.length + MAX_BUNDLE_RANGE_BYTES}-${archive.length - 1}`,
        ]);
        expect(sha256(await fse.readFile(destination))).toBe(sha256(source));
    });

    it("rejects a non-206 response without publishing a file", async () => {
        const source = Buffer.from("invalid range");
        const bundle = createBundle({
            archiveSize: source.length,
            dataOffset: 0,
            compressedSize: source.length,
            size: source.length,
            sha256: sha256(source),
            method: 0,
        });
        const destination = await createDestination();

        await expect(
            downloadBundleEntry({
                bundle,
                entry: bundle.entries[0],
                filePath: destination,
                signal: new AbortController().signal,
                fetcher: async () => new Response(source, { status: 200 }),
            }),
        ).rejects.toThrow("expected 206");
        expect(await fse.pathExists(destination)).toBe(false);
    });
});

function createBundle(
    entry: Omit<BundleDownload["entries"][0], "id" | "fileId" | "parentId" | "name" | "crc32"> & {
        archiveSize: number;
    },
): BundleDownload {
    return {
        id: "bundle-1",
        url: "https://bundle.test/archive.zip",
        etag: '"bundle-etag"',
        archiveSize: entry.archiveSize,
        entries: [
            {
                id: "item-1",
                fileId: "file-1",
                parentId: "root",
                name: "resource.bin",
                size: entry.size,
                sha256: entry.sha256,
                dataOffset: entry.dataOffset,
                compressedSize: entry.compressedSize,
                method: entry.method,
                crc32: 0,
            },
        ],
    };
}

function createRangeFetcher(archive: Buffer, bundle: BundleDownload, ranges: string[]) {
    return async (_url: string, init?: RequestInit) => {
        const headers = new Headers(init?.headers);
        const range = headers.get("range");
        expect(headers.get("if-match")).toBe(bundle.etag);
        if (!range) return new Response(null, { status: 400 });
        ranges.push(range);
        const match = /^bytes=(\d+)-(\d+)$/.exec(range);
        if (!match) return new Response(null, { status: 416 });
        const start = Number(match[1]);
        const end = Number(match[2]);
        return new Response(Uint8Array.from(archive.subarray(start, end + 1)), {
            status: 206,
            headers: {
                "content-range": `bytes ${start}-${end}/${archive.length}`,
                etag: bundle.etag,
            },
        });
    };
}

async function createDestination() {
    const directory = await fse.mkdtemp(path.join(os.tmpdir(), "nahida-bundle-test-"));
    temporaryDirectories.push(directory);
    return path.join(directory, "resource.bin");
}

function sha256(data: Uint8Array) {
    return createHash("sha256").update(data).digest("hex");
}
