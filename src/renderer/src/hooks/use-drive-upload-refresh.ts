import { useGlobalStore } from "@renderer/store/global";
import type { QueryKey } from "@tanstack/react-query";
import { useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef } from "react";

export function useDriveUploadRefresh(currentId: string, queryKey: QueryKey) {
  const queryClient = useQueryClient();
  const transfers = useGlobalStore((state) => state.transfers);
  const previousStatusesRef = useRef<Record<string, string>>({});

  useEffect(() => {
    let shouldRefresh = false;
    const nextStatuses: Record<string, string> = {};

    for (const transfer of transfers) {
      nextStatuses[transfer.pid] = transfer.status;

      if (
        transfer.type === "upload" &&
        transfer.currentId === currentId &&
        transfer.status === "completed" &&
        previousStatusesRef.current[transfer.pid] !== "completed"
      ) {
        shouldRefresh = true;
      }
    }

    previousStatusesRef.current = nextStatuses;

    if (!shouldRefresh) return;

    queryClient.invalidateQueries({ queryKey, exact: true });
  }, [currentId, queryClient, queryKey, transfers]);
}
