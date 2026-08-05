import { mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { NahidaDesktop } from "..";
import type { DownloadMetadata } from "./download";

import { BandwidthLimiter } from "./bandwidth-limiter";
import { DownloadLib } from "./download";

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
};

const desktop = {
    httpService: {
        getHeaders: vi.fn().mockResolvedValue({ Authorization: "Bearer token" }),
        getAgent: vi.fn().mockResolvedValue({}),
    },
    logger: {
        error: vi.fn(),
        info: vi.fn(),
        warn: vi.fn(),
    },
    service: {
        transfer: {
            downloadBandwidth: new BandwidthLimiter(),
        },
    },
} as unknown as NahidaDesktop;

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
        mocks.request.mockResolvedValue(new Response(" world", { status: 206 }));

        const task = (new DownloadLib(desktop) as unknown as { task: DownloadTask }).task;
        await task.performDownload(file, targetPath, new AbortController().signal, {
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

        const task = (new DownloadLib(desktop) as unknown as { task: DownloadTask }).task;
        await task.performDownload(file, targetPath, new AbortController().signal, {
            onResumeReset,
            resumeFrom: 5,
        });

        expect(await readFile(targetPath, "utf8")).toBe("complete");
        expect(onResumeReset).toHaveBeenCalledOnce();
    });

    it("retries without Range after a 416 response", async () => {
        const targetPath = path.join(tempDir, "file.bin.ntmp");
        await writeFile(targetPath, "stale partial data");
        mocks.request
            .mockResolvedValueOnce(new Response(null, { status: 416 }))
            .mockResolvedValueOnce(new Response("complete", { status: 200 }));
        const onResumeReset = vi.fn();

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
});
