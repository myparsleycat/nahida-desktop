import { globalStore, useGlobalStore } from "@renderer/store/global";
import { modStore } from "@renderer/store/mod";
import type { ModInfo, TransferWithoutData } from "@shared/types";
import { useCallback, useEffect, useMemo } from "react";

import { isActiveModDownloadTransfer, normalizeModDownloadPath } from "./use-mod-download-transfer";

export interface DownloadDirectoryTarget {
    normalizedPath: string;
    path: string;
    pid: string;
    startTime: number;
}

export function useModsWithDownloadPlaceholders(
    groupPath: string | undefined,
    installedMods: ModInfo[],
): ModInfo[] {
    const normalizedGroupPath = useMemo(
        () => (groupPath ? normalizeModDownloadPath(groupPath) : ""),
        [groupPath],
    );
    const targetIdentity = useGlobalStore(
        useCallback(
            (state) => getDownloadDirectoryTargetIdentity(state.transfers, normalizedGroupPath),
            [normalizedGroupPath],
        ),
    );
    const targets = useMemo(
        () => getDownloadDirectoryTargets(globalStore.getState().transfers, normalizedGroupPath),
        [normalizedGroupPath, targetIdentity],
    );

    useEffect(() => {
        const targetPaths = new Set(targets.map((target) => target.normalizedPath));
        if (targetPaths.size === 0) {
            return;
        }
        const state = modStore.getState();
        const selectedDownloads = getSelectedDownloadPaths(state.selectedModPaths, targetPaths);
        state.removeMergeSelections(selectedDownloads);
    }, [targets]);

    return useMemo(
        () => mergeDownloadPlaceholders(installedMods, targets),
        [installedMods, targets],
    );
}

export function getSelectedDownloadPaths(
    selectedModPaths: Iterable<string>,
    normalizedTargetPaths: ReadonlySet<string>,
): string[] {
    return Array.from(selectedModPaths).filter((path) =>
        normalizedTargetPaths.has(normalizeModDownloadPath(path)),
    );
}

export function getDownloadDirectoryTargetIdentity(
    transfers: TransferWithoutData[],
    normalizedGroupPath: string,
): string {
    return JSON.stringify(
        getDownloadDirectoryTargets(transfers, normalizedGroupPath).map((target) => [
            target.pid,
            target.normalizedPath,
            target.startTime,
        ]),
    );
}

export function getDownloadDirectoryTargets(
    transfers: TransferWithoutData[],
    normalizedGroupPath: string,
): DownloadDirectoryTarget[] {
    if (!normalizedGroupPath) {
        return [];
    }

    const seenPaths = new Set<string>();
    return transfers.flatMap((transfer) => {
        if (!isActiveModDownloadTransfer(transfer)) {
            return [];
        }

        return (transfer.destinationTargets ?? []).flatMap((target) => {
            if (target.kind !== "directory") {
                return [];
            }
            const normalizedPath = normalizeModDownloadPath(target.path);
            if (
                getNormalizedParentPath(normalizedPath) !== normalizedGroupPath ||
                seenPaths.has(normalizedPath)
            ) {
                return [];
            }
            seenPaths.add(normalizedPath);
            return [
                {
                    normalizedPath,
                    path: target.path,
                    pid: transfer.pid,
                    startTime: transfer.startTime,
                },
            ];
        });
    });
}

export function mergeDownloadPlaceholders(
    installedMods: ModInfo[],
    targets: DownloadDirectoryTarget[],
): ModInfo[] {
    const targetPaths = new Set(targets.map((target) => target.normalizedPath));
    const mergedInstalledMods = installedMods.map((mod) =>
        targetPaths.has(normalizeModDownloadPath(mod.path)) ? { ...mod, isDownloading: true } : mod,
    );
    const installedPaths = new Set(installedMods.map((mod) => normalizeModDownloadPath(mod.path)));
    const placeholders = targets.flatMap((target): ModInfo[] => {
        if (installedPaths.has(target.normalizedPath)) {
            return [];
        }
        return [
            {
                id: `download:${target.pid}:${target.normalizedPath}`,
                name: getWindowsBaseName(target.path),
                path: target.path,
                isEnabled: false,
                mtime: target.startTime,
                size: 0,
                inis: [],
                isDownloading: true,
                isDownloadPlaceholder: true,
            },
        ];
    });
    return [...mergedInstalledMods, ...placeholders];
}

function getNormalizedParentPath(path: string): string {
    const separatorIndex = path.lastIndexOf("/");
    return separatorIndex < 0 ? "" : path.slice(0, separatorIndex);
}

function getWindowsBaseName(path: string): string {
    const normalized = path.replaceAll("\\", "/").replace(/\/+$/, "");
    return normalized.slice(normalized.lastIndexOf("/") + 1);
}
