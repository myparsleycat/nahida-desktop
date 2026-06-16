import { useModStore } from "@renderer/store/mod";
import type { FolderGroup } from "@renderer/types/mod";
import type { SidebarLayoutMode } from "@shared/mod";
import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { memo, useCallback, useMemo } from "react";
import { CharacterSidebarItem, CharacterSidebarItemSkeleton } from "./character-sidebar-item";

export interface CharacterSidebarContentProps {
  groups: FolderGroup[];
  itemRefs: React.MutableRefObject<Map<string, { element: HTMLElement; group: FolderGroup }>>;
  onItemClick: (group: FolderGroup, e: React.MouseEvent) => void;
  onItemDrop: (group: FolderGroup, files: File[]) => void;
  canAcceptDrop: (files: File[]) => boolean;
  searchTerm: string;
  onCreateFolder: (group: FolderGroup) => void;
  onDeleteFolder: (group: FolderGroup) => void;
  onManualSubGroupChange: (group: FolderGroup, enabled: boolean) => void;
  showSkeleton: boolean;
  previewCacheKey: number;
  showWuwaFixer?: boolean;
  onOpenWuwaFixer?: (path: string) => Promise<void>;
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
    queryFn: () => window.api.invoke("mod:getSubGroups", group.path),
    enabled: shouldFetch,
    placeholderData: keepPreviousData,
  });

  return subGroups;
}

function useManualSubGroups(group: FolderGroup, shouldFetch: boolean) {
  const { data: manualSubGroups = [] } = useQuery<FolderGroup[]>({
    queryKey: ["manualSubGroups", group.path],
    queryFn: () => window.api.invoke("mod:getManualSubGroups", group.path),
    enabled: shouldFetch,
    placeholderData: keepPreviousData,
  });

  return manualSubGroups;
}

interface CharacterSidebarItemWithChildrenProps {
  group: FolderGroup;
  itemRefs: React.MutableRefObject<Map<string, { element: HTMLElement; group: FolderGroup }>>;
  onItemClick: (group: FolderGroup, e: React.MouseEvent) => void;
  onItemDrop: (group: FolderGroup, files: File[]) => void;
  canAcceptDrop: (files: File[]) => boolean;
  depth: number;
  searchTerm: string;
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
  showWuwaFixer?: boolean;
  onOpenWuwaFixer?: (path: string) => Promise<void>;
}

const CharacterSidebarItemWithChildren = memo(function CharacterSidebarItemWithChildren({
  group,
  itemRefs,
  onItemClick,
  onItemDrop,
  canAcceptDrop,
  depth,
  searchTerm,
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
  showWuwaFixer,
  onOpenWuwaFixer,
}: CharacterSidebarItemWithChildrenProps) {
  const isExpanded = useModStore((s) => s.expandedGroups.has(group.path));
  const isPersistent = useModStore((s) => s.persistentGroups.has(group.path));
  const toggleExpandedGroup = useModStore((s) => s.toggleExpandedGroup);
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
  const showChildGroups = showSubGroups
    ? childGroups.length > 0
    : manualSubGroups.length > 0 && (!isSearching || visibleChildGroups.length > 0);
  const groupsToRender = showSubGroups ? childGroups : visibleChildGroups;
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

  const handleItemClickInternal = useCallback(
    (clickedGroup: FolderGroup, e: React.MouseEvent) => {
      if (e.ctrlKey && collapseGroupPath) {
        toggleExpandedGroup(collapseGroupPath);
        return;
      }

      onItemClick(clickedGroup, e);
    },
    [collapseGroupPath, onItemClick, toggleExpandedGroup],
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
          onClick={handleItemClickInternal}
          onDrop={onItemDrop}
          canAcceptDrop={canAcceptDrop}
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
          showWuwaFixer={showWuwaFixer}
          onOpenWuwaFixer={onOpenWuwaFixer}
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
            canAcceptDrop={canAcceptDrop}
            collapseGroupPath={group.path}
            depth={depth + 1}
            searchTerm={searchTerm}
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
            showWuwaFixer={showWuwaFixer}
            onOpenWuwaFixer={onOpenWuwaFixer}
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
  canAcceptDrop,
  searchTerm,
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
  showWuwaFixer,
  onOpenWuwaFixer,
}: CharacterSidebarContentLayoutProps) {
  return (
    <div className={listClassName} style={listStyle}>
      {showSkeleton
        ? Array.from({ length: 8 }).map((_, index) => (
            <CharacterSidebarItemSkeleton key={index.toString()} layout={layout} />
          ))
        : groups.map((group) => (
            <CharacterSidebarItemWithChildren
              key={group.path}
              group={group}
              itemRefs={itemRefs}
              onItemClick={onItemClick}
              onItemDrop={onItemDrop}
              canAcceptDrop={canAcceptDrop}
              depth={0}
              searchTerm={searchTerm}
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
              showWuwaFixer={showWuwaFixer}
              onOpenWuwaFixer={onOpenWuwaFixer}
            />
          ))}
    </div>
  );
}
