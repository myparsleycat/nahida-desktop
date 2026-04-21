import path from "node:path";
import { pipeline } from "node:stream/promises";
import fse from "fs-extra";
import { PNG } from "pngjs";

export type PreparedTexture = {
    pngPath: string;
    alphaMode?: "MASK";
    alphaCutoff?: number;
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

export async function analyzeTextureSelection(
    texturePath: string,
    resourceName: string,
    png: PNG,
    normalizeKey: (value: string) => string,
): Promise<TextureSelectionAnalysis> {
    const color = analyzeTextureColor(png);
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

    try {
        const header = await fse.readFile(texturePath);
        return parseDdsSrgbState(header.subarray(0, 148));
    } catch {
        return null;
    }
}

export function analyzeAlpha(png: PNG): {
    hasAlpha: boolean;
    lowRatio: number;
    highRatio: number;
    partialRatio: number;
    lowAlphaRgbMean: number;
} {
    const pixelCount = png.width * png.height;
    let low = 0;
    let high = 0;
    let partial = 0;
    let lowAlphaRgbTotal = 0;

    for (let offset = 0; offset < png.data.length; offset += 4) {
        const alpha = png.data[offset + 3];
        if (alpha <= 16) {
            low++;
            lowAlphaRgbTotal +=
                (png.data[offset] + png.data[offset + 1] + png.data[offset + 2]) / 3;
        } else if (alpha >= 239) {
            high++;
        } else {
            partial++;
        }
    }

    return {
        hasAlpha: low > 0 || partial > 0,
        lowRatio: pixelCount > 0 ? low / pixelCount : 0,
        highRatio: pixelCount > 0 ? high / pixelCount : 0,
        partialRatio: pixelCount > 0 ? partial / pixelCount : 0,
        lowAlphaRgbMean: low > 0 ? lowAlphaRgbTotal / low : 0,
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
    for (let offset = 3; offset < png.data.length; offset += 4) {
        png.data[offset] = 255 - png.data[offset];
    }
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

function analyzeTextureColor(png: PNG): {
    channelRangeMax: number;
    luminanceStdDev: number;
    meanR: number;
    meanG: number;
    meanB: number;
    blueDominance: number;
} {
    let minR = 255;
    let minG = 255;
    let minB = 255;
    let maxR = 0;
    let maxG = 0;
    let maxB = 0;
    let count = 0;
    let sumR = 0;
    let sumG = 0;
    let sumB = 0;
    let luminanceSum = 0;
    let luminanceSquareSum = 0;
    const stride = Math.max(1, Math.floor(Math.sqrt((png.width * png.height) / 4096)));

    for (let y = 0; y < png.height; y += stride) {
        for (let x = 0; x < png.width; x += stride) {
            const offset = (y * png.width + x) * 4;
            const r = png.data[offset];
            const g = png.data[offset + 1];
            const b = png.data[offset + 2];
            minR = Math.min(minR, r);
            minG = Math.min(minG, g);
            minB = Math.min(minB, b);
            maxR = Math.max(maxR, r);
            maxG = Math.max(maxG, g);
            maxB = Math.max(maxB, b);
            sumR += r;
            sumG += g;
            sumB += b;
            const luminance = (0.2126 * r + 0.7152 * g + 0.0722 * b) / 255;
            luminanceSum += luminance;
            luminanceSquareSum += luminance * luminance;
            count++;
        }
    }

    const mean = count > 0 ? luminanceSum / count : 0;
    const variance = count > 0 ? Math.max(0, luminanceSquareSum / count - mean * mean) : 0;
    const meanR = count > 0 ? sumR / count / 255 : 0;
    const meanG = count > 0 ? sumG / count / 255 : 0;
    const meanB = count > 0 ? sumB / count / 255 : 0;
    return {
        channelRangeMax: Math.max(maxR - minR, maxG - minG, maxB - minB),
        luminanceStdDev: Math.sqrt(variance),
        meanR,
        meanG,
        meanB,
        blueDominance: meanB - Math.max(meanR, meanG),
    };
}

function parseDdsSrgbState(bytes: Buffer): boolean | null {
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
