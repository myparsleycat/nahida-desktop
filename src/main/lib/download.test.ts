import { mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { NahidaDesktop } from "..";
import type { DownloadMetadata } from "./download";

import { BandwidthLimiter } from "./bandwidth-limiter";
import { DownloadHttpError, DownloadLib } from "./download";
import { SlowChunkMonitor } from "./slow-chunk-monitor";

const mocks = vi.hoisted(() => ({
    request: vi.fn(),
    fetchPresignedUrl: vi.fn(),
}));

vi.mock("@main/client", () => ({
    eden: {
        akasha: {
            file: {
                download: {
                    get: mocks.fetchPresignedUrl,
                },
            },
        },
    },
}));

vi.mock("ky", () => ({
    default: mocks.request,
}));

vi.mock("./slow-chunk-monitor", async (importOriginal) => {
    const actual = await importOriginal<typeof import("./slow-chunk-monitor")>();
    return {
        ...actual,
        slowReconnectDelayMs: () => 0,
        sleepWithAbort: async (_ms: number, signal: AbortSignal) => {
            if (signal.aborted) {
                throw new DOMException("The operation was aborted.", "AbortError");
            }
        },
    };
});

function waitForSlowAbort(signal: AbortSignal) {
    return new Promise<never>((_resolve, reject) => {
        if (signal.aborted) {
            reject(signal.reason ?? new DOMException("The operation was aborted.", "AbortError"));
            return;
        }
        signal.addEventListener(
            "abort",
            () => {
                reject(
                    signal.reason ?? new DOMException("The operation was aborted.", "AbortError"),
                );
            },
            { once: true },
        );
    });
}

type DownloadTask = {
    performDownload: (
        file: DownloadMetadata["files"][number],
        targetPath: string,
        signal: AbortSignal,
        options?: {
            onResumeReset?: () => void;
            resumeFrom?: number;
            attachLinkToken?: boolean;
            onProgress?: (bytes: number) => void;
            link?: { linkId: string; token: string };
        },
    ) => Promise<void>;
    executeWithSlowRetry: (input: {
        file: DownloadMetadata["files"][number];
        filePath: string;
        signal: AbortSignal;
        link?: { linkId: string; token: string };
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
        mocks.fetchPresignedUrl.mockReset();
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

    it("omits the link token when attachLinkToken is false", async () => {
        const targetPath = path.join(tempDir, "file.bin.ntmp");
        mocks.request.mockResolvedValue(new Response("complete", { status: 200 }));
        const desktop = createDesktop();
        const task = (new DownloadLib(desktop) as unknown as { task: DownloadTask }).task;

        await task.performDownload(file, targetPath, new AbortController().signal, {
            link: { linkId: "link-1", token: "link-token" },
            attachLinkToken: false,
        });

        expect(mocks.request).toHaveBeenCalledWith(
            file.url,
            expect.objectContaining({
                headers: { Authorization: "Bearer token" },
            }),
        );
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
        mocks.fetchPresignedUrl.mockReset();
        mocks.fetchPresignedUrl.mockResolvedValue({
            data: { url: "https://r2.test/signed" },
            error: null,
        });
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

    it("does not complete when abort skips the rename", async () => {
        const filePath = path.join(tempDir, "file.bin");
        mocks.request.mockResolvedValue(
            new Response(" world", {
                status: 206,
                headers: { "Content-Range": "bytes 5-10/11" },
            }),
        );
        const desktop = createDesktop();
        const onComplete = vi.fn();
        const abort = new AbortController();
        const task = (new DownloadLib(desktop) as unknown as { task: DownloadTask }).task;
        vi.spyOn(task, "performDownload").mockImplementation(async () => {
            abort.abort();
        });

        await task.executeWithSlowRetry({
            file: { ...file, size: 11 },
            filePath,
            signal: abort.signal,
            onComplete,
        });

        expect(onComplete).not.toHaveBeenCalled();
    });

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

    it("keeps the original url through the first slow reconnect then switches to a presigned url", async () => {
        const filePath = path.join(tempDir, "file.bin");
        const targetPath = `${filePath}.ntmp`;
        await writeFile(targetPath, "hello");
        const slowFile = {
            ...file,
            size: 11,
            url: "https://n1.nahida.live/fil/file.bin",
        };
        const desktop = createDesktop();
        const task = (new DownloadLib(desktop) as unknown as { task: DownloadTask }).task;
        const registered: Array<{ abortSlow: () => void }> = [];
        const register = vi.spyOn(slowChunkMonitor, "register").mockImplementation((input) => {
            const transfer = {
                key: `transfer-${registered.length}`,
                abortReason: null as "slow-chunk" | null,
                detect: null as "stall" | null,
                chunkSpeedBps: 0,
                peerMedianBps: 0,
            };
            registered.push({
                abortSlow: () => {
                    transfer.abortReason = "slow-chunk";
                    transfer.detect = "stall";
                    input.attemptController.abort();
                },
            });
            return transfer as never;
        });
        const performDownload = vi.spyOn(task, "performDownload");
        performDownload
            .mockImplementationOnce(async (_file, _target, signal, options) => {
                options?.onProgress?.(3);
                await waitForSlowAbort(signal);
            })
            .mockImplementationOnce(async (_file, _target, signal, options) => {
                options?.onProgress?.(3);
                await waitForSlowAbort(signal);
            })
            .mockImplementationOnce(async (currentFile, currentTarget, _signal, options) => {
                expect(currentFile.url).toBe("https://r2.test/signed");
                expect(currentFile.urlOrigin).toBe("presign");
                expect(currentTarget).toBe(targetPath);
                expect(options?.resumeFrom).toBe(5);
                expect(options?.attachLinkToken).toBe(false);
                await writeFile(currentTarget, "hello world");
            });

        const run = task.executeWithSlowRetry({
            file: slowFile,
            filePath,
            signal: new AbortController().signal,
            link: { linkId: "link-1", token: "link-token" },
            onComplete: vi.fn(),
        });

        await vi.waitFor(() => expect(performDownload).toHaveBeenCalledTimes(1));
        registered[0]?.abortSlow();
        await vi.waitFor(() => expect(performDownload).toHaveBeenCalledTimes(2));
        expect(slowFile.url).toBe("https://n1.nahida.live/fil/file.bin");
        expect(slowFile.urlOrigin).toBeUndefined();
        expect(mocks.fetchPresignedUrl).not.toHaveBeenCalled();
        expect(register).toHaveBeenNthCalledWith(2, expect.objectContaining({ slowReconnects: 1 }));

        registered[1]?.abortSlow();
        await vi.waitFor(() => expect(performDownload).toHaveBeenCalledTimes(3));
        await run;

        expect(slowFile.url).toBe("https://r2.test/signed");
        expect(slowFile.urlOrigin).toBe("presign");
        expect(mocks.fetchPresignedUrl).toHaveBeenCalledWith({
            query: {
                uuid: "file-id",
                presign: true,
                linkId: "link-1",
            },
            headers: { "nhd-link-token": "link-token" },
            fetch: { signal: expect.any(AbortSignal) },
        });
        expect(performDownload).toHaveBeenNthCalledWith(
            3,
            slowFile,
            targetPath,
            expect.any(AbortSignal),
            expect.objectContaining({
                attachLinkToken: false,
                resumeFrom: 5,
            }),
        );

        register.mockRestore();
        performDownload.mockRestore();
    });

    it("refreshes an expired presigned url once after a 403", async () => {
        const filePath = path.join(tempDir, "file.bin");
        const targetPath = `${filePath}.ntmp`;
        await writeFile(targetPath, "hello");
        const slowFile = {
            ...file,
            size: 11,
            url: "https://n1.nahida.live/fil/file.bin",
        };
        mocks.fetchPresignedUrl
            .mockResolvedValueOnce({
                data: { url: "https://r2.test/signed-1" },
                error: null,
            })
            .mockResolvedValueOnce({
                data: { url: "https://r2.test/signed-2" },
                error: null,
            });
        const desktop = createDesktop();
        const task = (new DownloadLib(desktop) as unknown as { task: DownloadTask }).task;
        const registered: Array<{ abortSlow: () => void }> = [];
        const register = vi.spyOn(slowChunkMonitor, "register").mockImplementation((input) => {
            const transfer = {
                key: `transfer-${registered.length}`,
                abortReason: null as "slow-chunk" | null,
                detect: null,
                chunkSpeedBps: 0,
                peerMedianBps: 0,
            };
            registered.push({
                abortSlow: () => {
                    transfer.abortReason = "slow-chunk";
                    input.attemptController.abort();
                },
            });
            return transfer as never;
        });
        const performDownload = vi.spyOn(task, "performDownload");
        performDownload
            .mockImplementationOnce(async (_file, _target, signal) => {
                await waitForSlowAbort(signal);
            })
            .mockImplementationOnce(async (_file, _target, signal) => {
                await waitForSlowAbort(signal);
            })
            .mockRejectedValueOnce(new DownloadHttpError(403, "Forbidden"))
            .mockImplementationOnce(async (currentFile, currentTarget, _signal, options) => {
                expect(currentFile.url).toBe("https://r2.test/signed-2");
                expect(currentFile.urlOrigin).toBe("presign");
                expect(options?.resumeFrom).toBe(5);
                await writeFile(currentTarget, "hello world");
            });

        const run = task.executeWithSlowRetry({
            file: slowFile,
            filePath,
            signal: new AbortController().signal,
            onComplete: vi.fn(),
        });

        for (const expectedCalls of [1, 2]) {
            await vi.waitFor(() => expect(performDownload).toHaveBeenCalledTimes(expectedCalls));
            registered[expectedCalls - 1]?.abortSlow();
        }

        await vi.waitFor(() => expect(performDownload).toHaveBeenCalledTimes(4));
        await run;

        expect(slowFile.url).toBe("https://r2.test/signed-2");
        expect(slowFile.urlOrigin).toBe("presign");
        expect(mocks.fetchPresignedUrl).toHaveBeenCalledTimes(2);
        register.mockRestore();
        performDownload.mockRestore();
    });

    it("retries expired presign refresh after a transient fetch failure", async () => {
        const filePath = path.join(tempDir, "file.bin");
        const targetPath = `${filePath}.ntmp`;
        await writeFile(targetPath, "hello");
        const expiredFile = {
            ...file,
            size: 11,
            url: "https://r2.test/expired",
            urlOrigin: "presign" as const,
        };
        mocks.fetchPresignedUrl
            .mockRejectedValueOnce(new Error("temporary network error"))
            .mockResolvedValueOnce({
                data: { url: "https://r2.test/signed-fresh" },
                error: null,
            });
        const desktop = createDesktop();
        const task = (new DownloadLib(desktop) as unknown as { task: DownloadTask }).task;
        const performDownload = vi.spyOn(task, "performDownload");
        performDownload
            .mockRejectedValueOnce(new DownloadHttpError(403, "Forbidden"))
            .mockRejectedValueOnce(new DownloadHttpError(403, "Forbidden"))
            .mockImplementationOnce(async (currentFile, currentTarget, _signal, options) => {
                expect(currentFile.url).toBe("https://r2.test/signed-fresh");
                expect(currentFile.urlOrigin).toBe("presign");
                expect(options?.resumeFrom).toBe(5);
                await writeFile(currentTarget, "hello world");
            });

        await task.executeWithSlowRetry({
            file: expiredFile,
            filePath,
            signal: new AbortController().signal,
            onComplete: vi.fn(),
        });

        expect(expiredFile.url).toBe("https://r2.test/signed-fresh");
        expect(mocks.fetchPresignedUrl).toHaveBeenCalledTimes(2);
        expect(performDownload).toHaveBeenCalledTimes(3);
        performDownload.mockRestore();
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
