import { type FolderSortDirection, type FolderSortKey, useModStore } from "@renderer/store/mod";
import type { FolderGroup } from "@renderer/types/mod";
import { keepPreviousData, useQueries, useQueryClient } from "@tanstack/react-query";
import { useEffect, useMemo, useState } from "react";

import {
    buildVisibleSidebarRows,
    collectManualSubGroupPaths,
    type VisibleSidebarRow,
} from "./character-sidebar-visible-rows";

function readManualSubGroupsByPath(
    queryClient: ReturnType<typeof useQueryClient>,
    paths: Iterable<string>,
) {
    const map = new Map<string, FolderGroup[]>();
    for (const path of paths) {
        map.set(path, queryClient.getQueryData<FolderGroup[]>(["manualSubGroups", path]) ?? []);
    }
    return map;
}

function samePaths(a: string[], b: string[]) {
    return a.length === b.length && a.every((path, index) => path === b[index]);
}

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
    const [manualCacheEpoch, setManualCacheEpoch] = useState(0);

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

    const manualPaths = useMemo(() => {
        const knownPaths = new Set<string>();
        for (const group of groups) {
            if (group.hasManualSubGroups) {
                knownPaths.add(group.path);
            }
        }
        for (const children of subGroupsByPath.values()) {
            for (const child of children) {
                if (child.hasManualSubGroups) {
                    knownPaths.add(child.path);
                }
            }
        }
        for (const query of queryClient
            .getQueryCache()
            .findAll({ queryKey: ["manualSubGroups"] })) {
            const path = query.queryKey[1];
            if (typeof path === "string") {
                knownPaths.add(path);
            }
        }

        return collectManualSubGroupPaths(groups, {
            isSearching,
            expandedGroups,
            persistentGroups,
            subGroupsByPath,
            manualSubGroupsByPath: readManualSubGroupsByPath(queryClient, knownPaths),
        });
    }, [
        expandedGroups,
        groups,
        isSearching,
        manualCacheEpoch,
        persistentGroups,
        queryClient,
        subGroupsByPath,
    ]);

    const manualQueries = useQueries({
        queries: manualPaths.map((path) => ({
            queryKey: ["manualSubGroups", path] as const,
            queryFn: () => window.api.invoke("mod:getManualSubGroups", path),
            placeholderData: keepPreviousData,
        })),
    });

    useEffect(() => {
        const nextPaths = collectManualSubGroupPaths(groups, {
            isSearching,
            expandedGroups,
            persistentGroups,
            subGroupsByPath,
            manualSubGroupsByPath: readManualSubGroupsByPath(queryClient, [
                ...manualPaths,
                ...manualQueries.flatMap((query) =>
                    (query.data ?? [])
                        .filter((child) => child.hasManualSubGroups)
                        .map((child) => child.path),
                ),
            ]),
        });

        if (!samePaths(nextPaths, manualPaths)) {
            setManualCacheEpoch((value) => value + 1);
        }
    }, [
        expandedGroups,
        groups,
        isSearching,
        manualPaths,
        manualQueries,
        persistentGroups,
        queryClient,
        subGroupsByPath,
    ]);

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
