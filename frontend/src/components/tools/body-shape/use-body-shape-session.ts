import { Tools, type BodyShapeMeshDescriptor } from "@bindings/tools";
import type { BodyShapeMeshSummary } from "@bindings/tools/body_shape";
import { Logger } from "@renderer/lib/logger";
import { isAbortError } from "@renderer/wails/binary-memory";
import {
    DEFAULT_BLEND_STRIDE,
    createGeodesicBrushWorkspace,
    type BlendBoneInfo,
    type VertexAdjacency,
    type GeodesicBrushWorkspace,
} from "@shared/body-shape";
import { toErrorMessage } from "@shared/utils";
import { useLayoutEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

import { BodyShapeMeshWorkerClient } from "./body-shape-mesh-loader";

const MAX_CACHED_SELECTIONS = 8;

export type LoadedMeshSegment = {
    meshId: string;
    name: string;
    vertexOffset: number;
    vertexCount: number;
    blendBytes?: Uint8Array;
    blendStride: number;
    /** Blend indices are local to this buffer, so bones are never merged across segments. */
    bones: BlendBoneInfo[];
};

export type LoadedMesh = {
    id: string;
    meshIds: string[];
    vertexCount: number;
    originalPositions: Float32Array;
    previewPositions: Float32Array;
    indices?: Uint32Array;
    segments: LoadedMeshSegment[];
    weightCache: Map<string, Float32Array>;
    selectionWeights: Float32Array;
    adjacency?: VertexAdjacency;
    originalNormals?: Float32Array;
    componentIds?: Uint32Array;
    symmetryMap?: Int32Array;
    boundsCenter: [number, number, number];
    geodesicWorkspace: GeodesicBrushWorkspace;
};

type LoadResult = {
    sessionId: string;
    modRoot: string;
    iniPath: string;
    meshes: BodyShapeMeshSummary[];
    cache: Map<string, LoadedMesh>;
};

export function meshSelectionKey(meshIds: readonly string[]): string {
    return meshIds.join("\n");
}

async function loadBodyShapeMesh(
    worker: BodyShapeMeshWorkerClient,
    sessionId: string,
    summaries: BodyShapeMeshSummary[],
    descriptors: BodyShapeMeshDescriptor[],
    signal: AbortSignal,
): Promise<LoadedMesh> {
    if (
        descriptors.length !== summaries.length ||
        descriptors.some(
            (descriptor, index) =>
                descriptor.sessionId !== sessionId || descriptor.meshId !== summaries[index].id,
        )
    ) {
        throw new Error("Body shape mesh descriptor does not match the active session");
    }
    const result = await worker.process(
        {
            sessionId,
            meshes: summaries.map((summary, index) => ({
                meshId: summary.id,
                vertexCount: summary.vertexCount,
                positionsUrl: descriptors[index].positionsUrl,
                positionsCount: descriptors[index].positionsCount,
                indicesUrl: descriptors[index].indicesUrl ?? undefined,
                indexCount: descriptors[index].indexCount,
                blendUrl: descriptors[index].blendUrl ?? undefined,
                blendBytes: descriptors[index].blendBytes,
                blendStride: descriptors[index].blendStride ?? DEFAULT_BLEND_STRIDE,
            })),
        },
        signal,
    );
    const originalPositions = new Float32Array(result.originalPositions);
    const vertexCount = originalPositions.length / 3;
    const adjacency =
        result.adjacencyOffsets && result.adjacencyNeighbors
            ? {
                  offsets: new Uint32Array(result.adjacencyOffsets),
                  neighbors: new Uint32Array(result.adjacencyNeighbors),
              }
            : undefined;
    const meshIds = summaries.map((summary) => summary.id);
    return {
        id: meshSelectionKey(meshIds),
        meshIds,
        vertexCount,
        originalPositions,
        previewPositions: new Float32Array(originalPositions),
        indices: result.indices ? new Uint32Array(result.indices) : undefined,
        segments: result.segments.map((segment, index) => ({
            ...segment,
            name: summaries[index].name,
            blendBytes: segment.blendBytes ? new Uint8Array(segment.blendBytes) : undefined,
            bones: descriptors[index].bones ?? [],
        })),
        weightCache: new Map(),
        selectionWeights: new Float32Array(vertexCount),
        adjacency,
        originalNormals: result.originalNormals
            ? new Float32Array(result.originalNormals)
            : undefined,
        componentIds: result.componentIds ? new Uint32Array(result.componentIds) : undefined,
        symmetryMap: result.symmetryMap ? new Int32Array(result.symmetryMap) : undefined,
        boundsCenter: result.boundingCenter,
        geodesicWorkspace: createGeodesicBrushWorkspace(vertexCount),
    };
}

export function useBodyShapeSession(fixedTargetPath: string | undefined, onLoaded: () => void) {
    const { t } = useTranslation();
    const [loaded, setLoaded] = useState<LoadResult | null>(null);
    const [selectedMeshIds, setSelectedMeshIds] = useState<string[]>([]);
    const [loading, setLoading] = useState(Boolean(fixedTargetPath));
    const activeSessionRef = useRef<string | null>(null);
    const generationRef = useRef(0);
    const workerRef = useRef<BodyShapeMeshWorkerClient | null>(null);
    const meshRef = useRef<{
        generation: number;
        requests?: { cancel(): void }[];
        controller?: AbortController;
    }>({ generation: 0 });
    const onLoadedRef = useRef(onLoaded);
    useLayoutEffect(() => {
        onLoadedRef.current = onLoaded;
    });
    const cancelMesh = () => {
        for (const request of meshRef.current.requests ?? []) request.cancel();
        meshRef.current.controller?.abort();
        meshRef.current = { generation: meshRef.current.generation + 1 };
    };
    const closeSession = async (id: string) => {
        await Tools.BodyShapeCloseSession(id).catch((error: unknown) => {
            Logger.capture("body-shape:close-session", error);
        });
    };
    const loadMeshes = async (current: LoadResult, meshIds: readonly string[]) => {
        if (activeSessionRef.current !== current.sessionId) return;
        cancelMesh();
        const generation = meshRef.current.generation;
        const summaries = current.meshes.filter((mesh) => meshIds.includes(mesh.id));
        const ids = summaries.map((summary) => summary.id);
        const key = meshSelectionKey(ids);
        if (current.cache.has(key)) {
            setSelectedMeshIds(ids);
            setLoading(false);
            return;
        }
        const worker = workerRef.current;
        if (summaries.length === 0 || !worker) return;
        const controller = new AbortController();
        const requests = ids.map((meshId) =>
            Tools.BodyShapeGetMesh({ sessionId: current.sessionId, meshId }),
        );
        meshRef.current = { generation, controller, requests };
        setLoading(true);
        try {
            const descriptors = await Promise.all(requests);
            if (generation !== meshRef.current.generation) return;
            const mesh = await loadBodyShapeMesh(
                worker,
                current.sessionId,
                summaries,
                descriptors,
                controller.signal,
            );
            if (
                generation !== meshRef.current.generation ||
                activeSessionRef.current !== current.sessionId
            )
                return;
            if (current.cache.size >= MAX_CACHED_SELECTIONS) {
                const oldest = current.cache.keys().next().value;
                if (oldest !== undefined) current.cache.delete(oldest);
            }
            current.cache.set(key, mesh);
            setSelectedMeshIds(ids);
            setLoaded({ ...current });
        } catch (error) {
            if (generation !== meshRef.current.generation || isAbortError(error)) return;
            Logger.capture("body-shape:load-mesh", error);
            throw error;
        } finally {
            if (generation === meshRef.current.generation) {
                meshRef.current = { generation };
                setLoading(false);
            }
        }
    };
    const loadMod = async (path: string) => {
        if (!path || !workerRef.current) return;
        const generation = ++generationRef.current;
        cancelMesh();
        const previous = activeSessionRef.current;
        activeSessionRef.current = null;
        setLoading(true);
        setLoaded(null);
        try {
            if (previous) await closeSession(previous);
            if (generation !== generationRef.current) return;
            const result = await Tools.BodyShapeLoadMod(path);
            if (generation !== generationRef.current) {
                await closeSession(result.sessionId);
                return;
            }
            activeSessionRef.current = result.sessionId;
            const next: LoadResult = {
                sessionId: result.sessionId,
                modRoot: result.modRoot,
                iniPath: result.iniPath,
                meshes: result.meshes ?? [],
                cache: new Map(),
            };
            const initial = next.meshes[0] ? [next.meshes[0].id] : [];
            setLoaded(next);
            setSelectedMeshIds(initial);
            onLoadedRef.current();
            if (initial.length > 0) await loadMeshes(next, initial);
        } catch (error) {
            if (generation !== generationRef.current || isAbortError(error)) return;
            Logger.capture("body-shape:load-mod", error);
            toast.error(t("page.tools.body_shape.toast.load_failed"), {
                description: toErrorMessage(error),
            });
        } finally {
            if (generation === generationRef.current && !meshRef.current.requests)
                setLoading(false);
        }
    };
    useLayoutEffect(() => {
        workerRef.current = new BodyShapeMeshWorkerClient();
        // Synchronize the backend session with the selected target.
        // oxlint-disable-next-line react/set-state-in-effect
        if (fixedTargetPath) void loadMod(fixedTargetPath);
        else {
            setLoaded(null);
            setSelectedMeshIds([]);
            setLoading(false);
        }
        return () => {
            generationRef.current++;
            cancelMesh();
            workerRef.current?.dispose();
            workerRef.current = null;
            const sessionId = activeSessionRef.current;
            activeSessionRef.current = null;
            if (sessionId) void closeSession(sessionId);
        };
    }, [fixedTargetPath]);
    return { loaded, selectedMeshIds, loading, loadMod, loadMeshes };
}
