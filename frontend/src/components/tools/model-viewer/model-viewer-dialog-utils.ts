import type {
    ModelViewerThreeEnvironment,
    ModelViewerThreeToneMapping,
} from "./model-viewer-contract";
import type { ModelViewerDialogSource } from "./model-viewer-dialog-types";

import {
    DEFAULT_THREE_EXPOSURE,
    MAX_THREE_EXPOSURE,
    MIN_THREE_EXPOSURE,
} from "./model-viewer-dialog-types";

export function getSourceSessionKey(source: ModelViewerDialogSource | null): string | null {
    if (!source) {
        return null;
    }

    return `payload:${source.memorySessionId}`;
}

export function clampThreeExposure(value: number): number {
    if (!Number.isFinite(value)) {
        return DEFAULT_THREE_EXPOSURE;
    }

    return Math.min(
        MAX_THREE_EXPOSURE,
        Math.max(MIN_THREE_EXPOSURE, Math.round(value * 100) / 100),
    );
}

export function normalizeThreeToneMapping(
    value: string | null | undefined,
): ModelViewerThreeToneMapping {
    return value === "aces" || value === "none" || value === "neutral" ? value : "neutral";
}

export function normalizeThreeEnvironment(
    value: string | null | undefined,
): ModelViewerThreeEnvironment {
    return value === "soft" || value === "none" || value === "studio" ? value : "studio";
}
