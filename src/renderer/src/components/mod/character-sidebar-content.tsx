import { useModStore } from "@renderer/store/mod";
import type { FolderGroup } from "@renderer/types/mod";
import type { SidebarLayoutMode } from "@shared/mod";
import { memo, useCallback, useEffect, useState } from "react";
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
  refreshKey: number;
  showSkeleton: boolean;
  previewCacheKey: number;
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

function useSubGroups(group: FolderGroup, shouldFetch: boolean, refreshKey: number) {
  const [subGroups, setSubGroups] = useState<FolderGroup[]>([]);

  useEffect(() => {
    if (!shouldFetch) {
      setSubGroups([]);
      return;
    }

    let cancelled = false;

    window.api
      .invoke("mod:getSubGroups", group.path)
      .then((result: FolderGroup[]) => {
        if (!cancelled) {
          setSubGroups(result);
        }
      })
      .catch(() => {
        if (!cancelled) {
          setSubGroups([]);
        }
      });

    return () => {
      cancelled = true;
    };
  }, [shouldFetch, group.path, refreshKey]);

  return subGroups;
}

interface CharacterSidebarItemWithChildrenProps {
  group: FolderGroup;
  itemRefs: React.MutableRefObject<Map<string, { element: HTMLElement; group: FolderGroup }>>;
  onItemClick: (group: FolderGroup, e: React.MouseEvent) => void;
  onItemDrop: (group: FolderGroup, files: File[]) => void;
  depth: number;
  searchTerm: string;
  onCreateFolder: (group: FolderGroup) => void;
  onDeleteFolder: (group: FolderGroup) => void;
  refreshKey: number;
  layout: SidebarLayoutMode;
  listClassName: string;
  listStyle?: React.CSSProperties;
  itemClassName: string;
  selectedItemClassName: string;
  nestedItemClassName?: string;
  itemStyle?: (depth: number) => React.CSSProperties | undefined;
  parentGroupName?: string;
  onCollapseSelf?: () => void;
  previewCacheKey: number;
}

const CharacterSidebarItemWithChildren = memo(function CharacterSidebarItemWithChildren({
  group,
  itemRefs,
  onItemClick,
  onItemDrop,
  canAcceptDrop,
  onCollapseSelf,
  depth,
  searchTerm,
  onCreateFolder,
  onDeleteFolder,
  refreshKey,
  previewCacheKey,
  layout,
  listClassName: _listClassName,
  listStyle: _listStyle,
  itemClassName,
  selectedItemClassName,
  nestedItemClassName,
  itemStyle,
  parentGroupName,
}: CharacterSidebarItemWithChildrenProps) {
  const selectedGroup = useModStore((s) => s.selectedGroup);
  const expandedGroups = useModStore((s) => s.expandedGroups);
  const toggleExpandedGroup = useModStore((s) => s.toggleExpandedGroup);
  const setExpandedGroup = useModStore((s) => s.setExpandedGroup);
  const persistentGroups = useModStore((s) => s.persistentGroups);

  const isExpanded = expandedGroups.has(group.path);
  const isPersistent = persistentGroups.has(group.path);
  const shouldFetchSubGroups = isExpanded || (!!searchTerm && isPersistent);
  const subGroups = useSubGroups(group, shouldFetchSubGroups, refreshKey);
  const isSelfMatch = group.name.toLowerCase().includes(searchTerm.toLowerCase());
  const shouldShowParent = !searchTerm || isSelfMatch;
  const showSubGroups = isExpanded || (!!searchTerm && isPersistent);

  const handleChildItemClick = useCallback(
    (clickedGroup: FolderGroup, e: React.MouseEvent) => {
      if (!isExpanded) {
        setExpandedGroup(group.path, true);
      }
      onItemClick(clickedGroup, e);
    },
    [group.path, isExpanded, onItemClick, setExpandedGroup],
  );

  const handleItemClickInternal = useCallback(
    (clickedGroup: FolderGroup, e: React.MouseEvent) => {
      if (e.ctrlKey && onCollapseSelf) {
        onCollapseSelf();
        return;
      }

      onItemClick(clickedGroup, e);
    },
    [onCollapseSelf, onItemClick],
  );

  if (!shouldShowParent && !showSubGroups) {
    return null;
  }

  return (
    <>
      {shouldShowParent && (
        <CharacterSidebarItem
          itemRefs={itemRefs}
          group={group}
          isSelected={selectedGroup?.path === group.path}
          onClick={handleItemClickInternal}
          onDrop={onItemDrop}
          canAcceptDrop={canAcceptDrop}
          onCreateFolder={onCreateFolder}
          onDeleteFolder={onDeleteFolder}
          depth={depth}
          previewCacheKey={previewCacheKey}
          layout={layout}
          parentGroupName={parentGroupName}
          itemClassName={itemClassName}
          selectedItemClassName={selectedItemClassName}
          nestedItemClassName={nestedItemClassName}
          itemStyle={itemStyle?.(depth)}
        />
      )}
      {showSubGroups &&
        subGroups.map((sub) => (
          <CharacterSidebarItemWithChildren
            key={sub.path}
            group={sub}
            itemRefs={itemRefs}
            onItemClick={handleChildItemClick}
            onItemDrop={onItemDrop}
            canAcceptDrop={canAcceptDrop}
            onCollapseSelf={() => toggleExpandedGroup(group.path)}
            depth={depth + 1}
            searchTerm={searchTerm}
            onCreateFolder={onCreateFolder}
            onDeleteFolder={onDeleteFolder}
            refreshKey={refreshKey}
            previewCacheKey={previewCacheKey}
            layout={layout}
            listClassName={_listClassName}
            listStyle={_listStyle}
            itemClassName={itemClassName}
            selectedItemClassName={selectedItemClassName}
            nestedItemClassName={nestedItemClassName}
            itemStyle={itemStyle}
            parentGroupName={group.name}
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
  refreshKey,
  showSkeleton,
  previewCacheKey,
  layout,
  listClassName,
  listStyle,
  itemClassName,
  selectedItemClassName,
  nestedItemClassName,
  itemStyle,
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
              refreshKey={refreshKey}
              layout={layout}
              previewCacheKey={previewCacheKey}
              listClassName={listClassName}
              listStyle={listStyle}
              itemClassName={itemClassName}
              selectedItemClassName={selectedItemClassName}
              nestedItemClassName={nestedItemClassName}
              itemStyle={itemStyle}
            />
          ))}
    </div>
  );
}
