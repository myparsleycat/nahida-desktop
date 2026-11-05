import { Mod } from "@bindings/mod";
import { type FolderSortDirection, type FolderSortKey, useModStore } from "@renderer/store/mod";
import type { FolderGroup } from "@renderer/types/mod";
import type { Classification } from "@shared/types";
import {
    keepPreviousData,
    type UseQueryResult,
    useQueries,
    useQueryClient,
} from "@tanstack/react-query";
import { useMemo } from "react";

import {
    buildVisibleSidebarRows,
    collectClassifiedSubGroups,
    collectManualSubGroupPaths,
    type VisibleSidebarRow,
} from "./character-sidebar-visible-rows";

// Without a stable combine, useQueries returns a new array on every render and the rows are rebuilt each time.
const selectQueryData = (results: UseQueryResult<FolderGroup[]>[]) =>
    results.map((result) => result.data);

export function useCharacterSidebarVisibleRows(
    groups: FolderGroup[],
    searchTerm: string,
    sortKey: FolderSortKey,
    sortDirection: FolderSortDirection,
    hideEmptyGroups: boolean,
    classification?: Classification | null,
): VisibleSidebarRow[] {
    const queryClient = useQueryClient();
    const expandedGroups = useModStore((s) => s.expandedGroups);
    const persistentGroups = useModStore((s) => s.persistentGroups);
    const collapsedSections = useModStore((s) => s.collapsedSections);
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

    const subGroupData = useQueries({
        queries: subGroupPaths.map((path) => ({
            queryKey: ["subGroups", path] as const,
            queryFn: async () => ((await Mod.GetSubGroups(path, null)) ?? []) as FolderGroup[],
            placeholderData: keepPreviousData,
        })),
        combine: selectQueryData,
    });

    const subGroupsByPath = useMemo(() => {
        const map = new Map<string, FolderGroup[]>();
        for (const [index, path] of subGroupPaths.entries()) {
            map.set(path, subGroupData[index] ?? []);
        }
        return map;
    }, [subGroupData, subGroupPaths]);

    // Classified sub folders are listed without their parent being expanded, so they are walked as roots too.
    const roots = classification
        ? [...groups, ...collectClassifiedSubGroups(groups, classification)]
        : groups;
    const collectedManualPaths = collectManualSubGroupPaths(roots, {
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

    const manualData = useQueries({
        queries: manualPaths.map((path) => ({
            queryKey: ["manualSubGroups", path] as const,
            queryFn: async () =>
                ((await Mod.GetManualSubGroups(path, null)) ?? []) as FolderGroup[],
            placeholderData: keepPreviousData,
        })),
        combine: selectQueryData,
    });

    const manualSubGroupsByPath = useMemo(() => {
        const map = new Map<string, FolderGroup[]>();
        for (const [index, path] of manualPaths.entries()) {
            map.set(path, manualData[index] ?? []);
        }
        return map;
    }, [manualData, manualPaths]);

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
                classification,
                collapsedSections,
            }),
        [
            classification,
            collapsedSections,
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
