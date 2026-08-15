import { describe, expect, it } from "vitest";

import {
    getAvailableTextureUpscaleScales,
    getTextureUpscaleTarget,
    isTextureUpscaleOperation,
    isUnsupportedTextureUpscaleFormat,
    resolveTextureUpscaleScale,
} from "./utils";

describe("texture upscale helpers", () => {
    it("identifies upscale operations", () => {
        expect(isTextureUpscaleOperation("upscale")).toBe(true);
        expect(isTextureUpscaleOperation("upscale_and_convert")).toBe(true);
        expect(isTextureUpscaleOperation("resize")).toBe(false);
    });

    it("rejects unsupported channel formats", () => {
        expect(isUnsupportedTextureUpscaleFormat("DXGI_FORMAT_BC4_UNORM")).toBe(true);
        expect(isUnsupportedTextureUpscaleFormat("DXGI_FORMAT_BC5_SNORM")).toBe(true);
        expect(isUnsupportedTextureUpscaleFormat("DXGI_FORMAT_BC6H_UF16")).toBe(true);
        expect(isUnsupportedTextureUpscaleFormat("DXGI_FORMAT_BC7_UNORM_SRGB")).toBe(false);
    });

    it("computes upscale targets and rejects oversized results", () => {
        expect(getTextureUpscaleTarget(1024, 1024, 2)).toEqual({ width: 2048, height: 2048 });
        expect(getTextureUpscaleTarget(4096, 2048, 3)).toBeNull();
        expect(getTextureUpscaleTarget(2048, 2048, 5)).toBeNull();
    });

    it("limits x4plus models to 4x", () => {
        expect(getAvailableTextureUpscaleScales("realesr-animevideov3")).toEqual([2, 3, 4]);
        expect(getAvailableTextureUpscaleScales("realesrgan-x4plus-anime")).toEqual([4]);
        expect(resolveTextureUpscaleScale("realesrgan-x4plus", 2)).toBe(4);
        expect(resolveTextureUpscaleScale("realesr-animevideov3", 3)).toBe(3);
    });
});
