import type { ViewerDDSFormat, ViewerTextureRole } from "@shared/mod-viewer/types";
import {
    CompressedTexture,
    LinearFilter,
    LinearMipmapLinearFilter,
    Mesh,
    OrthographicCamera,
    PlaneGeometry,
    RED_GREEN_RGTC2_Format,
    RED_RGTC1_Format,
    RGBA_BPTC_Format,
    RGBA_S3TC_DXT1_Format,
    RGBA_S3TC_DXT3_Format,
    RGBA_S3TC_DXT5_Format,
    RGB_BPTC_SIGNED_Format,
    RGB_BPTC_UNSIGNED_Format,
    SIGNED_RED_GREEN_RGTC2_Format,
    SIGNED_RED_RGTC1_Format,
    ShaderMaterial,
    WebGLRenderTarget,
} from "three";
import type { ColorSpace, CompressedPixelFormat, Texture, WebGLRenderer } from "three";

const DDS_MAGIC = 0x20534444;
const DDS_HEADER_SIZE = 124;
const DDS_PIXEL_FORMAT_SIZE = 32;
const DDS_CAPS2_CUBEMAP = 0x200;
const DDS_CAPS2_VOLUME = 0x200000;
const DDS_RESOURCE_DIMENSION_TEXTURE2D = 3;
const DDS_RESOURCE_MISC_TEXTURECUBE = 0x4;
const DDS_MAX_PIXELS = 8192 * 8192;

// WebView2 reads a response body while occupying the host UI thread that every
// window shares, at about 2.4 ms per MB. Smaller responses leave gaps for input.
const DDS_RESPONSE_MAX_BYTES = 2 * 1024 * 1024;

// Mip generation targets textures large enough to alias at the viewer's scale, and
// caps the result at the size whose RGBA8 chain matches a 4096 BC7 texture in memory.
const MIPMAP_MIN_DIMENSION = 512;
const MIPMAP_MAX_DIMENSION = 2048;
const MIPMAP_MAX_ANISOTROPY = 8;

const FOURCC_DXT1 = fourCC("DXT1");
const FOURCC_DXT3 = fourCC("DXT3");
const FOURCC_DXT5 = fourCC("DXT5");
const FOURCC_DX10 = fourCC("DX10");
const FOURCC_ATI1 = fourCC("ATI1");
const FOURCC_ATI2 = fourCC("ATI2");
const FOURCC_BC4U = fourCC("BC4U");
const FOURCC_BC4S = fourCC("BC4S");
const FOURCC_BC5U = fourCC("BC5U");
const FOURCC_BC5S = fourCC("BC5S");

type DDSFormatInfo = {
    blockBytes: 8 | 16;
    threeFormat: CompressedPixelFormat;
};

type DDSHeader = {
    dataOffset: 128 | 148;
    format: ViewerDDSFormat;
    height: number;
    info: DDSFormatInfo;
    mipCount: number;
    width: number;
};

export type ModelViewerTextureCapabilities = {
    maxTextureSize: number;
    s3tc: boolean;
    s3tcSRGB: boolean;
    rgtc: boolean;
    bptc: boolean;
};

export type ParsedModelViewerDDS = {
    texture: CompressedTexture;
    format: ViewerDDSFormat;
};

export type ModelViewerDDSMetadata = {
    format: string;
    /** Uploaded as compressed DDS blocks rather than decoded to an image. */
    direct: boolean;
};

export function modelViewerDDSMetadataOf(
    texture: Texture | null | undefined,
): ModelViewerDDSMetadata | undefined {
    return texture?.userData.modelViewerDDS as ModelViewerDDSMetadata | undefined;
}

/** The DDS format a loaded texture came from; empty for other images. */
export function modelViewerDDSFormatOf(texture: Texture | null | undefined): string {
    return modelViewerDDSMetadataOf(texture)?.format ?? "";
}

export function getModelViewerTextureCapabilities(
    renderer: WebGLRenderer,
): ModelViewerTextureCapabilities {
    return {
        maxTextureSize: renderer.capabilities.maxTextureSize,
        s3tc: renderer.extensions.has("WEBGL_compressed_texture_s3tc"),
        s3tcSRGB: renderer.extensions.has("WEBGL_compressed_texture_s3tc_srgb"),
        rgtc: renderer.extensions.has("EXT_texture_compression_rgtc"),
        bptc: renderer.extensions.has("EXT_texture_compression_bptc"),
    };
}

export function canUploadModelViewerDDS(
    format: ViewerDDSFormat,
    role: ViewerTextureRole,
    capabilities: ModelViewerTextureCapabilities,
): boolean {
    if (format.startsWith("bc1-") || format.startsWith("bc2-") || format.startsWith("bc3-")) {
        return capabilities.s3tc && (role !== "diffuse" || capabilities.s3tcSRGB);
    }
    if (format.startsWith("bc4-") || format.startsWith("bc5-")) {
        return capabilities.rgtc && role !== "diffuse";
    }
    if (format.startsWith("bc6h-")) {
        return capabilities.bptc && role !== "diffuse";
    }
    return capabilities.bptc;
}

/**
 * Mods often ship DDS files without a mip chain, which alias when minified, and WebGL
 * cannot generate mips for a compressed texture. Four-channel unorm textures are
 * therefore drawn into an uncompressed render target that can have them. Other
 * formats keep their compressed upload because their shaders read the packed channels.
 */
export function createModelViewerMipmapper(renderer: WebGLRenderer): (texture: Texture) => Texture {
    const converted = new WeakMap<Texture, Texture>();

    // A restored context starts with empty render targets, while compressed
    // textures upload again from their data on their own.
    const live = new Map<Texture, () => void>();
    const redrawAll = () => {
        for (const draw of live.values()) {
            draw();
        }
    };
    return (texture) => {
        const existing = converted.get(texture);
        if (existing) {
            return existing;
        }
        const anisotropy = Math.min(
            MIPMAP_MAX_ANISOTROPY,
            renderer.capabilities.getMaxAnisotropy(),
        );
        texture.anisotropy = anisotropy;
        const format = (texture.userData.modelViewerDDS as { format?: string } | undefined)?.format;
        if (
            !(texture instanceof CompressedTexture) ||
            texture.mipmaps.length !== 1 ||
            !/^bc[1237]-unorm/.test(format ?? "")
        ) {
            return texture;
        }
        const { width, height } = texture.mipmaps[0];
        if (Math.max(width, height) <= MIPMAP_MIN_DIMENSION) {
            return texture;
        }

        const scale = Math.min(1, MIPMAP_MAX_DIMENSION / Math.max(width, height));
        const target = new WebGLRenderTarget(
            Math.max(1, Math.round(width * scale)),
            Math.max(1, Math.round(height * scale)),
            {
                anisotropy,
                colorSpace: texture.colorSpace as ColorSpace,
                depthBuffer: false,
                generateMipmaps: true,
                magFilter: LinearFilter,
                minFilter: LinearMipmapLinearFilter,
                wrapS: texture.wrapS,
                wrapT: texture.wrapT,
            },
        );
        const draw = () => {
            const quad = new Mesh(
                new PlaneGeometry(2, 2),
                new ShaderMaterial({
                    depthTest: false,
                    depthWrite: false,
                    uniforms: { source: { value: texture } },
                    vertexShader: `varying vec2 vUv;
void main() {
    vUv = uv;
    gl_Position = vec4( position.xy, 0.0, 1.0 );
}`,
                    // Written without color space conversion: an sRGB target encodes on write.
                    fragmentShader: `uniform sampler2D source;
varying vec2 vUv;
void main() {
    gl_FragColor = texture2D( source, vUv );
}`,
                }),
            );
            quad.frustumCulled = false;
            const previousTarget = renderer.getRenderTarget();
            renderer.setRenderTarget(target);
            renderer.render(quad, new OrthographicCamera());
            renderer.setRenderTarget(previousTarget);
            quad.geometry.dispose();
            quad.material.dispose();

            // Frees only the GPU copy; the mip data stays for a redraw.
            texture.dispose();
        };
        draw();

        const result = target.texture;
        result.userData = { ...texture.userData };
        result.repeat.copy(texture.repeat);
        result.offset.copy(texture.offset);
        if (live.size === 0) {
            renderer.domElement.addEventListener("webglcontextrestored", redrawAll);
        }
        live.set(result, draw);
        const disposeTarget = () => {
            result.removeEventListener("dispose", disposeTarget);
            live.delete(result);
            if (live.size === 0) {
                renderer.domElement.removeEventListener("webglcontextrestored", redrawAll);
            }
            target.dispose();
        };
        result.addEventListener("dispose", disposeTarget);
        converted.set(texture, result);
        return result;
    };
}

export function hasModelViewerDDSMipWithinLimit(
    width: number,
    height: number,
    mipCount: number,
    maxTextureSize: number,
): boolean {
    for (let mip = 0; mip < mipCount; mip++) {
        if (
            width <= maxTextureSize &&
            height <= maxTextureSize &&
            width * height <= DDS_MAX_PIXELS
        ) {
            return true;
        }
        width = Math.max(1, width >> 1);
        height = Math.max(1, height >> 1);
    }
    return false;
}

export async function fetchModelViewerDDSBuffer(
    url: string,
    expectedFormat: ViewerDDSFormat,
    maxTextureSize: number,
    signal: AbortSignal,
): Promise<ArrayBuffer> {
    const headerResponse = await fetch(url, {
        signal,
        cache: "no-store",
        headers: { Range: "bytes=0-147" },
    });
    if (!headerResponse.ok) throw new Error(`DDS request failed with ${headerResponse.status}`);
    const headerBuffer = await headerResponse.arrayBuffer();

    // A server that ignores Range has already returned the complete DDS.
    if (headerResponse.status !== 206) return headerBuffer;

    const header = parseDDSHeader(headerBuffer, expectedFormat);
    let dataOffset: number = header.dataOffset;
    let mipWidth = header.width;
    let mipHeight = header.height;
    let selectedMip = -1;
    let selectedOffset = 0;
    let selectedBytes = 0;
    for (let mip = 0; mip < header.mipCount; mip++) {
        const dataLength = ddsMipDataLength(mipWidth, mipHeight, header.info.blockBytes);
        if (
            selectedMip < 0 &&
            mipWidth <= maxTextureSize &&
            mipHeight <= maxTextureSize &&
            mipWidth * mipHeight <= DDS_MAX_PIXELS
        ) {
            selectedMip = mip;
            selectedOffset = dataOffset;
        }
        if (selectedMip >= 0) selectedBytes = checkedAdd(selectedBytes, dataLength);
        dataOffset = checkedAdd(dataOffset, dataLength);
        mipWidth = nextMipDimension(mipWidth);
        mipHeight = nextMipDimension(mipHeight);
    }
    if (selectedMip < 0 || selectedBytes === 0) {
        throw new Error("DDS has no mip within the model viewer limit");
    }

    const output = new ArrayBuffer(header.dataOffset + selectedBytes);
    const outputBytes = new Uint8Array(output);
    outputBytes.set(new Uint8Array(headerBuffer, 0, header.dataOffset));
    for (let loaded = 0; loaded < selectedBytes;) {
        const start = selectedOffset + loaded;
        const length = Math.min(selectedBytes - loaded, DDS_RESPONSE_MAX_BYTES);
        const response = await fetch(url, {
            signal,
            cache: "no-store",
            headers: { Range: `bytes=${start}-${start + length - 1}` },
        });
        if (!response.ok) throw new Error(`DDS request failed with ${response.status}`);
        const chunk = await response.arrayBuffer();
        if (response.status !== 206) return chunk;
        if (chunk.byteLength !== length) throw new Error("DDS mip range is truncated");
        outputBytes.set(new Uint8Array(chunk), header.dataOffset + loaded);
        loaded += length;
    }

    const selectedWidth = mipDimension(header.width, selectedMip);
    const selectedHeight = mipDimension(header.height, selectedMip);
    const outputView = new DataView(output);
    outputView.setUint32(12, selectedHeight, true);
    outputView.setUint32(16, selectedWidth, true);
    outputView.setUint32(28, header.mipCount - selectedMip, true);
    return output;
}

export function parseModelViewerDDS(
    buffer: ArrayBuffer,
    expectedFormat: ViewerDDSFormat | undefined,
    maxTextureSize: number,
): ParsedModelViewerDDS {
    const header = parseDDSHeader(buffer, expectedFormat);
    let dataOffset: number = header.dataOffset;
    const mipmaps: Array<{ data: Uint8Array; width: number; height: number }> = [];
    let mipWidth = header.width;
    let mipHeight = header.height;
    for (let mip = 0; mip < header.mipCount; mip++) {
        const dataLength = ddsMipDataLength(mipWidth, mipHeight, header.info.blockBytes);
        if (dataOffset + dataLength > buffer.byteLength)
            throw new Error("DDS mip data is truncated");
        if (
            mipWidth <= maxTextureSize &&
            mipHeight <= maxTextureSize &&
            mipWidth * mipHeight <= DDS_MAX_PIXELS
        ) {
            mipmaps.push({
                data: new Uint8Array(buffer, dataOffset, dataLength),
                width: mipWidth,
                height: mipHeight,
            });
        }
        dataOffset = checkedAdd(dataOffset, dataLength);
        mipWidth = nextMipDimension(mipWidth);
        mipHeight = nextMipDimension(mipHeight);
    }
    if (mipmaps.length === 0) throw new Error("DDS has no mip within the model viewer limit");

    const texture = new CompressedTexture(
        mipmaps,
        mipmaps[0].width,
        mipmaps[0].height,
        header.info.threeFormat,
    );
    texture.generateMipmaps = false;
    texture.flipY = false;
    texture.repeat.y = -1;
    texture.offset.y = 1;
    if (mipmaps.length === 1) texture.minFilter = LinearFilter;
    texture.needsUpdate = true;
    return { texture, format: header.format };
}

function parseDDSHeader(buffer: ArrayBuffer, expectedFormat?: ViewerDDSFormat): DDSHeader {
    if (buffer.byteLength < 128) throw new Error("DDS header is truncated");
    const view = new DataView(buffer);
    if (view.getUint32(0, true) !== DDS_MAGIC) throw new Error("Invalid DDS magic");
    if (view.getUint32(4, true) !== DDS_HEADER_SIZE) throw new Error("Invalid DDS header size");
    if (view.getUint32(76, true) !== DDS_PIXEL_FORMAT_SIZE) {
        throw new Error("Invalid DDS pixel format size");
    }
    const width = view.getUint32(16, true);
    const height = view.getUint32(12, true);
    const mipCount = Math.max(1, view.getUint32(28, true));
    if (!width || !height || mipCount > maxMipCount(width, height)) {
        throw new Error("Invalid DDS dimensions or mip count");
    }
    const caps2 = view.getUint32(112, true);
    if ((caps2 & (DDS_CAPS2_CUBEMAP | DDS_CAPS2_VOLUME)) !== 0) {
        throw new Error("DDS cubemap and volume textures are unsupported");
    }

    const fourcc = view.getUint32(84, true);
    let dataOffset: DDSHeader["dataOffset"] = 128;
    let format = legacyDDSFormat(fourcc);
    if (fourcc === FOURCC_DX10) {
        if (buffer.byteLength < 148) throw new Error("DDS DX10 header is truncated");
        if (
            view.getUint32(132, true) !== DDS_RESOURCE_DIMENSION_TEXTURE2D ||
            (view.getUint32(136, true) & DDS_RESOURCE_MISC_TEXTURECUBE) !== 0 ||
            view.getUint32(140, true) !== 1
        ) {
            throw new Error("DDS arrays, cubemaps, and non-2D resources are unsupported");
        }
        format = dxgiDDSFormat(view.getUint32(128, true));
        dataOffset = 148;
    }
    if (!format) throw new Error("Unsupported DDS compression format");
    if (expectedFormat && format !== expectedFormat) {
        throw new Error(`DDS format changed from ${expectedFormat} to ${format}`);
    }
    return { dataOffset, format, height, info: formatInfo(format), mipCount, width };
}

function legacyDDSFormat(value: number): ViewerDDSFormat | undefined {
    switch (value) {
        case FOURCC_DXT1:
            return "bc1-unorm";
        case FOURCC_DXT3:
            return "bc2-unorm";
        case FOURCC_DXT5:
            return "bc3-unorm";
        case FOURCC_ATI1:
        case FOURCC_BC4U:
            return "bc4-unorm";
        case FOURCC_BC4S:
            return "bc4-snorm";
        case FOURCC_ATI2:
        case FOURCC_BC5U:
            return "bc5-unorm";
        case FOURCC_BC5S:
            return "bc5-snorm";
        default:
            return undefined;
    }
}

function dxgiDDSFormat(value: number): ViewerDDSFormat | undefined {
    return {
        71: "bc1-unorm",
        72: "bc1-unorm-srgb",
        74: "bc2-unorm",
        75: "bc2-unorm-srgb",
        77: "bc3-unorm",
        78: "bc3-unorm-srgb",
        80: "bc4-unorm",
        81: "bc4-snorm",
        83: "bc5-unorm",
        84: "bc5-snorm",
        95: "bc6h-ufloat",
        96: "bc6h-sfloat",
        98: "bc7-unorm",
        99: "bc7-unorm-srgb",
    }[value] as ViewerDDSFormat | undefined;
}

function formatInfo(format: ViewerDDSFormat): DDSFormatInfo {
    if (format.startsWith("bc1-")) return { blockBytes: 8, threeFormat: RGBA_S3TC_DXT1_Format };
    if (format.startsWith("bc2-")) return { blockBytes: 16, threeFormat: RGBA_S3TC_DXT3_Format };
    if (format.startsWith("bc3-")) return { blockBytes: 16, threeFormat: RGBA_S3TC_DXT5_Format };
    if (format === "bc4-unorm") return { blockBytes: 8, threeFormat: RED_RGTC1_Format };
    if (format === "bc4-snorm") return { blockBytes: 8, threeFormat: SIGNED_RED_RGTC1_Format };
    if (format === "bc5-unorm") return { blockBytes: 16, threeFormat: RED_GREEN_RGTC2_Format };
    if (format === "bc5-snorm") {
        return { blockBytes: 16, threeFormat: SIGNED_RED_GREEN_RGTC2_Format };
    }
    if (format === "bc6h-ufloat") {
        return { blockBytes: 16, threeFormat: RGB_BPTC_UNSIGNED_Format };
    }
    if (format === "bc6h-sfloat") {
        return { blockBytes: 16, threeFormat: RGB_BPTC_SIGNED_Format };
    }
    return { blockBytes: 16, threeFormat: RGBA_BPTC_Format };
}

function ddsMipDataLength(width: number, height: number, blockBytes: 8 | 16): number {
    const length = Math.ceil(width / 4) * Math.ceil(height / 4) * blockBytes;
    if (!Number.isSafeInteger(length) || length <= 0) {
        throw new Error("DDS mip dimensions are too large");
    }
    return length;
}

function checkedAdd(left: number, right: number): number {
    const result = left + right;
    if (!Number.isSafeInteger(result)) throw new Error("DDS byte range is too large");
    return result;
}

function nextMipDimension(value: number): number {
    return Math.max(1, Math.floor(value / 2));
}

function mipDimension(value: number, mip: number): number {
    for (let index = 0; index < mip; index++) value = nextMipDimension(value);
    return value;
}

function maxMipCount(width: number, height: number): number {
    return Math.floor(Math.log2(Math.max(width, height))) + 1;
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
