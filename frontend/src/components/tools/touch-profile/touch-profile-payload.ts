import type { TouchMeshDescriptor, TouchProfilePreviewDescriptor } from "@bindings/tools";
import { fetchBinaryBytes, fetchFloat32, fetchUint32 } from "@renderer/wails/binary-memory";
import type {
    TouchProfileMeshPreview,
    TouchProfilePreview,
    TouchProfileZoneSource,
} from "@shared/touch-profile-preview";
import type { TouchZoneSettings } from "@shared/touch-profile-settings";

export async function loadTouchProfileMesh(
    payload: TouchMeshDescriptor,
    signal?: AbortSignal,
): Promise<TouchProfileMeshPreview> {
    const [positions, indices, blendBytes] = await Promise.all([
        fetchFloat32(payload.positionsUrl, payload.positionsCount, signal),
        payload.indicesUrl
            ? fetchUint32(payload.indicesUrl, payload.indexCount, signal)
            : Promise.resolve(new Uint32Array()),
        payload.blendUrl
            ? fetchBinaryBytes(payload.blendUrl, payload.blendBytes, signal)
            : Promise.resolve(undefined),
    ]);
    return {
        sessionId: payload.sessionId,
        componentId: payload.componentId,
        vertexCount: payload.vertexCount,
        positions,
        indices,
        bones: payload.bones ?? [],
        blendStride: payload.blendStride ?? undefined,
        blendBytes,
    };
}

export async function loadTouchProfilePreview(
    payload: TouchProfilePreviewDescriptor,
    mesh: TouchProfileMeshPreview,
    signal?: AbortSignal,
): Promise<TouchProfilePreview> {
    if (mesh.sessionId !== payload.sessionId || mesh.componentId !== payload.componentId) {
        throw new Error("Touch preview descriptor does not match the loaded topology");
    }
    const weights = await fetchFloat32(payload.weightsUrl, payload.weightsCount, signal);
    return {
        sessionId: payload.sessionId,
        componentId: payload.componentId,
        vertexCount: payload.vertexCount,
        positions: mesh.positions,
        indices: mesh.indices,
        zones: (payload.zones ?? []).map((zone) => ({
            ...zone,
            center: toVector3(zone.center),
            radius: toVector3(zone.radius),
            source: zone.source as TouchProfileZoneSource,
            settings: zone.settings as TouchZoneSettings,
            weights: weights.subarray(zone.weightOffset, zone.weightOffset + payload.vertexCount),
        })),
    };
}

function toVector3(value: number[]): [number, number, number] {
    if (value.length !== 3) throw new Error("Invalid vector payload from backend");
    return [value[0]!, value[1]!, value[2]!];
}
