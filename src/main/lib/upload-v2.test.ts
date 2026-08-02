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
    plan: vi.fn(),
    request: vi.fn(),
    post: vi.fn(),
}));

vi.mock("@main/client", () => ({
    eden: {
        akasha: {
            v2: {
                drive: {
                    "files:plan": { post: mocks.plan },
                },
            },
        },
    },
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

describe("uploadDriveFilesV2", () => {
    beforeEach(() => {
        vi.clearAllMocks();
    });

    it("marks files materialized by the plan as server-deduplicated", async () => {
        mocks.plan.mockResolvedValue({
            error: null,
            data: {
                requestId: "request-id",
                items: [
                    { clientId: "created", status: "created", itemId: "one" },
                    { clientId: "exists", status: "exists", itemId: "two" },
                ],
                uploads: [],
            },
        });
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

    it("uploads a shared hash once and completes every planned target", async () => {
        mocks.plan.mockResolvedValue({
            error: null,
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
        });
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
        expect(mocks.plan).toHaveBeenCalledWith(
            expect.objectContaining({ current: "shared-directory" }),
        );
        expect(progress.filter((event) => event.fileId).map((event) => event.fileId)).toEqual([
            "first",
            "second",
        ]);
        expect(progress.reduce((sum, event) => sum + event.bytes, 0)).toBe(20);
    });

    it("reports denied plan items as a failed upload", async () => {
        mocks.plan.mockResolvedValue({
            error: null,
            data: {
                requestId: "request-id",
                items: [{ clientId: "denied", status: "denied", reason: "invalid_parent" }],
                uploads: [],
            },
        });

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

    it("rolls back multipart progress when completion fails", async () => {
        const filePath = path.join(os.tmpdir(), `nahida-upload-v2-${randomUUID()}.bin`);
        const size = 80 * 1024 * 1024;
        await writeFile(filePath, "");
        await truncate(filePath, size);
        mocks.plan.mockResolvedValue({
            error: null,
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
        });
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
