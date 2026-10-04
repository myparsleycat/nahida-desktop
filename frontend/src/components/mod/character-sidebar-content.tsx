import { Mod } from "@bindings/mod";
import { type FolderSortDirection, type FolderSortKey, useModStore } from "@renderer/store/mod";
import type { FolderGroup } from "@renderer/types/mod";
import type { SidebarLayoutMode } from "@shared/mod";
import type { ModFixerAction } from "@shared/types";
import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { memo, useCallback, useMemo } from "react";

import { CharacterSidebarItem, CharacterSidebarItemSkeleton } from "./character-sidebar-item";
import { getVisibleGroups } from "./character-sidebar-visible-rows";

export interface CharacterSidebarContentProps {
  groups: FolderGroup[];
  itemRefs: React.MutableRefObject<Map<string, { element: HTMLElement; group: FolderGroup }>>;
  onItemClick: (group: FolderGroup, e: React.MouseEvent) => void;
  onItemDrop: (group: FolderGroup, paths: string[]) => void;
  searchTerm: string;
  sortKey: FolderSortKey;
  sortDirection: FolderSortDirection;
  hideEmptyGroups: boolean;
  onCreateFolder: (group: FolderGroup) => void;
  onDeleteFolder: (group: FolderGroup) => void;
  onManualSubGroupChange: (group: FolderGroup, enabled: boolean) => void;
  showSkeleton: boolean;
  previewCacheKey: number;
  modFixer?: ModFixerAction | null;
  onOpenModFixer?: (path: string) => Promise<void>;
}

interface CharacterSidebarContentLayoutProps extends CharacterSidebarContentProps {
  layout: SidebarLayoutMode;
  listClassName: string;
  listStyle?: React.CSSProperties;
  itemClassName: string;
  selectedItemClassName: string;
  nestedItemClassName?: string;
  itemStyle?: (depth: number) => React.CSSProperties | undefined;
}

function useSubGroups(group: FolderGroup, shouldFetch: boolean) {
  const { data: subGroups = [] } = useQuery<FolderGroup[]>({
    queryKey: ["subGroups", group.path],
    queryFn: async () => ((await Mod.GetSubGroups(group.path, null)) ?? []) as FolderGroup[],
    enabled: shouldFetch,
    placeholderData: keepPreviousData,
  });

  return subGroups;
}

function useManualSubGroups(group: FolderGroup, shouldFetch: boolean) {
  const { data: manualSubGroups = [] } = useQuery<FolderGroup[]>({
    queryKey: ["manualSubGroups", group.path],
    queryFn: async () => ((await Mod.GetManualSubGroups(group.path, null)) ?? []) as FolderGroup[],
    enabled: shouldFetch,
    placeholderData: keepPreviousData,
  });

  return manualSubGroups;
}

interface CharacterSidebarItemWithChildrenProps {
  group: FolderGroup;
  itemRefs: React.MutableRefObject<Map<string, { element: HTMLElement; group: FolderGroup }>>;
  onItemClick: (group: FolderGroup, e: React.MouseEvent) => void;
  onItemDrop: (group: FolderGroup, paths: string[]) => void;
  depth: number;
  searchTerm: string;
  sortKey: FolderSortKey;
  sortDirection: FolderSortDirection;
  hideEmptyGroups: boolean;
  onCreateFolder: (group: FolderGroup) => void;
  onDeleteFolder: (group: FolderGroup) => void;
  onManualSubGroupChange: (group: FolderGroup, enabled: boolean) => void;
  layout: SidebarLayoutMode;
  listClassName: string;
  listStyle?: React.CSSProperties;
  itemClassName: string;
  selectedItemClassName: string;
  nestedItemClassName?: string;
  itemStyle?: (depth: number) => React.CSSProperties | undefined;
  parentGroupName?: string;
  collapseGroupPath?: string;
  previewCacheKey: number;
  modFixer?: ModFixerAction | null;
  onOpenModFixer?: (path: string) => Promise<void>;
}

const CharacterSidebarItemWithChildren = memo(function CharacterSidebarItemWithChildren({
  group,
  itemRefs,
  onItemClick,
  onItemDrop,
  depth,
  searchTerm,
  sortKey,
  sortDirection,
  hideEmptyGroups,
  onCreateFolder,
  onDeleteFolder,
  onManualSubGroupChange,
  previewCacheKey,
  layout,
  listClassName: _listClassName,
  listStyle: _listStyle,
  itemClassName,
  selectedItemClassName,
  nestedItemClassName,
  itemStyle,
  parentGroupName,
  collapseGroupPath,
  modFixer,
  onOpenModFixer,
}: CharacterSidebarItemWithChildrenProps) {
  const isExpanded = useModStore((s) => s.expandedGroups.has(group.path));
  const isPersistent = useModStore((s) => s.persistentGroups.has(group.path));
  const setExpandedGroup = useModStore((s) => s.setExpandedGroup);
  const normalizedSearch = searchTerm.trim().toLowerCase();
  const isSearching = normalizedSearch.length > 0;
  const shouldFetchSubGroups = isExpanded || (isSearching && isPersistent);
  const subGroups = useSubGroups(group, shouldFetchSubGroups);
  const manualSubGroups = useManualSubGroups(
    group,
    !!group.hasManualSubGroups && !shouldFetchSubGroups,
  );
  const isSelfMatch = !isSearching || group.name.toLowerCase().includes(normalizedSearch);
  const shouldShowParent = isSelfMatch;
  const showSubGroups = isExpanded || (isSearching && isPersistent);
  const childGroups = showSubGroups ? subGroups : manualSubGroups;
  const visibleChildGroups =
    isSearching && !showSubGroups
      ? childGroups.filter((sub) => sub.name.toLowerCase().includes(normalizedSearch))
      : childGroups;
  const groupsToRender = getVisibleGroups(
    showSubGroups ? childGroups : visibleChildGroups,
    sortKey,
    sortDirection,
    hideEmptyGroups,
  );
  const showChildGroups = groupsToRender.length > 0;
  const resolvedItemStyle = useMemo(() => itemStyle?.(depth), [depth, itemStyle]);

  const handleChildItemClick = useCallback(
    (clickedGroup: FolderGroup, e: React.MouseEvent) => {
      if (showSubGroups && !isExpanded) {
        setExpandedGroup(group.path, true);
      }
      onItemClick(clickedGroup, e);
    },
    [group.path, isExpanded, onItemClick, setExpandedGroup, showSubGroups],
  );

  if (!shouldShowParent && !showChildGroups) {
    return null;
  }

  return (
    <>
      {shouldShowParent && (
        <CharacterSidebarItem
          itemRefs={itemRefs}
          group={group}
          onClick={onItemClick}
          collapseGroupPath={collapseGroupPath}
          onDrop={onItemDrop}
          onCreateFolder={onCreateFolder}
          onDeleteFolder={onDeleteFolder}
          onManualSubGroupChange={onManualSubGroupChange}
          depth={depth}
          previewCacheKey={previewCacheKey}
          layout={layout}
          parentGroupName={parentGroupName}
          itemClassName={itemClassName}
          selectedItemClassName={selectedItemClassName}
          nestedItemClassName={nestedItemClassName}
          itemStyle={resolvedItemStyle}
          modFixer={modFixer}
          onOpenModFixer={onOpenModFixer}
          forceSelectOnClick={isSearching}
        />
      )}
      {showChildGroups &&
        groupsToRender.map((sub) => (
          <CharacterSidebarItemWithChildren
            key={sub.path}
            group={sub}
            itemRefs={itemRefs}
            onItemClick={handleChildItemClick}
            onItemDrop={onItemDrop}
            collapseGroupPath={group.path}
            depth={depth + 1}
            searchTerm={searchTerm}
            sortKey={sortKey}
            sortDirection={sortDirection}
            hideEmptyGroups={hideEmptyGroups}
            onCreateFolder={onCreateFolder}
            onDeleteFolder={onDeleteFolder}
            onManualSubGroupChange={onManualSubGroupChange}
            previewCacheKey={previewCacheKey}
            layout={layout}
            listClassName={_listClassName}
            listStyle={_listStyle}
            itemClassName={itemClassName}
            selectedItemClassName={selectedItemClassName}
            nestedItemClassName={nestedItemClassName}
            itemStyle={itemStyle}
            parentGroupName={group.name}
            modFixer={modFixer}
            onOpenModFixer={onOpenModFixer}
          />
        ))}
    </>
  );
});

export function CharacterSidebarContent({
  groups,
  itemRefs,
  onItemClick,
  onItemDrop,
  searchTerm,
  sortKey,
  sortDirection,
  hideEmptyGroups,
  onCreateFolder,
  onDeleteFolder,
  onManualSubGroupChange,
  showSkeleton,
  previewCacheKey,
  layout,
  listClassName,
  listStyle,
  itemClassName,
  selectedItemClassName,
  nestedItemClassName,
  itemStyle,
  modFixer,
  onOpenModFixer,
}: CharacterSidebarContentLayoutProps) {
  return (
    <div className={listClassName} style={listStyle}>
      {showSkeleton
        ? Array.from({ length: 8 }).map((_, index) => (
            <CharacterSidebarItemSkeleton key={index.toString()} layout={layout} />
          ))
        : getVisibleGroups(groups, sortKey, sortDirection, hideEmptyGroups).map((group) => (
            <CharacterSidebarItemWithChildren
              key={group.path}
              group={group}
              itemRefs={itemRefs}
              onItemClick={onItemClick}
              onItemDrop={onItemDrop}
              depth={0}
              searchTerm={searchTerm}
              sortKey={sortKey}
              sortDirection={sortDirection}
              hideEmptyGroups={hideEmptyGroups}
              onCreateFolder={onCreateFolder}
              onDeleteFolder={onDeleteFolder}
              onManualSubGroupChange={onManualSubGroupChange}
              layout={layout}
              previewCacheKey={previewCacheKey}
              listClassName={listClassName}
              listStyle={listStyle}
              itemClassName={itemClassName}
              selectedItemClassName={selectedItemClassName}
              nestedItemClassName={nestedItemClassName}
              itemStyle={itemStyle}
              modFixer={modFixer}
              onOpenModFixer={onOpenModFixer}
            />
          ))}
    </div>
  );
}
