import {
    buildModFixTitlebarActivity,
    buildTransferTitlebarActivity,
} from "@renderer/components/titlebar/titlebar-activity";
import type { TransferWithoutData } from "@shared/types";
import { describe, expect, it, vi } from "vitest";

const t = (key: string) => key;

function transfer(partial: Partial<TransferWithoutData>): TransferWithoutData {
    return {
        pid: "1",
        type: "upload",
        status: "progress",
        totalSize: 100,
        transferedSize: 100,
        progress: 100,
        speed: 0,
        eta: 0,
        startTime: 0,
        name: "file",
        totalFiles: 1,
        transferedFiles: 1,
        failedFiles: 0,
        ...partial,
    };
}

describe("buildTransferTitlebarActivity", () => {
    it("labels a single finalizing upload as finalizing", () => {
        const activity = buildTransferTitlebarActivity([transfer({ progress: 100, speed: 0 })], t);
        expect(activity?.label).toBe("titlebar.activity.transfer.finalizing");
    });

    it("hides speed for finalizing transfers", () => {
        const activity = buildTransferTitlebarActivity(
            [transfer({ progress: 100, speed: 500 })],
            t,
        );
        expect(activity?.detail).not.toContain("/s");
    });

    it("labels a normal upload as uploading with speed", () => {
        const activity = buildTransferTitlebarActivity([transfer({ progress: 50, speed: 500 })], t);
        expect(activity?.label).toBe("titlebar.activity.transfer.uploading");
        expect(activity?.detail).toContain("/s");
    });

    it("shows uploading while some transfers are still progressing", () => {
        const activity = buildTransferTitlebarActivity(
            [
                transfer({ pid: "1", progress: 100, speed: 0 }),
                transfer({ pid: "2", progress: 50, speed: 500 }),
            ],
            t,
        );
        expect(activity?.label).toBe("titlebar.activity.transfer.uploading");
    });

    it("shows finalizing when all progressing uploads are finalizing", () => {
        const activity = buildTransferTitlebarActivity(
            [
                transfer({ pid: "1", progress: 100, speed: 0 }),
                transfer({ pid: "2", progress: 100, speed: 0 }),
            ],
            t,
        );
        expect(activity?.label).toBe("titlebar.activity.transfer.finalizing");
    });

    it("labels a finalizing download as finalizing", () => {
        const activity = buildTransferTitlebarActivity(
            [transfer({ type: "download", progress: 100 })],
            t,
        );
        expect(activity?.label).toBe("titlebar.activity.transfer.finalizing");
    });

    it("labels mixed uploads and downloads as transferring", () => {
        const activity = buildTransferTitlebarActivity(
            [
                transfer({ pid: "1", type: "upload", progress: 50 }),
                transfer({ pid: "2", type: "download", progress: 50 }),
            ],
            t,
        );
        expect(activity?.label).toBe("titlebar.activity.transfer.transferring");
    });

    it("labels all paused transfers as paused", () => {
        const activity = buildTransferTitlebarActivity([transfer({ status: "paused" })], t);
        expect(activity).not.toBeNull();
        expect(activity?.status).toBe("paused");
        expect(activity?.label).toBe("titlebar.activity.transfer.paused");
    });

    it("returns null when no transfers are active", () => {
        const activity = buildTransferTitlebarActivity(
            [transfer({ status: "completed" }), transfer({ status: "error" })],
            t,
        );
        expect(activity).toBeNull();
    });
});

describe("buildModFixTitlebarActivity", () => {
    it("builds warning activity with defaultOpen popover and wiring", () => {
        const onOpenFixer = vi.fn();
        const activity = buildModFixTitlebarActivity({
            modPath: "E:/Mods/TestMod",
            displayName: "TestMod",
            result: {
                needsFix: true,
                importer: "ZZMI",
                toolName: "ZZMI Fixer",
                summary: "1 file outdated",
                details: ["mod.ini"],
                affectedFiles: ["mod.ini"],
                actionTool: "hash",
            },
            onOpenFixer,
            t: (key) => key,
        });

        expect(activity.id).toBe("mod-fix:E:/Mods/TestMod");
        expect(activity.status).toBe("warning");
        expect(activity.order).toBe(5);
        expect(activity.label).toBe("titlebar.activity.modFix.label");
        expect(activity.detail).toBe("TestMod");
        expect(activity.popover).toBeDefined();
        expect(activity.popover?.defaultOpen).toBe(true);
        expect(activity.popover?.title).toBe("page.mod.fix_needed_toast.title");
        expect(activity.popover?.description).toBe("1 file outdated");
        expect(activity.popover?.actionLabel).toBe("page.mod.fix_needed_toast.action");
        expect(activity.popover?.dismissLabel).toBe("titlebar.activity.modFix.dismiss");

        activity.popover?.onAction?.();
        expect(onOpenFixer).toHaveBeenCalledWith("E:/Mods/TestMod", "hash");
    });

    it("truncates long mod names in detail", () => {
        const activity = buildModFixTitlebarActivity({
            modPath: "E:/Mods/Skimpier Burnice",
            displayName: "Skimpier Burnice",
            result: {
                needsFix: true,
                importer: "ZZMI",
                toolName: "ZZMI Fixer",
                summary: "outdated",
                details: null,
                affectedFiles: null,
                actionTool: "hash",
            },
            t: (key) => key,
        });

        expect(activity.detail).toBe("Skimpier…");
    });
});
