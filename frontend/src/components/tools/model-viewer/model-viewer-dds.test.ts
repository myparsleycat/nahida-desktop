import { RED_GREEN_RGTC2_Format, RGBA_S3TC_DXT1_Format, SIGNED_RED_RGTC1_Format } from "three";
import { describe, expect, it } from "vitest";

import {
    canUploadModelViewerDDS,
    hasModelViewerDDSMipWithinLimit,
    parseModelViewerDDS,
} from "./model-viewer-dds";

const maxTextureSize = 8192;

describe("parseModelViewerDDS", () => {
    it("parses a legacy BC1 texture without decoding its blocks", () => {
        const result = parseModelViewerDDS(
            ddsFixture({ fourcc: "DXT1", width: 4, height: 4 }),
            undefined,
            maxTextureSize,
        );

        expect(result.format).toBe("bc1-unorm");
        expect(result.texture.format).toBe(RGBA_S3TC_DXT1_Format);
        expect(result.texture.mipmaps).toHaveLength(1);
        expect(result.texture.mipmaps[0]).toMatchObject({ width: 4, height: 4 });
        expect(result.texture.flipY).toBe(false);
        expect(result.texture.repeat.y).toBe(-1);
        expect(result.texture.offset.y).toBe(1);
    });

    it("parses DX10 BC5 and signed BC4 formats", () => {
        const bc5 = parseModelViewerDDS(
            ddsFixture({ dxgi: 83, width: 8, height: 8 }),
            undefined,
            maxTextureSize,
        );
        const bc4 = parseModelViewerDDS(
            ddsFixture({ dxgi: 81, width: 4, height: 4 }),
            undefined,
            maxTextureSize,
        );

        expect(bc5.format).toBe("bc5-unorm");
        expect(bc5.texture.format).toBe(RED_GREEN_RGTC2_Format);
        expect(bc4.format).toBe("bc4-snorm");
        expect(bc4.texture.format).toBe(SIGNED_RED_RGTC1_Format);
    });

    it("keeps mips within the 64MP limit", () => {
        const result = parseModelViewerDDS(
            ddsFixture({ fourcc: "DXT1", width: 4096, height: 2048, mipCount: 3 }),
            undefined,
            maxTextureSize,
        );

        expect(result.texture.image).toMatchObject({ width: 4096, height: 2048 });
        expect(result.texture.mipmaps.map(({ width, height }) => [width, height])).toEqual([
            [4096, 2048],
            [2048, 1024],
            [1024, 512],
        ]);
    });

    it("discards mips larger than the renderer texture-size limit", () => {
        const result = parseModelViewerDDS(
            ddsFixture({ fourcc: "DXT1", width: 16384, height: 4, mipCount: 2 }),
            undefined,
            maxTextureSize,
        );

        expect(result.texture.mipmaps.map(({ width, height }) => [width, height])).toEqual([
            [8192, 2],
        ]);
    });

    it("rejects truncated data", () => {
        const truncated = ddsFixture({ fourcc: "DXT5", width: 8, height: 8 }).slice(0, -1);
        expect(() => parseModelViewerDDS(truncated, undefined, maxTextureSize)).toThrow(
            "truncated",
        );
    });

    it("rejects arrays, cubemaps, and metadata mismatches", () => {
        expect(() =>
            parseModelViewerDDS(
                ddsFixture({ dxgi: 98, width: 4, height: 4, arraySize: 2 }),
                undefined,
                maxTextureSize,
            ),
        ).toThrow("arrays");
        expect(() =>
            parseModelViewerDDS(
                ddsFixture({ fourcc: "DXT1", width: 4, height: 4, caps2: 0x200 }),
                undefined,
                maxTextureSize,
            ),
        ).toThrow("cubemap");
        expect(() =>
            parseModelViewerDDS(
                ddsFixture({ fourcc: "DXT1", width: 4, height: 4 }),
                "bc3-unorm",
                maxTextureSize,
            ),
        ).toThrow("format changed");
    });
});

describe("DDS capability selection", () => {
    const all = { maxTextureSize, s3tc: true, s3tcSRGB: true, rgtc: true, bptc: true };

    it("requires the matching WebGL extension family", () => {
        expect(canUploadModelViewerDDS("bc1-unorm", "diffuse", all)).toBe(true);
        expect(canUploadModelViewerDDS("bc5-unorm", "normal_map", all)).toBe(true);
        expect(canUploadModelViewerDDS("bc7-unorm", "diffuse", all)).toBe(true);
        expect(canUploadModelViewerDDS("bc5-unorm", "normal_map", { ...all, rgtc: false })).toBe(
            false,
        );
        expect(canUploadModelViewerDDS("bc7-unorm", "diffuse", { ...all, bptc: false })).toBe(
            false,
        );
    });

    it("falls back when compressed single-channel data cannot provide sRGB diffuse sampling", () => {
        expect(canUploadModelViewerDDS("bc1-unorm", "diffuse", { ...all, s3tcSRGB: false })).toBe(
            false,
        );
        expect(canUploadModelViewerDDS("bc4-unorm", "diffuse", all)).toBe(false);
        expect(canUploadModelViewerDDS("bc6h-ufloat", "diffuse", all)).toBe(false);
    });

    it("preflights the mip limit from transport metadata", () => {
        expect(hasModelViewerDDSMipWithinLimit(8192, 8192, 1, maxTextureSize)).toBe(true);
        expect(hasModelViewerDDSMipWithinLimit(16384, 8192, 1, maxTextureSize)).toBe(false);
        expect(hasModelViewerDDSMipWithinLimit(16384, 8192, 2, maxTextureSize)).toBe(true);
        expect(hasModelViewerDDSMipWithinLimit(16384, 4, 1, maxTextureSize)).toBe(false);
        expect(hasModelViewerDDSMipWithinLimit(16384, 4, 2, maxTextureSize)).toBe(true);
    });
});

function ddsFixture({
    fourcc,
    dxgi,
    width,
    height,
    mipCount = 1,
    arraySize = 1,
    caps2 = 0,
}: {
    fourcc?: string;
    dxgi?: number;
    width: number;
    height: number;
    mipCount?: number;
    arraySize?: number;
    caps2?: number;
}): ArrayBuffer {
    const dx10 = dxgi !== undefined;
    const blockBytes =
        fourcc === "DXT1" || dxgi === 71 || dxgi === 72 || dxgi === 80 || dxgi === 81 ? 8 : 16;
    let payloadBytes = 0;
    let mipWidth = width;
    let mipHeight = height;
    for (let mip = 0; mip < mipCount; mip++) {
        payloadBytes += Math.ceil(mipWidth / 4) * Math.ceil(mipHeight / 4) * blockBytes;
        mipWidth = Math.max(1, mipWidth >> 1);
        mipHeight = Math.max(1, mipHeight >> 1);
    }
    const buffer = new ArrayBuffer((dx10 ? 148 : 128) + payloadBytes);
    const view = new DataView(buffer);
    view.setUint32(0, 0x20534444, true);
    view.setUint32(4, 124, true);
    view.setUint32(12, height, true);
    view.setUint32(16, width, true);
    view.setUint32(28, mipCount, true);
    view.setUint32(76, 32, true);
    view.setUint32(84, fourCC(dx10 ? "DX10" : (fourcc ?? "DXT5")), true);
    view.setUint32(112, caps2, true);
    if (dx10) {
        view.setUint32(128, dxgi, true);
        view.setUint32(132, 3, true);
        view.setUint32(140, arraySize, true);
    }
    return buffer;
}

function fourCC(value: string): number {
    return (
        (value.charCodeAt(0) |
            (value.charCodeAt(1) << 8) |
            (value.charCodeAt(2) << 16) |
            (value.charCodeAt(3) << 24)) >>>
        0
    );
}
