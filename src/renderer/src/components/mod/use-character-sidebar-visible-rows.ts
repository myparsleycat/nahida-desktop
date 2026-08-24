import { type FolderSortDirection, type FolderSortKey, useModStore } from "@renderer/store/mod";
import type { FolderGroup } from "@renderer/types/mod";
import { keepPreviousData, useQueries, useQueryClient } from "@tanstack/react-query";
import { useMemo } from "react";

import {
    buildVisibleSidebarRows,
    collectManualSubGroupPaths,
    type VisibleSidebarRow,
} from "./character-sidebar-visible-rows";

export function useCharacterSidebarVisibleRows(
    groups: FolderGroup[],
    searchTerm: string,
    sortKey: FolderSortKey,
    sortDirection: FolderSortDirection,
    hideEmptyGroups: boolean,
): VisibleSidebarRow[] {
    const queryClient = useQueryClient();
    const expandedGroups = useModStore((s) => s.expandedGroups);
    const persistentGroups = useModStore((s) => s.persistentGroups);
    const isSearching = searchTerm.trim().length > 0;

    const subGroupPaths = useMemo(() => {
        const paths = new Set<string>();
        for (const path of expandedGroups) {
            paths.add(path);
        }
        if (isSearching) {
            for (const path of persistentGroups) {
                paths.add(path);
            }
        }
        return [...paths];
    }, [expandedGroups, isSearching, persistentGroups]);

    const subGroupQueries = useQueries({
        queries: subGroupPaths.map((path) => ({
            queryKey: ["subGroups", path] as const,
            queryFn: () => window.api.invoke("mod:getSubGroups", path),
            placeholderData: keepPreviousData,
        })),
    });

    const subGroupsByPath = useMemo(() => {
        const map = new Map<string, FolderGroup[]>();
        for (const [index, path] of subGroupPaths.entries()) {
            map.set(path, subGroupQueries[index]?.data ?? []);
        }
        return map;
    }, [subGroupPaths, subGroupQueries]);

    const collectedManualPaths = collectManualSubGroupPaths(groups, {
        isSearching,
        expandedGroups,
        persistentGroups,
        subGroupsByPath,
        manualSubGroupsByPath: {
            get: (path) => queryClient.getQueryData<FolderGroup[]>(["manualSubGroups", path]),
        },
    });
    const manualPathsKey = collectedManualPaths.join("\0");
    const manualPaths = useMemo(() => collectedManualPaths, [manualPathsKey]);

    const manualQueries = useQueries({
        queries: manualPaths.map((path) => ({
            queryKey: ["manualSubGroups", path] as const,
            queryFn: () => window.api.invoke("mod:getManualSubGroups", path),
            placeholderData: keepPreviousData,
        })),
    });

    const manualSubGroupsByPath = useMemo(() => {
        const map = new Map<string, FolderGroup[]>();
        for (const [index, path] of manualPaths.entries()) {
            map.set(path, manualQueries[index]?.data ?? []);
        }
        return map;
    }, [manualPaths, manualQueries]);

    return useMemo(
        () =>
            buildVisibleSidebarRows(groups, {
                searchTerm,
                sortKey,
                sortDirection,
                hideEmptyGroups,
                expandedGroups,
                persistentGroups,
                subGroupsByPath,
                manualSubGroupsByPath,
            }),
        [
            expandedGroups,
            groups,
            hideEmptyGroups,
            manualSubGroupsByPath,
            persistentGroups,
            searchTerm,
            sortDirection,
            sortKey,
            subGroupsByPath,
        ],
    );
}
