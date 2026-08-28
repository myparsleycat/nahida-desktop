import { buildTransferTitlebarActivity } from "@renderer/components/titlebar/titlebar-activity";
import type { TransferWithoutData } from "@shared/types";
import { describe, expect, it } from "vitest";

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
