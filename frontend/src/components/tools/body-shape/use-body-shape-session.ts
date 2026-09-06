import { Tools, type BodyShapeMeshDescriptor, type BodyShapeMeshSummary } from "@bindings/tools";
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

export type LoadedMesh = {
    id: string;
    name: string;
    vertexCount: number;
    originalPositions: Float32Array;
    previewPositions: Float32Array;
    indices?: Uint32Array;
    blendBytes?: Uint8Array;
    blendStride: number;
    bones: BlendBoneInfo[];
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

async function loadBodyShapeMesh(
    worker: BodyShapeMeshWorkerClient,
    sessionId: string,
    summary: BodyShapeMeshSummary,
    descriptor: BodyShapeMeshDescriptor,
    signal: AbortSignal,
): Promise<LoadedMesh> {
    if (descriptor.sessionId !== sessionId || descriptor.meshId !== summary.id) {
        throw new Error("Body shape mesh descriptor does not match the active session");
    }
    const result = await worker.process(
        {
            sessionId,
            meshId: summary.id,
            vertexCount: summary.vertexCount,
            positionsUrl: descriptor.positionsUrl,
            positionsCount: descriptor.positionsCount,
            indicesUrl: descriptor.indicesUrl ?? undefined,
            indexCount: descriptor.indexCount,
            blendUrl: descriptor.blendUrl ?? undefined,
            blendBytes: descriptor.blendBytes,
            blendStride: descriptor.blendStride ?? DEFAULT_BLEND_STRIDE,
        },
        signal,
    );
    const originalPositions = new Float32Array(result.originalPositions);
    const indices = result.indices ? new Uint32Array(result.indices) : undefined;
    const blendBytes = result.blendBytes ? new Uint8Array(result.blendBytes) : undefined;
    const adjacency =
        result.adjacencyOffsets && result.adjacencyNeighbors
            ? {
                  offsets: new Uint32Array(result.adjacencyOffsets),
                  neighbors: new Uint32Array(result.adjacencyNeighbors),
              }
            : undefined;
    return {
        id: summary.id,
        name: summary.name,
        vertexCount: summary.vertexCount,
        originalPositions,
        previewPositions: new Float32Array(originalPositions),
        indices,
        blendBytes,
        blendStride: descriptor.blendStride ?? DEFAULT_BLEND_STRIDE,
        bones: descriptor.bones ?? [],
        weightCache: new Map(),
        selectionWeights: new Float32Array(summary.vertexCount),
        adjacency,
        originalNormals: result.originalNormals
            ? new Float32Array(result.originalNormals)
            : undefined,
        componentIds: result.componentIds ? new Uint32Array(result.componentIds) : undefined,
        symmetryMap: result.symmetryMap ? new Int32Array(result.symmetryMap) : undefined,
        boundsCenter: result.boundingCenter,
        geodesicWorkspace: createGeodesicBrushWorkspace(summary.vertexCount),
    };
}

export function useBodyShapeSession(fixedTargetPath: string | undefined, onLoaded: () => void) {
    const { t } = useTranslation();
    const [loaded, setLoaded] = useState<LoadResult | null>(null);
    const [selectedMeshId, setSelectedMeshId] = useState("");
    const [loading, setLoading] = useState(Boolean(fixedTargetPath));
    const activeSessionRef = useRef<string | null>(null);
    const generationRef = useRef(0);
    const workerRef = useRef<BodyShapeMeshWorkerClient | null>(null);
    const meshRef = useRef<{
        generation: number;
        request?: { cancel(): void };
        controller?: AbortController;
    }>({ generation: 0 });
    const onLoadedRef = useRef(onLoaded);
    useLayoutEffect(() => {
        onLoadedRef.current = onLoaded;
    });
    const cancelMesh = () => {
        meshRef.current.request?.cancel();
        meshRef.current.controller?.abort();
        meshRef.current = { generation: meshRef.current.generation + 1 };
    };
    const closeSession = async (id: string) => {
        await Tools.BodyShapeCloseSession(id).catch((error: unknown) => {
            Logger.capture("body-shape:close-session", error);
        });
    };
    const loadMeshById = async (current: LoadResult, meshId: string) => {
        if (activeSessionRef.current !== current.sessionId) return;
        cancelMesh();
        const generation = meshRef.current.generation;
        if (current.cache.has(meshId)) {
            setSelectedMeshId(meshId);
            setLoading(false);
            return;
        }
        const summary = current.meshes.find((mesh) => mesh.id === meshId);
        const worker = workerRef.current;
        if (!summary || !worker) return;
        const controller = new AbortController();
        const request = Tools.BodyShapeGetMesh({ sessionId: current.sessionId, meshId });
        meshRef.current = { generation, controller, request };
        setLoading(true);
        try {
            const descriptor = await request;
            if (generation !== meshRef.current.generation) return;
            const mesh = await loadBodyShapeMesh(
                worker,
                current.sessionId,
                summary,
                descriptor,
                controller.signal,
            );
            if (
                generation !== meshRef.current.generation ||
                activeSessionRef.current !== current.sessionId
            )
                return;
            current.cache.set(meshId, mesh);
            setSelectedMeshId(meshId);
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
            setLoaded(next);
            setSelectedMeshId(next.meshes[0]?.id ?? "");
            onLoadedRef.current();
            if (next.meshes[0]) await loadMeshById(next, next.meshes[0].id);
        } catch (error) {
            if (generation !== generationRef.current || isAbortError(error)) return;
            Logger.capture("body-shape:load-mod", error);
            toast.error(t("page.tools.body_shape.toast.load_failed"), {
                description: toErrorMessage(error),
            });
        } finally {
            if (generation === generationRef.current && !meshRef.current.request) setLoading(false);
        }
    };
    useLayoutEffect(() => {
        workerRef.current = new BodyShapeMeshWorkerClient();
        // Synchronize the backend session with the selected target.
        // oxlint-disable-next-line react/set-state-in-effect
        if (fixedTargetPath) void loadMod(fixedTargetPath);
        else {
            setLoaded(null);
            setSelectedMeshId("");
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
    return { loaded, selectedMeshId, loading, loadMod, loadMeshById };
}
