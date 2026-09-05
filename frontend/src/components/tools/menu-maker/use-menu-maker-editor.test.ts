// @vitest-environment jsdom
import type { MenuMakerSource } from "@bindings/menumaker";
import { type MenuMakerDraftMeta, saveDraftMetadata } from "@shared/menu-maker/drafts";
import { slotSignature } from "@shared/menu-maker/parser";
import { DEFAULT_MENU_MAKER_SETTINGS, emptyMenuMakerGeometry } from "@shared/menu-maker/types";
import { act, cleanup, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { reducer, type EditorState } from "./menu-maker-editor-state";
import { useMenuMakerEditor } from "./use-menu-maker-editor";

const mocks = vi.hoisted(() => ({
    load: vi.fn(),
    generate: vi.fn(),
    parse: vi.fn(),
    saveINI: vi.fn(),
    saveFile: vi.fn(),
    loadBlobs: vi.fn(),
    saveBlobs: vi.fn(),
    deleteBlobs: vi.fn(),
    error: vi.fn(),
    t: (key: string) => key,
}));
vi.mock("@bindings/menumaker", () => ({
    MenuMaker: {
        LoadSource: mocks.load,
        Generate: mocks.generate,
        Parse: mocks.parse,
        SaveINI: mocks.saveINI,
    },
}));
vi.mock("@bindings/platform", () => ({ Dialog: { SaveFile: mocks.saveFile } }));
vi.mock("@renderer/lib/logger", () => ({ Logger: { error: vi.fn(), capture: vi.fn() } }));
vi.mock("@shared/menu-maker/resources", () => ({ renderMenuMakerAssets: vi.fn() }));
vi.mock("react-i18next", () => ({ useTranslation: () => ({ t: mocks.t }) }));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: mocks.error } }));
vi.mock("@shared/menu-maker/drafts", async (importOriginal) => ({
    ...(await importOriginal<typeof import("@shared/menu-maker/drafts")>()),
    loadDraftBlobs: mocks.loadBlobs,
    saveDraftBlobs: mocks.saveBlobs,
    deleteDraftBlobs: mocks.deleteBlobs,
}));

const source: MenuMakerSource = {
    path: "C:\\mods\\test.ini",
    fileName: "test.ini",
    text: "[KeyTest]\nkey = 5\n",
    sha256: "source-hash",
    encoding: "utf8",
    hasBOM: true,
    newline: "crlf",
    document: { text: "[KeyTest]\nkey = 5\n", sections: [], handlers: [], slots: [] },
};
const generated = {
    iniText: "generated",
    geometry: emptyMenuMakerGeometry(),
    slotStates: [],
    assetPaths: [],
};
const metadata: MenuMakerDraftMeta = {
    id: `source-hash:${slotSignature([])}`,
    sourcePath: source.path,
    sourceName: source.fileName,
    sourceSHA256: source.sha256,
    slotSignature: slotSignature([]),
    updatedAt: 1,
    sourceEncoding: source.encoding,
    sourceHasBOM: source.hasBOM,
    sourceNewline: source.newline,
    settings: { ...DEFAULT_MENU_MAKER_SETTINGS, title: "Restored" },
    slots: [],
};
beforeEach(() => {
    vi.clearAllMocks();
    const storage = new Map<string, string>();
    vi.stubGlobal("localStorage", {
        getItem: (key: string) => storage.get(key) ?? null,
        setItem: (key: string, value: string) => storage.set(key, value),
        removeItem: (key: string) => storage.delete(key),
        clear: () => storage.clear(),
    });
    mocks.load.mockResolvedValue(source);
    mocks.generate.mockResolvedValue(generated);
    mocks.parse.mockResolvedValue(source.document);
    mocks.loadBlobs.mockResolvedValue(undefined);
    mocks.saveBlobs.mockResolvedValue(undefined);
    mocks.deleteBlobs.mockResolvedValue(undefined);
    mocks.saveINI.mockResolvedValue({});
    mocks.saveFile.mockResolvedValue({ canceled: false, filePath: "C:\\output.ini" });
});
afterEach(() => {
    cleanup();
    vi.useRealTimers();
});
const onSourceLoaded = vi.fn();
const onDraftRestored = vi.fn();
function useEditor() {
    return useMenuMakerEditor({ path: "", ini: "", onSourceLoaded, onDraftRestored });
}

describe("Menu Maker editor boundaries", () => {
    it("keeps source/busy updates separate from edits that change the document revision", () => {
        const initial: EditorState = {
            slots: [],
            settings: DEFAULT_MENU_MAKER_SETTINGS,
            sourceAvailable: false,
            busy: false,
            revision: 0,
        };
        const loaded = reducer(initial, { type: "load", source, document: source.document });
        const busy = reducer(loaded, { type: "busy", value: true });
        const edited = reducer(busy, { type: "settings", value: { title: "New" } });
        const recolored = reducer(edited, { type: "palette", key: "accent", value: "#123456" });
        expect(loaded.revision).toBe(1);
        expect(busy.revision).toBe(1);
        expect(edited.revision).toBe(2);
        expect(recolored.revision).toBe(3);
        expect(initial.settings.title).toBe(DEFAULT_MENU_MAKER_SETTINGS.title);
        expect(recolored.settings.palette).toEqual({
            ...DEFAULT_MENU_MAKER_SETTINGS.palette,
            accent: "#123456",
        });
        const written = reducer(recolored, {
            type: "sourceContent",
            text: "new source",
            sha256: "new-hash",
        });
        expect(written.revision).toBe(3);
        expect(written.source?.sha256).toBe("new-hash");
        expect(source.sha256).toBe("source-hash");
    });
    it("restores matching source drafts and their media using the existing storage identity", async () => {
        saveDraftMetadata([metadata]);
        mocks.loadBlobs.mockResolvedValue({
            panelImageDataUrl: "data:image/png;base64,AA",
            originalText: source.text,
        });
        const hook = renderHook(useEditor);
        await act(async () => hook.result.current.loadSource(source.path));
        expect(mocks.loadBlobs).toHaveBeenCalledWith(metadata.id);
        expect(hook.result.current.state.settings.title).toBe("Restored");
        expect(hook.result.current.state.settings.panelImageDataUrl).toBe(
            "data:image/png;base64,AA",
        );
        expect(onSourceLoaded).toHaveBeenCalledOnce();
        expect(hook.result.current.state.sourceAvailable).toBe(true);
    });
    it("persists at 500ms and on unload/unmount without storing inline panel media in settings", async () => {
        vi.useFakeTimers();
        const hook = renderHook(useEditor);
        await act(async () => hook.result.current.loadSource(source.path));
        mocks.saveBlobs.mockClear();
        await act(async () => vi.advanceTimersByTimeAsync(499));
        expect(mocks.saveBlobs).not.toHaveBeenCalled();
        await act(async () => vi.advanceTimersByTimeAsync(1));
        expect(mocks.saveBlobs).toHaveBeenCalledTimes(1);
        const saved = JSON.parse(localStorage.getItem("nahida.menu-maker.drafts") ?? "[]");
        expect(saved[0]).toMatchObject({
            id: metadata.id,
            sourceEncoding: "utf8",
            sourceHasBOM: true,
            sourceNewline: "crlf",
        });
        await act(async () => {
            window.dispatchEvent(new Event("beforeunload"));
        });
        expect(mocks.saveBlobs).toHaveBeenCalledTimes(2);
        hook.unmount();
        expect(mocks.saveBlobs).toHaveBeenCalledTimes(3);
        window.dispatchEvent(new Event("beforeunload"));
        expect(mocks.saveBlobs).toHaveBeenCalledTimes(3);
        expect(
            JSON.parse(localStorage.getItem("nahida.menu-maker.settings") ?? "{}"),
        ).not.toHaveProperty("panelImageDataUrl");
    });
    it("restores a draft without its source file while preserving source availability", async () => {
        mocks.loadBlobs.mockResolvedValue({ originalText: source.text });
        const hook = renderHook(useEditor);
        await act(async () => hook.result.current.restoreDraft(metadata));
        expect(mocks.parse).toHaveBeenCalledWith(source.text);
        expect(onDraftRestored).toHaveBeenCalledOnce();
        expect(hook.result.current.state.sourceAvailable).toBe(false);
        expect(hook.result.current.state.source).toMatchObject({
            text: source.text,
            encoding: "utf8",
            hasBOM: true,
            newline: "crlf",
        });
    });
    it("keeps the editor unchanged when restoring a draft with missing original text", async () => {
        const hook = renderHook(useEditor);
        await act(async () => hook.result.current.restoreDraft(metadata));
        expect(hook.result.current.state.source).toBeUndefined();
        expect(onDraftRestored).not.toHaveBeenCalled();
        expect(mocks.error).toHaveBeenCalledWith("page.tools.menu_maker.draft_restore_failed");
    });
    it("ignores a stale generated preview after newer edits", async () => {
        let resolveFirst!: (value: typeof generated) => void;
        const pending = new Promise<typeof generated>((resolve) => {
            resolveFirst = resolve;
        });
        mocks.generate.mockReturnValueOnce(pending);
        const hook = renderHook(useEditor);
        await act(async () => hook.result.current.loadSource(source.path));
        mocks.generate.mockResolvedValue({ ...generated, iniText: "latest" });
        await act(async () =>
            hook.result.current.dispatch({ type: "settings", value: { title: "Latest" } }),
        );
        expect(hook.result.current.preview?.iniText).toBe("latest");
        await act(async () => resolveFirst({ ...generated, iniText: "stale" }));
        expect(hook.result.current.preview?.iniText).toBe("latest");
    });
    it("preserves the save request's source encoding and line endings", async () => {
        const hook = renderHook(useEditor);
        await act(async () => hook.result.current.loadSource(source.path));
        await act(async () => hook.result.current.saveINI());
        expect(mocks.saveINI).toHaveBeenCalledWith({
            destinationPath: "C:\\output.ini",
            sourceText: source.text,
            slots: [],
            settings: hook.result.current.state.settings,
            encoding: "utf8",
            hasBOM: true,
            newline: "crlf",
        });
        expect(hook.result.current.state.busy).toBe(false);
    });
});
