import path from "node:path";
import type { NahidaDesktop } from "@main/index";
import { setting } from "@main/internal/db/schema";
import { resizeTextures } from "@native/mod-tools";
import type {
    TextureColorSpace,
    TextureResizeFileRunInput,
    TextureResizeListItem,
    TextureResizeOperation,
    TextureResizeRunInput,
    TextureResizeSettings,
} from "@shared/types";
import { getTextureResizeCandidates, pickTextureResizeCandidate } from "@shared/utils";
import fg from "fast-glob";
import fse from "fs-extra";
import pLimit from "p-limit";

const MODE_KEY = "texture_resize_mode";
const OPERATION_KEY = "texture_resize_operation";
const OUTPUT_FORMAT_KEY = "texture_resize_output_format";
const PERCENT_KEY = "texture_resize_percent";
const CUSTOM_WIDTH_KEY = "texture_resize_custom_width";
const CUSTOM_HEIGHT_KEY = "texture_resize_custom_height";
const BACKUP_KEY = "texture_resize_backup";
const MIN_DIMENSION = 1024;
const DIMENSION_STEP = 1024;
const DDS_MAGIC = 0x20534444;
const DDS_HEADER_SIZE = 124;
const DDS_FLAGS_OFFSET = 8;
const DDS_HEIGHT_OFFSET = 12;
const DDS_WIDTH_OFFSET = 16;
const DDS_MIPMAP_COUNT_OFFSET = 28;
const DDS_PIXEL_FORMAT_FLAGS_OFFSET = 80;
const DDS_PIXEL_FORMAT_FOUR_CC_OFFSET = 84;
const DDS_PIXEL_FORMAT_RGB_BIT_COUNT_OFFSET = 88;
const DDS_PIXEL_FORMAT_R_BIT_MASK_OFFSET = 92;
const DDS_PIXEL_FORMAT_G_BIT_MASK_OFFSET = 96;
const DDS_PIXEL_FORMAT_B_BIT_MASK_OFFSET = 100;
const DDS_PIXEL_FORMAT_A_BIT_MASK_OFFSET = 104;
const DDS_CAPS2_OFFSET = 112;
const DDS_DX10_HEADER_OFFSET = 128;
const DDS_FOUR_CC_DX10 = 0x30315844;
const DDS_FOUR_CC_DXT1 = 0x31545844;
const DDS_FOUR_CC_DXT3 = 0x33545844;
const DDS_FOUR_CC_DXT5 = 0x35545844;
const DDS_FOUR_CC_ATI1 = 0x31495441;
const DDS_FOUR_CC_ATI2 = 0x32495441;
const DDS_FOUR_CC_BC4U = 0x55344342;
const DDS_FOUR_CC_BC4S = 0x53344342;
const DDS_FOUR_CC_BC5U = 0x55354342;
const DDS_FOUR_CC_BC5S = 0x53354342;
const DDPF_FOURCC = 0x4;
const DDPF_RGB = 0x40;
const DDSCAPS2_CUBEMAP = 0x200;
const DDSD_MIPMAPCOUNT = 0x00020000;

type TextureOutputFormat = (typeof ALL_OUTPUT_FORMATS)[number];

const SRGB_OUTPUT_FORMATS = [
    "DXGI_FORMAT_R8G8B8A8_UNORM_SRGB",
    "DXGI_FORMAT_B8G8R8A8_UNORM_SRGB",
    "DXGI_FORMAT_BC1_UNORM_SRGB",
    "DXGI_FORMAT_BC2_UNORM_SRGB",
    "DXGI_FORMAT_BC3_UNORM_SRGB",
    "DXGI_FORMAT_BC7_UNORM_SRGB",
] as const;

const LINEAR_OUTPUT_FORMATS = [
    "DXGI_FORMAT_R8_UNORM",
    "DXGI_FORMAT_R8_SNORM",
    "DXGI_FORMAT_R8G8_UNORM",
    "DXGI_FORMAT_R8G8_SNORM",
    "DXGI_FORMAT_R8G8B8A8_UNORM",
    "DXGI_FORMAT_R8G8B8A8_SNORM",
    "DXGI_FORMAT_R16_UNORM",
    "DXGI_FORMAT_R16_SNORM",
    "DXGI_FORMAT_R16_FLOAT",
    "DXGI_FORMAT_R16G16_UNORM",
    "DXGI_FORMAT_R16G16_SNORM",
    "DXGI_FORMAT_R16G16_FLOAT",
    "DXGI_FORMAT_R16G16B16A16_UNORM",
    "DXGI_FORMAT_R16G16B16A16_SNORM",
    "DXGI_FORMAT_R16G16B16A16_FLOAT",
    "DXGI_FORMAT_R32_FLOAT",
    "DXGI_FORMAT_R32G32_FLOAT",
    "DXGI_FORMAT_R32G32B32_FLOAT",
    "DXGI_FORMAT_R32G32B32A32_FLOAT",
    "DXGI_FORMAT_B8G8R8A8_UNORM",
    "DXGI_FORMAT_B4G4R4A4_UNORM",
    "DXGI_FORMAT_B5G5R5A1_UNORM",
    "DXGI_FORMAT_BC1_UNORM",
    "DXGI_FORMAT_BC2_UNORM",
    "DXGI_FORMAT_BC3_UNORM",
    "DXGI_FORMAT_BC4_UNORM",
    "DXGI_FORMAT_BC4_SNORM",
    "DXGI_FORMAT_BC5_UNORM",
    "DXGI_FORMAT_BC5_SNORM",
    "DXGI_FORMAT_BC6H_UF16",
    "DXGI_FORMAT_BC6H_SF16",
    "DXGI_FORMAT_BC7_UNORM",
] as const;

const ALL_OUTPUT_FORMATS = [...SRGB_OUTPUT_FORMATS, ...LINEAR_OUTPUT_FORMATS] as const;

const DXGI_SRGB_FORMATS = new Set([
    72, // BC1_UNORM_SRGB
    75, // BC2_UNORM_SRGB
    78, // BC3_UNORM_SRGB
    29, // R8G8B8A8_UNORM_SRGB
    91, // B8G8R8A8_UNORM_SRGB
    93, // B8G8R8X8_UNORM_SRGB
    99, // BC7_UNORM_SRGB
]);

const DXGI_LINEAR_FORMATS = new Set([
    71, // BC1_UNORM
    74, // BC2_UNORM
    77, // BC3_UNORM
    80, // BC4_UNORM
    81, // BC4_SNORM
    83, // BC5_UNORM
    84, // BC5_SNORM
    95, // BC6H_UF16
    96, // BC6H_SF16
    28, // R8G8B8A8_UNORM
    87, // B8G8R8A8_UNORM
    88, // B8G8R8X8_UNORM
    98, // BC7_UNORM
]);

const DXGI_FORMAT_NAMES = new Map<number, string>([
    [0, "DXGI_FORMAT_UNKNOWN"],
    [2, "DXGI_FORMAT_R32G32B32A32_FLOAT"],
    [10, "DXGI_FORMAT_R16G16B16A16_FLOAT"],
    [24, "DXGI_FORMAT_R10G10B10A2_UNORM"],
    [28, "DXGI_FORMAT_R8G8B8A8_UNORM"],
    [29, "DXGI_FORMAT_R8G8B8A8_UNORM_SRGB"],
    [41, "DXGI_FORMAT_R32_FLOAT"],
    [49, "DXGI_FORMAT_R8G8_UNORM"],
    [54, "DXGI_FORMAT_R16_FLOAT"],
    [56, "DXGI_FORMAT_R16_UNORM"],
    [57, "DXGI_FORMAT_R16_UINT"],
    [58, "DXGI_FORMAT_R16_SNORM"],
    [60, "DXGI_FORMAT_R8G8_SNORM"],
    [61, "DXGI_FORMAT_R8_UNORM"],
    [63, "DXGI_FORMAT_R8_SNORM"],
    [71, "DXGI_FORMAT_BC1_UNORM"],
    [72, "DXGI_FORMAT_BC1_UNORM_SRGB"],
    [74, "DXGI_FORMAT_BC2_UNORM"],
    [75, "DXGI_FORMAT_BC2_UNORM_SRGB"],
    [77, "DXGI_FORMAT_BC3_UNORM"],
    [78, "DXGI_FORMAT_BC3_UNORM_SRGB"],
    [80, "DXGI_FORMAT_BC4_UNORM"],
    [81, "DXGI_FORMAT_BC4_SNORM"],
    [83, "DXGI_FORMAT_BC5_UNORM"],
    [84, "DXGI_FORMAT_BC5_SNORM"],
    [87, "DXGI_FORMAT_B8G8R8A8_UNORM"],
    [88, "DXGI_FORMAT_B8G8R8X8_UNORM"],
    [91, "DXGI_FORMAT_B8G8R8A8_UNORM_SRGB"],
    [93, "DXGI_FORMAT_B8G8R8X8_UNORM_SRGB"],
    [95, "DXGI_FORMAT_BC6H_UF16"],
    [96, "DXGI_FORMAT_BC6H_SF16"],
    [98, "DXGI_FORMAT_BC7_UNORM"],
    [99, "DXGI_FORMAT_BC7_UNORM_SRGB"],
    [115, "DXGI_FORMAT_B4G4R4A4_UNORM"],
]);

interface ParsedDDSMetadata {
    width: number;
    height: number;
    format: string;
    colorSpace: TextureColorSpace;
    layerCount: number;
}

const DEFAULT_SETTINGS: TextureResizeSettings = {
    mode: "custom",
    operation: "resize",
    percent: 50,
    customWidth: 2048,
    customHeight: 2048,
    outputFormat: "",
    backup: true,
};

export class TextureResizer {
    constructor(private readonly desktop: NahidaDesktop) {}

    public async getSettings() {
        const [mode, operation, outputFormat, percent, customWidth, customHeight, backup] =
            await Promise.all([
                this.getSettingValue(MODE_KEY),
                this.getSettingValue(OPERATION_KEY),
                this.getSettingValue(OUTPUT_FORMAT_KEY),
                this.getSettingValue(PERCENT_KEY),
                this.getSettingValue(CUSTOM_WIDTH_KEY),
                this.getSettingValue(CUSTOM_HEIGHT_KEY),
                this.getSettingValue(BACKUP_KEY),
            ]);

        return {
            mode: normalizeResizeMode(mode),
            operation: normalizeOperation(operation),
            percent: normalizePercent(percent),
            customWidth: normalizeDimension(customWidth),
            customHeight: normalizeDimension(customHeight),
            outputFormat: normalizeOutputFormat(outputFormat),
            backup: normalizeBoolean(backup, DEFAULT_SETTINGS.backup),
        };
    }

    public async saveSettings(nextSettings: Partial<TextureResizeSettings>) {
        const current = await this.getSettings();
        const merged = mergeTextureResizeSettings(current, nextSettings);

        await Promise.all([
            this.saveSettingValue(MODE_KEY, merged.mode),
            this.saveSettingValue(OPERATION_KEY, merged.operation),
            this.saveSettingValue(OUTPUT_FORMAT_KEY, merged.outputFormat),
            this.saveSettingValue(PERCENT_KEY, String(merged.percent)),
            this.saveSettingValue(CUSTOM_WIDTH_KEY, String(merged.customWidth)),
            this.saveSettingValue(CUSTOM_HEIGHT_KEY, String(merged.customHeight)),
            this.saveSettingValue(BACKUP_KEY, merged.backup ? "1" : "0"),
        ]);

        return merged;
    }

    public async listFolderTextures(targetPath: string, settings?: Partial<TextureResizeSettings>) {
        const normalizedTargetPathInput = targetPath?.trim();
        if (!normalizedTargetPathInput) {
            throw new Error("Target path is required.");
        }

        const normalizedTargetPath = path.resolve(normalizedTargetPathInput);
        const mergedSettings = settings
            ? mergeTextureResizeSettings(await this.getSettings(), settings)
            : await this.getSettings();
        const files = await resolveTargetFiles(normalizedTargetPath);
        return await buildTextureList(files, normalizedTargetPath, mergedSettings);
    }

    public async listModTextures(modPath: string, settings?: Partial<TextureResizeSettings>) {
        return await this.listFolderTextures(modPath, settings);
    }

    public async resizeFolder(input: TextureResizeRunInput) {
        const targetPath = input.targetPath?.trim();
        if (!targetPath) {
            throw new Error("Target path is required.");
        }

        const settings = await this.saveSettings(input.settings);
        return await resizeTextures(buildResizeRequest(path.resolve(targetPath), settings));
    }

    public async resizeMod(modPath: string, input: Omit<TextureResizeRunInput, "targetPath">) {
        const settings = await this.saveSettings(input.settings);
        return await resizeTextures(buildResizeRequest(path.resolve(modPath), settings));
    }

    public async resizeFile(input: TextureResizeFileRunInput) {
        const filePath = input.filePath?.trim();
        if (!filePath) {
            throw new Error("File path is required.");
        }

        const settings = await this.saveSettings(input.settings);
        return await resizeTextures(buildResizeRequest(path.resolve(filePath), settings));
    }

    private async getSettingValue(key: string) {
        const saved = await this.desktop.lib.db.query.setting.findFirst({
            where: (table, { eq }) => eq(table.key, key),
        });
        return saved?.value ?? null;
    }

    private async saveSettingValue(key: string, value: string) {
        await this.desktop.lib.db.insert(setting).values({ key, value }).onConflictDoUpdate({
            target: setting.key,
            set: { value },
        });
    }
}

async function resolveTargetFiles(targetPath: string) {
    const stat = await fse.stat(targetPath).catch(() => null);
    if (!stat) {
        throw new Error(`Target path '${targetPath}' does not exist.`);
    }

    if (stat.isFile()) {
        if (path.extname(targetPath).toLowerCase() !== ".dds") {
            throw new Error(`Target file '${targetPath}' is not a DDS texture.`);
        }
        return [targetPath];
    }

    if (!stat.isDirectory()) {
        throw new Error(`Target path '${targetPath}' must be a directory or DDS file.`);
    }

    return (
        await fg("**/*.dds", {
            cwd: targetPath,
            absolute: true,
            onlyFiles: true,
            caseSensitiveMatch: false,
        })
    ).sort((left, right) => left.localeCompare(right));
}

async function buildTextureList(
    files: string[],
    rootPath: string,
    settings: TextureResizeSettings,
) {
    const limit = pLimit(4);
    const items = await Promise.all(
        files.map((filePath) =>
            limit(async () => {
                try {
                    return await buildTextureListItem(filePath, rootPath, settings);
                } catch (error) {
                    if (shouldIgnoreTextureListError(error)) {
                        console.warn(
                            `[texture-resizer] Skipping texture with unreadable DDS header: '${filePath}'`,
                        );
                        return null;
                    }
                    throw error;
                }
            }),
        ),
    );
    return items.filter((item): item is TextureResizeListItem => item != null);
}

async function buildTextureListItem(
    filePath: string,
    rootPath: string,
    settings: TextureResizeSettings,
): Promise<TextureResizeListItem> {
    const buffer = await fse.readFile(filePath);
    const info = parseDDSMetadata(buffer);

    const originalWidth = info.width;
    const originalHeight = info.height;
    const targetSize = calculateTargetDimensions(originalWidth, originalHeight, settings);
    const previewSize = calculatePreviewDimensions(originalWidth, originalHeight, settings);
    const relativePath = toRelativePath(rootPath, filePath);
    const availableOutputFormats = resolveAvailableOutputFormats(info.colorSpace);
    const outputFormatDefault = resolveDefaultOutputFormat(
        settings.outputFormat,
        info.format,
        availableOutputFormats,
    );
    const canConvertFormat = availableOutputFormats.length > 0;
    const formatConversionMessage = resolveFormatConversionMessage(
        info.colorSpace,
        canConvertFormat,
    );
    const canProcess = targetSize != null || canConvertFormat;

    return {
        filePath,
        relativePath,
        fileName: path.basename(filePath),
        fileSize: buffer.byteLength,
        format: info.format,
        colorSpace: info.colorSpace,
        layerCount: info.layerCount,
        originalWidth,
        originalHeight,
        targetWidth: previewSize.width,
        targetHeight: previewSize.height,
        canResize: targetSize != null,
        canConvertFormat,
        canProcess,
        availableOutputFormats,
        outputFormatDefault,
        formatConversionMessage,
        message:
            targetSize == null && settings.operation !== "convert"
                ? "No valid downscale candidate matched the requested bounds."
                : null,
    };
}

function buildResizeRequest(targetPath: string, settings: TextureResizeSettings) {
    return {
        targetPath,
        mode: settings.mode,
        operation: settings.operation,
        percent: settings.percent,
        customWidth: settings.customWidth,
        customHeight: settings.customHeight,
        outputFormat: settings.outputFormat,
        backup: settings.backup,
    };
}

function mergeTextureResizeSettings(
    current: TextureResizeSettings,
    nextSettings: Partial<TextureResizeSettings>,
): TextureResizeSettings {
    return {
        mode: normalizeResizeMode(nextSettings.mode ?? current.mode),
        operation: normalizeOperation(nextSettings.operation ?? current.operation),
        percent: normalizePercent(nextSettings.percent ?? current.percent),
        customWidth: normalizeDimension(nextSettings.customWidth ?? current.customWidth),
        customHeight: normalizeDimension(nextSettings.customHeight ?? current.customHeight),
        outputFormat: normalizeOutputFormat(nextSettings.outputFormat ?? current.outputFormat),
        backup: nextSettings.backup ?? current.backup,
    };
}

function parseDDSMetadata(buffer: Buffer): ParsedDDSMetadata {
    if (buffer.byteLength < DDS_DX10_HEADER_OFFSET + 4) {
        throw new Error("DDS header could not be parsed: file is too small.");
    }

    if (buffer.readUInt32LE(0) !== DDS_MAGIC) {
        throw new Error("DDS header could not be parsed: invalid magic.");
    }

    if (buffer.readUInt32LE(4) !== DDS_HEADER_SIZE) {
        throw new Error("DDS header could not be parsed: invalid header size.");
    }

    const flags = buffer.readUInt32LE(DDS_FLAGS_OFFSET);
    const width = buffer.readUInt32LE(DDS_WIDTH_OFFSET);
    const height = buffer.readUInt32LE(DDS_HEIGHT_OFFSET);
    const mipmapCount =
        (flags & DDSD_MIPMAPCOUNT) !== 0
            ? Math.max(1, buffer.readUInt32LE(DDS_MIPMAP_COUNT_OFFSET))
            : 1;
    const caps2 = buffer.readUInt32LE(DDS_CAPS2_OFFSET);
    const layerCount = (caps2 & DDSCAPS2_CUBEMAP) !== 0 ? 6 : mipmapCount;
    const format = detectDDSFormat(buffer);
    const colorSpace = detectDDSColorSpace(buffer, format);

    if (width === 0 || height === 0) {
        throw new Error("DDS header could not be parsed: invalid dimensions.");
    }

    return {
        width,
        height,
        format,
        colorSpace,
        layerCount,
    };
}

function detectDDSFormat(buffer: Buffer) {
    const pixelFormatFlags = buffer.readUInt32LE(DDS_PIXEL_FORMAT_FLAGS_OFFSET);
    const fourCC = buffer.readUInt32LE(DDS_PIXEL_FORMAT_FOUR_CC_OFFSET);

    if ((pixelFormatFlags & DDPF_FOURCC) !== 0) {
        if (fourCC === DDS_FOUR_CC_DX10) {
            const dxgiFormat = buffer.readUInt32LE(DDS_DX10_HEADER_OFFSET);
            return DXGI_FORMAT_NAMES.get(dxgiFormat) ?? `DXGI_FORMAT_${dxgiFormat}`;
        }

        const legacyFormat = detectLegacyFourCCFormat(fourCC);
        if (legacyFormat) {
            return legacyFormat;
        }
    }

    if ((pixelFormatFlags & DDPF_RGB) !== 0) {
        const bitCount = buffer.readUInt32LE(DDS_PIXEL_FORMAT_RGB_BIT_COUNT_OFFSET);
        const rMask = buffer.readUInt32LE(DDS_PIXEL_FORMAT_R_BIT_MASK_OFFSET);
        const gMask = buffer.readUInt32LE(DDS_PIXEL_FORMAT_G_BIT_MASK_OFFSET);
        const bMask = buffer.readUInt32LE(DDS_PIXEL_FORMAT_B_BIT_MASK_OFFSET);
        const aMask = buffer.readUInt32LE(DDS_PIXEL_FORMAT_A_BIT_MASK_OFFSET);

        const rgbFormat = detectRgbMaskFormat(bitCount, rMask, gMask, bMask, aMask);
        if (rgbFormat) {
            return rgbFormat;
        }
    }

    return "UNKNOWN_DDS_FORMAT";
}

function detectDDSColorSpace(buffer: Buffer, format: string) {
    if (buffer.byteLength < DDS_DX10_HEADER_OFFSET + 4) {
        return format.endsWith("_SRGB") ? "srgb" : "unknown";
    }

    if (buffer.readUInt32LE(0) !== DDS_MAGIC) {
        return "unknown";
    }

    if (buffer.readUInt32LE(4) !== DDS_HEADER_SIZE) {
        return "unknown";
    }

    const pixelFormatFlags = buffer.readUInt32LE(DDS_PIXEL_FORMAT_FLAGS_OFFSET);
    const fourCC = buffer.readUInt32LE(DDS_PIXEL_FORMAT_FOUR_CC_OFFSET);

    if ((pixelFormatFlags & DDPF_FOURCC) === 0 || fourCC !== DDS_FOUR_CC_DX10) {
        return format.endsWith("_SRGB") ? "srgb" : "unknown";
    }

    const dxgiFormat = buffer.readUInt32LE(DDS_DX10_HEADER_OFFSET);
    if (DXGI_SRGB_FORMATS.has(dxgiFormat)) {
        return "srgb";
    }

    if (DXGI_LINEAR_FORMATS.has(dxgiFormat)) {
        return "linear";
    }

    return "unknown";
}

function detectLegacyFourCCFormat(fourCC: number) {
    switch (fourCC) {
        case DDS_FOUR_CC_DXT1:
            return "DXGI_FORMAT_BC1_UNORM";
        case DDS_FOUR_CC_DXT3:
            return "DXGI_FORMAT_BC2_UNORM";
        case DDS_FOUR_CC_DXT5:
            return "DXGI_FORMAT_BC3_UNORM";
        case DDS_FOUR_CC_ATI1:
        case DDS_FOUR_CC_BC4U:
            return "DXGI_FORMAT_BC4_UNORM";
        case DDS_FOUR_CC_BC4S:
            return "DXGI_FORMAT_BC4_SNORM";
        case DDS_FOUR_CC_ATI2:
        case DDS_FOUR_CC_BC5U:
            return "DXGI_FORMAT_BC5_UNORM";
        case DDS_FOUR_CC_BC5S:
            return "DXGI_FORMAT_BC5_SNORM";
        default:
            return null;
    }
}

function detectRgbMaskFormat(
    bitCount: number,
    rMask: number,
    gMask: number,
    bMask: number,
    aMask: number,
) {
    if (
        bitCount === 32 &&
        rMask === 0x00ff0000 &&
        gMask === 0x0000ff00 &&
        bMask === 0x000000ff &&
        aMask === 0xff000000
    ) {
        return "DXGI_FORMAT_B8G8R8A8_UNORM";
    }

    if (
        bitCount === 32 &&
        rMask === 0x000000ff &&
        gMask === 0x0000ff00 &&
        bMask === 0x00ff0000 &&
        aMask === 0xff000000
    ) {
        return "DXGI_FORMAT_R8G8B8A8_UNORM";
    }

    if (
        bitCount === 32 &&
        rMask === 0x00ff0000 &&
        gMask === 0x0000ff00 &&
        bMask === 0x000000ff &&
        aMask === 0x00000000
    ) {
        return "DXGI_FORMAT_B8G8R8X8_UNORM";
    }

    return null;
}

function resolveAvailableOutputFormats(colorSpace: TextureColorSpace) {
    if (colorSpace === "srgb") {
        return [...SRGB_OUTPUT_FORMATS];
    }

    if (colorSpace === "linear") {
        return [...LINEAR_OUTPUT_FORMATS];
    }

    return [...SRGB_OUTPUT_FORMATS, ...LINEAR_OUTPUT_FORMATS];
}

function resolveDefaultOutputFormat(
    requestedOutputFormat: string,
    currentFormat: string,
    availableOutputFormats: TextureOutputFormat[],
) {
    if (availableOutputFormats.includes(currentFormat as TextureOutputFormat)) {
        return currentFormat;
    }

    if (availableOutputFormats.includes(requestedOutputFormat as TextureOutputFormat)) {
        return requestedOutputFormat;
    }

    return availableOutputFormats[0] ?? currentFormat;
}

function resolveFormatConversionMessage(
    colorSpace: TextureColorSpace,
    canConvertFormat: boolean,
): string | null {
    if (colorSpace === "unknown" && canConvertFormat) {
        return "DDS color space could not be detected. Choose either an sRGB or Linear output format before converting.";
    }

    if (canConvertFormat) {
        return null;
    }

    if (colorSpace === "unknown") {
        return "Format conversion is unavailable because the DDS color space is unknown.";
    }

    return "No compatible output formats are available for this DDS texture.";
}

function toRelativePath(rootPath: string, filePath: string) {
    if (rootPath === filePath) {
        return path.basename(filePath);
    }

    const relativePath = path.relative(rootPath, filePath);
    return relativePath && !relativePath.startsWith("..") ? relativePath : path.basename(filePath);
}

function shouldIgnoreTextureListError(error: unknown) {
    return error instanceof Error && error.message.includes("DDS header could not be parsed");
}

function normalizeResizeMode(value?: string | null): TextureResizeSettings["mode"] {
    if (value === "percent") {
        return "percent";
    }

    return value === "custom" ? value : "custom";
}

function normalizeOperation(value?: string | null): TextureResizeOperation {
    if (value === "convert" || value === "resize_and_convert") {
        return value;
    }

    return "resize";
}

function normalizeOutputFormat(value?: string | null) {
    if (!value) {
        return "";
    }

    return ALL_OUTPUT_FORMATS.includes(value as TextureOutputFormat) ? value : "";
}

function normalizePercent(value?: string | number | null) {
    const parsed =
        typeof value === "number"
            ? value
            : typeof value === "string"
              ? Number.parseInt(value, 10)
              : Number.NaN;

    if (!Number.isFinite(parsed)) {
        return DEFAULT_SETTINGS.percent;
    }

    return Math.max(1, Math.min(99, Math.round(parsed)));
}

function normalizeDimension(value?: string | number | null) {
    const parsed =
        typeof value === "number"
            ? value
            : typeof value === "string"
              ? Number.parseInt(value, 10)
              : Number.NaN;

    if (!Number.isFinite(parsed)) {
        return DEFAULT_SETTINGS.customWidth;
    }

    const normalized = Math.max(MIN_DIMENSION, Math.round(parsed));
    const remainder = normalized % DIMENSION_STEP;

    if (remainder === 0) {
        return normalized;
    }

    return remainder >= DIMENSION_STEP / 2
        ? normalized + (DIMENSION_STEP - remainder)
        : normalized - remainder;
}

function normalizeBoolean(value: string | null | undefined, fallback: boolean) {
    if (value == null) {
        return fallback;
    }

    return value === "1" || value.toLowerCase() === "true";
}

function calculatePreviewDimensions(
    width: number,
    height: number,
    settings: TextureResizeSettings,
) {
    if (settings.operation === "convert") {
        return { width, height };
    }

    return calculateTargetDimensions(width, height, settings) ?? { width, height };
}

function calculateTargetDimensions(width: number, height: number, settings: TextureResizeSettings) {
    if (settings.operation === "convert") {
        return null;
    }

    const candidates = getTextureResizeCandidates(width, height, MIN_DIMENSION, DIMENSION_STEP);
    if (candidates.length === 0) {
        return null;
    }

    const bounds =
        settings.mode === "percent"
            ? {
                  width: Math.floor((width * settings.percent) / 100),
                  height: Math.floor((height * settings.percent) / 100),
              }
            : {
                  width: settings.customWidth,
                  height: settings.customHeight,
              };

    return pickTextureResizeCandidate(candidates, bounds.width, bounds.height);
}
