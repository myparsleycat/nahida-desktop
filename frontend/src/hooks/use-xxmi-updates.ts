import { XXMI } from "@bindings/xxmi";
import type { UpdateStatus } from "@renderer/components/xxmi/xxmi-update-dialog";
import { useQuery } from "@tanstack/react-query";
import { uniqBy } from "es-toolkit";

export function useXXMIUpdates(enabled: boolean) {
    return useQuery({
        queryKey: ["xxmi:updates"],
        queryFn: () => XXMI.CheckUpdates(false),
        enabled,
        staleTime: 60 * 60 * 1000,
        retry: false,
    }).data;
}

// A shared package appears once per importer; only unpinned ones can be installed in bulk.
export function installableUpdates(updates: UpdateStatus[] | null | undefined) {
    return uniqBy(
        updates?.filter((entry) => entry.available && !entry.pinned) ?? [],
        (entry) => entry.package,
    );
}
