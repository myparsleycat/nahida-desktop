import { serializeDiagnostic } from "@shared/diagnostic";
import type { ViewerComputeBinarySource, ViewerComputeDeformer } from "@shared/mod-viewer/types";

import {
    computeCyclicPackedFrame,
    validateCyclicPackedBuffers,
} from "./model-viewer-compute-cyclic";
import {
    compactGIMIShapePoseFrame,
    computeGIMIShapePoseFrame,
    type GIMIShapePoseBuffers,
    type GIMIShapePoseFrame,
    validateGIMIShapePoseBuffers,
} from "./model-viewer-compute-kernel";
import {
    computePackedShapeFrame,
    type PackedShapeStageBuffers,
    validatePackedShapeBuffers,
} from "./model-viewer-compute-packed-shape";

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

type WorkerScope = {
    onmessage: ((event: MessageEvent<InitRequest | FrameRequest>) => void) | null;
    postMessage(message: unknown, transfer?: Transferable[]): void;
};

const scope = self as unknown as WorkerScope;
let active:
    | {
          generation: number;
          meshes: Array<ComputeMesh & { sourceIndices: Uint32Array }>;
          compute: (poseFrame: number, phaseSeconds: number) => GIMIShapePoseFrame;
      }
    | undefined;

scope.onmessage = (event) => {
    const message = event.data;
    if (message.type === "init") {
        void initialize(message);
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
        const compute = await bindDeformerCompute(request.deformer);
        active = { generation, meshes, compute };
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
        const frame = current.compute(request.poseFrame, request.phaseSeconds);
        const meshes = current.meshes.map((mesh) => {
            const compact = compactGIMIShapePoseFrame(frame, mesh.sourceIndices);
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

async function bindDeformerCompute(
    deformer: ViewerComputeDeformer,
): Promise<(poseFrame: number, phaseSeconds: number) => GIMIShapePoseFrame> {
    switch (deformer.kind) {
        case "gimi_cyclic_packed_shape_v1": {
            const stages = await loadPackedShapeStages(deformer);
            validatePackedShapeBuffers(deformer, stages);
            return (_poseFrame, phaseSeconds) =>
                computePackedShapeFrame(deformer, stages, phaseSeconds);
        }
        case "gimi_cyclic_packed_v1": {
            const buffers = await loadShapePoseBuffers(deformer);
            validateCyclicPackedBuffers(deformer, buffers);
            return (poseFrame) => computeCyclicPackedFrame(deformer, buffers, poseFrame);
        }
        case "gimi_shape_pose_v1": {
            const buffers = await loadShapePoseBuffers(deformer);
            validateGIMIShapePoseBuffers(deformer, buffers);
            return (poseFrame, phaseSeconds) =>
                computeGIMIShapePoseFrame(deformer, buffers, poseFrame, phaseSeconds);
        }
    }
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
