import { mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";

import fse from "fs-extra";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
    head: vi.fn(),
    request: vi.fn(),
}));

vi.mock("ky", () => ({
    default: Object.assign(mocks.request, { head: mocks.head }),
}));

import { ParallelDownloader } from "./parallel-downloader";

type DownloadChunk = (input: {
    url: string;
    start: number;
    end: number;
    fileSize: number;
    resumeBytes: number;
    chunkPath: string;
    onProgress?: (transferredBytes: number, incrementalBytes: number) => void;
}) => Promise<void>;

describe("ParallelDownloader range capability probe", () => {
    beforeEach(() => {
        mocks.head.mockReset();
        mocks.request.mockReset();
        mocks.head.mockResolvedValue(
            new Response(null, {
                status: 200,
                headers: { "Accept-Ranges": "bytes" },
            }),
        );
    });

    it("does not cache negative range probes", async () => {
        const getHeaders = vi.fn().mockResolvedValue({ Authorization: "Bearer token" });
        const downloader = new ParallelDownloader({ getHeaders });
        mocks.head.mockResolvedValue(
            new Response(null, {
                status: 200,
            }),
        );

        await expect(
            downloader.checkRangeSupport("https://example.test/files/abc?signature=one"),
        ).resolves.toBe(false);
        await expect(
            downloader.checkRangeSupport("https://example.test/files/abc?signature=two"),
        ).resolves.toBe(false);

        expect(mocks.head).toHaveBeenCalledTimes(2);
        expect(getHeaders).toHaveBeenCalledTimes(2);
    });

    it("reuses the probe for signed URLs that share a resource path", async () => {
        const getHeaders = vi.fn().mockResolvedValue({ Authorization: "Bearer token" });
        const downloader = new ParallelDownloader({ getHeaders });

        await expect(
            downloader.checkRangeSupport("https://example.test/files/abc?signature=one"),
        ).resolves.toBe(true);
        await expect(
            downloader.checkRangeSupport("https://example.test/files/abc?signature=two"),
        ).resolves.toBe(true);

        expect(mocks.head).toHaveBeenCalledOnce();
        expect(getHeaders).toHaveBeenCalledOnce();
        expect(mocks.head).toHaveBeenCalledWith(
            "https://example.test/files/abc?signature=one",
            expect.objectContaining({ retry: 0 }),
        );
    });
});

describe("ParallelDownloader partial chunk resume", () => {
    let tempDir: string;

    beforeEach(async () => {
        mocks.request.mockReset();
        tempDir = await mkdtemp(path.join(os.tmpdir(), "parallel-download-test-"));
    });

    afterEach(async () => {
        await rm(tempDir, { recursive: true, force: true });
    });

    it("appends the remaining range to a preserved chunk", async () => {
        const chunkPath = path.join(tempDir, "file.chunk0");
        await writeFile(chunkPath, "abc");
        mocks.request.mockResolvedValue(
            new Response("def", {
                status: 206,
                headers: { "Content-Range": "bytes 3-5/6" },
            }),
        );
        const downloader = new ParallelDownloader({
            getHeaders: vi.fn().mockResolvedValue({ Authorization: "Bearer token" }),
        });
        const progress = vi.fn();

        await (
            downloader as unknown as {
                downloadChunk: DownloadChunk;
            }
        ).downloadChunk({
            url: "https://n3.nahida.live/132/123412341234",
            start: 0,
            end: 5,
            fileSize: 6,
            resumeBytes: 3,
            chunkPath,
            onProgress: progress,
        });

        expect(await readFile(chunkPath, "utf8")).toBe("abcdef");
        expect(mocks.request).toHaveBeenCalledWith(
            "https://n3.nahida.live/132/123412341234",
            expect.objectContaining({
                headers: expect.objectContaining({ Range: "bytes=3-5" }),
            }),
        );
        expect(progress).toHaveBeenLastCalledWith(6, 3);
    });

    it("rejects a mismatched content range without changing the preserved chunk", async () => {
        const chunkPath = path.join(tempDir, "file.chunk0");
        await writeFile(chunkPath, "abc");
        mocks.request.mockResolvedValue(
            new Response("abcdef", {
                status: 206,
                headers: { "Content-Range": "bytes 0-5/6" },
            }),
        );
        const downloader = new ParallelDownloader({
            getHeaders: vi.fn().mockResolvedValue({}),
        });

        await expect(
            (
                downloader as unknown as {
                    downloadChunk: DownloadChunk;
                }
            ).downloadChunk({
                url: "https://n3.nahida.live/132/123412341234",
                start: 0,
                end: 5,
                fileSize: 6,
                resumeBytes: 3,
                chunkPath,
            }),
        ).rejects.toThrow("unexpected Content-Range");

        expect(await readFile(chunkPath, "utf8")).toBe("abc");
    });

    it("preserves bytes written before a connection failure", async () => {
        const chunkPath = path.join(tempDir, "file.chunk0");
        let firstChunkWritten: (() => void) | undefined;
        const firstChunkObserved = new Promise<void>((resolve) => {
            firstChunkWritten = resolve;
        });
        let sent = false;
        mocks.request.mockResolvedValue(
            new Response(
                new ReadableStream({
                    async pull(controller) {
                        if (sent) {
                            await firstChunkObserved;
                            while ((await readFile(chunkPath, "utf8").catch(() => "")) !== "abc") {
                                await new Promise((resolve) => setImmediate(resolve));
                            }
                            controller.error(new Error("connection reset"));
                            return;
                        }
                        sent = true;
                        controller.enqueue(new TextEncoder().encode("abc"));
                    },
                }),
                {
                    status: 206,
                    headers: { "Content-Range": "bytes 0-5/6" },
                },
            ),
        );
        const downloader = new ParallelDownloader({
            getHeaders: vi.fn().mockResolvedValue({}),
        });

        await expect(
            (
                downloader as unknown as {
                    downloadChunk: DownloadChunk;
                }
            ).downloadChunk({
                url: "https://n3.nahida.live/132/123412341234",
                start: 0,
                end: 5,
                fileSize: 6,
                resumeBytes: 0,
                chunkPath,
                onProgress: () => firstChunkWritten?.(),
            }),
        ).rejects.toThrow("connection reset");

        expect(await readFile(chunkPath, "utf8")).toBe("abc");
    });

    it("stops retrying mismatched content ranges when the partial chunk cannot be deleted", async () => {
        const savePath = path.join(tempDir, "file.bin");
        await writeFile(`${savePath}.chunk0`, "abc");
        await writeFile(
            `${savePath}.chunk-meta.json`,
            JSON.stringify({
                resource: "https://n3.nahida.live/132/123412341234",
                fileSize: 6,
            }),
        );
        mocks.request.mockResolvedValue(
            new Response("abcdef", {
                status: 206,
                headers: { "Content-Range": "bytes 0-4/6" },
            }),
        );
        const remove = vi.spyOn(fse, "remove").mockRejectedValue(new Error("busy"));
        const downloader = new ParallelDownloader({
            getHeaders: vi.fn().mockResolvedValue({}),
        });

        try {
            await expect(
                downloader.download({
                    url: "https://n3.nahida.live/132/123412341234",
                    savePath,
                    fileSize: 6,
                    maxChunks: 1,
                    adaptive: false,
                }),
            ).rejects.toThrow("unexpected Content-Range");
            expect(mocks.request.mock.calls.length).toBeLessThanOrEqual(3);
        } finally {
            remove.mockRestore();
        }
    });

    it("discards leftover chunks that do not match the current resource", async () => {
        const savePath = path.join(tempDir, "file.bin");
        await writeFile(`${savePath}.chunk0`, "stale!");
        await writeFile(
            `${savePath}.chunk-meta.json`,
            JSON.stringify({ resource: "https://other.example/file", fileSize: 6 }),
        );
        mocks.request.mockResolvedValue(
            new Response("abcdef", {
                status: 206,
                headers: { "Content-Range": "bytes 0-5/6" },
            }),
        );
        const downloader = new ParallelDownloader({
            getHeaders: vi.fn().mockResolvedValue({}),
        });

        await downloader.download({
            url: "https://n3.nahida.live/132/123412341234",
            savePath,
            fileSize: 6,
            maxChunks: 1,
            adaptive: false,
        });

        expect(await readFile(savePath, "utf8")).toBe("abcdef");
        expect(mocks.request).toHaveBeenCalled();
    });

    it("does not resume leftover chunks when mismatched artifacts cannot be deleted", async () => {
        const savePath = path.join(tempDir, "file.bin");
        await writeFile(`${savePath}.chunk0`, "stale!");
        await writeFile(
            `${savePath}.chunk-meta.json`,
            JSON.stringify({ resource: "https://other.example/file", fileSize: 6 }),
        );
        const remove = vi.spyOn(fse, "remove").mockRejectedValue(new Error("busy"));
        const downloader = new ParallelDownloader({
            getHeaders: vi.fn().mockResolvedValue({}),
        });

        try {
            await expect(
                downloader.download({
                    url: "https://n3.nahida.live/132/123412341234",
                    savePath,
                    fileSize: 6,
                    maxChunks: 1,
                    adaptive: false,
                }),
            ).rejects.toThrow("Failed to discard leftover download chunks");
            expect(mocks.request).not.toHaveBeenCalled();
        } finally {
            remove.mockRestore();
        }
    });

    it("rejects a short response body even when Content-Range looks complete", async () => {
        const chunkPath = path.join(tempDir, "file.chunk0");
        mocks.request.mockResolvedValue(
            new Response("abc", {
                status: 206,
                headers: { "Content-Range": "bytes 0-5/6" },
            }),
        );
        const downloader = new ParallelDownloader({
            getHeaders: vi.fn().mockResolvedValue({}),
        });

        await expect(
            (
                downloader as unknown as {
                    downloadChunk: DownloadChunk;
                }
            ).downloadChunk({
                url: "https://n3.nahida.live/132/123412341234",
                start: 0,
                end: 5,
                fileSize: 6,
                resumeBytes: 0,
                chunkPath,
            }),
        ).rejects.toThrow("ended early");

        expect(await readFile(chunkPath, "utf8")).toBe("abc");
    });

    it("resumes leftover chunks that match the current resource", async () => {
        const savePath = path.join(tempDir, "file.bin");
        await writeFile(`${savePath}.chunk0`, "abc");
        await writeFile(
            `${savePath}.chunk-meta.json`,
            JSON.stringify({
                resource: "https://n3.nahida.live/132/123412341234",
                fileSize: 6,
            }),
        );
        mocks.request.mockResolvedValue(
            new Response("def", {
                status: 206,
                headers: { "Content-Range": "bytes 3-5/6" },
            }),
        );
        const downloader = new ParallelDownloader({
            getHeaders: vi.fn().mockResolvedValue({}),
        });

        await downloader.download({
            url: "https://n3.nahida.live/132/123412341234",
            savePath,
            fileSize: 6,
            maxChunks: 1,
            adaptive: false,
        });

        expect(await readFile(savePath, "utf8")).toBe("abcdef");
        expect(mocks.request).toHaveBeenCalledWith(
            "https://n3.nahida.live/132/123412341234",
            expect.objectContaining({
                headers: expect.objectContaining({ Range: "bytes=3-5" }),
            }),
        );
    });
});
