import type {
    TouchMeshPreview as TouchMeshPreviewPayload,
    TouchProfilePreview as TouchProfilePreviewPayload,
} from "@bindings/tools";
import {
    toBodyShapeBytes,
    toFloat32Array,
    toUint32Array,
} from "@renderer/components/tools/body-shape/body-shape-payload";
import type {
    TouchProfileMeshPreview,
    TouchProfilePreview,
    TouchProfileZoneSource,
} from "@shared/touch-profile-preview";
import type { TouchZoneSettings } from "@shared/touch-profile-settings";

export function toTouchProfileMeshPreview(
    payload: TouchMeshPreviewPayload,
): TouchProfileMeshPreview {
    return {
        ...payload,
        positions: toFloat32Array(payload.positions ?? []),
        indices: toUint32Array(payload.indices) ?? new Uint32Array(),
        bones: payload.bones ?? [],
        blendStride: payload.blendStride ?? undefined,
        blendBytes: toBodyShapeBytes(payload.blendBytes),
    };
}

export function toTouchProfilePreview(payload: TouchProfilePreviewPayload): TouchProfilePreview {
    return {
        ...payload,
        positions: toFloat32Array(payload.positions ?? []),
        indices: toUint32Array(payload.indices) ?? new Uint32Array(),
        zones: (payload.zones ?? []).map((zone) => ({
            ...zone,
            center: toVector3(zone.center),
            radius: toVector3(zone.radius),
            source: zone.source as TouchProfileZoneSource,
            settings: zone.settings as TouchZoneSettings,
            weights: toFloat32Array(zone.weights ?? []),
        })),
    };
}

function toVector3(value: number[]): [number, number, number] {
    if (value.length !== 3) throw new Error("Invalid vector payload from backend");
    return [value[0]!, value[1]!, value[2]!];
}
