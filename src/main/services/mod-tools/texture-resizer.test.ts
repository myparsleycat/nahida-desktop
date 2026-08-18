import path from "node:path";

import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@native/mod-tools", () => ({
    decodeDdsToRgba8: vi.fn(),
    encodeRgba8ToDds: vi.fn(),
    resizeTextures: vi.fn(),
}));

vi.mock("./realesrgan-runtime", () => ({
    RealesrganRuntime: class {},
}));

import type { NahidaDesktop } from "@main/index";
import { resizeTextures } from "@native/mod-tools";
import type { TextureResizeSettings } from "@shared/types";

import {
    TextureResizer,
    mergeTextureResizeSettings,
    resolveUpscaleOutputFormat,
    resolveUpscaleSkipReason,
} from "./texture-resizer";

const resizeTexturesMock = vi.mocked(resizeTextures);

const DEFAULT_RUN_SETTINGS: TextureResizeSettings = {
    mode: "custom",
    operation: "resize",
    percent: 50,
    customWidth: 2048,
    customHeight: 2048,
    outputFormat: "",
    backup: true,
    upscaleScale: 2,
    upscaleModel: "realesr-animevideov3",
};

function deferred<T>() {
    let resolve!: (value: T) => void;
    let reject!: (reason?: unknown) => void;
    const promise = new Promise<T>((resolvePromise, rejectPromise) => {
        resolve = resolvePromise;
        reject = rejectPromise;
    });
    return { promise, resolve, reject };
}

function resizeResult(targetPath: string): Awaited<ReturnType<typeof resizeTextures>> {
    return {
        targetPath,
        processed: 1,
        updated: 1,
        skipped: 0,
        failed: 0,
        files: [],
    };
}

function createResizer() {
    const broadcast = vi.fn();
    const resizer = new TextureResizer({
        lib: {
            db: {
                settings: {
                    getValue: vi.fn(async () => null),
                    upsert: vi.fn(async () => {}),
                },
            },
        },
        ipc: { broadcast },
        logger: { info: vi.fn(), warn: vi.fn(), error: vi.fn() },
    } as unknown as NahidaDesktop);
    return { resizer, broadcast };
}

describe("texture resizer settings", () => {
    it("merges upscale settings and clamps x4plus models to 4x", () => {
        const merged = mergeTextureResizeSettings(
            {
                mode: "custom",
                operation: "resize",
                percent: 50,
                customWidth: 2048,
                customHeight: 2048,
                outputFormat: "",
                backup: true,
                upscaleScale: 2,
                upscaleModel: "realesr-animevideov3",
            },
            {
                operation: "upscale",
                upscaleModel: "realesrgan-x4plus-anime",
                upscaleScale: 2,
            },
        );

        expect(merged.operation).toBe("upscale");
        expect(merged.upscaleModel).toBe("realesrgan-x4plus-anime");
        expect(merged.upscaleScale).toBe(4);
    });

    it("skips cubemaps, BC4/5/6, and oversized results", () => {
        expect(
            resolveUpscaleSkipReason(
                {
                    width: 1024,
                    height: 1024,
                    format: "DXGI_FORMAT_BC7_UNORM_SRGB",
                    layerCount: 6,
                },
                2,
            ),
        ).toContain("Cubemap");
        expect(
            resolveUpscaleSkipReason(
                {
                    width: 1024,
                    height: 1024,
                    format: "DXGI_FORMAT_BC5_UNORM",
                    layerCount: 1,
                },
                2,
            ),
        ).toContain("format");
        expect(
            resolveUpscaleSkipReason(
                {
                    width: 4096,
                    height: 4096,
                    format: "DXGI_FORMAT_BC7_UNORM_SRGB",
                    layerCount: 1,
                },
                3,
            ),
        ).toContain("8192");
        expect(
            resolveUpscaleSkipReason(
                {
                    width: 1024,
                    height: 1024,
                    format: "DXGI_FORMAT_BC7_UNORM_SRGB",
                    layerCount: 1,
                },
                2,
            ),
        ).toBeNull();
    });

    it("re-encodes upscale-only files with the decoded format", () => {
        expect(
            resolveUpscaleOutputFormat(
                "upscale",
                "DXGI_FORMAT_BC7_UNORM_SRGB",
                "DXGI_FORMAT_BC1_UNORM",
                "linear",
            ),
        ).toBe("DXGI_FORMAT_BC1_UNORM");
        expect(
            resolveUpscaleOutputFormat(
                "upscale_and_convert",
                "DXGI_FORMAT_BC7_UNORM_SRGB",
                "DXGI_FORMAT_BC1_UNORM",
                "srgb",
            ),
        ).toBe("DXGI_FORMAT_BC7_UNORM_SRGB");
        expect(
            resolveUpscaleOutputFormat("upscale", "", "DXGI_FORMAT_B8G8R8X8_UNORM", "linear"),
        ).toBeNull();
        expect(
            resolveUpscaleOutputFormat(
                "upscale_and_convert",
                "DXGI_FORMAT_BC7_UNORM",
                "DXGI_FORMAT_BC1_UNORM_SRGB",
                "srgb",
            ),
        ).toBe("DXGI_FORMAT_BC1_UNORM_SRGB");
        expect(
            resolveUpscaleOutputFormat(
                "upscale_and_convert",
                "DXGI_FORMAT_BC7_UNORM",
                "DXGI_FORMAT_BC1_UNORM",
                "srgb",
            ),
        ).toBeNull();
    });
});

describe("texture resizer progress ownership", () => {
    beforeEach(() => {
        resizeTexturesMock.mockReset();
    });

    it("keeps running state when a later resizeFile finishes first", async () => {
        const { resizer, broadcast } = createResizer();
        const first = deferred<Awaited<ReturnType<typeof resizeTextures>>>();
        const second = deferred<Awaited<ReturnType<typeof resizeTextures>>>();
        const firstPath = "/tmp/first.dds";
        const secondPath = "/tmp/second.dds";

        resizeTexturesMock.mockImplementation(async (request) => {
            if (request.targetPath === path.resolve(firstPath)) {
                return await first.promise;
            }
            return await second.promise;
        });

        const firstJob = resizer.resizeFile({
            filePath: firstPath,
            settings: DEFAULT_RUN_SETTINGS,
        });
        await vi.waitFor(() => {
            expect(resizer.getState()).toMatchObject({
                status: "running",
                fileName: "first.dds",
            });
        });

        const secondJob = resizer.resizeFile({
            filePath: secondPath,
            settings: DEFAULT_RUN_SETTINGS,
        });
        await vi.waitFor(() => {
            expect(resizer.getState()).toMatchObject({
                status: "running",
                fileName: "second.dds",
            });
        });

        second.resolve(resizeResult(path.resolve(secondPath)));
        await secondJob;

        expect(resizer.getState()).toMatchObject({
            status: "running",
            fileName: "first.dds",
        });
        expect(broadcast).not.toHaveBeenCalledWith(
            "tools:textureResizeProgress",
            expect.objectContaining({ status: "completed", fileName: "second.dds" }),
        );
        expect(broadcast).not.toHaveBeenCalledWith(
            "tools:textureResizeProgress",
            expect.objectContaining({ status: "idle" }),
        );

        first.resolve(resizeResult(path.resolve(firstPath)));
        await firstJob;

        expect(broadcast).toHaveBeenCalledWith(
            "tools:textureResizeProgress",
            expect.objectContaining({ status: "completed", fileName: "first.dds" }),
        );
        expect(resizer.getState()).toEqual({ status: "idle" });
    });

    it("does not idle after a failed resize while another job is in flight", async () => {
        const { resizer, broadcast } = createResizer();
        const first = deferred<Awaited<ReturnType<typeof resizeTextures>>>();
        const second = deferred<Awaited<ReturnType<typeof resizeTextures>>>();
        const firstPath = "/tmp/first.dds";
        const secondPath = "/tmp/second.dds";

        resizeTexturesMock.mockImplementation(async (request) => {
            if (request.targetPath === path.resolve(firstPath)) {
                return await first.promise;
            }
            return await second.promise;
        });

        const firstJob = resizer.resizeFile({
            filePath: firstPath,
            settings: DEFAULT_RUN_SETTINGS,
        });
        await vi.waitFor(() => {
            expect(resizer.getState()).toMatchObject({ status: "running", fileName: "first.dds" });
        });

        const secondJob = resizer.resizeFile({
            filePath: secondPath,
            settings: DEFAULT_RUN_SETTINGS,
        });
        await vi.waitFor(() => {
            expect(resizer.getState()).toMatchObject({ status: "running", fileName: "second.dds" });
        });

        second.reject(new Error("boom"));
        await expect(secondJob).rejects.toThrow("boom");

        expect(resizer.getState()).toMatchObject({
            status: "running",
            fileName: "first.dds",
        });
        expect(broadcast).not.toHaveBeenCalledWith(
            "tools:textureResizeProgress",
            expect.objectContaining({ status: "failed" }),
        );

        first.resolve(resizeResult(path.resolve(firstPath)));
        await firstJob;
        expect(resizer.getState()).toEqual({ status: "idle" });
    });

    it("keeps folder resize activity when a file resize finishes first", async () => {
        const { resizer } = createResizer();
        const folder = deferred<Awaited<ReturnType<typeof resizeTextures>>>();
        const file = deferred<Awaited<ReturnType<typeof resizeTextures>>>();
        const folderPath = "/tmp/mod-folder";
        const filePath = "/tmp/dialog.dds";

        resizeTexturesMock.mockImplementation(async (request) => {
            if (request.targetPath === path.resolve(folderPath)) {
                return await folder.promise;
            }
            return await file.promise;
        });

        const folderJob = resizer.resizeFolder({
            targetPath: folderPath,
            settings: DEFAULT_RUN_SETTINGS,
        });
        await vi.waitFor(() => {
            expect(resizer.getState()).toMatchObject({
                status: "running",
                fileName: "mod-folder",
            });
        });

        const fileJob = resizer.resizeFile({ filePath, settings: DEFAULT_RUN_SETTINGS });
        await vi.waitFor(() => {
            expect(resizer.getState()).toMatchObject({ status: "running", fileName: "dialog.dds" });
        });

        file.resolve(resizeResult(path.resolve(filePath)));
        await fileJob;

        expect(resizer.getState()).toMatchObject({
            status: "running",
            fileName: "mod-folder",
        });

        folder.resolve(resizeResult(path.resolve(folderPath)));
        await folderJob;
        expect(resizer.getState()).toEqual({ status: "idle" });
    });
});
