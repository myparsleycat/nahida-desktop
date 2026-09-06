// @vitest-environment jsdom
import { act, cleanup, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { useBodyShapeSession } from "./use-body-shape-session";

const mocks = vi.hoisted(() => ({
    load: vi.fn(),
    close: vi.fn(),
    mesh: vi.fn(),
    process: vi.fn(),
    dispose: vi.fn(),
    error: vi.fn(),
    capture: vi.fn(),
    reset: vi.fn(),
    workers: [] as { disposed: boolean }[],
    t: (key: string) => key,
}));
vi.mock("@bindings/tools", () => ({
    Tools: {
        BodyShapeLoadMod: mocks.load,
        BodyShapeCloseSession: mocks.close,
        BodyShapeGetMesh: mocks.mesh,
    },
}));
vi.mock("@renderer/lib/logger", () => ({ Logger: { capture: mocks.capture } }));
vi.mock("react-i18next", () => ({ useTranslation: () => ({ t: mocks.t }) }));
vi.mock("sonner", () => ({ toast: { error: mocks.error } }));
vi.mock("@renderer/wails/binary-memory", () => ({
    isAbortError: (error: unknown) => error instanceof DOMException && error.name === "AbortError",
}));
vi.mock("./body-shape-mesh-loader", () => ({
    BodyShapeMeshWorkerClient: class {
        disposed = false;
        constructor() {
            mocks.workers.push(this);
        }
        process(...args: unknown[]) {
            if (this.disposed) throw new Error("disposed worker");
            return mocks.process(...args);
        }
        dispose() {
            this.disposed = true;
            mocks.dispose();
        }
    },
}));
function deferred<T>() {
    let resolve!: (value: T) => void;
    let reject!: (error: Error) => void;
    const promise = Object.assign(
        new Promise<T>((yes, no) => {
            resolve = yes;
            reject = no;
        }),
        { cancel: vi.fn() },
    );
    return { promise, resolve, reject };
}
function session(id: string, ids: string[] = []) {
    return {
        sessionId: id,
        modRoot: id,
        iniPath: `${id}/mod.ini`,
        meshes: ids.map((meshId) => ({ id: meshId, name: meshId, vertexCount: 1 })),
    };
}
function descriptor(sessionId: string, meshId: string) {
    return {
        sessionId,
        meshId,
        positionsUrl: "memory",
        positionsCount: 3,
        indexCount: 0,
        blendBytes: 0,
        bones: [],
    };
}
beforeEach(() => {
    vi.resetAllMocks();
    mocks.workers.length = 0;
    mocks.close.mockResolvedValue(true);
    mocks.process.mockResolvedValue({
        originalPositions: new Float32Array([0, 0, 0]).buffer,
        boundingCenter: [0, 0, 0],
    });
    mocks.mesh.mockImplementation(({ sessionId, meshId }) =>
        Object.assign(Promise.resolve(descriptor(sessionId, meshId)), { cancel: vi.fn() }),
    );
});
afterEach(cleanup);
describe("body shape session ownership", () => {
    it("keeps a newer mesh request busy when initial mesh loading finishes", async () => {
        const first = deferred<ReturnType<typeof descriptor>>();
        const second = deferred<ReturnType<typeof descriptor>>();
        mocks.load.mockResolvedValue(session("A", ["first", "second"]));
        mocks.mesh.mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise);
        const { result } = renderHook(() => useBodyShapeSession("A", mocks.reset));
        await act(async () => {});
        let pending!: Promise<void>;
        act(() => {
            pending = result.current.loadMeshById(result.current.loaded!, "second");
        });
        await act(async () => {
            first.resolve(descriptor("A", "first"));
        });
        expect(result.current.loading).toBe(true);
        await act(async () => {
            second.resolve(descriptor("A", "second"));
            await pending;
        });
        expect(result.current.selectedMeshId).toBe("second");
        expect(result.current.loading).toBe(false);
    });

    it("clears the closed session when a fixed target is removed", async () => {
        mocks.load.mockResolvedValue(session("A", ["mesh"]));
        const { result, rerender } = renderHook(
            ({ path }: { path: string | undefined }) => useBodyShapeSession(path, mocks.reset),
            { initialProps: { path: "A" as string | undefined } },
        );
        await act(async () => {});
        rerender({ path: undefined });
        expect(result.current.loaded).toBeNull();
        expect(result.current.loading).toBe(false);
        expect(mocks.close).toHaveBeenCalledExactlyOnceWith("A");
    });

    it("keeps B alive when A completes after a target change", async () => {
        const a = deferred<ReturnType<typeof session>>();
        const b = deferred<ReturnType<typeof session>>();
        mocks.load.mockReturnValueOnce(a.promise).mockReturnValueOnce(b.promise);
        const { result, rerender, unmount } = renderHook(
            ({ path }) => useBodyShapeSession(path, mocks.reset),
            { initialProps: { path: "A" } },
        );
        rerender({ path: "B" });
        await act(async () => {
            b.resolve(session("B", ["mesh"]));
        });
        await act(async () => {
            a.resolve(session("A"));
        });
        expect(result.current.loaded?.sessionId).toBe("B");
        expect(result.current.loaded?.cache.has("mesh")).toBe(true);
        expect(mocks.close.mock.calls).toEqual([["A"]]);
        expect(mocks.reset).toHaveBeenCalledTimes(1);
        unmount();
        expect(mocks.close.mock.calls).toEqual([["A"], ["B"]]);
    });
    it.each([false, true])(
        "does not clear B loading or report stale A completion (failure=%s)",
        async (failure) => {
            const a = deferred<ReturnType<typeof session>>();
            const b = deferred<ReturnType<typeof session>>();
            mocks.load.mockReturnValueOnce(a.promise).mockReturnValueOnce(b.promise);
            const { result, rerender } = renderHook(
                ({ path }) => useBodyShapeSession(path, mocks.reset),
                { initialProps: { path: "A" } },
            );
            rerender({ path: "B" });
            await act(async () => {
                if (failure) a.reject(new Error("old"));
                else a.resolve(session("A"));
            });
            expect(result.current.loading).toBe(true);
            expect(mocks.error).not.toHaveBeenCalled();
            await act(async () => {
                b.resolve(session("B"));
            });
            expect(result.current.loading).toBe(false);
            expect(result.current.loaded?.sessionId).toBe("B");
        },
    );
    it("closes a session that arrives after unmount", async () => {
        const pending = deferred<ReturnType<typeof session>>();
        mocks.load.mockReturnValue(pending.promise);
        const { unmount } = renderHook(() => useBodyShapeSession("A", mocks.reset));
        unmount();
        await act(async () => {
            pending.resolve(session("A"));
        });
        expect(mocks.close).toHaveBeenCalledExactlyOnceWith("A");
        expect(mocks.reset).not.toHaveBeenCalled();
    });
    it("creates a fresh worker for StrictMode setup after cleanup", async () => {
        const a = deferred<ReturnType<typeof session>>();
        const b = deferred<ReturnType<typeof session>>();
        mocks.load.mockReturnValueOnce(a.promise).mockReturnValueOnce(b.promise);
        const { result } = renderHook(() => useBodyShapeSession("A", mocks.reset), {
            reactStrictMode: true,
        });
        await act(async () => {
            b.resolve(session("active", ["mesh"]));
            a.resolve(session("old"));
        });
        expect(mocks.workers.map((worker) => worker.disposed)).toEqual([true, false]);
        expect(result.current.loaded?.cache.has("mesh")).toBe(true);
        expect(mocks.error).not.toHaveBeenCalled();
    });
    it("cancels an uncached selection when selecting a cached mesh", async () => {
        mocks.load.mockResolvedValue(session("A", ["cached", "slow"]));
        const { result } = renderHook(() => useBodyShapeSession("A", mocks.reset));
        await act(async () => {});
        const slow = deferred<ReturnType<typeof descriptor>>();
        mocks.mesh.mockReturnValueOnce(slow.promise);
        let request!: Promise<void>;
        act(() => {
            request = result.current.loadMeshById(result.current.loaded!, "slow");
        });
        await act(() => result.current.loadMeshById(result.current.loaded!, "cached"));
        expect(slow.promise.cancel).toHaveBeenCalledTimes(1);
        await act(async () => {
            slow.resolve(descriptor("A", "slow"));
            await request;
        });
        expect(result.current.selectedMeshId).toBe("cached");
        expect(result.current.loading).toBe(false);
        expect(result.current.loaded?.cache.has("slow")).toBe(false);
    });
    it("reports a current failure and allows retry", async () => {
        mocks.load
            .mockRejectedValueOnce(new Error("failed"))
            .mockResolvedValueOnce(session("retry"));
        const { result } = renderHook(() => useBodyShapeSession("A", mocks.reset));
        await act(async () => {});
        expect(mocks.error).toHaveBeenCalledTimes(1);
        expect(result.current.loading).toBe(false);
        await act(() => result.current.loadMod("A"));
        expect(result.current.loaded?.sessionId).toBe("retry");
    });
});
