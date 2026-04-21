import path from "node:path";
import { pipeline } from "node:stream/promises";
import { analyzePng, invertRgbaAlpha, parseDdsSrgbState as parseDdsSrgbStateNative } from "@native/static-glb";
import fse from "fs-extra";
import { PNG } from "pngjs";
import sharp from "sharp";

export type StaticGlbTextureFormat = "png" | "jpeg-safe" | "jpeg-force";

export type PreparedTexture = {
    imagePath: string;
    mimeType: "image/png" | "image/jpeg";
    alphaMode?: "MASK";
    alphaCutoff?: number;
    usesAlpha: boolean;
    invertedAlpha: boolean;
    selectionScore: number;
    srgbConfidence: "srgb" | "linear" | "unknown";
};

export type TextureSelectionAnalysis = {
    isLikelyFlatColor: boolean;
    isLikelySrgb: boolean | null;
    isLikelyNormalMap: boolean;
    srgbConfidence: "srgb" | "linear" | "unknown";
};

const DDS_SRGB_DXGI_FORMATS = new Set([29, 72, 75, 78, 91, 93, 99]);
const DDS_LINEAR_DXGI_FORMATS = new Set([28, 71, 74, 77, 80, 83, 87, 88, 95, 98]);
const ddsSrgbStateCache = new Map<string, Promise<boolean | null>>();
const MAX_DDS_SRGB_CACHE = 1024;
const pngAnalysisCache = new WeakMap<PNG, ReturnType<typeof analyzePng>>();

export async function analyzeTextureSelection(
    texturePath: string,
    resourceName: string,
    png: PNG,
    normalizeKey: (value: string) => string,
): Promise<TextureSelectionAnalysis> {
    const color = getPngAnalysis(png);
    const nameKey = normalizeKey(resourceName);
    const isLikelySrgb = await readDdsSrgbState(texturePath);
    const isLikelyNormalMap =
        color.meanB >= 0.7 &&
        Math.abs(color.meanR - 0.5) <= 0.18 &&
        Math.abs(color.meanG - 0.5) <= 0.18 &&
        color.blueDominance >= 0.12 &&
        color.channelRangeMax <= 72 &&
        color.luminanceStdDev <= 0.12;

    return {
        isLikelyFlatColor:
            color.channelRangeMax <= 12 ||
            (color.luminanceStdDev <= 0.035 &&
                color.channelRangeMax <= 24 &&
                !nameKey.includes("shadow")),
        isLikelySrgb,
        isLikelyNormalMap,
        srgbConfidence: classifySrgbConfidence(isLikelySrgb),
    };
}

export async function readDdsSrgbState(texturePath: string): Promise<boolean | null> {
    if (path.extname(texturePath).toLowerCase() !== ".dds") {
        return null;
    }

    const cached = ddsSrgbStateCache.get(texturePath);
    if (cached) {
        return cached;
    }

    const pending = (async () => {
        try {
            const header = await fse.readFile(texturePath);
            return parseDdsSrgbState(header.subarray(0, 148));
        } catch {
            return null;
        }
    })();

    if (ddsSrgbStateCache.size >= MAX_DDS_SRGB_CACHE) {
        ddsSrgbStateCache.clear();
    }
    ddsSrgbStateCache.set(texturePath, pending);
    return pending;
}

export function analyzeAlpha(png: PNG): {
    hasAlpha: boolean;
    lowRatio: number;
    highRatio: number;
    partialRatio: number;
    lowAlphaRgbMean: number;
} {
    const analysis = getPngAnalysis(png);
    return {
        hasAlpha: analysis.hasAlpha,
        lowRatio: analysis.lowRatio,
        highRatio: analysis.highRatio,
        partialRatio: analysis.partialRatio,
        lowAlphaRgbMean: analysis.lowAlphaRgbMean,
    };
}

export function materialAlphaMode(
    alpha: ReturnType<typeof analyzeAlpha>,
): Pick<PreparedTexture, "alphaMode" | "alphaCutoff"> {
    if (isCutoutAlpha(alpha)) {
        return { alphaMode: "MASK", alphaCutoff: 0.5 };
    }

    return {};
}

export function textureUsesAlpha(alpha: ReturnType<typeof analyzeAlpha>): boolean {
    if (isCutoutAlpha(alpha)) {
        return true;
    }

    return alpha.partialRatio > 0 || alpha.lowRatio >= 0.005;
}

export function resolveTextureMimeType(
    format: StaticGlbTextureFormat,
    usesAlpha: boolean | null,
): PreparedTexture["mimeType"] {
    if (format === "png") {
        return "image/png";
    }

    if (format === "jpeg-force") {
        return "image/jpeg";
    }

    if (usesAlpha === null) {
        return "image/png";
    }

    return usesAlpha ? "image/png" : "image/jpeg";
}

export function shouldInvertAlpha(
    resourceName: string,
    texturePath: string,
    alpha: ReturnType<typeof analyzeAlpha>,
    normalizeKey: (value: string) => string,
): boolean {
    const key = normalizeKey(`${resourceName} ${path.basename(texturePath)}`);
    if (key.includes("invertalpha") || key.includes("alphainvert")) {
        return true;
    }

    return alpha.lowRatio >= 0.95 && alpha.highRatio <= 0.03 && alpha.lowAlphaRgbMean >= 8;
}

export function invertPngAlpha(png: PNG): void {
    const next = invertRgbaAlpha(Buffer.from(png.data));
    next.copy(png.data);
    pngAnalysisCache.delete(png);
}

export async function readPngAsync(pngPath: string): Promise<PNG> {
    const buffer = await fse.readFile(pngPath);
    return await new Promise((resolve, reject) => {
        new PNG().parse(buffer, (error, png) => {
            if (error || !png) {
                reject(error ?? new Error(`Failed to parse PNG: ${pngPath}`));
                return;
            }

            resolve(png);
        });
    });
}

export async function writePngAsync(png: PNG, pngPath: string): Promise<void> {
    await pipeline(png.pack(), fse.createWriteStream(pngPath));
}

export async function writeJpegAsync(
    png: PNG,
    jpegPath: string,
    quality: number,
): Promise<void> {
    await sharp(png.data, {
        raw: {
            width: png.width,
            height: png.height,
            channels: 4,
        },
    })
        .jpeg({
            quality,
            chromaSubsampling: "4:4:4",
        })
        .toFile(jpegPath);
}

export function scoreTextureSelection(
    resourceName: string,
    analysis: TextureSelectionAnalysis,
    normalizeKey: (value: string) => string,
): number {
    let score = textureNamePriority(resourceName, normalizeKey);
    if (analysis.srgbConfidence === "srgb") score += 120;
    if (analysis.srgbConfidence === "linear") score -= 120;
    if (analysis.isLikelyNormalMap) score -= 120;
    if (analysis.isLikelyFlatColor) score -= 80;
    else score += 20;
    return score;
}

export function classifySrgbConfidence(
    isLikelySrgb: boolean | null,
): "srgb" | "linear" | "unknown" {
    if (isLikelySrgb === true) return "srgb";
    if (isLikelySrgb === false) return "linear";
    return "unknown";
}

export function textureNamePriority(
    resourceName: string,
    normalizeKey: (value: string) => string,
): number {
    const key = normalizeKey(resourceName);
    let score = 0;

    if (key.includes("basecolor") || key.includes("albedo")) score += 80;
    if (key.includes("diffuse")) score += 60;
    if (key.includes("color")) score += 25;
    if (key.includes("shadow")) score -= 20;
    if (key.includes("lightmap")) score -= 12;
    if (key.includes("light")) score -= 10;
    if (key.includes("metal") || key.includes("rough") || key.includes("ao")) score -= 24;
    if (key.includes("mask")) score -= 28;
    if (key.includes("normal") || key.includes("bump")) score -= 60;

    return score;
}

function parseDdsSrgbState(bytes: Buffer): boolean | null {
    const nativeValue = parseDdsSrgbStateNative(bytes);
    if (nativeValue !== null) {
        return nativeValue;
    }

    if (bytes.length < 148 || bytes.toString("ascii", 0, 4) !== "DDS ") {
        return null;
    }

    const fourCC = bytes.toString("ascii", 84, 88);
    if (fourCC !== "DX10") {
        return null;
    }

    const dxgiFormat = bytes.readUInt32LE(128);
    return DDS_SRGB_DXGI_FORMATS.has(dxgiFormat)
        ? true
        : DDS_LINEAR_DXGI_FORMATS.has(dxgiFormat)
          ? false
          : null;
}

function isCutoutAlpha(alpha: ReturnType<typeof analyzeAlpha>): boolean {
    return alpha.lowRatio >= 0.005 && alpha.highRatio >= 0.5 && alpha.partialRatio <= 0.02;
}

function getPngAnalysis(png: PNG) {
    const cached = pngAnalysisCache.get(png);
    if (cached) {
        return cached;
    }

    const analysis = analyzePng(Buffer.from(png.data), png.width, png.height);
    pngAnalysisCache.set(png, analysis);
    return analysis;
}
