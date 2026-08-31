import type { TransferWithoutData } from "@shared/types";
import { describe, expect, it } from "vitest";

import { findModDownloadTransfer, normalizeModDownloadPath } from "./use-mod-download-transfer";

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
        path: "C:/Mods/Character",
        destinationPaths: ["C:/Mods/Character/Mod"],
        ...overrides,
    };
}

describe("findModDownloadTransfer", () => {
    it("normalizes Windows separators, casing, and trailing separators", () => {
        expect(normalizeModDownloadPath("C:\\Mods\\Character\\Mod\\")).toBe(
            "c:/mods/character/mod",
        );
        expect(
            findModDownloadTransfer(
                [transfer({ destinationPaths: ["C:\\MODS\\Character\\Mod\\"] })],
                "c:/mods/character/mod",
            )?.pid,
        ).toBe("download");
    });

    it.each(["pending", "preparing", "progress", "paused"] as const)(
        "matches %s downloads",
        (status) => {
            expect(
                findModDownloadTransfer([transfer({ status })], "C:/Mods/Character/Mod")?.pid,
            ).toBe("download");
        },
    );

    it.each(["completed", "error", "canceled"] as const)(
        "does not match %s downloads",
        (status) => {
            expect(findModDownloadTransfer([transfer({ status })], "C:/Mods/Character/Mod")).toBe(
                undefined,
            );
        },
    );

    it("does not match uploads or an unrelated child of the same parent", () => {
        expect(
            findModDownloadTransfer([transfer({ type: "upload" })], "C:/Mods/Character/Mod"),
        ).toBeUndefined();
        expect(findModDownloadTransfer([transfer()], "C:/Mods/Character/Other")).toBeUndefined();
    });

    it("matches every destination in a batch to the same transfer", () => {
        const batch = transfer({
            pid: "batch",
            destinationPaths: ["C:/Mods/Character/One", "C:/Mods/Character/Two"],
        });

        expect(findModDownloadTransfer([batch], "C:/Mods/Character/One")?.pid).toBe("batch");
        expect(findModDownloadTransfer([batch], "C:/Mods/Character/Two")?.pid).toBe("batch");
    });
});
