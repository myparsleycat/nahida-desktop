import { Tools } from "@bindings/tools";
import { isAbortError } from "@renderer/wails/binary-memory";
import type { TouchProfileMeshPreview, TouchProfilePreview } from "@shared/touch-profile-preview";
import { toErrorMessage } from "@shared/utils";
import { useEffect, useRef, useState } from "react";

import { loadTouchProfileMesh, loadTouchProfilePreview } from "./touch-profile-payload";

type TouchPreviewInput = {
    draftSessionId?: string;
    inspectionSessionId?: string;
    activeComponentId: string;
    selectedMeshComponentId?: string;
    phase: "select" | "review";
};

function cacheTouchTopology(
    cache: Map<string, TouchProfileMeshPreview>,
    componentId: string,
    mesh: TouchProfileMeshPreview,
) {
    cache.delete(componentId);
    cache.set(componentId, mesh);
    while (cache.size > 2) {
        const oldest = cache.keys().next().value;
        if (oldest) cache.delete(oldest);
    }
}

export function useTouchProfilePreview({
    draftSessionId,
    inspectionSessionId,
    activeComponentId,
    selectedMeshComponentId,
    phase,
}: TouchPreviewInput) {
    const [meshPreview, setMeshPreview] = useState<TouchProfileMeshPreview | null>(null);
    const [meshPreviewLoading, setMeshPreviewLoading] = useState(false);
    const [meshPreviewError, setMeshPreviewError] = useState<string | null>(null);
    const [preview, setPreview] = useState<TouchProfilePreview | null>(null);
    const [lastValidPreview, setLastValidPreview] = useState<TouchProfilePreview | null>(null);
    const [previewLoading, setPreviewLoading] = useState(false);
    const [previewError, setPreviewError] = useState<string | null>(null);
    const [previewReloadVersion, setPreviewReloadVersion] = useState(0);
    const topologyCacheRef = useRef(new Map<string, TouchProfileMeshPreview>());
    const previewActivityRef = useRef<{
        descriptor?: { cancel(): void };
        meshDescriptor?: { cancel(): void };
        controller?: AbortController;
    }>({});
    useEffect(() => {
        if (!draftSessionId || !activeComponentId) {
            return;
        }

        previewActivityRef.current.descriptor?.cancel();
        previewActivityRef.current.meshDescriptor?.cancel();
        previewActivityRef.current.controller?.abort();
        const controller = new AbortController();
        const cachedMesh = topologyCacheRef.current.get(activeComponentId);
        const meshDescriptor = cachedMesh
            ? undefined
            : Tools.TouchProfileGetMeshDescriptor({
                  sessionId: draftSessionId,
                  componentId: activeComponentId,
              });
        const descriptor = Tools.TouchProfileGetPreviewDescriptor({
            sessionId: draftSessionId,
            componentId: activeComponentId,
        });
        previewActivityRef.current = { descriptor, meshDescriptor, controller };

        const fetchPreview = async () => {
            setPreviewError(null);
            setPreviewLoading(true);
            try {
                const mesh =
                    cachedMesh ??
                    (await loadTouchProfileMesh(await meshDescriptor!, controller.signal));
                if (!cachedMesh) {
                    cacheTouchTopology(topologyCacheRef.current, activeComponentId, mesh);
                }
                const typedPreview = await loadTouchProfilePreview(
                    await descriptor,
                    mesh,
                    controller.signal,
                );
                if (controller.signal.aborted) return;
                setPreview(typedPreview);
                setLastValidPreview(typedPreview);
            } catch (error) {
                if (controller.signal.aborted || isAbortError(error)) return;
                setPreviewError(toErrorMessage(error));
            } finally {
                if (!controller.signal.aborted) setPreviewLoading(false);
            }
        };

        void fetchPreview();

        return () => {
            void descriptor.cancel();
            void meshDescriptor?.cancel();
            controller.abort();
        };
    }, [draftSessionId, activeComponentId, previewReloadVersion]);
    useEffect(() => {
        if (phase !== "select" || !inspectionSessionId || !selectedMeshComponentId) {
            return;
        }

        previewActivityRef.current.descriptor?.cancel();
        previewActivityRef.current.meshDescriptor?.cancel();
        previewActivityRef.current.controller?.abort();
        const controller = new AbortController();
        const meshDescriptor = Tools.TouchProfileGetMeshDescriptor({
            sessionId: inspectionSessionId,
            componentId: selectedMeshComponentId,
        });
        previewActivityRef.current = { meshDescriptor, controller };

        const fetchMeshPreview = async () => {
            setMeshPreviewError(null);
            setMeshPreviewLoading(true);
            try {
                const next = await loadTouchProfileMesh(await meshDescriptor, controller.signal);
                if (controller.signal.aborted) return;
                cacheTouchTopology(topologyCacheRef.current, selectedMeshComponentId, next);
                setMeshPreview(next);
            } catch (error) {
                if (controller.signal.aborted || isAbortError(error)) return;
                setMeshPreviewError(toErrorMessage(error));
            } finally {
                if (!controller.signal.aborted) setMeshPreviewLoading(false);
            }
        };

        void fetchMeshPreview();

        return () => {
            void meshDescriptor.cancel();
            controller.abort();
        };
    }, [phase, inspectionSessionId, selectedMeshComponentId]);
    return {
        meshPreview,
        meshPreviewLoading,
        meshPreviewError,
        preview,
        lastValidPreview,
        previewLoading,
        previewError,
        previewReloadVersion,
        cancelPreview: () => {
            previewActivityRef.current.descriptor?.cancel();
            previewActivityRef.current.meshDescriptor?.cancel();
            previewActivityRef.current.controller?.abort();
        },
        clearTopology: () => {
            topologyCacheRef.current.clear();
        },
        resetPreview: () => {
            setPreview(null);
            setLastValidPreview(null);
            setPreviewError(null);
        },
        resetMeshPreview: () => {
            setMeshPreview(null);
            setMeshPreviewError(null);
        },
        clearPreviewError: () => {
            setPreviewError(null);
        },
        reloadPreview: () => {
            setPreviewReloadVersion((version) => version + 1);
        },
    };
}
