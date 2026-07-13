import { createHash } from "node:crypto";
import { createReadStream } from "node:fs";
import { open } from "node:fs/promises";
import path from "node:path";

import { retry } from "es-toolkit";
import { Inflate } from "fflate";
import fse from "fs-extra";

export const MAX_BUNDLE_RANGE_BYTES = 25 * 1024 * 1024;
const INFLATE_INPUT_BYTES = 64 * 1024;

export type BundleDownloadEntry = {
    id: string;
    fileId: string;
    parentId: string | null;
    name: string;
    size: number;
    sha256: string;
    dataOffset: number;
    compressedSize: number;
    method: 0 | 8;
    crc32: number;
};

export type BundleDownload = {
    id: string;
    url: string;
    etag: string;
    archiveSize: number;
    entries: BundleDownloadEntry[];
};

type BundleEntryDownloadOptions = {
    bundle: BundleDownload;
    entry: BundleDownloadEntry;
    filePath: string;
    signal: AbortSignal;
    fetcher: (url: string, init?: RequestInit) => Promise<Response>;
    onProgress?: (bytes: number) => void;
};

export async function downloadBundleEntry(options: BundleEntryDownloadOptions) {
    if (options.entry.method !== 0 && options.entry.method !== 8) {
        throw new Error(`Unsupported ZIP method: ${String(options.entry.method)}`);
    }
    if (options.entry.dataOffset < 0 || options.entry.compressedSize < 0) {
        throw new Error("Invalid bundle entry range");
    }

    await fse.ensureDir(path.dirname(options.filePath));
    const temporaryPath = `${options.filePath}.ntmp`;
    const resumableBytes = await getResumableBytes(options, temporaryPath);
    const output = await open(temporaryPath, resumableBytes > 0 ? "a" : "w");
    const hash = createHash("sha256");
    let writtenBytes = resumableBytes;

    try {
        if (resumableBytes > 0) {
            for await (const chunk of createReadStream(temporaryPath, {
                start: 0,
                end: resumableBytes - 1,
            })) {
                hash.update(chunk);
            }
        }

        const writeOutput = async (chunks: Uint8Array[]) => {
            for (const chunk of chunks) {
                if (options.signal.aborted) throw createAbortError();
                await output.write(chunk);
                hash.update(chunk);
                writtenBytes += chunk.byteLength;
                options.onProgress?.(chunk.byteLength);
            }
        };

        if (options.entry.method === 0) {
            for await (const chunk of fetchEntryRanges(options, resumableBytes)) {
                await writeOutput([chunk]);
            }
        } else {
            const outputChunks: Uint8Array[] = [];
            const inflate = new Inflate((chunk) => outputChunks.push(chunk));
            let receivedBytes = 0;

            for await (const range of fetchEntryRanges(options, 0)) {
                for (let offset = 0; offset < range.byteLength; offset += INFLATE_INPUT_BYTES) {
                    const chunk = range.subarray(
                        offset,
                        Math.min(offset + INFLATE_INPUT_BYTES, range.byteLength),
                    );
                    receivedBytes += chunk.byteLength;
                    inflate.push(chunk, receivedBytes === options.entry.compressedSize);
                    await writeOutput(outputChunks.splice(0));
                }
            }

            if (options.entry.compressedSize === 0) {
                inflate.push(new Uint8Array(), true);
                await writeOutput(outputChunks);
            }
        }

        if (writtenBytes !== options.entry.size) {
            throw new Error(
                `Bundle entry size mismatch: expected ${options.entry.size}, received ${writtenBytes}`,
            );
        }
        if (hash.digest("hex").toLowerCase() !== options.entry.sha256.toLowerCase()) {
            throw new Error(`Bundle entry SHA-256 mismatch: ${options.entry.name}`);
        }

        await output.close();
        await fse.move(temporaryPath, options.filePath, { overwrite: true });
    } catch (error) {
        await output.close().catch(() => {});
        if (!options.signal.aborted) await fse.remove(temporaryPath).catch(() => {});
        throw error;
    }
}

export async function isBundleEntryComplete(filePath: string, entry: BundleDownloadEntry) {
    if (!(await fse.pathExists(filePath)) || (await fse.stat(filePath)).size !== entry.size) {
        return false;
    }

    const hash = createHash("sha256");
    for await (const chunk of createReadStream(filePath)) hash.update(chunk);
    return hash.digest("hex").toLowerCase() === entry.sha256.toLowerCase();
}

async function getResumableBytes(options: BundleEntryDownloadOptions, temporaryPath: string) {
    if (options.entry.method !== 0 || !(await fse.pathExists(temporaryPath))) return 0;
    const size = (await fse.stat(temporaryPath)).size;
    if (size > options.entry.size || size > options.entry.compressedSize) {
        await fse.remove(temporaryPath);
        return 0;
    }
    return size;
}

async function* fetchEntryRanges(options: BundleEntryDownloadOptions, completedBytes: number) {
    const firstByte = options.entry.dataOffset + completedBytes;
    const lastByte = options.entry.dataOffset + options.entry.compressedSize - 1;

    for (let start = firstByte; start <= lastByte; start += MAX_BUNDLE_RANGE_BYTES) {
        if (options.signal.aborted) throw createAbortError();
        const end = Math.min(start + MAX_BUNDLE_RANGE_BYTES - 1, lastByte);
        yield await retry(() => fetchBundleRange(options, start, end), {
            retries: 2,
            delay: (attempt) => 2 ** (attempt - 1) * 250,
            shouldRetry: () => !options.signal.aborted,
            signal: options.signal,
        });
    }
}

async function fetchBundleRange(options: BundleEntryDownloadOptions, start: number, end: number) {
    const response = await options.fetcher(options.bundle.url, {
        headers: {
            Range: `bytes=${start}-${end}`,
            "If-Match": options.bundle.etag,
        },
        signal: options.signal,
    });

    if (response.status !== 206) {
        throw new Error(`Bundle Range request failed: expected 206, received ${response.status}`);
    }
    if (
        response.headers.get("content-range") !==
        `bytes ${start}-${end}/${options.bundle.archiveSize}`
    ) {
        throw new Error(`Invalid Content-Range for bundle ${options.bundle.id}`);
    }
    const responseEtag = response.headers.get("etag");
    if (responseEtag && normalizeEtag(responseEtag) !== normalizeEtag(options.bundle.etag)) {
        throw new Error(`Bundle ETag changed during download: ${options.bundle.id}`);
    }

    const body = new Uint8Array(await response.arrayBuffer());
    if (body.byteLength !== end - start + 1) {
        throw new Error(`Incomplete Range response for bundle ${options.bundle.id}`);
    }
    return body;
}

function normalizeEtag(etag: string) {
    return etag.replace(/^W\//, "").replace(/^"|"$/g, "");
}

function createAbortError() {
    return new DOMException("The operation was aborted", "AbortError");
}
