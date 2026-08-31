import { useGlobalStore } from "@renderer/store/global";
import type { TransferWithoutData } from "@shared/types";
import { useCallback, useMemo } from "react";

const MOD_DOWNLOAD_STATUSES = new Set<TransferWithoutData["status"]>([
    "pending",
    "preparing",
    "progress",
    "paused",
]);

export function isActiveModDownloadTransfer(transfer: TransferWithoutData): boolean {
    return transfer.type === "download" && MOD_DOWNLOAD_STATUSES.has(transfer.status);
}

export function normalizeModDownloadPath(path: string): string {
    return path.replaceAll("\\", "/").replace(/\/+$/, "").toLowerCase();
}

export function findModDownloadTransfer(
    transfers: TransferWithoutData[],
    modPath: string,
): TransferWithoutData | undefined {
    const normalizedModPath = normalizeModDownloadPath(modPath);
    return findModDownloadTransferByNormalizedPath(transfers, normalizedModPath);
}

export function useModDownloadTransfer(modPath: string): TransferWithoutData | undefined {
    const normalizedModPath = useMemo(() => normalizeModDownloadPath(modPath), [modPath]);
    return useGlobalStore(
        useCallback(
            (state) => findModDownloadTransferByNormalizedPath(state.transfers, normalizedModPath),
            [normalizedModPath],
        ),
    );
}

function findModDownloadTransferByNormalizedPath(
    transfers: TransferWithoutData[],
    normalizedModPath: string,
): TransferWithoutData | undefined {
    return transfers.find(
        (transfer) =>
            isActiveModDownloadTransfer(transfer) &&
            transfer.destinationPaths?.some(
                (destinationPath) =>
                    normalizeModDownloadPath(destinationPath) === normalizedModPath,
            ),
    );
}
