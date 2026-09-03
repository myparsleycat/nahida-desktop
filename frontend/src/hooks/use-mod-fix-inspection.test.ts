// @vitest-environment jsdom

import { Tools, type FixInspectionSnapshot } from "@bindings/tools";
import { Logger } from "@renderer/lib/logger";
import { modStore } from "@renderer/store/mod";
import { titlebarActivityStore } from "@renderer/store/titlebar-activity";
import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import {
    syncFixInspectionActivities,
    useModFixInspectionTitlebarActivity,
} from "./use-mod-fix-inspection";

const mocks = vi.hoisted(() => ({
    listeners: new Map<string, (event: { data: unknown }) => void>(),
    navigate: vi.fn(),
    refresh: vi.fn(),
    t: (key: string, opts?: Record<string, unknown>) =>
        opts?.name ? `${key}:${String(opts.name)}` : key,
    unsubscribers: new Map<string, ReturnType<typeof vi.fn>>(),
}));

vi.mock("@bindings/tools", () => ({
    Tools: {
        RefreshFixInspections: mocks.refresh,
    },
}));

vi.mock("@renderer/lib/logger", () => ({
    Logger: {
        warn: vi.fn(),
        error: vi.fn(),
    },
}));

vi.mock("@tanstack/react-router", () => ({
    useNavigate: () => mocks.navigate,
}));

vi.mock("@wailsio/runtime", () => ({
    Events: {
        On: vi.fn((name: string, listener: (event: { data: unknown }) => void) => {
            mocks.listeners.set(name, listener);
            const unsubscribe = vi.fn();
            mocks.unsubscribers.set(name, unsubscribe);
            return unsubscribe;
        }),
    },
}));

vi.mock("react-i18next", () => ({
    useTranslation: () => ({ t: mocks.t }),
}));

const t = mocks.t;

const pendingSnapshot = (revision = 1): FixInspectionSnapshot => ({
    revision,
    inspections: [
        {
            modPath: "E:\\ZZZ\\ModA",
            displayName: "ModA",
            result: {
                needsFix: true,
                importer: "ZZMI",
                toolName: "ZZMI Mod Fixer",
                summary: "Found 1 file with an outdated hash",
                details: ["outdated hash"],
                affectedFiles: ["mod.ini"],
                actionTool: "hash",
            },
        },
    ],
});

describe("syncFixInspectionActivities", () => {
    beforeEach(() => {
        resetStores();
    });

    it("restores pending inspections and wires the fixer action", () => {
        const onOpenFixer = vi.fn();
        const snapshot = pendingSnapshot();

        syncFixInspectionActivities(snapshot, onOpenFixer, t);

        const activity = titlebarActivityStore.getState().activities["mod-fix:E:\\ZZZ\\ModA"];
        expect(activity).toMatchObject({
            label: "titlebar.activity.modFix.label",
            detail: "ModA",
            status: "warning",
        });
        expect(activity?.popover?.description).toBe("Found 1 file with an outdated hash");

        activity?.popover?.onAction?.();
        expect(onOpenFixer).toHaveBeenCalledWith(snapshot.inspections?.[0]);
    });

    it("removes only stale fix activities", () => {
        syncFixInspectionActivities(pendingSnapshot(), vi.fn(), t);
        titlebarActivityStore.getState().upsertActivity({
            id: "unrelated",
            label: "Unrelated",
            status: "running",
            icon: () => null,
        });

        syncFixInspectionActivities({ revision: 2, inspections: [] }, vi.fn(), t);

        expect(
            titlebarActivityStore.getState().activities["mod-fix:E:\\ZZZ\\ModA"],
        ).toBeUndefined();
        expect(titlebarActivityStore.getState().activities.unrelated).toBeDefined();
    });
});

describe("useModFixInspectionTitlebarActivity", () => {
    beforeEach(() => {
        resetStores();
        mocks.listeners.clear();
        mocks.navigate.mockReset();
        mocks.refresh.mockReset();
        mocks.refresh.mockResolvedValue({ revision: 0, inspections: [] });
        mocks.unsubscribers.clear();
        vi.mocked(Logger.error).mockClear();
    });

    it("refreshes on mount and focus and unsubscribes on unmount", async () => {
        mocks.refresh
            .mockResolvedValueOnce(pendingSnapshot())
            .mockResolvedValueOnce({ revision: 2, inspections: [] });

        const { unmount } = renderHook(() => useModFixInspectionTitlebarActivity());

        await waitFor(() => {
            expect(Tools.RefreshFixInspections).toHaveBeenCalledTimes(1);
            expect(
                titlebarActivityStore.getState().activities["mod-fix:E:\\ZZZ\\ModA"],
            ).toBeDefined();
        });

        act(() => mocks.listeners.get("window:focus")?.({ data: undefined }));

        await waitFor(() => {
            expect(Tools.RefreshFixInspections).toHaveBeenCalledTimes(2);
            expect(
                titlebarActivityStore.getState().activities["mod-fix:E:\\ZZZ\\ModA"],
            ).toBeUndefined();
        });

        unmount();

        expect(mocks.unsubscribers.get("tools:fix-inspections")).toHaveBeenCalledOnce();
        expect(mocks.unsubscribers.get("window:focus")).toHaveBeenCalledOnce();
    });

    it("applies events and rejects snapshots with lower revisions", async () => {
        mocks.refresh.mockResolvedValueOnce(pendingSnapshot(2));
        renderHook(() => useModFixInspectionTitlebarActivity());

        await waitFor(() => {
            expect(
                titlebarActivityStore.getState().activities["mod-fix:E:\\ZZZ\\ModA"],
            ).toBeDefined();
        });

        const newest = pendingSnapshot(3);
        newest.inspections![0].displayName = "Newest";
        act(() => mocks.listeners.get("tools:fix-inspections")?.({ data: newest }));
        act(() =>
            mocks.listeners.get("tools:fix-inspections")?.({
                data: { revision: 1, inspections: [] },
            }),
        );

        const activity = titlebarActivityStore.getState().activities["mod-fix:E:\\ZZZ\\ModA"];
        expect(activity?.detail).toBe("Newest");

        act(() => activity?.popover?.onAction?.());
        expect(modStore.getState().pendingModFixerRequest).toEqual({
            modPath: "E:\\ZZZ\\ModA",
            importer: "ZZMI",
            actionTool: "hash",
        });
        expect(mocks.navigate).toHaveBeenCalledWith({ to: "/mod" });
    });

    it("logs refresh failures", async () => {
        const error = new Error("refresh failed");
        mocks.refresh.mockRejectedValueOnce(error);

        renderHook(() => useModFixInspectionTitlebarActivity());

        await waitFor(() => {
            expect(Logger.error).toHaveBeenCalledWith(error, "ModFixInspection:restore");
        });
    });
});

function resetStores() {
    for (const id of Object.keys(titlebarActivityStore.getState().activities)) {
        titlebarActivityStore.getState().removeActivity(id);
    }
    modStore.getState().setPendingModFixerRequest(null);
}
