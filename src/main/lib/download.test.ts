import { mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { NahidaDesktop } from "..";
import type { DownloadMetadata } from "./download";

import { BandwidthLimiter } from "./bandwidth-limiter";
import { DownloadLib } from "./download";
import { SlowChunkMonitor } from "./slow-chunk-monitor";

const mocks = vi.hoisted(() => ({
    request: vi.fn(),
}));

vi.mock("@main/client", () => ({
    eden: {},
}));

vi.mock("ky", () => ({
    default: mocks.request,
}));

type DownloadTask = {
    performDownload: (
        file: DownloadMetadata["files"][number],
        targetPath: string,
        signal: AbortSignal,
        options?: {
            onResumeReset?: () => void;
            resumeFrom?: number;
        },
    ) => Promise<void>;
    executeWithSlowRetry: (input: {
        file: DownloadMetadata["files"][number];
        filePath: string;
        signal: AbortSignal;
        onComplete: () => void;
        onProgress?: (bytes: number) => void;
    }) => Promise<void>;
    getResumeOffset: (
        file: DownloadMetadata["files"][number],
        targetPath: string,
    ) => Promise<number>;
};

const slowChunkMonitor = new SlowChunkMonitor();

function createDesktop() {
    return {
        httpService: {
            getHeaders: vi.fn().mockResolvedValue({ Authorization: "Bearer token" }),
        },
        logger: {
            error: vi.fn(),
            info: vi.fn(),
            warn: vi.fn(),
        },
        service: {
            transfer: {
                downloadBandwidth: new BandwidthLimiter(),
                slowChunkMonitor,
                markFileCompleted: vi.fn(),
            },
        },
        lib: {
            fs: {
                rename: vi.fn(async (_from: string, _to: string) => {}),
                pathExists: vi.fn().mockResolvedValue(false),
                stat: vi.fn(),
            },
        },
    } as unknown as NahidaDesktop;
}

const file: DownloadMetadata["files"][number] = {
    id: "file-id",
    fileId: "file-id",
    parentId: null,
    name: "file.bin",
    size: 8,
    compAlg: null,
    url: "https://example.test/file.bin",
};

describe("FileDownloadTask range handling", () => {
    let tempDir: string;

    beforeEach(async () => {
        mocks.request.mockReset();
        tempDir = await mkdtemp(path.join(os.tmpdir(), "nahida-download-test-"));
    });

    afterEach(async () => {
        await rm(tempDir, { recursive: true, force: true });
    });

    it("appends an existing temporary file when the server honors Range", async () => {
        const targetPath = path.join(tempDir, "file.bin.ntmp");
        await writeFile(targetPath, "hello");
        const rangedFile = { ...file, size: 11 };
        mocks.request.mockResolvedValue(
            new Response(" world", {
                status: 206,
                headers: { "Content-Range": "bytes 5-10/11" },
            }),
        );
        const desktop = createDesktop();

        const task = (new DownloadLib(desktop) as unknown as { task: DownloadTask }).task;
        await task.performDownload(rangedFile, targetPath, new AbortController().signal, {
            resumeFrom: 5,
        });

        expect(await readFile(targetPath, "utf8")).toBe("hello world");
        expect(mocks.request).toHaveBeenCalledWith(
            file.url,
            expect.objectContaining({
                headers: {
                    Authorization: "Bearer token",
                    Range: "bytes=5-",
                },
            }),
        );
    });

    it("restarts from the beginning when Range is ignored", async () => {
        const targetPath = path.join(tempDir, "file.bin.ntmp");
        await writeFile(targetPath, "stale partial data");
        mocks.request.mockResolvedValue(new Response("complete", { status: 200 }));
        const onResumeReset = vi.fn();
        const desktop = createDesktop();

        const task = (new DownloadLib(desktop) as unknown as { task: DownloadTask }).task;
        await task.performDownload(file, targetPath, new AbortController().signal, {
            onResumeReset,
            resumeFrom: 5,
        });

        expect(await readFile(targetPath, "utf8")).toBe("complete");
        expect(onResumeReset).toHaveBeenCalledOnce();
    });

    it("restarts when the resumed Content-Range does not match", async () => {
        const targetPath = path.join(tempDir, "file.bin.ntmp");
        await writeFile(targetPath, "hello");
        mocks.request
            .mockResolvedValueOnce(
                new Response("wrong", {
                    status: 206,
                    headers: { "Content-Range": "bytes 0-4/8" },
                }),
            )
            .mockResolvedValueOnce(new Response("complete", { status: 200 }));
        const onResumeReset = vi.fn();
        const desktop = createDesktop();

        const task = (new DownloadLib(desktop) as unknown as { task: DownloadTask }).task;
        await task.performDownload(file, targetPath, new AbortController().signal, {
            onResumeReset,
            resumeFrom: 5,
        });

        expect(await readFile(targetPath, "utf8")).toBe("complete");
        expect(onResumeReset).toHaveBeenCalledOnce();
        expect(mocks.request).toHaveBeenNthCalledWith(
            2,
            file.url,
            expect.objectContaining({ headers: { Authorization: "Bearer token" } }),
        );
    });

    it("retries without Range after a 416 response", async () => {
        const targetPath = path.join(tempDir, "file.bin.ntmp");
        await writeFile(targetPath, "stale partial data");
        mocks.request
            .mockResolvedValueOnce(new Response(null, { status: 416 }))
            .mockResolvedValueOnce(new Response("complete", { status: 200 }));
        const onResumeReset = vi.fn();
        const desktop = createDesktop();

        const task = (new DownloadLib(desktop) as unknown as { task: DownloadTask }).task;
        await task.performDownload(file, targetPath, new AbortController().signal, {
            onResumeReset,
            resumeFrom: 5,
        });

        expect(await readFile(targetPath, "utf8")).toBe("complete");
        expect(onResumeReset).toHaveBeenCalledOnce();
        expect(mocks.request).toHaveBeenNthCalledWith(
            2,
            file.url,
            expect.objectContaining({ headers: { Authorization: "Bearer token" } }),
        );
    });

    it("treats 416 as already complete when the temp file covers the full size", async () => {
        const targetPath = path.join(tempDir, "file.bin.ntmp");
        await writeFile(targetPath, "complete");
        mocks.request.mockResolvedValue(new Response(null, { status: 416 }));
        const desktop = createDesktop();
        const fullFile = { ...file, size: 8 };

        const task = (new DownloadLib(desktop) as unknown as { task: DownloadTask }).task;
        await task.performDownload(fullFile, targetPath, new AbortController().signal, {
            resumeFrom: 8,
        });

        expect(await readFile(targetPath, "utf8")).toBe("complete");
        expect(mocks.request).toHaveBeenCalledTimes(1);
    });

    it("deletes the temp file and returns zero for compressed files", async () => {
        const targetPath = path.join(tempDir, "file.bin.ntmp");
        await writeFile(targetPath, "stale-gzip");
        const desktop = createDesktop();
        const gzipFile = { ...file, compAlg: "gzip" as const };

        const task = (new DownloadLib(desktop) as unknown as { task: DownloadTask }).task;
        const offset = await task.getResumeOffset(gzipFile, targetPath);

        expect(offset).toBe(0);
        await expect(readFile(targetPath, "utf8")).rejects.toThrow();
    });
});

describe("FileDownloadTask executeWithSlowRetry progress correction", () => {
    let tempDir: string;

    beforeEach(async () => {
        mocks.request.mockReset();
        tempDir = await mkdtemp(path.join(os.tmpdir(), "nahida-download-test-"));
    });

    afterEach(async () => {
        await rm(tempDir, { recursive: true, force: true });
    });

    it("reports resume delta across multiple failed attempts", async () => {
        const filePath = path.join(tempDir, "file.bin");
        const targetPath = `${filePath}.ntmp`;
        await writeFile(targetPath, "hello");

        const fullFile = { ...file, size: 11 };
        let partialChunkSent = false;

        mocks.request
            .mockResolvedValueOnce(
                new Response(
                    new ReadableStream({
                        async pull(controller) {
                            if (partialChunkSent || controller.desiredSize === null) return;
                            partialChunkSent = true;
                            controller.enqueue(new TextEncoder().encode(" wo"));
                            await new Promise((resolve) => setTimeout(resolve, 50));
                            controller.error(new Error("network failure"));
                        },
                    }),
                    {
                        status: 206,
                        headers: { "Content-Range": "bytes 5-10/11" },
                    },
                ),
            )
            .mockImplementationOnce(async () => {
                throw new Error("network failure");
            })
            .mockResolvedValueOnce(
                new Response("rld", {
                    status: 206,
                    headers: { "Content-Range": "bytes 8-10/11" },
                }),
            );

        const desktop = createDesktop();
        (desktop.lib.fs.rename as ReturnType<typeof vi.fn>).mockImplementation(
            async (_from: string, to: string) => {
                const data = await readFile(targetPath, "utf8");
                await writeFile(to, data);
            },
        );

        const progressCalls: number[] = [];
        const onComplete = vi.fn();

        const task = (new DownloadLib(desktop) as unknown as { task: DownloadTask }).task;
        await task.executeWithSlowRetry({
            file: fullFile,
            filePath,
            signal: new AbortController().signal,
            onComplete,
            onProgress: (bytes) => {
                progressCalls.push(bytes);
            },
        });

        expect(onComplete).toHaveBeenCalledOnce();
        expect(mocks.request).toHaveBeenCalledTimes(3);
        expect(progressCalls.every((bytes) => bytes >= 0)).toBe(true);
        expect(progressCalls.reduce((sum, bytes) => sum + bytes, 0)).toBe(11);
    }, 15000);

    it("records resumed progress using the cumulative file offset", async () => {
        const filePath = path.join(tempDir, "file.bin");
        const targetPath = `${filePath}.ntmp`;
        await writeFile(targetPath, "a".repeat(97));
        mocks.request.mockResolvedValue(
            new Response("xyz", {
                status: 206,
                headers: { "Content-Range": "bytes 97-99/100" },
            }),
        );
        const desktop = createDesktop();
        const recordSample = vi.spyOn(slowChunkMonitor, "recordSample");
        const task = (new DownloadLib(desktop) as unknown as { task: DownloadTask }).task;

        await task.executeWithSlowRetry({
            file: { ...file, size: 100 },
            filePath,
            signal: new AbortController().signal,
            onComplete: vi.fn(),
        });

        expect(recordSample).toHaveBeenCalledWith(expect.any(String), 100);
        recordSample.mockRestore();
    });
});

describe("DownloadLib existing file handling", () => {
    beforeEach(() => {
        mocks.request.mockReset();
    });

    it("skips an existing file without comparing its size", async () => {
        const desktop = createDesktop();
        desktop.lib.fs.pathExists = vi
            .fn()
            .mockResolvedValue(true) as typeof desktop.lib.fs.pathExists;
        desktop.lib.fs.stat = vi
            .fn()
            .mockResolvedValue({ isFile: () => true }) as typeof desktop.lib.fs.stat;
        const onProgress = vi.fn();
        const onComplete = vi.fn();
        const downloadLib = new DownloadLib(desktop);
        const processFileDownloadTask = (
            downloadLib as unknown as {
                processFileDownloadTask: (input: {
                    pid: string;
                    file: DownloadMetadata["files"][number];
                    filePath: string;
                    abort: AbortController;
                    onProgress: (bytes: number) => void;
                    onComplete: () => void;
                }) => Promise<boolean>;
            }
        ).processFileDownloadTask;

        await expect(
            processFileDownloadTask.call(downloadLib, {
                pid: "transfer-id",
                file,
                filePath: path.join(os.tmpdir(), "existing-file.bin"),
                abort: new AbortController(),
                onProgress,
                onComplete,
            }),
        ).resolves.toBe(true);

        expect(onProgress).toHaveBeenCalledWith(file.size);
        expect(onComplete).toHaveBeenCalledOnce();
        expect(mocks.request).not.toHaveBeenCalled();
    });
});
