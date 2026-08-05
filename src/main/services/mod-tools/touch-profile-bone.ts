import { extractBoneWeights, type BlendBoneInfo } from "@shared/body-shape";
import type { TouchBoneZoneSelection } from "@shared/touch-profile-preview";
import { createDefaultTouchZoneSettings } from "@shared/touch-profile-settings";

import type {
    TouchComponentAnalysis,
    TouchComponentDraft,
    TouchZoneSpec,
} from "./touch-profile-types";

export const DEFAULT_BONE_WEIGHT_THRESHOLD = 0.01;
export const BONE_WEIGHT_THRESHOLD_RANGE = { min: 0.005, max: 0.5, step: 0.005 } as const;

export type BoneAnalysisInput = {
    component: TouchComponentAnalysis;
    positions: Float32Array;
    indices: Uint32Array;
    blendBytes: Uint8Array;
    blendStride: number;
    bones: BlendBoneInfo[];
    selections: TouchBoneZoneSelection[];
    weightThreshold: number;
    objectId: number;
};

export function analyzeComponentWithBones(input: BoneAnalysisInput): TouchComponentDraft {
    const { component, positions, blendBytes, blendStride, selections, weightThreshold, objectId } =
        input;
    const vertexCount = component.vertexCount;

    if (component.supportGrade === "C") {
        return {
            componentId: component.id,
            interactive: false,
            objectId,
            zones: [],
            confidence: 0,
            warnings: ["Component support grade is C (unsupported mesh layout)"],
        };
    }

    if (!blendBytes || blendStride === undefined || blendBytes.byteLength === 0) {
        return {
            componentId: component.id,
            interactive: false,
            objectId,
            zones: [],
            confidence: 0,
            warnings: ["Component has no blend buffer for bone-based selection"],
        };
    }

    const zones: TouchZoneSpec[] = [];
    const warnings: string[] = [];

    for (const selection of selections) {
        if (selection.channel === null) continue;
        const boneWeights = extractBoneWeights(
            blendBytes,
            selection.boneId,
            vertexCount,
            blendStride,
        );
        const seedVertices: number[] = [];
        for (let vertex = 0; vertex < vertexCount; vertex++) {
            if (boneWeights[vertex] >= weightThreshold) seedVertices.push(vertex);
        }

        const minSelected = Math.min(12, Math.max(3, Math.floor(vertexCount * 0.01)));
        if (seedVertices.length < minSelected) {
            warnings.push(
                `Bone ${selection.boneId}: only ${seedVertices.length} vertices above threshold ${weightThreshold} (need ${minSelected})`,
            );
            continue;
        }

        const center: [number, number, number] = [0, 0, 0];
        for (const vertex of seedVertices) {
            center[0] += positions[vertex * 3];
            center[1] += positions[vertex * 3 + 1];
            center[2] += positions[vertex * 3 + 2];
        }
        center[0] /= seedVertices.length;
        center[1] /= seedVertices.length;
        center[2] /= seedVertices.length;

        let maxDx = 0;
        let maxDy = 0;
        let maxDz = 0;
        for (const vertex of seedVertices) {
            maxDx = Math.max(maxDx, Math.abs(positions[vertex * 3] - center[0]));
            maxDy = Math.max(maxDy, Math.abs(positions[vertex * 3 + 1] - center[1]));
            maxDz = Math.max(maxDz, Math.abs(positions[vertex * 3 + 2] - center[2]));
        }

        const radiusScale = 1.05;
        const zoneId = `bone_${selection.boneId}_ch${selection.channel}`;
        const zoneLabel = selection.label ?? `Bone ${selection.boneId}`;

        zones.push({
            id: zoneId,
            label: zoneLabel,
            channel: selection.channel,
            confidence: 1,
            center,
            radius: [
                Math.max(maxDx * radiusScale, 0.02),
                Math.max(maxDy * radiusScale, 0.02),
                Math.max(maxDz * radiusScale, 0.02),
            ],
            source: "bone",
            settings: createDefaultTouchZoneSettings(),
            seedVertices,
        });
    }

    if (zones.length === 0) {
        return {
            componentId: component.id,
            interactive: false,
            objectId,
            zones: [],
            confidence: 0,
            warnings: warnings.length > 0 ? warnings : ["No zones produced from bone selection"],
        };
    }

    return {
        componentId: component.id,
        interactive: true,
        objectId,
        zones,
        confidence: 1,
        warnings,
    };
}
