import type { ViewerAnimationClip, ViewerComputeDeformer } from "@shared/mod-viewer/types";
import { BufferAttribute, BufferGeometry, Mesh, MeshBasicMaterial, Object3D } from "three";
import { describe, expect, it, vi } from "vitest";

import { ModelViewerComputeController } from "./model-viewer-compute-controller";

class FakeWorker {
    onmessage: Worker["onmessage"] = null;
    onerror: Worker["onerror"] = null;
    messages: unknown[] = [];
    terminate = vi.fn();

    postMessage(message: unknown) {
        this.messages.push(message);
    }
}

const deformer: ViewerComputeDeformer = {
    kind: "gimi_shape_pose_v1",
    id: "cloth",
    meshIds: ["mesh"],
    vertexCount: 1,
    base: { url: "/base", byteLength: 40, stride: 40 },
    shapePasses: [],
};

const clip: ViewerAnimationClip = {
    id: "clip",
    label: "Clip",
    deformerId: "cloth",
    variableIds: [],
    fps: 30,
    frameStart: 40,
    frameEnd: 42,
    loop: true,
    frames: [40, 41, 42].map((index) => ({ index, time: (index - 40) / 30, values: {} })),
};

function fixtureRoot(): Object3D {
    const root = new Object3D();
    const geometry = new BufferGeometry();
    geometry.setAttribute("position", new BufferAttribute(new Float32Array([1, 2, 3]), 3));
    geometry.setAttribute("normal", new BufferAttribute(new Float32Array([0, 1, 0]), 3));
    geometry.setAttribute("tangent", new BufferAttribute(new Float32Array([1, 0, 0, 1]), 4));
    const mesh = new Mesh(geometry, new MeshBasicMaterial());
    mesh.userData.meshId = "mesh";
    root.add(mesh);
    return root;
}

describe("ModelViewerComputeController", () => {
    it("drops intermediate requests and dispatches only the latest pending frame", () => {
        const worker = new FakeWorker();
        const controller = new ModelViewerComputeController(
            fixtureRoot(),
            deformer,
            [
                {
                    id: "mesh",
                    component: "mesh",
                    positionsUrl: "",
                    indicesUrl: "",
                    conditions: [],
                    texKey: null,
                    textureVariants: [],
                    normalMapKey: null,
                    normalMapVariants: [],
                    lightMapKey: null,
                    lightMapVariants: [],
                    materialMapKey: null,
                    materialMapVariants: [],
                    shapeTargets: [],
                    positionVariants: [],
                },
            ],
            vi.fn(),
            vi.fn(),
            worker,
        );
        worker.onmessage?.(new MessageEvent("message", { data: { type: "ready", generation: 0 } }));
        const init = worker.messages[0] as { generation: number };
        worker.onmessage?.(
            new MessageEvent("message", {
                data: { type: "ready", generation: init.generation },
            }),
        );
        controller.request({ clip, frameIndex: 0 });
        controller.request({ clip, frameIndex: 1 });
        controller.request({ clip, frameIndex: 2 });
        expect(
            worker.messages.filter((message) => (message as { type: string }).type === "frame"),
        ).toHaveLength(1);
        worker.onmessage?.(
            new MessageEvent("message", {
                data: { type: "frame", generation: init.generation, meshes: [] },
            }),
        );
        const frames = worker.messages.filter(
            (message) => (message as { type: string }).type === "frame",
        ) as Array<{ poseFrame: number }>;
        expect(frames).toHaveLength(2);
        expect(frames[1]?.poseFrame).toBe(42);
        controller.dispose();
    });

    it("restores the static baseline after a worker error and reports it once", () => {
        const root = fixtureRoot();
        const worker = new FakeWorker();
        const onError = vi.fn();
        const controller = new ModelViewerComputeController(
            root,
            deformer,
            [],
            vi.fn(),
            onError,
            worker,
        );
        const init = worker.messages[0] as { generation: number };
        const mesh = root.children[0] as Mesh;
        (mesh.geometry.getAttribute("position").array as Float32Array).set([9, 9, 9]);
        const error = { type: "error", generation: init.generation, message: "broken" };
        worker.onmessage?.(new MessageEvent("message", { data: error }));
        worker.onmessage?.(new MessageEvent("message", { data: error }));
        expect([...(mesh.geometry.getAttribute("position").array as Float32Array)]).toEqual([
            1, 2, 3,
        ]);
        expect(onError).toHaveBeenCalledOnce();
        controller.dispose();
    });

    it("removes controller-created attributes that were absent from the baseline", () => {
        const root = new Object3D();
        const geometry = new BufferGeometry();
        geometry.setAttribute("position", new BufferAttribute(new Float32Array([1, 2, 3]), 3));
        const mesh = new Mesh(geometry, new MeshBasicMaterial());
        mesh.userData.meshId = "mesh";
        root.add(mesh);
        const worker = new FakeWorker();
        const controller = new ModelViewerComputeController(
            root,
            deformer,
            [],
            vi.fn(),
            vi.fn(),
            worker,
        );
        const init = worker.messages[0] as { generation: number };
        worker.onmessage?.(
            new MessageEvent("message", {
                data: { type: "ready", generation: init.generation },
            }),
        );
        controller.request({ clip, frameIndex: 0 });
        worker.onmessage?.(
            new MessageEvent("message", {
                data: {
                    type: "frame",
                    generation: init.generation,
                    meshes: [
                        {
                            meshId: "mesh",
                            positions: new Float32Array([9, 9, 9]).buffer,
                            normals: new Float32Array([0, 0, 1]).buffer,
                            tangents: new Float32Array([0, 1, 0, 1]).buffer,
                        },
                    ],
                },
            }),
        );
        expect(mesh.geometry.getAttribute("normal")).toBeDefined();
        expect(mesh.geometry.getAttribute("tangent")).toBeDefined();
        controller.dispose();
        expect([...(mesh.geometry.getAttribute("position").array as Float32Array)]).toEqual([
            1, 2, 3,
        ]);
        expect(mesh.geometry.getAttribute("normal")).toBeUndefined();
        expect(mesh.geometry.getAttribute("tangent")).toBeUndefined();
    });

    it("reports a deformer whose mesh IDs do not match the rendered model", () => {
        const worker = new FakeWorker();
        const onError = vi.fn();
        const missingDeformer = { ...deformer, meshIds: ["missing"] };
        const controller = new ModelViewerComputeController(
            fixtureRoot(),
            missingDeformer,
            [],
            vi.fn(),
            onError,
            worker,
        );

        expect(worker.terminate).toHaveBeenCalledOnce();
        expect(worker.messages).toEqual([]);
        expect(onError).toHaveBeenCalledWith(
            expect.objectContaining({
                message: "GIMI shape/pose deformer cloth did not match any rendered mesh.",
            }),
        );
        controller.dispose();
    });
});
