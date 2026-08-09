import type { FolderSortDirection, FolderSortKey } from "@renderer/store/mod";
import type { FolderGroup } from "@renderer/types/mod";

export interface VisibleSidebarRow {
    group: FolderGroup;
    depth: number;
    parentGroupName?: string;
    collapseGroupPath?: string;
}

export function getVisibleGroups(
    groups: FolderGroup[],
    sortKey: FolderSortKey,
    sortDirection: FolderSortDirection,
    hideEmptyGroups: boolean,
) {
    return groups
        .filter(
            (group) =>
                !hideEmptyGroups ||
                (group.modCount ?? group.mods.length) > 0 ||
                group.hasSubGroups ||
                group.hasManualSubGroups,
        )
        .toSorted((a, b) => {
            const comparison =
                sortKey === "name"
                    ? a.name.localeCompare(b.name, undefined, {
                          numeric: true,
                          sensitivity: "base",
                      })
                    : sortKey === "mod-count"
                      ? (a.modCount ?? a.mods.length) - (b.modCount ?? b.mods.length)
                      : (a.enabledModCount ?? a.mods.filter((mod) => mod.isEnabled).length) -
                        (b.enabledModCount ?? b.mods.filter((mod) => mod.isEnabled).length);

            return sortDirection === "ascending" ? comparison : -comparison;
        });
}

export function buildVisibleSidebarRows(
    groups: FolderGroup[],
    options: {
        searchTerm: string;
        sortKey: FolderSortKey;
        sortDirection: FolderSortDirection;
        hideEmptyGroups: boolean;
        expandedGroups: Set<string>;
        persistentGroups: Set<string>;
        subGroupsByPath: Map<string, FolderGroup[]>;
        manualSubGroupsByPath: Map<string, FolderGroup[]>;
    },
): VisibleSidebarRow[] {
    const normalizedSearch = options.searchTerm.trim().toLowerCase();
    const isSearching = normalizedSearch.length > 0;
    const rows: VisibleSidebarRow[] = [];

    const visit = (
        group: FolderGroup,
        depth: number,
        parentGroupName: string | undefined,
        collapseGroupPath: string | undefined,
    ) => {
        const isExpanded = options.expandedGroups.has(group.path);
        const isPersistent = options.persistentGroups.has(group.path);
        const showSubGroups = isExpanded || (isSearching && isPersistent);
        const childGroups = showSubGroups
            ? (options.subGroupsByPath.get(group.path) ?? [])
            : (options.manualSubGroupsByPath.get(group.path) ?? []);
        const visibleChildGroups =
            isSearching && !showSubGroups
                ? childGroups.filter((sub) => sub.name.toLowerCase().includes(normalizedSearch))
                : childGroups;
        const groupsToRender = getVisibleGroups(
            showSubGroups ? childGroups : visibleChildGroups,
            options.sortKey,
            options.sortDirection,
            options.hideEmptyGroups,
        );
        const shouldShowParent =
            !isSearching || group.name.toLowerCase().includes(normalizedSearch);
        const showChildGroups = groupsToRender.length > 0;

        if (!shouldShowParent && !showChildGroups) {
            return;
        }

        if (shouldShowParent) {
            rows.push({ group, depth, parentGroupName, collapseGroupPath });
        }

        if (!showChildGroups) {
            return;
        }

        for (const sub of groupsToRender) {
            visit(sub, depth + 1, group.name, group.path);
        }
    };

    for (const group of getVisibleGroups(
        groups,
        options.sortKey,
        options.sortDirection,
        options.hideEmptyGroups,
    )) {
        visit(group, 0, undefined, undefined);
    }

    return rows;
}

export function collectManualSubGroupPaths(
    groups: FolderGroup[],
    options: {
        isSearching: boolean;
        expandedGroups: Set<string>;
        persistentGroups: Set<string>;
        subGroupsByPath: Map<string, FolderGroup[]>;
        manualSubGroupsByPath: Map<string, FolderGroup[]>;
    },
) {
    const paths: string[] = [];
    const seen = new Set<string>();
    const visited = new Set<string>();

    const visit = (group: FolderGroup) => {
        if (visited.has(group.path)) return;
        visited.add(group.path);

        const shouldFetchSubGroups =
            options.expandedGroups.has(group.path) ||
            (options.isSearching && options.persistentGroups.has(group.path));

        if (shouldFetchSubGroups) {
            for (const child of options.subGroupsByPath.get(group.path) ?? []) {
                visit(child);
            }
            return;
        }

        if (group.hasManualSubGroups && !seen.has(group.path)) {
            seen.add(group.path);
            paths.push(group.path);
            for (const child of options.manualSubGroupsByPath.get(group.path) ?? []) {
                visit(child);
            }
        }
    };

    for (const group of groups) {
        visit(group);
    }

    return paths;
}
