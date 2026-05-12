import type {
    ModelViewerRealtimeShapeKey,
    ModelViewerThreeEnvironment,
    ModelViewerThreeToneMapping,
} from "./model-viewer-contract";
import type {
    ModelViewerDialogSource,
    ModelViewerVariantManifest,
    VariableStateValue,
} from "./model-viewer-dialog-types";
import {
    DEFAULT_THREE_EXPOSURE,
    MAX_THREE_EXPOSURE,
    MIN_THREE_EXPOSURE,
} from "./model-viewer-dialog-types";

export function withCacheBuster(url: string): string {
    const separator = url.includes("?") ? "&" : "?";
    return `${url}${separator}v=${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;
}

export function createStateKey(state: Record<string, VariableStateValue>): string {
    return Object.entries(state)
        .sort(([left], [right]) => left.localeCompare(right))
        .map(([key, value]) => `${key.toLowerCase()}=${String(value)}`)
        .join("&");
}

export function getSourceSessionKey(source: ModelViewerDialogSource | null): string | null {
    if (!source) {
        return null;
    }

    if (source.mode === "single") {
        return `single:${source.glbPath}`;
    }

    return `variant:${source.manifestPath}`;
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

export function stripRealtimeShapeKeyState(
    state: Record<string, VariableStateValue>,
    shapeKeys?: ModelViewerRealtimeShapeKey[],
): Record<string, VariableStateValue> {
    if (!shapeKeys?.length) {
        return state;
    }

    const stripped = { ...state };
    for (const shapeKey of shapeKeys) {
        for (const dimension of shapeKey.dimensions) {
            delete stripped[dimension.variableId];
        }
    }
    return stripped;
}

export function normalizeRealtimeShapeKeyState(
    state: Record<string, VariableStateValue>,
    variables: ModelViewerVariantManifest["variables"],
    shapeKeys?: ModelViewerRealtimeShapeKey[],
): Record<string, VariableStateValue> {
    if (!shapeKeys?.length) {
        return state;
    }

    const normalized = { ...state };
    const realtimeVariableIds = new Set(
        shapeKeys.flatMap((shapeKey) =>
            shapeKey.dimensions.map((dimension) => dimension.variableId),
        ),
    );

    for (const variable of variables) {
        if (!realtimeVariableIds.has(variable.id) || !variable.slider) {
            continue;
        }

        const rawValue = normalized[variable.id];
        if (typeof rawValue !== "number") {
            continue;
        }

        const range = variable.slider.max - variable.slider.min;
        if (range <= 0) {
            normalized[variable.id] = 0.5;
            continue;
        }

        normalized[variable.id] = Math.min(
            1,
            Math.max(0, (rawValue - variable.slider.min) / range),
        );
    }

    return normalized;
}
