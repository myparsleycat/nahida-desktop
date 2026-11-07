// @vitest-environment jsdom

import type { ModViewerTransport, ViewerAnimationClip } from "@shared/mod-viewer/types";
import { act, cleanup, render, waitFor } from "@testing-library/react";
import { Children, isValidElement, type ReactNode } from "react";
import {
  BufferAttribute,
  BufferGeometry,
  Group,
  Mesh,
  MeshBasicMaterial,
  PerspectiveCamera,
  Scene,
} from "three";
import { afterEach, beforeEach, expect, it, vi } from "vitest";

import { buildPayloadModel } from "./model-viewer-payload";
import { ThreeModelViewer } from "./three-model-viewer";

vi.mock("@react-three/fiber", () => ({
  Canvas: ({ children }: { children: ReactNode }) => (
    <>
      {Children.toArray(children).filter(
        (child) => isValidElement(child) && typeof child.type === "function",
      )}
    </>
  ),
  useFrame: vi.fn(),
  useThree: () => three,
}));
vi.mock("@react-three/drei", () => ({ OrbitControls: () => null }));
vi.mock("@renderer/components/tools/three-renderer", () => ({ createThreeRenderer: vi.fn() }));
vi.mock("@renderer/lib/logger", () => ({ Logger: { capture: vi.fn() } }));
vi.mock("./model-viewer-axis-gizmo", () => ({ ModelViewerAxisGizmo: () => null }));
vi.mock("./model-viewer-payload", async (importOriginal) => ({
  ...(await importOriginal<typeof import("./model-viewer-payload")>()),
  buildPayloadModel: vi.fn(),
}));
vi.mock("react/jsx-dev-runtime", async (importOriginal) => {
  const runtime = await importOriginal<typeof import("react/jsx-dev-runtime")>();
  const jsxDEV: typeof runtime.jsxDEV = (type, props, key, isStaticChildren, source, self) => {
    // Three host nodes must not assign DOM elements to scene resource refs.
    if (type === "group")
      return runtime.jsxDEV(
        runtime.Fragment,
        { children: (props as { children?: ReactNode }).children },
        key,
        isStaticChildren,
        source,
        self,
      );
    if (type === "primitive")
      return runtime.jsxDEV(runtime.Fragment, {}, key, isStaticChildren, source, self);
    return runtime.jsxDEV(type, props, key, isStaticChildren, source, self);
  };
  return { ...runtime, jsxDEV };
});

const three = {
  camera: new PerspectiveCamera(45),
  scene: new Scene(),
  gl: {
    setClearAlpha: vi.fn(),
    renderLists: { dispose: vi.fn() },
    capabilities: { maxTextureSize: 4096 },
    extensions: { has: () => false },
  },
  invalidate: vi.fn(),
};

class FakeWorker {
  static instances: FakeWorker[] = [];
  onmessage: ((event: MessageEvent) => void) | null = null;
  onerror: ((event: ErrorEvent) => void) | null = null;
  messages: Array<{ type: string; generation: number; poseFrame?: number }> = [];
  terminate = vi.fn();

  constructor() {
    FakeWorker.instances.push(this);
  }

  postMessage(message: FakeWorker["messages"][number]) {
    this.messages.push(message);
  }

  ready() {
    this.onmessage?.(
      new MessageEvent("message", {
        data: { type: "ready", generation: this.messages[0].generation },
      }),
    );
  }
}

function fixtureRoot() {
  const geometry = new BufferGeometry();
  geometry.setAttribute("position", new BufferAttribute(new Float32Array([1, 2, 3]), 3));
  geometry.setAttribute("normal", new BufferAttribute(new Float32Array([0, 1, 0]), 3));
  const mesh = new Mesh(geometry, new MeshBasicMaterial());
  mesh.userData = { meshId: "mesh", shapeTargets: [], positionVariants: [] };
  return new Group().add(mesh);
}

const transport: ModViewerTransport = {
  memorySessionId: "session",
  modPath: "mod",
  iniPath: "mod.ini",
  name: "model",
  meshes: [],
  textures: {},
  variables: [],
  defaultState: {},
  stateRules: [],
  uiAssets: {},
  forms: [],
  inis: [],
  animations: [],
  computeDeformers: ["first", "second"].map((id) => ({
    id,
    kind: "gimi_shape_pose_v1",
    meshIds: ["mesh"],
    vertexCount: 1,
    base: { url: "/base", byteLength: 40, stride: 40 },
    shapePasses: [],
    shapeStages: [],
  })),
};

function clip(id: string, deformerId = "first"): ViewerAnimationClip {
  return {
    id,
    deformerId,
    label: id,
    variableIds: [],
    fps: 30,
    frameStart: 0,
    frameEnd: 1,
    loop: true,
    frames: [0, 1].map((index) => ({ index, time: index / 30, values: {} })),
  };
}

function viewer(animationClip?: ViewerAnimationClip, payloadTransport = transport) {
  return (
    <ThreeModelViewer
      orientation="0deg 0deg 0deg"
      threeEnvironment="none"
      payloadTransport={payloadTransport}
      animationClip={animationClip}
      animationFrame={1}
      onLoad={vi.fn()}
    />
  );
}

beforeEach(() => {
  FakeWorker.instances = [];
  vi.stubGlobal("Worker", FakeWorker);
  vi.mocked(buildPayloadModel).mockImplementation(async () => fixtureRoot());
});

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
  vi.unstubAllGlobals();
});

it("reuses an initializing worker when switching clips of the same deformer", async () => {
  const first = clip("idle");
  const next = { ...clip("walk"), frames: [{ index: 42, time: 0, values: {} }] };
  const view = render(viewer(first));
  await waitFor(() => expect(FakeWorker.instances).toHaveLength(1));
  const worker = FakeWorker.instances[0];
  view.rerender(viewer(next));
  expect(FakeWorker.instances).toHaveLength(1);
  expect(worker.terminate).not.toHaveBeenCalled();
  act(() => worker.ready());
  expect(worker.messages.filter((message) => message.type === "init")).toHaveLength(1);
  expect(worker.messages.filter((message) => message.type === "frame").at(-1)?.poseFrame).toBe(42);
  view.rerender(viewer(first));
  act(() =>
    worker.onmessage?.(
      new MessageEvent("message", {
        data: { type: "frame", generation: worker.messages[0].generation, meshes: [] },
      }),
    ),
  );
  expect(FakeWorker.instances).toHaveLength(1);
  expect(worker.messages.filter((message) => message.type === "frame").at(-1)?.poseFrame).toBe(1);
  view.unmount();
  expect(worker.terminate).toHaveBeenCalledOnce();
});

it("replaces workers for a different deformer or model and disposes them when animation is disabled", async () => {
  const root = fixtureRoot();
  vi.mocked(buildPayloadModel).mockResolvedValueOnce(root);
  const view = render(viewer(clip("idle")));
  await waitFor(() => expect(FakeWorker.instances).toHaveLength(1));
  const worker = FakeWorker.instances[0];
  act(() => {
    worker.ready();
    worker.onmessage?.(
      new MessageEvent("message", {
        data: {
          type: "frame",
          generation: worker.messages[0].generation,
          meshes: [
            {
              meshId: "mesh",
              positions: new Float32Array([9, 8, 7]).buffer,
              normals: new Float32Array([0, 1, 0]).buffer,
            },
          ],
        },
      }),
    );
  });
  const mesh = root.children[0] as Mesh;
  expect([...mesh.geometry.getAttribute("position").array]).toEqual([9, 8, 7]);
  view.rerender(viewer(clip("other", "second")));
  expect(FakeWorker.instances).toHaveLength(2);
  expect(FakeWorker.instances[0].terminate).toHaveBeenCalledOnce();
  expect([...mesh.geometry.getAttribute("position").array]).toEqual([1, 2, 3]);
  const nextTransport = { ...transport, memorySessionId: "other-session" };
  view.rerender(viewer(clip("other", "second"), nextTransport));
  await waitFor(() => expect(FakeWorker.instances).toHaveLength(3));
  view.rerender(viewer(undefined, nextTransport));
  expect(FakeWorker.instances.every((worker) => worker.terminate.mock.calls.length === 1)).toBe(
    true,
  );
  view.unmount();
  expect(FakeWorker.instances.every((worker) => worker.terminate.mock.calls.length === 1)).toBe(
    true,
  );
});
