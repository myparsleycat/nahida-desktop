import type { FixInspectionSnapshot } from "@bindings/tools";
import { titlebarActivityStore } from "@renderer/store/titlebar-activity";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { syncFixInspectionActivities } from "./use-mod-fix-inspection";

vi.mock("@renderer/lib/logger", () => ({
    Logger: {
        warn: vi.fn(),
        error: vi.fn(),
    },
}));

const t = (key: string, opts?: Record<string, unknown>) =>
    opts?.name ? `${key}:${String(opts.name)}` : key;

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
        for (const id of Object.keys(titlebarActivityStore.getState().activities)) {
            titlebarActivityStore.getState().removeActivity(id);
        }
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
