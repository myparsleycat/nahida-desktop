import { describe, expect, it, vi } from "vitest";

vi.mock("@native/mod-tools", () => ({
    decodeDdsToRgba8: vi.fn(),
    encodeRgba8ToDds: vi.fn(),
    resizeTextures: vi.fn(),
}));

vi.mock("./realesrgan-runtime", () => ({
    RealesrganRuntime: class {},
}));

import {
    mergeTextureResizeSettings,
    resolveUpscaleOutputFormat,
    resolveUpscaleSkipReason,
} from "./texture-resizer";

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
