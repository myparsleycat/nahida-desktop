import { randomUUID } from "node:crypto";
import { rm, truncate, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";

import PQueue from "p-queue";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { NahidaDesktop } from "..";
import type { FinalFile, UploadProgress } from "./upload";

import { uploadDriveFilesV2 } from "./upload-v2";

const mocks = vi.hoisted(() => ({
    networkFetch: vi.fn(),
    request: vi.fn(),
    post: vi.fn(),
}));

vi.mock("@main/internal/network-fetch", () => ({
    networkFetch: mocks.networkFetch,
}));

vi.mock("ky", () => ({
    default: Object.assign(mocks.request, { post: mocks.post }),
}));

const desktop = {
    httpService: { getHeaders: vi.fn().mockResolvedValue({ Authorization: "Bearer token" }) },
    logger: { error: vi.fn() },
} as unknown as NahidaDesktop;

function file(id: string): FinalFile {
    return {
        FID: id,
        path: `${id}.txt`,
        name: `${id}.txt`,
        size: 10,
        parentPath: "",
        parentId: "parent",
        fullPath: `C:/${id}.txt`,
        sha256: "a".repeat(64),
    };
}

function sseResponse(events: Array<{ event: string; data: unknown }>): Response {
    const lines = events.flatMap((e) => {
        const dataStr = typeof e.data === "string" ? e.data : JSON.stringify(e.data);
        return [`event: ${e.event}`, `data: ${dataStr}`, ""];
    });
    const body = new ReadableStream<Uint8Array>({
        start(controller) {
            controller.enqueue(new TextEncoder().encode(lines.join("\n")));
            controller.close();
        },
    });
    return new Response(body, { status: 200 });
}

function sseResponseText(events: Array<{ event: string; data: string }>): Response {
    const lines = events.flatMap((e) => [`event: ${e.event}`, `data: ${e.data}`, ""]);
    const body = new ReadableStream<Uint8Array>({
        start(controller) {
            controller.enqueue(new TextEncoder().encode(lines.join("\n")));
            controller.close();
        },
    });
    return new Response(body, { status: 200 });
}

describe("uploadDriveFilesV2", () => {
    beforeEach(() => {
        vi.clearAllMocks();
    });

    it("marks files materialized by the plan as server-deduplicated", async () => {
        mocks.networkFetch.mockResolvedValue(
            sseResponse([
                {
                    event: "complete",
                    data: {
                        requestId: "request-id",
                        items: [
                            { clientId: "created", status: "created", itemId: "one" },
                            { clientId: "exists", status: "exists", itemId: "two" },
                        ],
                        uploads: [],
                    },
                },
            ]),
        );
        const progress: UploadProgress[] = [];

        await uploadDriveFilesV2({
            desktop,
            currentId: "current",
            requestId: "request-id",
            files: [file("created"), file("exists")],
            queue: new PQueue({ concurrency: 2 }),
            prepareDirectFile: async () => ({ data: Buffer.from("unused") }),
            onProgress: (event) => progress.push(event),
        });

        expect(progress).toEqual([
            { bytes: 10, fileId: "created", isServerDeduplicated: true },
            { bytes: 10, fileId: "exists", isServerDeduplicated: true },
        ]);
        expect(mocks.request).not.toHaveBeenCalled();
    });

    it("emits plan progress events via onPlanProgress", async () => {
        mocks.networkFetch.mockResolvedValue(
            sseResponse([
                { event: "status", data: "permission_check" },
                {
                    event: "progress",
                    data: { phase: "permission_check", processed: 1, total: 1 },
                },
                { event: "status", data: "processing" },
                {
                    event: "progress",
                    data: { phase: "processing", processed: 1, total: 1 },
                },
                {
                    event: "complete",
                    data: {
                        requestId: "request-id",
                        items: [{ clientId: "first", status: "created", itemId: "one" }],
                        uploads: [],
                    },
                },
            ]),
        );
        const planProgress: Array<{ phase: string; processed: number; total: number }> = [];

        await uploadDriveFilesV2({
            desktop,
            currentId: "current",
            requestId: "request-id",
            files: [file("first")],
            queue: new PQueue({ concurrency: 2 }),
            prepareDirectFile: async () => ({ data: Buffer.from("unused") }),
            onPlanProgress: (p) => planProgress.push(p),
        });

        expect(planProgress).toEqual([
            { phase: "permission_check", processed: 1, total: 1 },
            { phase: "processing", processed: 1, total: 1 },
        ]);
    });

    it("uploads a shared hash once and completes every planned target", async () => {
        mocks.networkFetch.mockResolvedValue(
            sseResponse([
                {
                    event: "complete",
                    data: {
                        requestId: "request-id",
                        items: [
                            { clientId: "first", status: "pending", intentId: "intent" },
                            { clientId: "second", status: "pending", intentId: "intent" },
                        ],
                        uploads: [
                            {
                                intentId: "intent",
                                url: "https://api.nahida.live/akasha/v2/uploads/intent",
                                method: "POST",
                                form: { token: "token", sha256: "a".repeat(64) },
                            },
                        ],
                    },
                },
            ]),
        );
        mocks.request.mockResolvedValue(
            new Response(JSON.stringify({ status: "completed" }), { status: 200 }),
        );
        const progress: UploadProgress[] = [];

        await uploadDriveFilesV2({
            desktop,
            currentId: "shared-directory",
            requestId: "request-id",
            files: [file("first"), file("second")],
            queue: new PQueue({ concurrency: 2 }),
            prepareDirectFile: async () => ({ data: Buffer.from("payload") }),
            onProgress: (event) => progress.push(event),
        });

        expect(mocks.request).toHaveBeenCalledTimes(1);
        expect(progress.filter((event) => event.fileId).map((event) => event.fileId)).toEqual([
            "first",
            "second",
        ]);
        expect(progress.reduce((sum, event) => sum + event.bytes, 0)).toBe(20);
    });

    it("reports denied plan items as a failed upload", async () => {
        mocks.networkFetch.mockResolvedValue(
            sseResponse([
                {
                    event: "complete",
                    data: {
                        requestId: "request-id",
                        items: [{ clientId: "denied", status: "denied", reason: "invalid_parent" }],
                        uploads: [],
                    },
                },
            ]),
        );

        await expect(
            uploadDriveFilesV2({
                desktop,
                currentId: "current",
                requestId: "request-id",
                files: [file("denied")],
                queue: new PQueue({ concurrency: 1 }),
                prepareDirectFile: async () => ({ data: Buffer.from("unused") }),
            }),
        ).rejects.toThrow("denied.txt: invalid_parent");
    });

    it("throws on SSE error event", async () => {
        mocks.networkFetch.mockResolvedValue(
            sseResponseText([{ event: "error", data: "An error occurred during planning" }]),
        );

        await expect(
            uploadDriveFilesV2({
                desktop,
                currentId: "current",
                requestId: "request-id",
                files: [file("error-file")],
                queue: new PQueue({ concurrency: 1 }),
                prepareDirectFile: async () => ({ data: Buffer.from("unused") }),
            }),
        ).rejects.toThrow("[upload plan failed] An error occurred during planning");
    });

    it("removes the abort listener after a retry delay completes", async () => {
        vi.useFakeTimers({ toFake: ["setTimeout"] });
        const controller = new AbortController();
        const addEventListener = vi.spyOn(controller.signal, "addEventListener");
        const removeEventListener = vi.spyOn(controller.signal, "removeEventListener");
        mocks.networkFetch.mockResolvedValue(
            sseResponse([
                {
                    event: "complete",
                    data: {
                        requestId: "request-id",
                        items: [{ clientId: "retry", status: "pending", intentId: "intent" }],
                        uploads: [
                            {
                                intentId: "intent",
                                url: "https://api.nahida.live/akasha/v2/uploads/intent",
                                method: "POST",
                                form: { token: "token", sha256: "a".repeat(64) },
                            },
                        ],
                    },
                },
            ]),
        );
        mocks.request
            .mockResolvedValueOnce(new Response("server error", { status: 500 }))
            .mockResolvedValueOnce(
                new Response(JSON.stringify({ status: "completed" }), { status: 200 }),
            );

        try {
            const upload = uploadDriveFilesV2({
                desktop,
                currentId: "current",
                requestId: "request-id",
                files: [file("retry")],
                queue: new PQueue({ concurrency: 1 }),
                prepareDirectFile: async () => ({ data: Buffer.from("payload") }),
                signal: controller.signal,
            });
            while (mocks.request.mock.calls.length === 0) {
                await new Promise<void>((resolve) => setImmediate(resolve));
            }
            await new Promise<void>((resolve) => setImmediate(resolve));

            const abortHandler = addEventListener.mock.calls.find(
                ([event]) => event === "abort",
            )?.[1];
            expect(abortHandler).toBeDefined();
            await vi.advanceTimersByTimeAsync(1_000);
            await upload;

            expect(removeEventListener).toHaveBeenCalledWith("abort", abortHandler);
        } finally {
            vi.useRealTimers();
        }
    });

    it("fails multipart upload when the source file ends before the planned size", async () => {
        const filePath = path.join(os.tmpdir(), `nahida-upload-v2-${randomUUID()}.bin`);
        const size = 80 * 1024 * 1024;
        await writeFile(filePath, Buffer.from([0]));
        mocks.networkFetch.mockResolvedValue(
            sseResponse([
                {
                    event: "complete",
                    data: {
                        requestId: "request-id",
                        items: [{ clientId: "large", status: "pending", intentId: "intent" }],
                        uploads: [
                            {
                                intentId: "intent",
                                url: "https://api.nahida.live/akasha/v2/uploads/intent",
                                method: "POST",
                                form: { token: "token", sha256: "a".repeat(64) },
                            },
                        ],
                    },
                },
            ]),
        );

        try {
            await expect(
                uploadDriveFilesV2({
                    desktop,
                    currentId: "current",
                    requestId: "request-id",
                    files: [{ ...file("large"), fullPath: filePath, size }],
                    queue: new PQueue({ concurrency: 1 }),
                    prepareDirectFile: async () => ({ data: Buffer.from("unused") }),
                }),
            ).rejects.toThrow("Unexpected EOF while reading large.txt");
        } finally {
            await rm(filePath, { force: true });
        }

        expect(mocks.request).not.toHaveBeenCalled();
    });

    it("rolls back multipart progress when completion fails", async () => {
        const filePath = path.join(os.tmpdir(), `nahida-upload-v2-${randomUUID()}.bin`);
        const size = 80 * 1024 * 1024;
        await writeFile(filePath, "");
        await truncate(filePath, size);
        mocks.networkFetch.mockResolvedValue(
            sseResponse([
                {
                    event: "complete",
                    data: {
                        requestId: "request-id",
                        items: [{ clientId: "large", status: "pending", intentId: "intent" }],
                        uploads: [
                            {
                                intentId: "intent",
                                url: "https://api.nahida.live/akasha/v2/uploads/intent",
                                method: "POST",
                                form: { token: "token", sha256: "a".repeat(64) },
                            },
                        ],
                    },
                },
            ]),
        );
        mocks.request.mockImplementation(async (_url, options: { body: ReadableStream }) => {
            const reader = options.body.getReader();
            while (!(await reader.read()).done) {}
            return new Response(JSON.stringify({ status: "pending" }), { status: 202 });
        });
        mocks.post.mockResolvedValue(
            new Response(JSON.stringify("chunk_owner_mismatch"), { status: 409 }),
        );
        const progress: UploadProgress[] = [];

        try {
            await expect(
                uploadDriveFilesV2({
                    desktop,
                    currentId: "current",
                    requestId: "request-id",
                    files: [{ ...file("large"), fullPath: filePath, size }],
                    queue: new PQueue({ concurrency: 1 }),
                    prepareDirectFile: async () => ({ data: Buffer.from("unused") }),
                    onProgress: (event) => progress.push(event),
                }),
            ).rejects.toThrow("chunk_owner_mismatch");
        } finally {
            await rm(filePath, { force: true });
        }

        expect(progress.reduce((sum, event) => sum + event.bytes, 0)).toBe(0);
    });
});
