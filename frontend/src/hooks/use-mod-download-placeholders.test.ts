import type { ModInfo, TransferWithoutData } from "@shared/types";
import { describe, expect, it } from "vitest";

import {
    getDownloadDirectoryTargetIdentity,
    getDownloadDirectoryTargets,
    getSelectedDownloadPaths,
    mergeDownloadPlaceholders,
} from "./use-mod-download-placeholders";
import { normalizeModDownloadPath } from "./use-mod-download-transfer";

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
        startTime: 123,
        name: "Mod",
        totalFiles: 1,
        transferedFiles: 0,
        failedFiles: 0,
        path: "C:/Mods/Character",
        destinationPaths: ["C:/Mods/Character/Mod"],
        destinationTargets: [{ path: "C:/Mods/Character/Mod", kind: "directory" }],
        ...overrides,
    };
}

function mod(overrides: Partial<ModInfo> = {}): ModInfo {
    return {
        id: "installed",
        name: "Installed",
        path: "C:/Mods/Character/Installed",
        isEnabled: true,
        mtime: 1,
        size: 10,
        inis: [],
        ...overrides,
    };
}

describe("download mod placeholders", () => {
    const groupPath = normalizeModDownloadPath("C:\\Mods\\Character\\");

    it.each(["pending", "preparing", "progress", "paused"] as const)(
        "collects direct directory targets from %s downloads",
        (status) => {
            expect(getDownloadDirectoryTargets([transfer({ status })], groupPath)).toEqual([
                {
                    normalizedPath: "c:/mods/character/mod",
                    path: "C:/Mods/Character/Mod",
                    pid: "download",
                    startTime: 123,
                },
            ]);
        },
    );

    it("excludes files, nested directories, other groups, uploads, and terminal transfers", () => {
        const targets = [
            transfer({
                destinationTargets: [
                    { path: "C:/Mods/Character/file.zip", kind: "file" },
                    { path: "C:/Mods/Character/Mod/Nested", kind: "directory" },
                    { path: "C:/Mods/Other/Mod", kind: "directory" },
                ],
            }),
            transfer({ pid: "upload", type: "upload" }),
            transfer({ pid: "completed", status: "completed" }),
            transfer({ pid: "error", status: "error" }),
            transfer({ pid: "canceled", status: "canceled" }),
        ];

        expect(getDownloadDirectoryTargets(targets, groupPath)).toEqual([]);
    });

    it("deduplicates equivalent Windows paths", () => {
        const duplicate = transfer({
            pid: "duplicate",
            destinationTargets: [{ path: "c:\\mods\\character\\MOD\\", kind: "directory" }],
        });

        expect(getDownloadDirectoryTargets([transfer(), duplicate], groupPath)).toHaveLength(1);
    });

    it("finds existing merge selections that become download targets", () => {
        const selected = new Set(["C:\\Mods\\Character\\Mod\\", "C:\\Mods\\Character\\Other"]);
        const targets = new Set(["c:/mods/character/mod"]);

        expect(getSelectedDownloadPaths(selected, targets)).toEqual(["C:\\Mods\\Character\\Mod\\"]);
    });

    it("keeps list identity stable across progress-only updates", () => {
        const before = getDownloadDirectoryTargetIdentity([transfer()], groupPath);
        const after = getDownloadDirectoryTargetIdentity(
            [transfer({ progress: 70, transferedSize: 70, speed: 2048, eta: 2 })],
            groupPath,
        );

        expect(after).toBe(before);
        expect(
            getDownloadDirectoryTargetIdentity(
                [
                    transfer({
                        destinationTargets: [
                            { path: "C:/Mods/Character/Renamed", kind: "directory" },
                        ],
                    }),
                ],
                groupPath,
            ),
        ).not.toBe(before);
    });

    it("adds a minimal placeholder and prefers a scanned mod at the same path", () => {
        const targets = getDownloadDirectoryTargets([transfer()], groupPath);
        const placeholder = mergeDownloadPlaceholders([], targets);

        expect(placeholder).toEqual([
            {
                id: "download:download:c:/mods/character/mod",
                name: "Mod",
                path: "C:/Mods/Character/Mod",
                isEnabled: false,
                mtime: 123,
                size: 0,
                inis: [],
                isDownloading: true,
                isDownloadPlaceholder: true,
            },
        ]);

        const installed = mod({ path: "c:\\MODS\\Character\\Mod\\" });
        expect(mergeDownloadPlaceholders([installed], targets)).toEqual([
            { ...installed, isDownloading: true },
        ]);
    });
});
