import { buildVertexMasks } from "@main/services/mod-tools/touch-profile-assets";
import type {
    TouchComponentAnalysis,
    TouchZoneSpec,
} from "@main/services/mod-tools/touch-profile-types";

export type TouchMaskWorkerInput = {
    vertexCount: number;
    positions: Float32Array;
    indices: Uint32Array;
    component: TouchComponentAnalysis;
    zones: TouchZoneSpec[];
};

export type TouchMaskWorkerOutput = {
    masks: Float32Array;
};

export default function (input: TouchMaskWorkerInput): TouchMaskWorkerOutput {
    const masks = buildVertexMasks(
        input.vertexCount,
        input.positions,
        input.indices,
        input.component,
        input.zones,
    );
    return { masks };
}
