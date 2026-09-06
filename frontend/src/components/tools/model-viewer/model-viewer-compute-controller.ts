import type {
    ViewerAnimationClip,
    ViewerComputeDeformer,
    ViewerMeshTransport,
} from "@shared/mod-viewer/types";
import { BufferAttribute, Mesh, Object3D } from "three";

type ComputeWorker = Pick<Worker, "onmessage" | "onerror" | "postMessage" | "terminate">;

type ComputeFrameRequest = {
    clip: ViewerAnimationClip;
    frameIndex: number;
};

type MeshBaseline = {
    mesh: Mesh;
    positions: Float32Array;
    normals?: Float32Array;
    tangents?: Float32Array;
    frustumCulled: boolean;
};

export class ModelViewerComputeController {
    private readonly worker: ComputeWorker;
    private readonly generation: number;
    private readonly baselines = new Map<string, MeshBaseline>();
    private ready = false;
    private inFlight = false;
    private latest?: ComputeFrameRequest;
    private activeRequest?: ComputeFrameRequest;
    private nextId = 1;
    private disposed = false;
    private reportedError = false;

    constructor(
        root: Object3D,
        private readonly deformer: ViewerComputeDeformer,
        meshes: ViewerMeshTransport[],
        private readonly invalidate: () => void,
        private readonly onError: (error: unknown) => void,
        worker: ComputeWorker = new Worker(
            new URL("./model-viewer-compute.worker.ts", import.meta.url),
            { type: "module" },
        ),
    ) {
        this.worker = worker;
        this.generation = Date.now() + Math.floor(Math.random() * 1_000_000);
        root.traverse((object) => {
            if (!(object instanceof Mesh) || !deformer.meshIds.includes(object.userData.meshId)) {
                return;
            }
            const position = object.geometry.getAttribute("position");
            const normal = object.geometry.getAttribute("normal");
            const tangent = object.geometry.getAttribute("tangent");
            this.baselines.set(object.userData.meshId, {
                mesh: object,
                positions: new Float32Array(position.array as Float32Array),
                normals: normal ? new Float32Array(normal.array as Float32Array) : undefined,
                tangents: tangent ? new Float32Array(tangent.array as Float32Array) : undefined,
                frustumCulled: object.frustumCulled,
            });
            object.frustumCulled = false;
        });
        this.worker.onmessage = (event: MessageEvent) => this.handleMessage(event.data);
        this.worker.onerror = (event) => {
            this.fail(
                new Error(event.message || "Model viewer compute worker failed.", {
                    cause: event.error,
                }),
            );
        };
        if (this.baselines.size === 0) {
            this.fail(
                new Error(
                    `GIMI shape/pose deformer ${deformer.id} did not match any rendered mesh.`,
                    {
                        cause: { meshIds: deformer.meshIds },
                    },
                ),
            );
            return;
        }
        this.worker.postMessage({
            type: "init",
            generation: this.generation,
            deformer,
            meshes: meshes
                .filter((mesh) => deformer.meshIds.includes(mesh.id))
                .map((mesh) => ({
                    id: mesh.id,
                    sourceIndicesUrl: mesh.sourceIndicesUrl,
                    vertexCount: this.baselines.get(mesh.id)?.positions.length
                        ? this.baselines.get(mesh.id)!.positions.length / 3
                        : 0,
                })),
        });
    }

    request(request: ComputeFrameRequest): void {
        if (this.disposed || this.reportedError) {
            return;
        }
        this.latest = request;
        this.dispatchLatest();
    }

    dispose(restore = true): void {
        if (this.disposed) {
            return;
        }
        this.disposed = true;
        this.worker.terminate();
        if (restore) {
            this.restoreBaselines();
        }
        this.baselines.clear();
        this.latest = undefined;
        this.activeRequest = undefined;
    }

    private dispatchLatest(): void {
        if (!this.ready || this.inFlight || !this.latest || this.disposed) {
            return;
        }
        const request = this.latest;
        this.latest = undefined;
        const frame = request.clip.frames[request.frameIndex];
        if (!frame) {
            return;
        }
        this.activeRequest = request;
        this.inFlight = true;
        this.worker.postMessage({
            type: "frame",
            generation: this.generation,
            id: this.nextId++,
            poseFrame: frame.index,
            phaseSeconds: frame.time,
        });
    }

    private handleMessage(message: unknown): void {
        if (!isComputeMessage(message) || message.generation !== this.generation || this.disposed) {
            return;
        }
        if (message.type === "ready") {
            this.ready = true;
            this.dispatchLatest();
            return;
        }
        if (message.type === "error") {
            this.fail(new Error(message.message, { cause: message.diagnostic }));
            return;
        }
        for (const result of message.meshes) {
            const baseline = this.baselines.get(result.meshId);
            if (!baseline) {
                continue;
            }
            setAttribute(baseline.mesh, "position", new Float32Array(result.positions), 3);
            setAttribute(baseline.mesh, "normal", new Float32Array(result.normals), 3);
            setAttribute(baseline.mesh, "tangent", new Float32Array(result.tangents), 4);
        }
        this.inFlight = false;
        this.activeRequest = undefined;
        this.invalidate();
        this.dispatchLatest();
    }

    private fail(error: Error): void {
        if (this.reportedError || this.disposed) {
            return;
        }
        this.reportedError = true;
        this.inFlight = false;
        this.latest = undefined;
        const active = this.activeRequest;
        this.activeRequest = undefined;
        this.restoreBaselines();
        this.worker.terminate();
        this.onError(
            new Error(error.message, {
                cause: {
                    error: error.cause,
                    deformerId: this.deformer.id,
                    clipId: active?.clip.id,
                    frame: active?.clip.frames[active.frameIndex]?.index,
                },
            }),
        );
    }

    private restoreBaselines(): void {
        for (const baseline of this.baselines.values()) {
            setAttribute(baseline.mesh, "position", baseline.positions, 3);
            if (baseline.normals) {
                setAttribute(baseline.mesh, "normal", baseline.normals, 3);
            }
            if (baseline.tangents) {
                setAttribute(baseline.mesh, "tangent", baseline.tangents, 4);
            }
            baseline.mesh.frustumCulled = baseline.frustumCulled;
        }
        this.invalidate();
    }
}

function setAttribute(mesh: Mesh, name: string, values: Float32Array, itemSize: number): void {
    const attribute = mesh.geometry.getAttribute(name);
    if (attribute && attribute.array.length === values.length) {
        (attribute.array as Float32Array).set(values);
        attribute.needsUpdate = true;
        return;
    }
    mesh.geometry.setAttribute(name, new BufferAttribute(values, itemSize));
}

type ComputeMessage =
    | { type: "ready"; generation: number }
    | { type: "error"; generation: number; message: string; diagnostic?: unknown }
    | {
          type: "frame";
          generation: number;
          meshes: Array<{
              meshId: string;
              positions: ArrayBuffer;
              normals: ArrayBuffer;
              tangents: ArrayBuffer;
          }>;
      };

function isComputeMessage(value: unknown): value is ComputeMessage {
    if (!value || typeof value !== "object") {
        return false;
    }
    const message = value as Partial<ComputeMessage>;
    return (
        (message.type === "ready" || message.type === "error" || message.type === "frame") &&
        typeof message.generation === "number"
    );
}
