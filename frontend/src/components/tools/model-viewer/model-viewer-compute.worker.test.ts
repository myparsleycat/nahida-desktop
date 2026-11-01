import type { ViewerComputeDeformer } from "@shared/mod-viewer/types";
import { afterEach, beforeEach, expect, it, vi } from "vitest";

const scope = {
    onmessage: null as ((event: MessageEvent) => void) | null,
    postMessage: vi.fn(),
};
const pending = new Map<string, ReadableStreamDefaultController<Uint8Array>>();

function descriptor(targets: string[] = []): ViewerComputeDeformer {
    return {
        id: "shape",
        kind: "gimi_shape_pose_v1",
        meshIds: ["mesh"],
        vertexCount: 1,
        base: { url: "/base", byteLength: 40, stride: 40 },
        shapeStages: [],
        shapePasses: targets.map((url) => ({
            target: { url, byteLength: 40, stride: 40 },
            phaseRate: 1,
            phaseOffset: 0,
            angularScale: 1,
            amplitude: 0,
            bias: 0,
        })),
    };
}

function initialize(deformer = descriptor(), meshCount = 1) {
    scope.onmessage?.(
        new MessageEvent("message", {
            data: {
                type: "init",
                generation: 1,
                deformer,
                meshes: Array.from({ length: meshCount }, (_, index) => ({
                    id: `mesh-${index}`,
                    vertexCount: 1,
                    sourceIndicesUrl: meshCount > 1 ? `/index/${index}` : undefined,
                })),
            },
        }),
    );
}

function deferredFetch() {
    return vi.fn(async (url: string, options?: RequestInit) => {
        if (url === "/base") return new Response(new ArrayBuffer(40));
        return new Response(
            new ReadableStream<Uint8Array>({
                start(controller) {
                    pending.set(url, controller);
                    options?.signal?.addEventListener(
                        "abort",
                        () => {
                            if (pending.delete(url)) controller.error(options.signal?.reason);
                        },
                        { once: true },
                    );
                },
            }),
        );
    });
}

function finish(url: string) {
    const controller = pending.get(url)!;
    pending.delete(url);
    controller.enqueue(new Uint8Array(4));
    controller.close();
}

beforeEach(async () => {
    vi.resetModules();
    scope.postMessage.mockClear();
    vi.stubGlobal("self", scope);
    await import("./model-viewer-compute.worker");
});

afterEach(() => {
    for (const controller of pending.values()) controller.error(new Error("test cleanup"));
    pending.clear();
    vi.unstubAllGlobals();
});

it("fetches a URL shared by base and shape passes only once", async () => {
    const fetch = vi.fn(async () => new Response(new ArrayBuffer(40)));
    vi.stubGlobal("fetch", fetch);
    initialize(descriptor(["/base", "/base"]));
    await vi.waitFor(() =>
        expect(scope.postMessage).toHaveBeenCalledWith({ type: "ready", generation: 1 }),
    );
    expect(fetch).toHaveBeenCalledOnce();
});

it("holds all eight request slots until their bodies finish and refills released slots", async () => {
    const fetch = deferredFetch();
    vi.stubGlobal("fetch", fetch);
    initialize(descriptor(), 20);
    await vi.waitFor(() => expect(fetch).toHaveBeenCalledTimes(8));
    finish("/index/7");
    await vi.waitFor(() => expect(fetch).toHaveBeenCalledTimes(9));
    expect(pending.size).toBe(8);
    for (const expected of [17, 20]) {
        for (const url of [...pending.keys()]) finish(url);
        await vi.waitFor(() => expect(fetch).toHaveBeenCalledTimes(expected));
        expect(pending.size).toBeLessThanOrEqual(8);
    }
    for (const url of [...pending.keys()]) finish(url);
    await vi.waitFor(() =>
        expect(scope.postMessage).toHaveBeenCalledWith({ type: "ready", generation: 1 }),
    );
    expect(fetch).toHaveBeenCalledTimes(21);
});

it("validates every caller's expected length even when its URL is cached", async () => {
    const fetch = vi.fn(async () => new Response(new ArrayBuffer(40)));
    vi.stubGlobal("fetch", fetch);
    const deformer = descriptor(["/base"]);
    deformer.shapePasses[0].target.byteLength = 44;
    initialize(deformer);
    await vi.waitFor(() =>
        expect(scope.postMessage).toHaveBeenCalledWith(
            expect.objectContaining({
                type: "error",
                message: expect.stringMatching(/expected 44, received 40/),
            }),
        ),
    );
    expect(fetch).toHaveBeenCalledOnce();
});

it("cancels active bodies and leaves queued URLs unrequested after a source fails", async () => {
    const fetch = deferredFetch();
    vi.stubGlobal("fetch", fetch);
    initialize(descriptor(), 20);
    await vi.waitFor(() => expect(pending.size).toBeGreaterThanOrEqual(8));
    const failed = pending.get("/index/0")!;
    pending.delete("/index/0");
    failed.error(new Error("broken source"));
    await vi.waitFor(() =>
        expect(scope.postMessage).toHaveBeenCalledWith(
            expect.objectContaining({ type: "error", message: "broken source" }),
        ),
    );
    expect(fetch).toHaveBeenCalledTimes(8);
    expect(fetch.mock.calls.every(([, options]) => options?.signal?.aborted)).toBe(true);
    expect(pending.size).toBe(0);
});
