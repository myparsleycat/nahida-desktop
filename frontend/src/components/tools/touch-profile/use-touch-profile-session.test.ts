import { createDefaultTouchZoneSettings } from "@shared/touch-profile-settings";
// @vitest-environment jsdom
import { act, cleanup, render, renderHook, waitFor } from "@testing-library/react";
import { createElement } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { TouchProfileSelection } from "./touch-profile-selection";
import { useTouchProfileControls } from "./use-touch-profile-controls";
import { useTouchProfileSession } from "./use-touch-profile-session";

const mocks = vi.hoisted(() => ({
    prepare: vi.fn(),
    close: vi.fn(),
    analyze: vi.fn(),
    apply: vi.fn(),
    regenerate: vi.fn(),
    rollback: vi.fn(),
    save: vi.fn(),
    mesh: vi.fn(),
    preview: vi.fn(),
    loadMesh: vi.fn(),
    loadPreview: vi.fn(),
    error: vi.fn(),
    t: (key: string) => key,
}));
vi.mock("@bindings/tools", () => ({
    Tools: {
        TouchProfilePrepare: mocks.prepare,
        TouchProfileCloseSession: mocks.close,
        TouchProfileAnalyzeComponents: mocks.analyze,
        TouchProfileApply: mocks.apply,
        TouchProfileRegenerate: mocks.regenerate,
        TouchProfileRollback: mocks.rollback,
        TouchProfileUpdateZoneSettingsBatch: mocks.save,
        TouchProfileGetMeshDescriptor: mocks.mesh,
        TouchProfileGetPreviewDescriptor: mocks.preview,
    },
}));
vi.mock("@bindings/platform", () => ({ Dialog: {}, Shell: {} }));
vi.mock("./touch-profile-payload", () => ({
    loadTouchProfileMesh: mocks.loadMesh,
    loadTouchProfilePreview: mocks.loadPreview,
}));
vi.mock("@renderer/wails/binary-memory", () => ({
    isAbortError: (error: unknown) => error instanceof DOMException && error.name === "AbortError",
}));
vi.mock("react-i18next", () => ({ useTranslation: () => ({ t: mocks.t }) }));
vi.mock("sonner", () => ({ toast: { error: mocks.error, success: vi.fn() } }));

function deferred<T>() {
    let resolve!: (value: T) => void;
    const promise = new Promise<T>((accept) => {
        resolve = accept;
    });
    return { promise, resolve };
}
function inspection(path: string) {
    return {
        sessionId: path,
        modRoot: path,
        supportGrade: "supported",
        supportReasons: [],
        components: [
            {
                id: "mesh",
                name: "mesh",
                kind: "body",
                supportGrade: "supported",
                interactiveCandidate: true,
                vertexCount: 3,
                indexCount: 3,
                hasBlend: true,
                bones: [],
            },
        ],
    };
}
const settings = createDefaultTouchZoneSettings();
const draft = {
    sessionId: "A",
    sourceModRoot: "A",
    canAutoApply: true,
    warnings: [],
    analysis: { supportGrade: "supported" },
    components: [
        {
            componentId: "mesh",
            interactive: true,
            confidence: 1,
            warnings: [],
            hasBlend: true,
            bones: [],
            zones: [
                {
                    id: "zone",
                    settings,
                    label: "Zone",
                    channel: 0,
                    confidence: 1,
                    center: [0, 0, 0],
                    radius: [1, 1, 1],
                    source: "bone",
                },
            ],
        },
    ],
};
beforeEach(() => {
    vi.clearAllMocks();
    mocks.prepare.mockImplementation(({ modPath }: { modPath: string }) =>
        Promise.resolve(inspection(modPath)),
    );
    mocks.close.mockResolvedValue(undefined);
    mocks.analyze.mockResolvedValue(draft);
    mocks.apply.mockResolvedValue({
        sessionId: "A",
        sourceModRoot: "A",
        outputModRoot: "output",
        reenableSourceOnRollback: true,
    });
    mocks.rollback.mockResolvedValue({ sourceModRoot: "A" });
    mocks.save.mockResolvedValue({ previewChanged: true });
    mocks.mesh.mockImplementation(() => Object.assign(Promise.resolve({}), { cancel: vi.fn() }));
    mocks.preview.mockImplementation(() => Object.assign(Promise.resolve({}), { cancel: vi.fn() }));
    mocks.loadMesh.mockResolvedValue({
        sessionId: "A",
        componentId: "mesh",
        vertexCount: 3,
        positions: new Float32Array(9),
        indices: new Uint32Array(3),
        bones: [],
    });
    mocks.loadPreview.mockResolvedValue({
        sessionId: "A",
        componentId: "mesh",
        vertexCount: 3,
        positions: new Float32Array(9),
        indices: new Uint32Array(3),
        zones: [],
    });
});
afterEach(() => {
    cleanup();
    vi.useRealTimers();
});
function useSession(path: string, callbacks = {}) {
    return useTouchProfileSession({
        fixedTargetPath: path,
        onResetSelectionPreview: vi.fn(),
        ...callbacks,
    });
}
describe("Touch Profile session", () => {
    it("hides selection controls during review even while the inspection remains available", async () => {
        const hook = renderHook(() => {
            const session = useSession("A");
            return { session, controls: useTouchProfileControls(session) };
        });
        await waitFor(() => expect(hook.result.current.session.inspection).not.toBeNull());
        const inspection = hook.result.current.session.inspection;
        const view = render(createElement(TouchProfileSelection, hook.result.current));
        expect(view.getByText("page.tools.touch_profile.mesh_select_title")).toBeTruthy();
        expect(
            view.getByRole("button", { name: "page.tools.touch_profile.mesh_next" }),
        ).toBeTruthy();

        await act(async () => hook.result.current.session.analyzeSelected());
        expect(hook.result.current.session.phase).toBe("review");
        expect(hook.result.current.session.inspection).toBe(inspection);
        view.rerender(createElement(TouchProfileSelection, hook.result.current));
        expect(view.queryByText("page.tools.touch_profile.mesh_select_title")).toBeNull();
        expect(
            view.queryByRole("button", { name: "page.tools.touch_profile.mesh_next" }),
        ).toBeNull();

        act(() => hook.result.current.session.backToSelect());
        view.rerender(createElement(TouchProfileSelection, hook.result.current));
        expect(view.getByText("page.tools.touch_profile.mesh_select_title")).toBeTruthy();
        expect(
            view.getByRole("button", { name: "page.tools.touch_profile.mesh_next" }),
        ).toBeTruthy();
    });
    it("keeps the previous mesh visible while the next component preview is pending", async () => {
        mocks.analyze.mockResolvedValue({
            ...draft,
            components: [...draft.components, { ...draft.components[0], componentId: "next" }],
        });
        const hook = renderHook(() => useSession("A"));
        await waitFor(() => expect(hook.result.current.inspection).not.toBeNull());
        await act(async () => hook.result.current.analyzeSelected());
        await waitFor(() => expect(hook.result.current.displayPreview?.componentId).toBe("mesh"));
        const previous = hook.result.current.displayPreview!;
        const pending = deferred<typeof previous>();
        mocks.loadPreview.mockReturnValueOnce(pending.promise);
        await act(async () => hook.result.current.setSelectedComponentId("next"));
        expect(hook.result.current.previewLoading).toBe(true);
        expect(hook.result.current.activePreview).toBeNull();
        expect(hook.result.current.displayPreview).toBe(previous);
        await act(async () => pending.resolve({ ...previous, componentId: "next" }));
        expect(hook.result.current.displayPreview?.componentId).toBe("next");
        expect(hook.result.current.previewLoading).toBe(false);
    });
    it("keeps B's inspection and loading state when A completes after the target changes", async () => {
        const pendingA = deferred<ReturnType<typeof inspection>>();
        const pendingB = deferred<ReturnType<typeof inspection>>();
        mocks.prepare.mockImplementation(({ modPath }: { modPath: string }) =>
            modPath === "A" ? pendingA.promise : pendingB.promise,
        );
        const hook = renderHook(({ path }) => useSession(path), { initialProps: { path: "A" } });
        await waitFor(() => expect(mocks.prepare).toHaveBeenCalledWith({ modPath: "A" }));
        hook.rerender({ path: "B" });
        await waitFor(() => expect(mocks.prepare).toHaveBeenCalledWith({ modPath: "B" }));
        await act(async () => pendingA.resolve(inspection("A")));
        expect(hook.result.current.inspection).toBeNull();
        expect(hook.result.current.loading).toBe(true);
        await act(async () => pendingB.resolve(inspection("B")));
        expect(hook.result.current.inspection?.modRoot).toBe("B");
        expect(hook.result.current.loading).toBe(false);
        hook.unmount();
        expect(mocks.close).toHaveBeenCalledWith("B");
    });
    it("closes the previous session on replacement and cancels previews on unmount", async () => {
        const hook = renderHook(({ path }) => useSession(path), { initialProps: { path: "A" } });
        await waitFor(() => expect(hook.result.current.inspection?.sessionId).toBe("A"));
        hook.rerender({ path: "B" });
        await waitFor(() => expect(hook.result.current.inspection?.sessionId).toBe("B"));
        expect(mocks.close).toHaveBeenCalledWith("A");
        await act(async () => hook.result.current.setSelectedMeshId("mesh"));
        await waitFor(() => expect(mocks.loadMesh).toHaveBeenCalled());
        const descriptor = mocks.mesh.mock.results.at(-1)?.value;
        const signal = mocks.loadMesh.mock.calls.at(-1)?.[1] as AbortSignal;
        hook.unmount();
        expect(descriptor.cancel).toHaveBeenCalled();
        expect(signal.aborted).toBe(true);
        expect(mocks.close).toHaveBeenCalledWith("B");
    });
    it("coalesces setting edits, blocks apply while saving, and preserves apply/rollback callbacks", async () => {
        const onApplied = vi.fn();
        const onRolledBack = vi.fn();
        const hook = renderHook(() => useSession("A", { onApplied, onRolledBack }));
        await waitFor(() => expect(hook.result.current.inspection).not.toBeNull());
        await act(async () => hook.result.current.analyzeSelected());
        expect(hook.result.current.phase).toBe("review");
        await waitFor(() => expect(hook.result.current.previewLoading).toBe(false));
        vi.useFakeTimers();
        act(() => {
            hook.result.current.updateZoneSettings(
                "mesh",
                "zone",
                { ...settings, maskStrength: 1.2 },
                { refreshPreview: true },
            );
            hook.result.current.updateZoneSettings(
                "mesh",
                "zone",
                { ...settings, maskStrength: 1.5 },
                { refreshPreview: true },
            );
        });
        expect(hook.result.current.pendingSettingsSaves).toBe(1);
        await act(async () => hook.result.current.applyDraft());
        expect(mocks.apply).not.toHaveBeenCalled();
        await act(async () => vi.advanceTimersByTimeAsync(120));
        expect(mocks.save).toHaveBeenCalledTimes(1);
        expect(mocks.save).toHaveBeenCalledWith({
            sessionId: "A",
            changes: [
                {
                    componentId: "mesh",
                    zoneId: "zone",
                    settings: { ...settings, maskStrength: 1.5 },
                },
            ],
        });
        expect(hook.result.current.pendingSettingsSaves).toBe(0);
        await act(async () => hook.result.current.applyDraft());
        expect(onApplied).toHaveBeenCalledWith({ sourceModRoot: "A", outputModRoot: "output" });
        await act(async () => hook.result.current.rollbackResult());
        expect(onRolledBack).toHaveBeenCalledWith("A");
        expect(hook.result.current.result).toBeNull();
    });
    it("preserves failure state for apply and rollback", async () => {
        const hook = renderHook(() => useSession("A"));
        await waitFor(() => expect(hook.result.current.inspection).not.toBeNull());
        await act(async () => hook.result.current.analyzeSelected());
        mocks.apply.mockRejectedValueOnce(new Error("apply failed"));
        await act(async () => hook.result.current.applyDraft());
        expect(hook.result.current.applying).toBe(false);
        expect(hook.result.current.result).toBeNull();
        expect(mocks.error).toHaveBeenCalledWith("page.tools.touch_profile.toast.create_failed", {
            description: "apply failed",
        });
        await act(async () => hook.result.current.applyDraft());
        mocks.rollback.mockRejectedValueOnce(new Error("rollback failed"));
        await act(async () => hook.result.current.rollbackResult());
        expect(hook.result.current.rollingBack).toBe(false);
        expect(hook.result.current.result?.outputModRoot).toBe("output");
    });
});
