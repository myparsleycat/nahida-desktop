import { type BodyShapeViewportHandle } from "@renderer/components/tools/body-shape/body-shape-viewport";
import {
    formatOrientation,
    parseOrientation,
} from "@renderer/components/tools/model-viewer/model-viewer-contract";
import { DEFAULT_MODEL_ORIENTATION } from "@renderer/components/tools/model-viewer/model-viewer-dialog-types";
import {
    computeBoundingCenter,
    computeRegionPivot,
    extractBoneWeights,
    type ActiveRegionDeform,
} from "@shared/body-shape";
import { useRef, useState } from "react";

import type { TouchProfileSession } from "./use-touch-profile-session";

export function useTouchProfileControls({
    meshPreview,
    weightThreshold,
}: Pick<TouchProfileSession, "meshPreview" | "weightThreshold">) {
    const viewportRef = useRef<BodyShapeViewportHandle | null>(null);
    const bonePreviewRef = useRef<string | null>(null);
    const boneHoverRef = useRef<number | null>(null);
    const channelSelectBoneRef = useRef<number | null>(null);
    const [modelOrientation, setModelOrientation] = useState(DEFAULT_MODEL_ORIENTATION);
    const handleBoneHighlight = (boneId: number | null) => {
        bonePreviewRef.current = boneId !== null ? `bone:${boneId}` : null;
        if (!meshPreview || !meshPreview.blendBytes || meshPreview.blendStride === undefined) {
            viewportRef.current?.updateColors([]);
            return;
        }
        if (boneId === null) {
            viewportRef.current?.updateColors([]);
            return;
        }
        const weights = extractBoneWeights(
            meshPreview.blendBytes,
            boneId,
            meshPreview.vertexCount,
            meshPreview.blendStride,
        );
        for (let i = 0; i < weights.length; i++) {
            if (weights[i] < weightThreshold[0] || weights[i] > weightThreshold[1]) weights[i] = 0;
        }
        const boundsCenter = computeBoundingCenter(meshPreview.positions);
        const regions: ActiveRegionDeform[] = [
            {
                id: `bone:${boneId}`,
                weights,
                amount: 1,
                axisScale: [1, 1, 1],
                pivot: computeRegionPivot(meshPreview.positions, weights, boundsCenter),
            },
        ];
        viewportRef.current?.updateColors(regions);
    };
    const syncAssignmentBonePreview = () => {
        handleBoneHighlight(channelSelectBoneRef.current ?? boneHoverRef.current);
    };
    const rotateModel = (delta: [number, number, number]) => {
        setModelOrientation((current) => {
            const [roll, pitch, yaw] = parseOrientation(current);
            return formatOrientation([roll + delta[0], pitch + delta[1], yaw + delta[2]]);
        });
    };
    const resetView = () => {
        setModelOrientation(DEFAULT_MODEL_ORIENTATION);
        viewportRef.current?.resetCamera();
    };
    const resetSelectionPreview = () => {
        bonePreviewRef.current = null;
        boneHoverRef.current = null;
        channelSelectBoneRef.current = null;
        viewportRef.current?.updateColors([]);
    };
    return {
        viewportRef,
        bonePreviewRef,
        boneHoverRef,
        channelSelectBoneRef,
        modelOrientation,
        handleBoneHighlight,
        syncAssignmentBonePreview,
        rotateModel,
        resetView,
        resetSelectionPreview,
    };
}
export type TouchProfileControls = ReturnType<typeof useTouchProfileControls>;
