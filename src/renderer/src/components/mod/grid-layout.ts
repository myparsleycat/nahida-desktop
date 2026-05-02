import type { ModGridLayoutMode } from "@shared/mod";
import type { CSSProperties } from "react";

const WIDTH_MIN = 240;
const WIDTH_MAX = 640;
const COLUMN_MIN = 1;
const COLUMN_MAX = 8;
const DEFAULT_RESPONSIVE_BASE_WIDTH = 400;
const DEFAULT_FIXED_CARD_WIDTH = 360;
const DEFAULT_FIXED_COLUMN_COUNT = 4;

export interface ModGridLayoutSettings {
    mode: ModGridLayoutMode;
    responsiveBaseWidth: number;
    fixedCardWidth: number;
    fixedColumnCount: number;
}

export interface ResolvedModGridLayout {
    columnCount: number;
    gridTemplateColumns: string;
    justifyContent?: CSSProperties["justifyContent"];
}

export function clampModGridWidth(value: number, fallback: number) {
    if (!Number.isFinite(value)) {
        return fallback;
    }

    return Math.min(WIDTH_MAX, Math.max(WIDTH_MIN, Math.trunc(value)));
}

export function clampModGridColumnCount(value: number, fallback: number) {
    if (!Number.isFinite(value)) {
        return fallback;
    }

    return Math.min(COLUMN_MAX, Math.max(COLUMN_MIN, Math.trunc(value)));
}

export function normalizeModGridLayoutSettings(
    settings: Partial<ModGridLayoutSettings> | null | undefined,
): ModGridLayoutSettings {
    return {
        mode: settings?.mode ?? "responsive",
        responsiveBaseWidth: clampModGridWidth(
            settings?.responsiveBaseWidth ?? DEFAULT_RESPONSIVE_BASE_WIDTH,
            DEFAULT_RESPONSIVE_BASE_WIDTH,
        ),
        fixedCardWidth: clampModGridWidth(
            settings?.fixedCardWidth ?? DEFAULT_FIXED_CARD_WIDTH,
            DEFAULT_FIXED_CARD_WIDTH,
        ),
        fixedColumnCount: clampModGridColumnCount(
            settings?.fixedColumnCount ?? DEFAULT_FIXED_COLUMN_COUNT,
            DEFAULT_FIXED_COLUMN_COUNT,
        ),
    };
}

export function resolveModGridLayout(
    availableWidth: number,
    rawSettings: Partial<ModGridLayoutSettings> | null | undefined,
): ResolvedModGridLayout {
    const settings = normalizeModGridLayoutSettings(rawSettings);
    const normalizedWidth = Number.isFinite(availableWidth) ? availableWidth : 0;
    const safeWidth = Math.max(0, Math.trunc(normalizedWidth));

    if (settings.mode === "fixed_card_width") {
        const columnCount = Math.max(1, Math.floor(safeWidth / settings.fixedCardWidth));
        return {
            columnCount,
            gridTemplateColumns: `repeat(${columnCount}, ${settings.fixedCardWidth}px)`,
            justifyContent: "center",
        };
    }

    if (settings.mode === "fixed_column_count") {
        return {
            columnCount: settings.fixedColumnCount,
            gridTemplateColumns: `repeat(${settings.fixedColumnCount}, minmax(0, 1fr))`,
        };
    }

    const columnCount = Math.max(1, Math.floor(safeWidth / settings.responsiveBaseWidth));
    return {
        columnCount,
        gridTemplateColumns: `repeat(${columnCount}, minmax(0, 1fr))`,
    };
}
