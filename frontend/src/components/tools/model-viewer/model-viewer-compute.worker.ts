import { serializeDiagnostic } from "@shared/diagnostic";
import type { ViewerComputeDeformer } from "@shared/mod-viewer/types";

import {
    compactGIMIShapePoseFrame,
    computeGIMIShapePoseFrame,
    type GIMIShapePoseBuffers,
    validateGIMIShapePoseBuffers,
} from "./model-viewer-compute-kernel";

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
          deformer: ViewerComputeDeformer;
          buffers: GIMIShapePoseBuffers;
          meshes: Array<ComputeMesh & { sourceIndices: Uint32Array }>;
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
        const [base, shapeTargets, blend, pose, meshes] = await Promise.all([
            fetchSource(request.deformer.base.url, request.deformer.base.byteLength),
            Promise.all(
                request.deformer.shapePasses.map((pass) =>
                    fetchSource(pass.target.url, pass.target.byteLength),
                ),
            ),
            request.deformer.pose
                ? fetchSource(
                      request.deformer.pose.blend.url,
                      request.deformer.pose.blend.byteLength,
                  )
                : undefined,
            request.deformer.pose
                ? fetchSource(
                      request.deformer.pose.frames.url,
                      request.deformer.pose.frames.byteLength,
                  )
                : undefined,
            Promise.all(
                request.meshes.map(async (mesh) => ({
                    ...mesh,
                    sourceIndices: mesh.sourceIndicesUrl
                        ? new Uint32Array(await fetchSource(mesh.sourceIndicesUrl))
                        : Uint32Array.from({ length: mesh.vertexCount }, (_, index) => index),
                })),
            ),
        ]);
        const buffers = { base, shapeTargets, blend, pose };
        validateGIMIShapePoseBuffers(request.deformer, buffers);
        for (const mesh of meshes) {
            if (mesh.sourceIndices.length !== mesh.vertexCount) {
                throw new Error(
                    `GIMI shape/pose source index count is invalid for mesh ${mesh.id}.`,
                );
            }
        }
        active = { generation, deformer: request.deformer, buffers, meshes };
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
        const frame = computeGIMIShapePoseFrame(
            current.deformer,
            current.buffers,
            request.poseFrame,
            request.phaseSeconds,
        );
        const meshes = current.meshes.map((mesh) => {
            const compact = compactGIMIShapePoseFrame(frame, mesh.sourceIndices);
            return {
                meshId: mesh.id,
                positions: compact.positions.buffer,
                normals: compact.normals.buffer,
                tangents: compact.tangents.buffer,
            };
        });
        scope.postMessage(
            { type: "frame", generation: request.generation, id: request.id, meshes },
            meshes.flatMap((mesh) => [mesh.positions, mesh.normals, mesh.tangents]),
        );
    } catch (error) {
        postError(request.generation, request.id, "compute-frame", error);
    }
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
