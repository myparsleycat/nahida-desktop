// @vitest-environment jsdom
import { Tools } from "@bindings/tools";
import { act, cleanup, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";

import {
    INEFFECTIVE_VALUES_DEBOUNCE_MS,
    useModelViewerIneffectiveValues,
} from "./model-viewer-ineffective";

vi.mock("@bindings/tools", () => ({ Tools: { GetModelViewerIneffectiveValues: vi.fn() } }));
vi.mock("@renderer/lib/logger", () => ({ Logger: { capture: vi.fn() } }));
beforeEach(() => {
    vi.useFakeTimers();
});
afterEach(() => {
    cleanup();
    vi.clearAllMocks();
    vi.useRealTimers();
});

function pending() {
    type Result = Awaited<ReturnType<typeof Tools.GetModelViewerIneffectiveValues>>;
    let resolve!: (result: Result) => void;
    const promise = new Promise<Result>((done) => {
        resolve = done;
    });
    const cancel = vi.fn(async () => {});
    return {
        resolve,
        cancel,
        promise: Object.assign(promise, { cancel }) as ReturnType<
            typeof Tools.GetModelViewerIneffectiveValues
        >,
    };
}

function settle() {
    act(() => {
        vi.advanceTimersByTime(INEFFECTIVE_VALUES_DEBOUNCE_MS);
    });
}

it("ignores stale responses after state changes and cancels work on unmount", async () => {
    const first = pending();
    const second = pending();
    vi.mocked(Tools.GetModelViewerIneffectiveValues)
        .mockReturnValueOnce(first.promise)
        .mockReturnValueOnce(second.promise);
    const hook = renderHook(({ state }) => useModelViewerIneffectiveValues("session", state), {
        initialProps: { state: { outfit: 0 } },
    });
    settle();
    hook.rerender({ state: { outfit: 1 } });
    expect(first.cancel).toHaveBeenCalledOnce();
    settle();
    await act(async () =>
        second.resolve([{ variableId: "skirt", value: "1", blockingVars: [], suggestions: [] }]),
    );
    expect(hook.result.current.get("skirt")?.has("1")).toBe(true);
    await act(async () =>
        first.resolve([{ variableId: "old", value: "0", blockingVars: [], suggestions: [] }]),
    );
    expect(hook.result.current.has("old")).toBe(false);
    expect(hook.result.current.get("skirt")?.has("1")).toBe(true);
    hook.unmount();
    expect(second.cancel).toHaveBeenCalledOnce();
});

it("coalesces rapid state changes into one backend request", () => {
    vi.mocked(Tools.GetModelViewerIneffectiveValues).mockReturnValue(pending().promise);
    const hook = renderHook(({ state }) => useModelViewerIneffectiveValues("session", state), {
        initialProps: { state: { size: 0 } },
    });
    for (let tick = 1; tick <= 5; tick += 1) {
        act(() => {
            vi.advanceTimersByTime(16);
        });
        hook.rerender({ state: { size: tick / 10 } });
    }
    expect(Tools.GetModelViewerIneffectiveValues).not.toHaveBeenCalled();
    settle();
    expect(Tools.GetModelViewerIneffectiveValues).toHaveBeenCalledOnce();
    expect(Tools.GetModelViewerIneffectiveValues).toHaveBeenCalledWith("session", { size: 0.5 });
});

it("does not display another session's results while the new request is pending", async () => {
    const first = pending();
    const second = pending();
    const state = { outfit: 0 };
    vi.mocked(Tools.GetModelViewerIneffectiveValues)
        .mockReturnValueOnce(first.promise)
        .mockReturnValueOnce(second.promise);
    const hook = renderHook(({ session }) => useModelViewerIneffectiveValues(session, state), {
        initialProps: { session: "first" },
    });
    settle();
    await act(async () =>
        first.resolve([{ variableId: "skirt", value: "1", blockingVars: [], suggestions: [] }]),
    );
    expect(hook.result.current.size).toBe(1);
    hook.rerender({ session: "second" });
    expect(hook.result.current.size).toBe(0);
});
