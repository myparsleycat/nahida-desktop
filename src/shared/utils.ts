import { format, formatDuration, intervalToDuration } from "date-fns";
import { enUS, ko, zhCN } from "date-fns/locale";
import { isNil } from "es-toolkit";
import { type FilesizeOptions, filesize } from "filesize";

import type { TextureUpscaleScale } from "./types";

export interface TextureResizeCandidate {
    width: number;
    height: number;
}

export function toErrorMessage(error: unknown): string {
    return formatErrorMessage(error, new WeakSet()) ?? "Unknown error";
}

function formatErrorMessage(error: unknown, seen: WeakSet<object>): string | undefined {
    if (error instanceof Error) {
        const message = error.message.trim();
        return message || undefined;
    }
    if (typeof error === "string") {
        const message = error.trim();
        return message || undefined;
    }
    if (error === null || error === undefined) return undefined;

    if (typeof error === "object") {
        if (seen.has(error)) return undefined;
        seen.add(error);

        const record = error as Record<string, unknown>;
        const nestedMessage = formatErrorMessage(record.value, seen);
        if (nestedMessage) return nestedMessage;

        for (const key of ["message", "error", "detail", "title", "code"]) {
            const message = record[key];
            if (typeof message === "string" && message.trim()) return message.trim();
        }

        try {
            const serialized = JSON.stringify(error);
            if (serialized && serialized !== "{}" && serialized !== "[]") return serialized;
        } catch {
            return undefined;
        }
        return undefined;
    }

    if (typeof error === "number" || typeof error === "boolean" || typeof error === "bigint") {
        return error.toString();
    }
    if (typeof error === "symbol") return error.toString();
    return undefined;
}

export function formatSize(size?: number | null, options?: FilesizeOptions) {
    if (isNil(size)) return "0 B";
    if (!Number.isFinite(size)) return "--";
    return filesize(size, { standard: "jedec", ...options });
}

export const formatDate = (date: Date | string, lang?: string | null, formatStr?: string) => {
    return format(date, formatStr || "PPpp", {
        locale: (() => {
            if (lang?.startsWith("ko")) return ko;
            else if (lang?.startsWith("zh")) return zhCN;
            return enUS;
        })(),
    });
};

export function formatTime(seconds: number): string {
    if (!Number.isFinite(seconds) || seconds < 0) {
        return "--";
    }

    const duration = intervalToDuration({
        start: 0,
        end: Math.ceil(seconds) * 1000,
    });

    return (
        formatDuration(duration, {
            format: ["hours", "minutes", "seconds"],
            locale: ko,
        }) || "0초"
    );
}

export function normalizePath(path: string) {
    return path.replace(/\\/g, "/").replace(/^\/|\/$/g, "");
}

export function getRandInt(min: number, max: number) {
    return Math.floor(Math.random() * (max - min + 1)) + min;
}

export function getRandFloat(min: number, max: number): number {
    return Math.random() * (max - min) + min;
}

export function getTextureResizeCandidates(
    width: number,
    height: number,
    minDimension = 1024,
    dimensionStep = 1024,
): TextureResizeCandidate[] {
    if (width < minDimension || height < minDimension) {
        return [];
    }

    const divisor = gcd(width, height);
    const ratioWidth = width / divisor;
    const ratioHeight = height / divisor;

    if (ratioWidth === 0 || ratioHeight === 0) {
        return [];
    }

    const maxScale = Math.min(
        Math.floor(width / (ratioWidth * dimensionStep)),
        Math.floor(height / (ratioHeight * dimensionStep)),
    );

    if (maxScale === 0) {
        return [];
    }

    const candidates: TextureResizeCandidate[] = [];
    for (let scale = 1; scale <= maxScale; scale += 1) {
        const candidate = {
            width: ratioWidth * dimensionStep * scale,
            height: ratioHeight * dimensionStep * scale,
        };

        if (candidate.width < width || candidate.height < height) {
            candidates.push(candidate);
        }
    }

    return candidates;
}

export const TEXTURE_UPSCALE_MAX_DIMENSION = 8192;
export const TEXTURE_UPSCALE_SCALES = [2, 3, 4] as const;
export const TEXTURE_UPSCALE_MODELS = [
    "realesr-animevideov3",
    "realesrgan-x4plus-anime",
    "realesrgan-x4plus",
] as const;

const UNSUPPORTED_UPSCALE_FORMAT_MARKERS = ["BC4", "BC5", "BC6H"] as const;

export function isTextureUpscaleOperation(
    operation: string | null | undefined,
): operation is "upscale" | "upscale_and_convert" {
    return operation === "upscale" || operation === "upscale_and_convert";
}

export function isUnsupportedTextureUpscaleFormat(format: string) {
    return UNSUPPORTED_UPSCALE_FORMAT_MARKERS.some((marker) => format.includes(marker));
}

export function getAvailableTextureUpscaleScales(model: string): readonly TextureUpscaleScale[] {
    if (model === "realesr-animevideov3") {
        return TEXTURE_UPSCALE_SCALES;
    }

    return [4];
}

export function resolveTextureUpscaleScale(model: string, scale: number): TextureUpscaleScale {
    const available = getAvailableTextureUpscaleScales(model);
    if (available.includes(scale as TextureUpscaleScale)) {
        return scale as TextureUpscaleScale;
    }

    return available[available.length - 1] ?? 4;
}

export function getTextureUpscaleTarget(
    width: number,
    height: number,
    scale: number,
): TextureResizeCandidate | null {
    if (!Number.isFinite(width) || !Number.isFinite(height) || width <= 0 || height <= 0) {
        return null;
    }

    if (!TEXTURE_UPSCALE_SCALES.includes(scale as (typeof TEXTURE_UPSCALE_SCALES)[number])) {
        return null;
    }

    const targetWidth = width * scale;
    const targetHeight = height * scale;
    if (
        targetWidth > TEXTURE_UPSCALE_MAX_DIMENSION ||
        targetHeight > TEXTURE_UPSCALE_MAX_DIMENSION
    ) {
        return null;
    }

    return {
        width: targetWidth,
        height: targetHeight,
    };
}

export function pickTextureResizeCandidate(
    candidates: TextureResizeCandidate[],
    maxWidth: number,
    maxHeight: number,
): TextureResizeCandidate | null {
    let selected: TextureResizeCandidate | null = null;

    for (const candidate of candidates) {
        if (candidate.width > maxWidth || candidate.height > maxHeight) {
            continue;
        }

        if (!selected || candidate.width * candidate.height > selected.width * selected.height) {
            selected = candidate;
        }
    }

    return selected;
}

function gcd(left: number, right: number): number {
    let currentLeft = left;
    let currentRight = right;

    while (currentRight !== 0) {
        const remainder = currentLeft % currentRight;
        currentLeft = currentRight;
        currentRight = remainder;
    }

    return currentLeft;
}
