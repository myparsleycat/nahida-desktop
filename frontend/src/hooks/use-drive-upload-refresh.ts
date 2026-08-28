import { useGlobalStore } from "@renderer/store/global";
import type { TransferWithoutData } from "@shared/types";
import type { QueryKey } from "@tanstack/react-query";
import { useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef } from "react";

type TransferStatuses = Record<string, TransferWithoutData["status"]>;

export function getNewlyCompletedUploadDestinations(
    transfers: TransferWithoutData[],
    previousStatuses: TransferStatuses,
) {
    const destinations = new Set<string>();

    for (const transfer of transfers) {
        if (
            transfer.type === "upload" &&
            transfer.currentId &&
            transfer.status === "completed" &&
            previousStatuses[transfer.pid] !== "completed"
        ) {
            destinations.add(transfer.currentId);
        }
    }

    return destinations;
}

export function useDriveUploadRefresh(currentId: string, queryKey: QueryKey) {
    const queryClient = useQueryClient();
    const transfers = useGlobalStore((state) => state.transfers);
    const previousStatusesRef = useRef<TransferStatuses>({});

    useEffect(() => {
        const destinations = getNewlyCompletedUploadDestinations(
            transfers,
            previousStatusesRef.current,
        );

        previousStatusesRef.current = Object.fromEntries(
            transfers.map((transfer) => [transfer.pid, transfer.status]),
        );

        if (destinations.has(currentId)) {
            void queryClient.invalidateQueries({ queryKey, exact: true });
        }
    }, [currentId, queryClient, queryKey, transfers]);
}
