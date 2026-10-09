// @vitest-environment jsdom
import { Tools } from "@bindings/tools";
import type { ModInfo } from "@renderer/types/mod";
import type { ModViewerTransport, ViewerVariable } from "@shared/mod-viewer/types";
import { act, cleanup, render, screen } from "@testing-library/react";
import { createElement } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import {
    createGridModelPreviewRequestKey,
    GridModelPreviewCache,
    GridModelPreviewController,
    GridModelPreviewProvider,
    GridModelPreviewRenderer,
    type PreviewRenderTask,
    resolveGridModelPreviewState,
    useGridModelPreview,
} from "./grid-model-preview";

vi.mock("@bindings/tools", () => ({
    Tools: {
        LoadModGridPreview: vi.fn(),
        CleanupModelViewer: vi.fn(),
    },
}));

vi.mock("@renderer/lib/logger", () => ({ Logger: { capture: vi.fn() } }));

vi.mock("@renderer/hooks/use-settings", () => ({
    useSettings: () => ({
        isLoading: false,
        settings: { enabled: true, ...renderSettings },
    }),
}));

const renderSettings = {
    toneMapping: "neutral" as const,
    environment: "studio" as const,
    exposure: 0.7,
    toonShadows: false,
};

beforeEach(() => {
    vi.clearAllMocks();
    vi.mocked(Tools.LoadModGridPreview).mockReset();
    vi.mocked(Tools.CleanupModelViewer).mockResolvedValue(true);
    let nextUrl = 0;
    Object.defineProperty(URL, "createObjectURL", {
        configurable: true,
        value: vi.fn(() => `blob:test-${nextUrl++}`),
    });
    Object.defineProperty(URL, "revokeObjectURL", {
        configurable: true,
        value: vi.fn(),
    });
});

afterEach(() => {
    cleanup();
    vi.restoreAllMocks();
});

describe("resolveGridModelPreviewState", () => {
    it("applies a single INI current value case-insensitively", () => {
        const transport = makeTransport([
            makeVariable("Outfit", [{ var: "Hat", value: "0" }]),
            makeVariable("Hat"),
        ]);
        const mod = makeMod([
            makeToggle("Character.ini", "$outfit", "1"),
            makeToggle("Character.ini", "$missing", "9"),
        ]);

        expect(resolveGridModelPreviewState(transport, mod)).toEqual({
            Hat: "0",
            Outfit: "1",
        });
    });

    it("keeps same-named variables scoped to their INI", () => {
        const transport = makeTransport([
            makeVariable("First::Swap"),
            makeVariable("Second::Swap"),
        ]);
        const mod = makeMod([
            makeToggle("First.ini", "$swap", "1"),
            makeToggle("Second.ini", "$swap", "2"),
        ]);

        expect(resolveGridModelPreviewState(transport, mod)).toEqual({
            "First::Swap": "1",
            "Second::Swap": "2",
        });
    });

    it("changes the cache key with toggle state and render settings", () => {
        const mod = makeMod([makeToggle("Character.ini", "$outfit", "0")]);
        const first = createGridModelPreviewRequestKey(mod, renderSettings);
        const second = createGridModelPreviewRequestKey(
            makeMod([makeToggle("Character.ini", "$outfit", "1")]),
            renderSettings,
        );

        expect(first).not.toBe(second);
        expect(first).not.toBe(
            createGridModelPreviewRequestKey(mod, { ...renderSettings, exposure: 1 }),
        );
    });
});

describe("GridModelPreviewCache", () => {
    it("evicts and revokes the least recently used entry after 64 images", () => {
        const cache = new GridModelPreviewCache();
        for (let index = 0; index < 65; index++) {
            cache.set(String(index), new Blob([new Uint8Array(1)]));
        }

        expect(cache.get("0")).toBeUndefined();
        expect(cache.get("64")).toBe("blob:test-64");
        expect(URL.revokeObjectURL).toHaveBeenCalledWith("blob:test-0");
    });

    it("evicts by total Blob size and clears remaining URLs", () => {
        const cache = new GridModelPreviewCache();
        for (let index = 0; index < 33; index++) {
            cache.set(String(index), new Blob([new Uint8Array(2 * 1024 * 1024)]));
        }

        expect(cache.get("0")).toBeUndefined();
        cache.clear();
        expect(URL.revokeObjectURL).toHaveBeenCalledTimes(33);
    });
});

describe("GridModelPreviewController", () => {
    it("reuses a rendered image without loading a model again", async () => {
        vi.mocked(Tools.LoadModGridPreview).mockResolvedValue(rawTransport("first"));
        const renderTask = vi.fn<(task: PreviewRenderTask | null) => void>();
        const controller = new GridModelPreviewController(renderSettings, renderTask);
        const mod = makeMod([]);
        const firstListener = vi.fn();
        controller.subscribe(mod, firstListener);
        await vi.waitFor(() => expect(renderTask).toHaveBeenCalledTimes(1));
        await controller.complete(
            renderTask.mock.calls[0][0]!,
            new Blob(["png"], { type: "image/png" }),
        );
        expect(firstListener).toHaveBeenLastCalledWith({
            status: "ready",
            url: "blob:test-0",
        });

        const nextListener = vi.fn();
        controller.subscribe(mod, nextListener);
        expect(nextListener).toHaveBeenCalledWith({
            status: "ready",
            url: "blob:test-0",
        });
        expect(Tools.LoadModGridPreview).toHaveBeenCalledTimes(1);
        controller.dispose();
    });

    it("rerenders when the mod mtime changes", async () => {
        vi.mocked(Tools.LoadModGridPreview).mockResolvedValue(rawTransport("model"));
        const renderTask = vi.fn<(task: PreviewRenderTask | null) => void>();
        const controller = new GridModelPreviewController(renderSettings, renderTask);
        const mod = makeMod([]);
        controller.subscribe(mod, vi.fn());
        await vi.waitFor(() => expect(renderTask).toHaveBeenCalledTimes(1));
        await controller.complete(renderTask.mock.calls[0][0]!, new Blob(["png"]));

        controller.subscribe({ ...mod, mtime: 2 }, vi.fn());
        await vi.waitFor(() => expect(Tools.LoadModGridPreview).toHaveBeenCalledTimes(2));
        controller.dispose();
    });

    it("deduplicates requests and runs model loads serially", async () => {
        const first = deferred<ReturnType<typeof rawTransport>>();
        vi.mocked(Tools.LoadModGridPreview)
            .mockImplementationOnce(
                () => first.promise as ReturnType<typeof Tools.LoadModGridPreview>,
            )
            .mockResolvedValueOnce(rawTransport("second"));
        const renderTask = vi.fn<(task: PreviewRenderTask | null) => void>();
        const controller = new GridModelPreviewController(renderSettings, renderTask);
        const firstMod = makeMod([makeToggle("Character.ini", "$outfit", "0")]);
        const secondMod = { ...firstMod, path: "C:/Mods/Second", id: "second" };

        controller.subscribe(firstMod, vi.fn());
        controller.subscribe(firstMod, vi.fn());
        controller.subscribe(secondMod, vi.fn());
        await vi.waitFor(() => expect(Tools.LoadModGridPreview).toHaveBeenCalledTimes(1));

        first.resolve(rawTransport("first"));
        await vi.waitFor(() => expect(renderTask).toHaveBeenCalledTimes(1));
        expect(Tools.LoadModGridPreview).toHaveBeenCalledTimes(1);

        const task = renderTask.mock.calls[0][0];
        expect(task).not.toBeNull();
        await controller.complete(task!, new Blob([new Uint8Array(1)]));
        expect(task!.timeout).toBeNull();
        await vi.waitFor(() => expect(Tools.LoadModGridPreview).toHaveBeenCalledTimes(2));
        expect(Tools.CleanupModelViewer).toHaveBeenCalledWith("first");
        controller.dispose();
    });

    it("returns an in-memory cached image immediately while another preview is still rendering", async () => {
        const slow = deferred<ReturnType<typeof rawTransport>>();
        vi.mocked(Tools.LoadModGridPreview)
            .mockResolvedValueOnce(rawTransport("cached-model"))
            .mockImplementationOnce(
                () => slow.promise as ReturnType<typeof Tools.LoadModGridPreview>,
            );

        const renderTask = vi.fn<(task: PreviewRenderTask | null) => void>();
        const controller = new GridModelPreviewController(renderSettings, renderTask);
        const cachedMod = { ...makeMod([]), path: "C:/Mods/Cached", id: "cached" };
        controller.subscribe(cachedMod, vi.fn());
        await vi.waitFor(() => expect(renderTask).toHaveBeenCalledTimes(1));
        await controller.complete(renderTask.mock.calls[0][0]!, new Blob(["png"]));

        const slowMod = { ...makeMod([]), path: "C:/Mods/Slow", id: "slow" };
        controller.subscribe(slowMod, vi.fn());
        await vi.waitFor(() => expect(Tools.LoadModGridPreview).toHaveBeenCalledTimes(2));

        const cachedListener = vi.fn();
        controller.subscribe(cachedMod, cachedListener);
        expect(cachedListener).toHaveBeenCalledWith({
            status: "ready",
            url: "blob:test-0",
        });

        slow.resolve(rawTransport("slow-model"));
        controller.dispose();
    });

    it("negative-caches a failed request", async () => {
        vi.mocked(Tools.LoadModGridPreview).mockRejectedValueOnce(new Error("broken"));
        const controller = new GridModelPreviewController(renderSettings, vi.fn());
        const mod = makeMod([makeToggle("Character.ini", "$outfit", "0")]);
        const firstListener = vi.fn();

        controller.subscribe(mod, firstListener);
        await vi.waitFor(() =>
            expect(firstListener).toHaveBeenLastCalledWith({ status: "unavailable" }),
        );
        controller.subscribe(mod, vi.fn());

        expect(Tools.LoadModGridPreview).toHaveBeenCalledTimes(1);
        controller.dispose();
    });

    it("cleans up a session that arrives after disposal without rendering it", async () => {
        const loaded = deferred<ReturnType<typeof rawTransport>>();
        vi.mocked(Tools.LoadModGridPreview).mockImplementationOnce(
            () => loaded.promise as ReturnType<typeof Tools.LoadModGridPreview>,
        );
        const renderTask = vi.fn<(task: PreviewRenderTask | null) => void>();
        const controller = new GridModelPreviewController(renderSettings, renderTask);

        controller.subscribe(makeMod([]), vi.fn());
        await vi.waitFor(() => expect(Tools.LoadModGridPreview).toHaveBeenCalledTimes(1));
        controller.dispose();
        loaded.resolve(rawTransport("late"));

        await vi.waitFor(() => expect(Tools.CleanupModelViewer).toHaveBeenCalledWith("late"));
        expect(renderTask).not.toHaveBeenCalledWith(expect.objectContaining({ sessionId: "late" }));
    });

    it("cleans up and negative-caches a capture failure", async () => {
        vi.mocked(Tools.LoadModGridPreview).mockResolvedValueOnce(rawTransport("capture-failure"));
        const renderTask = vi.fn<(task: PreviewRenderTask | null) => void>();
        const controller = new GridModelPreviewController(renderSettings, renderTask);
        const mod = makeMod([]);
        const listener = vi.fn();

        controller.subscribe(mod, listener);
        await vi.waitFor(() => expect(renderTask).toHaveBeenCalledTimes(1));
        const task = renderTask.mock.calls[0][0];
        await controller.complete(task!, null, new Error("capture failed"));
        controller.subscribe(mod, vi.fn());

        expect(Tools.CleanupModelViewer).toHaveBeenCalledWith("capture-failure");
        expect(Tools.LoadModGridPreview).toHaveBeenCalledTimes(1);
        expect(listener).toHaveBeenLastCalledWith({ status: "unavailable" });
        controller.dispose();
    });

    it("times out a stalled render and starts the next preview", async () => {
        vi.useFakeTimers();
        try {
            vi.mocked(Tools.LoadModGridPreview)
                .mockResolvedValueOnce(rawTransport("stalled"))
                .mockResolvedValueOnce(rawTransport("next"));
            const renderTask = vi.fn<(task: PreviewRenderTask | null) => void>();
            const controller = new GridModelPreviewController(renderSettings, renderTask);
            const firstMod = makeMod([]);
            const secondMod = { ...firstMod, path: "C:/Mods/Second", id: "second" };

            controller.subscribe(firstMod, vi.fn());
            controller.subscribe(secondMod, vi.fn());
            await vi.advanceTimersByTimeAsync(0);
            expect(renderTask).toHaveBeenCalledTimes(1);

            await vi.advanceTimersByTimeAsync(30_000);
            expect(Tools.CleanupModelViewer).toHaveBeenCalledWith("stalled");
            expect(Tools.LoadModGridPreview).toHaveBeenCalledTimes(2);
            expect(renderTask).toHaveBeenLastCalledWith(
                expect.objectContaining({ sessionId: "next" }),
            );
            const nextTask = renderTask.mock.calls.at(-1)?.[0];
            controller.dispose();
            expect(nextTask?.timeout).toBeNull();
        } finally {
            vi.useRealTimers();
        }
    });
});

describe("GridModelPreviewProvider", () => {
    it("rerenders unchanged metadata after a source refresh", async () => {
        vi.mocked(Tools.LoadModGridPreview)
            .mockResolvedValueOnce(rawTransport("before-refresh"))
            .mockResolvedValueOnce(rawTransport("after-refresh"));
        vi.spyOn(GridModelPreviewRenderer.prototype, "render").mockResolvedValue(new Blob(["png"]));
        const mod = makeMod([]);
        const props = {
            viewport: null,
            sourceRevision: 1,
            children: createElement(PreviewState, { mod }),
        };
        const view = render(createElement(GridModelPreviewProvider, props));
        await vi.waitFor(() =>
            expect(screen.getByTestId("preview").textContent).toBe("blob:test-0"),
        );

        view.rerender(createElement(GridModelPreviewProvider, { ...props }));
        expect(Tools.LoadModGridPreview).toHaveBeenCalledTimes(1);

        props.sourceRevision = 2;
        view.rerender(createElement(GridModelPreviewProvider, props));
        await vi.waitFor(() =>
            expect(screen.getByTestId("preview").textContent).toBe("blob:test-1"),
        );
        expect(Tools.LoadModGridPreview).toHaveBeenCalledTimes(2);
        expect(URL.revokeObjectURL).toHaveBeenCalledWith("blob:test-0");
    });

    it("discards a model load that finishes after a source refresh", async () => {
        const oldLoad = deferred<ReturnType<typeof rawTransport>>();
        vi.mocked(Tools.LoadModGridPreview)
            .mockImplementationOnce(
                () => oldLoad.promise as ReturnType<typeof Tools.LoadModGridPreview>,
            )
            .mockResolvedValueOnce(rawTransport("after-refresh"));
        const renderPreview = vi
            .spyOn(GridModelPreviewRenderer.prototype, "render")
            .mockResolvedValue(new Blob(["png"]));
        const props = {
            viewport: null,
            sourceRevision: 1,
            children: createElement(PreviewState, { mod: makeMod([]) }),
        };
        const view = render(createElement(GridModelPreviewProvider, props));
        await vi.waitFor(() => expect(Tools.LoadModGridPreview).toHaveBeenCalledTimes(1));

        props.sourceRevision = 2;
        view.rerender(createElement(GridModelPreviewProvider, props));
        await vi.waitFor(() =>
            expect(screen.getByTestId("preview").textContent).toBe("blob:test-0"),
        );
        oldLoad.resolve(rawTransport("before-refresh"));
        await vi.waitFor(() =>
            expect(Tools.CleanupModelViewer).toHaveBeenCalledWith("before-refresh"),
        );
        expect(renderPreview).toHaveBeenCalledTimes(1);
        expect(renderPreview).toHaveBeenCalledWith(
            expect.objectContaining({ sessionId: "after-refresh" }),
        );
        expect(screen.getByTestId("preview").textContent).toBe("blob:test-0");
    });

    it("discards a render that finishes after a source refresh", async () => {
        vi.mocked(Tools.LoadModGridPreview)
            .mockResolvedValueOnce(rawTransport("before-refresh"))
            .mockResolvedValueOnce(rawTransport("after-refresh"));
        const oldRender = deferred<Blob>();
        const renderPreview = vi
            .spyOn(GridModelPreviewRenderer.prototype, "render")
            .mockImplementationOnce(() => oldRender.promise)
            .mockResolvedValueOnce(new Blob(["new"]));
        const props = {
            viewport: null,
            sourceRevision: 1,
            children: createElement(PreviewState, { mod: makeMod([]) }),
        };
        const view = render(createElement(GridModelPreviewProvider, props));
        await vi.waitFor(() => expect(renderPreview).toHaveBeenCalledTimes(1));

        props.sourceRevision = 2;
        view.rerender(createElement(GridModelPreviewProvider, props));
        await vi.waitFor(() =>
            expect(screen.getByTestId("preview").textContent).toBe("blob:test-0"),
        );
        await act(async () => oldRender.resolve(new Blob(["old"])));
        expect(screen.getByTestId("preview").textContent).toBe("blob:test-0");
        expect(Tools.CleanupModelViewer).toHaveBeenCalledWith("before-refresh");
        expect(URL.createObjectURL).toHaveBeenCalledTimes(1);
    });
});

function PreviewState({ mod }: { mod: ModInfo }) {
    const [ref, , state] = useGridModelPreview(mod, true);
    return createElement(
        "div",
        { ref, "data-testid": "preview" },
        state.status === "ready" ? state.url : state.status,
    );
}

function makeVariable(id: string, effects: ViewerVariable["effects"] = []): ViewerVariable {
    return {
        id,
        label: id,
        defaultValue: "0",
        values: [],
        order: 0,
        effects,
    };
}

function makeTransport(variables: ViewerVariable[]): ModViewerTransport {
    return {
        memorySessionId: "session",
        iniPath: "Character.ini",
        modPath: "C:/Mods/Test",
        name: "Test",
        meshes: [],
        textures: {},
        variables,
        defaultState: {},
        stateRules: [],
        uiAssets: {},
        animations: [],
        computeDeformers: [],
    };
}

function makeMod(toggles: ModInfo["inis"][number]["toggleKeys"]): ModInfo {
    return {
        id: "mod",
        name: "Test",
        path: "C:/Mods/Test",
        isEnabled: true,
        mtime: 1,
        size: 1,
        inis: [{ name: "Character.ini", path: "C:/Mods/Test/Character.ini", toggleKeys: toggles }],
    };
}

function makeToggle(iniFileName: string, variable: string, currentValue: string) {
    return {
        sectionName: "KeySwap",
        iniFileName,
        variable,
        values: ["0", "1"],
        currentValue,
    };
}

function rawTransport(sessionId: string) {
    return {
        memorySessionId: sessionId,
        iniPath: "C:/Mods/Test/Character.ini",
        modPath: "C:/Mods/Test",
        name: "Test",
        meshes: [],
        textures: {},
        variables: [],
        defaultState: {},
        stateRules: [],
        uiAssets: {},
        animations: [],
        computeDeformers: [],
    } as Awaited<ReturnType<typeof Tools.LoadModGridPreview>>;
}

function deferred<T>() {
    let resolve!: (value: T) => void;
    const promise = new Promise<T>((resolvePromise) => {
        resolve = resolvePromise;
    });
    return { promise, resolve };
}
