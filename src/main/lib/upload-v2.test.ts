import { randomUUID } from "node:crypto";
import { rm, truncate, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";

import PQueue from "p-queue";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { NahidaDesktop } from "..";
import type { FinalFile, UploadProgress } from "./upload";

import { MAX_UPLOAD_FILE_SIZE, paginateUploadFiles, uploadDriveFilesV2 } from "./upload-v2";

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

function nteFile(id: string, extension: "pak" | "utoc" | "ucas"): FinalFile {
    return {
        ...file(id),
        path: `mod.${extension}`,
        name: `mod.${extension}`,
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

    it("keeps an NTE group together across 500-file plan pages", () => {
        const ordinary = Array.from({ length: 499 }, (_, index) => file(`ordinary-${index}`));
        const nte = [nteFile("pak", "pak"), nteFile("utoc", "utoc"), nteFile("ucas", "ucas")];

        const pages = paginateUploadFiles([...ordinary, ...nte]);

        expect(pages.map((page) => page.length)).toEqual([499, 3]);
        expect(pages[1].map((entry) => entry.FID)).toEqual(["pak", "utoc", "ucas"]);
    });

    it("rejects an NTE group that cannot fit in one plan page", () => {
        const files = Array.from({ length: 501 }, (_, index) => ({
            ...nteFile(`ucas-${index}`, "ucas"),
            name: index === 0 ? "mod.ucas" : `mod_s${index}.ucas`,
        }));

        expect(() => paginateUploadFiles(files)).toThrow("nte_bundle_too_large");
    });

    it("sends the NTE capability in every plan request", async () => {
        mocks.networkFetch.mockResolvedValue(
            sseResponse([
                {
                    event: "complete",
                    data: {
                        items: [{ clientId: "first", status: "created", itemId: "one" }],
                        uploads: [],
                        nteBundles: [],
                    },
                },
            ]),
        );

        await uploadDriveFilesV2({
            desktop,
            currentId: "current",
            requestId: "request-id",
            files: [file("first")],
            queue: new PQueue({ concurrency: 1 }),
            prepareDirectFile: async () => ({ data: Buffer.from("unused") }),
        });

        expect(
            JSON.parse(String(mocks.networkFetch.mock.calls[0][1]?.body)) as unknown,
        ).toMatchObject({ capabilities: ["nte-bundle-v1"] });
    });

    it("accepts exactly 1 GiB and rejects one byte more without allocating the file", async () => {
        const exact = { ...file("exact"), size: MAX_UPLOAD_FILE_SIZE };
        mocks.networkFetch.mockResolvedValue(
            sseResponse([
                {
                    event: "complete",
                    data: {
                        items: [{ clientId: "exact", status: "created", itemId: "one" }],
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
                files: [exact],
                queue: new PQueue({ concurrency: 1 }),
                prepareDirectFile: async () => ({ data: Buffer.from("unused") }),
            }),
        ).resolves.toBeUndefined();
        await expect(
            uploadDriveFilesV2({
                desktop,
                currentId: "current",
                requestId: "request-id",
                files: [{ ...exact, size: MAX_UPLOAD_FILE_SIZE + 1 }],
                queue: new PQueue({ concurrency: 1 }),
                prepareDirectFile: async () => ({ data: Buffer.from("unused") }),
            }),
        ).rejects.toMatchObject({ code: "upload_file_too_large" });
        expect(mocks.networkFetch).toHaveBeenCalledTimes(1);
    });

    it("stages NTE members and marks them complete only after bundle finalization", async () => {
        const utoc = nteFile("utoc", "utoc");
        const ucas = nteFile("ucas", "ucas");
        mocks.networkFetch.mockResolvedValue(
            sseResponse([
                {
                    event: "complete",
                    data: {
                        items: [
                            {
                                clientId: "utoc",
                                status: "pending",
                                intentId: "intent-utoc",
                                bundleId: "bundle",
                            },
                            {
                                clientId: "ucas",
                                status: "pending",
                                intentId: "intent-ucas",
                                bundleId: "bundle",
                            },
                        ],
                        uploads: [
                            {
                                intentId: "intent-utoc",
                                url: "https://api.nahida.live/uploads/intent-utoc",
                                method: "POST",
                                form: { token: "upload-token", sha256: "a".repeat(64) },
                            },
                        ],
                        nteBundles: [
                            {
                                id: "bundle",
                                memberClientIds: ["utoc", "ucas"],
                                completeUrl:
                                    "https://api.nahida.live/upload-bundles/bundle/complete",
                                abortUrl: "https://api.nahida.live/upload-bundles/bundle/abort",
                                form: { token: "bundle-token" },
                            },
                        ],
                    },
                },
            ]),
        );
        const order: string[] = [];
        mocks.request.mockImplementation(async () => {
            order.push("upload");
            return new Response(JSON.stringify({ status: "completed" }), { status: 200 });
        });
        mocks.post.mockImplementation(async () => {
            order.push("complete");
            return new Response(JSON.stringify({ status: "completed" }), { status: 200 });
        });

        await uploadDriveFilesV2({
            desktop,
            currentId: "current",
            requestId: "request-id",
            files: [utoc, ucas],
            queue: new PQueue({ concurrency: 2 }),
            prepareDirectFile: async () => ({ data: Buffer.from("payload") }),
            onProgress: (progress) => {
                if (progress.fileId) order.push(`ready:${progress.fileId}`);
            },
        });

        expect(order).toEqual(["upload", "complete", "ready:utoc", "ready:ucas"]);
    });

    it("aborts only the invalid NTE bundle and preserves the structured error code", async () => {
        const utoc = nteFile("utoc", "utoc");
        const ucas = nteFile("ucas", "ucas");
        mocks.networkFetch.mockResolvedValue(
            sseResponse([
                {
                    event: "complete",
                    data: {
                        items: [
                            {
                                clientId: "utoc",
                                status: "pending",
                                intentId: "dedup-utoc",
                                bundleId: "bundle",
                            },
                            {
                                clientId: "ucas",
                                status: "pending",
                                intentId: "dedup-ucas",
                                bundleId: "bundle",
                            },
                        ],
                        uploads: [],
                        nteBundles: [
                            {
                                id: "bundle",
                                memberClientIds: ["utoc", "ucas"],
                                completeUrl:
                                    "https://api.nahida.live/upload-bundles/bundle/complete",
                                abortUrl: "https://api.nahida.live/upload-bundles/bundle/abort",
                                form: { token: "bundle-token" },
                            },
                        ],
                    },
                },
            ]),
        );
        mocks.post
            .mockResolvedValueOnce(
                new Response(JSON.stringify({ code: "invalid_nte_mod_file" }), { status: 400 }),
            )
            .mockResolvedValueOnce(
                new Response(JSON.stringify({ status: "cancelled" }), { status: 200 }),
            );
        const progress: UploadProgress[] = [];

        await expect(
            uploadDriveFilesV2({
                desktop,
                currentId: "current",
                requestId: "request-id",
                files: [utoc, ucas],
                queue: new PQueue({ concurrency: 2 }),
                prepareDirectFile: async () => ({ data: Buffer.from("unused") }),
                onProgress: (event) => progress.push(event),
            }),
        ).rejects.toMatchObject({ code: "invalid_nte_mod_file" });
        expect(mocks.request).not.toHaveBeenCalled();
        expect(mocks.post).toHaveBeenCalledTimes(2);
        expect(progress.reduce((total, event) => total + event.bytes, 0)).toBe(0);
        expect(progress.every((event) => event.fileId === undefined)).toBe(true);
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

    it("notifies plan completion after plan-side progress and before intent uploads", async () => {
        const order: string[] = [];
        mocks.networkFetch.mockResolvedValue(
            sseResponse([
                {
                    event: "progress",
                    data: { phase: "processing", processed: 2, total: 2 },
                },
                {
                    event: "complete",
                    data: {
                        requestId: "request-id",
                        items: [
                            { clientId: "deduped", status: "created", itemId: "one" },
                            { clientId: "first", status: "pending", intentId: "intent" },
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
        mocks.request.mockImplementation(async () => {
            order.push("upload");
            return new Response(JSON.stringify({ status: "completed" }), { status: 200 });
        });

        await uploadDriveFilesV2({
            desktop,
            currentId: "current",
            requestId: "request-id",
            files: [file("deduped"), file("first")],
            queue: new PQueue({ concurrency: 1 }),
            prepareDirectFile: async () => ({ data: Buffer.from("payload") }),
            onPlanProgress: () => order.push("plan-progress"),
            onPlanComplete: () => order.push("plan-complete"),
            onProgress: (event) =>
                order.push(event.fileId ? `progress:${event.fileId}` : "progress:bytes"),
        });

        expect(order.indexOf("plan-progress")).toBeLessThan(order.indexOf("progress:deduped"));
        expect(order.indexOf("progress:deduped")).toBeLessThan(order.indexOf("plan-complete"));
        expect(order.indexOf("plan-complete")).toBeLessThan(order.indexOf("upload"));
        expect(order.indexOf("plan-complete")).toBeLessThan(order.indexOf("progress:bytes"));
        expect(order.indexOf("plan-complete")).toBeLessThan(order.indexOf("progress:first"));
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

    it("uploads distinct small files in one pack request", async () => {
        mocks.networkFetch.mockResolvedValue(
            sseResponse([
                {
                    event: "complete",
                    data: {
                        requestId: "request-id",
                        items: [
                            { clientId: "first", status: "pending", intentId: "intent-a" },
                            { clientId: "second", status: "pending", intentId: "intent-b" },
                        ],
                        uploads: [
                            {
                                intentId: "intent-a",
                                url: "https://api.nahida.live/akasha/v2/uploads/intent-a",
                                method: "POST",
                                form: { token: "token-a", sha256: "a".repeat(64) },
                            },
                            {
                                intentId: "intent-b",
                                url: "https://api.nahida.live/akasha/v2/uploads/intent-b",
                                method: "POST",
                                form: { token: "token-b", sha256: "b".repeat(64) },
                            },
                        ],
                    },
                },
            ]),
        );
        mocks.request.mockResolvedValue(
            new Response(
                JSON.stringify({
                    results: [
                        { intentId: "intent-a", status: "completed", fileId: "file-a" },
                        { intentId: "intent-b", status: "completed", fileId: "file-b" },
                    ],
                }),
                { status: 200 },
            ),
        );
        const progress: UploadProgress[] = [];

        await uploadDriveFilesV2({
            desktop,
            currentId: "current",
            requestId: "request-id",
            files: [
                { ...file("first"), sha256: "a".repeat(64) },
                { ...file("second"), sha256: "b".repeat(64) },
            ],
            queue: new PQueue({ concurrency: 2 }),
            prepareDirectFile: async () => ({ data: Buffer.from("payload") }),
            onProgress: (event) => progress.push(event),
        });

        expect(mocks.request).toHaveBeenCalledTimes(1);
        expect(mocks.request.mock.calls[0]?.[0]).toBe(
            "https://api.nahida.live/akasha/v2/uploads:pack",
        );
        expect(progress.filter((event) => event.fileId).map((event) => event.fileId)).toEqual([
            "first",
            "second",
        ]);
        expect(progress.reduce((sum, event) => sum + event.bytes, 0)).toBe(20);
    });

    it("keeps a file above the pack member limit on the single-intent path", async () => {
        mocks.networkFetch.mockResolvedValue(
            sseResponse([
                {
                    event: "complete",
                    data: {
                        requestId: "request-id",
                        items: [{ clientId: "big", status: "pending", intentId: "intent" }],
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

        await uploadDriveFilesV2({
            desktop,
            currentId: "current",
            requestId: "request-id",
            files: [{ ...file("big"), size: 5 * 1024 * 1024 }],
            queue: new PQueue({ concurrency: 1 }),
            prepareDirectFile: async () => ({ data: Buffer.alloc(5 * 1024 * 1024) }),
        });

        expect(mocks.request).toHaveBeenCalledTimes(1);
        expect(mocks.request.mock.calls[0]?.[0]).toBe(
            "https://api.nahida.live/akasha/v2/uploads/intent",
        );
    });

    it("keeps completed pack members and fails the rest", async () => {
        mocks.networkFetch.mockResolvedValue(
            sseResponse([
                {
                    event: "complete",
                    data: {
                        requestId: "request-id",
                        items: [
                            { clientId: "first", status: "pending", intentId: "intent-a" },
                            { clientId: "second", status: "pending", intentId: "intent-b" },
                        ],
                        uploads: [
                            {
                                intentId: "intent-a",
                                url: "https://api.nahida.live/akasha/v2/uploads/intent-a",
                                method: "POST",
                                form: { token: "token-a", sha256: "a".repeat(64) },
                            },
                            {
                                intentId: "intent-b",
                                url: "https://api.nahida.live/akasha/v2/uploads/intent-b",
                                method: "POST",
                                form: { token: "token-b", sha256: "b".repeat(64) },
                            },
                        ],
                    },
                },
            ]),
        );
        mocks.request.mockResolvedValue(
            new Response(
                JSON.stringify({
                    results: [
                        { intentId: "intent-a", status: "completed", fileId: "file-a" },
                        { intentId: "intent-b", status: "failed", reason: "sha256_mismatch" },
                    ],
                }),
                { status: 200 },
            ),
        );

        const progress: UploadProgress[] = [];
        await expect(
            uploadDriveFilesV2({
                desktop,
                currentId: "current",
                requestId: "request-id",
                files: [
                    { ...file("first"), sha256: "a".repeat(64) },
                    { ...file("second"), sha256: "b".repeat(64) },
                ],
                queue: new PQueue({ concurrency: 1 }),
                prepareDirectFile: async () => ({ data: Buffer.from("payload") }),
                onProgress: (event) => progress.push(event),
            }),
        ).rejects.toThrow("second.txt: sha256_mismatch");
        expect(progress.map((event) => event.fileId).filter(Boolean)).toEqual(["first"]);
        expect(progress.reduce((sum, event) => sum + event.bytes, 0)).toBe(10);
    });

    it("attributes reversed pack results by intentId", async () => {
        mocks.networkFetch.mockResolvedValue(
            sseResponse([
                {
                    event: "complete",
                    data: {
                        requestId: "request-id",
                        items: [
                            { clientId: "first", status: "pending", intentId: "intent-a" },
                            { clientId: "second", status: "pending", intentId: "intent-b" },
                        ],
                        uploads: [
                            {
                                intentId: "intent-a",
                                url: "https://api.nahida.live/akasha/v2/uploads/intent-a",
                                method: "POST",
                                form: { token: "token-a", sha256: "a".repeat(64) },
                            },
                            {
                                intentId: "intent-b",
                                url: "https://api.nahida.live/akasha/v2/uploads/intent-b",
                                method: "POST",
                                form: { token: "token-b", sha256: "b".repeat(64) },
                            },
                        ],
                    },
                },
            ]),
        );
        mocks.request.mockResolvedValue(
            new Response(
                JSON.stringify({
                    results: [
                        { intentId: "intent-b", status: "failed", reason: "sha256_mismatch" },
                        { intentId: "intent-a", status: "completed", fileId: "file-a" },
                    ],
                }),
                { status: 200 },
            ),
        );
        const progress: UploadProgress[] = [];

        await expect(
            uploadDriveFilesV2({
                desktop,
                currentId: "current",
                requestId: "request-id",
                files: [
                    { ...file("first"), sha256: "a".repeat(64) },
                    { ...file("second"), sha256: "b".repeat(64) },
                ],
                queue: new PQueue({ concurrency: 1 }),
                prepareDirectFile: async () => ({ data: Buffer.from("payload") }),
                onProgress: (event) => progress.push(event),
            }),
        ).rejects.toThrow("second.txt: sha256_mismatch");
        expect(progress.map((event) => event.fileId).filter(Boolean)).toEqual(["first"]);
        expect(progress.reduce((sum, event) => sum + event.bytes, 0)).toBe(10);
    });

    it("propagates cancellation after the final pack flush", async () => {
        const requestStarted = Promise.withResolvers<void>();
        const requestFinished = Promise.withResolvers<Response>();
        mocks.networkFetch.mockResolvedValue(
            sseResponse([
                {
                    event: "complete",
                    data: {
                        requestId: "request-id",
                        items: [
                            { clientId: "first", status: "pending", intentId: "intent-a" },
                            { clientId: "second", status: "pending", intentId: "intent-b" },
                        ],
                        uploads: [
                            {
                                intentId: "intent-a",
                                url: "https://api.nahida.live/akasha/v2/uploads/intent-a",
                                method: "POST",
                                form: { token: "token-a", sha256: "a".repeat(64) },
                            },
                            {
                                intentId: "intent-b",
                                url: "https://api.nahida.live/akasha/v2/uploads/intent-b",
                                method: "POST",
                                form: { token: "token-b", sha256: "b".repeat(64) },
                            },
                        ],
                    },
                },
            ]),
        );
        mocks.request.mockImplementation(() => {
            requestStarted.resolve();
            return requestFinished.promise;
        });
        const controller = new AbortController();
        const upload = uploadDriveFilesV2({
            desktop,
            currentId: "current",
            requestId: "request-id",
            files: [
                { ...file("first"), sha256: "a".repeat(64) },
                { ...file("second"), sha256: "b".repeat(64) },
            ],
            queue: new PQueue({ concurrency: 1 }),
            prepareDirectFile: async () => ({ data: Buffer.from("payload") }),
            signal: controller.signal,
        });
        await requestStarted.promise;
        controller.abort();
        requestFinished.resolve(
            new Response(
                JSON.stringify({
                    results: [
                        { intentId: "intent-a", status: "completed", fileId: "file-a" },
                        { intentId: "intent-b", status: "completed", fileId: "file-b" },
                    ],
                }),
                { status: 200 },
            ),
        );
        await expect(upload).rejects.toMatchObject({ name: "AbortError" });
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
