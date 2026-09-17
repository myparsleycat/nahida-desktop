import { Tools } from "@bindings/tools";
import type { ModInfo } from "@renderer/types/mod";
import type { ModViewerTransport, ViewerVariable } from "@shared/mod-viewer/types";
import { beforeEach, describe, expect, it, vi } from "vitest";

import {
    createGridModelPreviewFinalKey,
    GridModelPreviewCache,
    GridModelPreviewController,
    type PreviewRenderTask,
    resolveGridModelPreviewState,
} from "./grid-model-preview";

vi.mock("@bindings/tools", () => ({
    Tools: {
        LoadModGridPreview: vi.fn(),
        CleanupModelViewer: vi.fn(),
    },
}));

const renderSettings = {
    toneMapping: "neutral" as const,
    environment: "studio" as const,
    exposure: 0.7,
    toonShadows: false,
};

beforeEach(() => {
    vi.clearAllMocks();
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

    it("changes the final cache key with resolved state", () => {
        const mod = makeMod([makeToggle("Character.ini", "$outfit", "0")]);
        const first = createGridModelPreviewFinalKey(mod, { outfit: "0" }, renderSettings);
        const second = createGridModelPreviewFinalKey(mod, { outfit: "1" }, renderSettings);

        expect(first).not.toBe(second);
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
    it("deduplicates requests and runs model loads serially", async () => {
        const first = deferred<ReturnType<typeof rawTransport>>();
        vi.mocked(Tools.LoadModGridPreview)
            .mockImplementationOnce(
                () => first.promise as ReturnType<typeof Tools.LoadModGridPreview>,
            )
            .mockResolvedValueOnce(rawTransport("second"));
        const renderTask = vi.fn<(task: PreviewRenderTask | null) => void>();
        const controller = new GridModelPreviewController(1, renderSettings, renderTask);
        const firstMod = makeMod([makeToggle("Character.ini", "$outfit", "0")]);
        const secondMod = { ...firstMod, path: "C:/Mods/Second", id: "second" };

        controller.subscribe(firstMod, vi.fn());
        controller.subscribe(firstMod, vi.fn());
        controller.subscribe(secondMod, vi.fn());
        expect(Tools.LoadModGridPreview).toHaveBeenCalledTimes(1);

        first.resolve(rawTransport("first"));
        await vi.waitFor(() => expect(renderTask).toHaveBeenCalledTimes(1));
        expect(Tools.LoadModGridPreview).toHaveBeenCalledTimes(1);

        const task = renderTask.mock.calls[0][0];
        expect(task).not.toBeNull();
        await controller.complete(task!, new Blob([new Uint8Array(1)]));
        await vi.waitFor(() => expect(Tools.LoadModGridPreview).toHaveBeenCalledTimes(2));
        expect(Tools.CleanupModelViewer).toHaveBeenCalledWith("first");
        controller.dispose();
    });

    it("negative-caches a failed request", async () => {
        vi.mocked(Tools.LoadModGridPreview).mockRejectedValueOnce(new Error("broken"));
        const controller = new GridModelPreviewController(1, renderSettings, vi.fn());
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
        const controller = new GridModelPreviewController(1, renderSettings, renderTask);

        controller.subscribe(makeMod([]), vi.fn());
        controller.dispose();
        loaded.resolve(rawTransport("late"));

        await vi.waitFor(() => expect(Tools.CleanupModelViewer).toHaveBeenCalledWith("late"));
        expect(renderTask).not.toHaveBeenCalledWith(expect.objectContaining({ sessionId: "late" }));
    });

    it("cleans up and negative-caches a capture failure", async () => {
        vi.mocked(Tools.LoadModGridPreview).mockResolvedValueOnce(rawTransport("capture-failure"));
        const renderTask = vi.fn<(task: PreviewRenderTask | null) => void>();
        const controller = new GridModelPreviewController(1, renderSettings, renderTask);
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
});

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
