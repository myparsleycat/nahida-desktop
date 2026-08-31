import type { TransferWithoutData } from "@shared/types";
import { describe, expect, it } from "vitest";

import { getModDownloadDisplay } from "./mod-download-overlay";

function transfer(overrides: Partial<TransferWithoutData> = {}): TransferWithoutData {
    return {
        pid: "download",
        type: "download",
        status: "progress",
        totalSize: 100,
        transferedSize: 25,
        progress: 25,
        speed: 10,
        eta: 8,
        startTime: 1,
        name: "Mod",
        totalFiles: 1,
        transferedFiles: 0,
        failedFiles: 0,
        ...overrides,
    };
}

describe("getModDownloadDisplay", () => {
    it("uses pulsing queued and preparing states", () => {
        expect(getModDownloadDisplay(transfer({ status: "pending", totalSize: 0 }))).toEqual({
            status: "queued",
            progress: null,
            speed: null,
            pulse: true,
        });
        expect(getModDownloadDisplay(transfer({ status: "preparing", totalSize: 0 }))).toEqual({
            status: "preparing",
            progress: null,
            speed: null,
            pulse: true,
        });
    });

    it("shows progress and speed while downloading", () => {
        expect(getModDownloadDisplay(transfer({ progress: 47.4, speed: 2048 }))).toEqual({
            status: "downloading",
            progress: 47.4,
            speed: 2048,
            pulse: false,
        });
    });

    it("uses an unknown pulsing progress when total size is unavailable", () => {
        expect(
            getModDownloadDisplay(transfer({ totalSize: 0, progress: 100, speed: 512 })),
        ).toEqual({
            status: "downloading",
            progress: null,
            speed: 512,
            pulse: true,
        });
    });

    it("shows finalizing at 100 percent and hides stale speed", () => {
        expect(getModDownloadDisplay(transfer({ progress: 100, speed: 512 }))).toEqual({
            status: "finalizing",
            progress: 100,
            speed: null,
            pulse: false,
        });
    });

    it("keeps paused progress but hides stale speed", () => {
        expect(
            getModDownloadDisplay(transfer({ status: "paused", progress: 63, speed: 512 })),
        ).toEqual({
            status: "paused",
            progress: 63,
            speed: null,
            pulse: false,
        });
    });
});
