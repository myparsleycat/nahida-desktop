import { serializeDiagnostic } from "@shared/diagnostic";
import type { ViewerComputeBinarySource, ViewerComputeDeformer } from "@shared/mod-viewer/types";

import {
    createCyclicPackedComputer,
    validateCyclicPackedBuffers,
} from "./model-viewer-compute-cyclic";
import {
    createPackedDualQuaternionComputer,
    validatePackedDualQuaternionBuffers,
} from "./model-viewer-compute-dual-quaternion";
import {
    collectUsedVertices,
    compactGIMIShapePoseFrame,
    createGIMIShapePoseComputer,
    ensureGIMIShapePoseFrame,
    type GIMIComputeOptions,
    type GIMIShapePoseBuffers,
    type GIMIShapePoseFrame,
    remapSourceIndices,
} from "./model-viewer-compute-kernel";
import {
    computePackedShapeFrame,
    type PackedShapeStageBuffers,
    validatePackedShapeBuffers,
} from "./model-viewer-compute-packed-shape";
import { decodePackedVertexSource } from "./model-viewer-packed-vertex";

type ComputeMesh = {
    id: string;
    sourceIndicesUrl?: string;
    vertexCount: number;
};

type InitRequest = {
    type: "init";
    generation: number;
    deformer: ViewerComputeDeformer;
    meshes: ComputeMesh[];
};

type FrameRequest = {
    type: "frame";
    generation: number;
    id: number;
    poseFrame: number;
    phaseSeconds: number;
};

type RecycleRequest = {
    type: "recycle";
    generation: number;
    meshes: Array<{
        meshId: string;
        positions: ArrayBuffer;
        normals: ArrayBuffer;
        tangents?: ArrayBuffer;
    }>;
};

type ActiveMesh = ComputeMesh & {
    compactIndices: Uint32Array;
    pool: GIMIShapePoseFrame[];
};

type WorkerScope = {
    onmessage: ((event: MessageEvent<InitRequest | FrameRequest | RecycleRequest>) => void) | null;
    postMessage(message: unknown, transfer?: Transferable[]): void;
};

const scope = self as unknown as WorkerScope;
let active:
    | {
          generation: number;
          meshes: Map<string, ActiveMesh>;
          vertexCount: number;
          vertices?: Uint32Array;
          scratch?: GIMIShapePoseFrame;
          hasTangents: boolean;
          compute: (
              poseFrame: number,
              phaseSeconds: number,
              options?: GIMIComputeOptions,
          ) => GIMIShapePoseFrame;
      }
    | undefined;

scope.onmessage = (event) => {
    const message = event.data;
    if (message.type === "init") {
        void initialize(message);
        return;
    }
    if (message.type === "recycle") {
        recycleBuffers(message);
        return;
    }
    computeFrame(message);
};

async function initialize(request: InitRequest): Promise<void> {
    const generation = request.generation;
    try {
        const meshes = await Promise.all(
            request.meshes.map(async (mesh) => ({
                ...mesh,
                sourceIndices: mesh.sourceIndicesUrl
                    ? new Uint32Array(await fetchSource(mesh.sourceIndicesUrl))
                    : Uint32Array.from({ length: mesh.vertexCount }, (_, index) => index),
            })),
        );
        for (const mesh of meshes) {
            if (mesh.sourceIndices.length !== mesh.vertexCount) {
                throw new Error(
                    `GIMI shape/pose source index count is invalid for mesh ${mesh.id}.`,
                );
            }
        }
        const usedVertices = collectUsedVertices(
            meshes.map((mesh) => mesh.sourceIndices),
            request.deformer.vertexCount,
        );
        const subset =
            usedVertices.length === request.deformer.vertexCount ? undefined : usedVertices;
        const compactIndices = subset
            ? remapSourceIndices(
                  meshes.map((mesh) => mesh.sourceIndices),
                  usedVertices,
                  request.deformer.vertexCount,
              )
            : meshes.map((mesh) => mesh.sourceIndices);
        const compute = await bindDeformerCompute(request.deformer);
        active = {
            generation,
            vertexCount: request.deformer.vertexCount,
            vertices: subset,
            hasTangents:
                request.deformer.kind === "gimi_shape_pose_v1" &&
                request.deformer.base.stride === 40,
            meshes: new Map(
                meshes.map((mesh, index) => [
                    mesh.id,
                    {
                        ...mesh,
                        compactIndices: compactIndices[index]!,
                        pool: [],
                    },
                ]),
            ),
            compute,
        };
        scope.postMessage({ type: "ready", generation });
    } catch (error) {
        postError(generation, undefined, "initialize", error, request.deformer.base.url);
    }
}

function computeFrame(request: FrameRequest): void {
    const current = active;
    if (!current || current.generation !== request.generation) {
        return;
    }
    try {
        current.scratch = ensureGIMIShapePoseFrame(
            current.vertices?.length ?? current.vertexCount,
            current.hasTangents,
            current.scratch,
        );
        const frame = current.compute(request.poseFrame, request.phaseSeconds, {
            vertices: current.vertices,
            out: current.scratch,
        });
        current.scratch = frame;
        const meshes = Array.from(current.meshes.values(), (mesh) => {
            const compact = compactGIMIShapePoseFrame(frame, mesh.compactIndices, mesh.pool.pop());
            return {
                meshId: mesh.id,
                positions: compact.positions.buffer,
                normals: compact.normals.buffer,
                tangents: compact.tangents?.buffer,
            };
        });
        scope.postMessage(
            { type: "frame", generation: request.generation, id: request.id, meshes },
            meshes.flatMap((mesh) =>
                mesh.tangents
                    ? [mesh.positions, mesh.normals, mesh.tangents]
                    : [mesh.positions, mesh.normals],
            ),
        );
    } catch (error) {
        postError(request.generation, request.id, "compute-frame", error);
    }
}

function recycleBuffers(request: RecycleRequest): void {
    const current = active;
    if (!current || current.generation !== request.generation) {
        return;
    }
    for (const mesh of request.meshes) {
        const target = current.meshes.get(mesh.meshId);
        if (!target) {
            continue;
        }
        target.pool.push({
            positions: new Float32Array(mesh.positions),
            normals: new Float32Array(mesh.normals),
            tangents: mesh.tangents ? new Float32Array(mesh.tangents) : undefined,
        });
    }
}

async function bindDeformerCompute(
    deformer: ViewerComputeDeformer,
): Promise<
    (poseFrame: number, phaseSeconds: number, options?: GIMIComputeOptions) => GIMIShapePoseFrame
> {
    switch (deformer.kind) {
        case "gimi_cyclic_packed_shape_v1": {
            const stages = await loadPackedShapeStages(deformer);
            validatePackedShapeBuffers(deformer, stages);
            const prepared = preparePackedShapeStages(deformer, stages);
            return (_poseFrame, phaseSeconds, options) =>
                computePackedShapeFrame(prepared.deformer, prepared.stages, phaseSeconds, options);
        }
        case "gimi_cyclic_packed_v1": {
            const buffers = await loadShapePoseBuffers(deformer);
            validateCyclicPackedBuffers(deformer, buffers);
            const prepared = prepareShapePoseBuffers(deformer, buffers);
            const compute = createCyclicPackedComputer(prepared.deformer, prepared.buffers);
            return (poseFrame, _phaseSeconds, options) => compute(poseFrame, options);
        }
        case "gimi_packed_dual_quaternion_v1": {
            const buffers = await loadShapePoseBuffers(deformer);
            validatePackedDualQuaternionBuffers(deformer, buffers);
            const prepared = prepareShapePoseBuffers(deformer, buffers);
            const compute = createPackedDualQuaternionComputer(prepared.deformer, prepared.buffers);
            return (poseFrame, _phaseSeconds, options) => compute(poseFrame, options);
        }
        case "gimi_shape_pose_v1": {
            const buffers = await loadShapePoseBuffers(deformer);
            return createGIMIShapePoseComputer(deformer, buffers);
        }
    }
}

function prepareShapePoseBuffers(
    deformer: ViewerComputeDeformer,
    buffers: GIMIShapePoseBuffers,
): { deformer: ViewerComputeDeformer; buffers: GIMIShapePoseBuffers } {
    const base = decodePackedVertexSource(deformer.base, buffers.base);
    return {
        deformer: { ...deformer, base: base.source },
        buffers: { ...buffers, base: base.buffer },
    };
}

function preparePackedShapeStages(
    deformer: ViewerComputeDeformer,
    stages: PackedShapeStageBuffers,
): { deformer: ViewerComputeDeformer; stages: PackedShapeStageBuffers } {
    const prepared = stages.map((buffers, index) => {
        const stage = deformer.shapeStages[index]!;
        const base = decodePackedVertexSource(stage.base, buffers.base);
        const target = decodePackedVertexSource(stage.target, buffers.target);
        return {
            stage: { ...stage, base: base.source, target: target.source },
            buffers: { base: base.buffer, target: target.buffer },
        };
    });
    return {
        deformer: { ...deformer, shapeStages: prepared.map((entry) => entry.stage) },
        stages: prepared.map((entry) => entry.buffers),
    };
}

async function loadShapePoseBuffers(
    deformer: ViewerComputeDeformer,
): Promise<GIMIShapePoseBuffers> {
    const [base, shapeTargets, blend, pose] = await Promise.all([
        fetchSource(deformer.base.url, deformer.base.byteLength),
        Promise.all(
            deformer.shapePasses.map((pass) =>
                fetchSource(pass.target.url, pass.target.byteLength),
            ),
        ),
        deformer.pose
            ? fetchSource(deformer.pose.blend.url, deformer.pose.blend.byteLength)
            : undefined,
        deformer.pose
            ? fetchSource(deformer.pose.frames.url, deformer.pose.frames.byteLength)
            : undefined,
    ]);
    return { base, shapeTargets, blend, pose };
}

async function loadPackedShapeStages(
    deformer: ViewerComputeDeformer,
): Promise<PackedShapeStageBuffers> {
    const cache = new Map<string, Promise<ArrayBuffer>>();
    const load = (source: ViewerComputeBinarySource) => {
        const hit = cache.get(source.url);
        if (hit) {
            return hit;
        }
        const pending = fetchSource(source.url, source.byteLength);
        cache.set(source.url, pending);
        return pending;
    };
    return Promise.all(
        deformer.shapeStages.map(async (stage) => ({
            base: await load(stage.base),
            target: await load(stage.target),
        })),
    );
}

async function fetchSource(url: string, expectedBytes?: number): Promise<ArrayBuffer> {
    const response = await fetch(url, { cache: "no-store" });
    if (!response.ok) {
        throw new Error(`Failed to load GIMI shape/pose source (${response.status}).`);
    }
    const buffer = await response.arrayBuffer();
    if (expectedBytes !== undefined && buffer.byteLength !== expectedBytes) {
        throw new Error(
            `GIMI shape/pose source size changed: expected ${expectedBytes}, received ${buffer.byteLength}.`,
        );
    }
    return buffer;
}

function postError(
    generation: number,
    id: number | undefined,
    stage: string,
    error: unknown,
    sourceUrl?: string,
): void {
    scope.postMessage({
        type: "error",
        generation,
        id,
        stage,
        message: error instanceof Error ? error.message : String(error),
        diagnostic: serializeDiagnostic({ error, stage, sourceUrl }),
    });
}
