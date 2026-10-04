import type { FolderSortDirection, FolderSortKey } from "@renderer/store/mod";
import type { FolderGroup } from "@renderer/types/mod";
import type { Classification } from "@shared/types";

export interface VisibleGroupRow {
    kind: "group";
    group: FolderGroup;
    depth: number;
    parentGroupName?: string;
    collapseGroupPath?: string;
}

export interface VisibleSectionRow {
    kind: "section";
    key: string;
    // null marks the section of folders that are not assigned to any group.
    name: string | null;
    count: number;
    collapsed: boolean;
}

export type VisibleSidebarRow = VisibleGroupRow | VisibleSectionRow;

export interface SidebarSection {
    key: string;
    name: string | null;
    groups: FolderGroup[];
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

// Splits top-level folders into one section per classification group, in the classification's order.
// Folders without an assignment, or assigned to a group that no longer exists, land in the last section.
export function partitionByClassification(
    groups: FolderGroup[],
    classification: Classification,
): SidebarSection[] {
    const sections = new Map<string, SidebarSection>(
        classification.groups.map((group) => [
            group.id,
            { key: `${classification.id}:${group.id}`, name: group.name, groups: [] },
        ]),
    );
    const unassigned: SidebarSection = { key: `${classification.id}:`, name: null, groups: [] };

    for (const group of groups) {
        const section = sections.get(group.classifications?.[classification.id] ?? "");
        (section ?? unassigned).groups.push(group);
    }

    return [...sections.values(), unassigned];
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
        classification?: Classification | null;
        collapsedSections?: Set<string>;
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
            rows.push({ kind: "group", group, depth, parentGroupName, collapseGroupPath });
        }

        if (!showChildGroups) {
            return;
        }

        for (const sub of groupsToRender) {
            visit(sub, depth + 1, group.name, group.path);
        }
    };

    if (!options.classification) {
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

    for (const section of partitionByClassification(groups, options.classification)) {
        const visibleGroups = getVisibleGroups(
            section.groups,
            options.sortKey,
            options.sortDirection,
            options.hideEmptyGroups,
        );
        if (section.name === null && visibleGroups.length === 0) {
            continue;
        }

        // Searching ignores collapse so matches inside a collapsed section stay reachable.
        const collapsed = !isSearching && !!options.collapsedSections?.has(section.key);
        const headerIndex = rows.length;
        rows.push({
            kind: "section",
            key: section.key,
            name: section.name,
            count: visibleGroups.length,
            collapsed,
        });
        if (collapsed) {
            continue;
        }

        for (const group of visibleGroups) {
            visit(group, 0, undefined, undefined);
        }
        if (isSearching && rows.length === headerIndex + 1) {
            rows.pop();
        }
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
        manualSubGroupsByPath: { get(path: string): FolderGroup[] | undefined };
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
