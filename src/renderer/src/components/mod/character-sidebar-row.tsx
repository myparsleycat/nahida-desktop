import { useVirtualizationSettings } from "@renderer/hooks/use-settings";
import { useModStore } from "@renderer/store/mod";
import type { FolderGroup } from "@renderer/types/mod";
import { useVirtualizer } from "@tanstack/react-virtual";
import { type CSSProperties, useCallback, useEffect, useMemo, useRef } from "react";

import type { CharacterSidebarContentProps } from "./character-sidebar-content";

import { CharacterSidebarItem, CharacterSidebarItemSkeleton } from "./character-sidebar-item";
import { useCharacterSidebarVisibleRows } from "./use-character-sidebar-visible-rows";

const ROW_HEIGHT = 56;

const containerStyle: CSSProperties = {
  gridTemplateColumns: "auto 1fr auto",
};

const itemClassName =
  "relative grid h-14 items-center gap-3 overflow-hidden py-2 pr-4 hover:bg-[#cecece] dark:hover:bg-[#2a2a2a]";
const selectedItemClassName = "bg-[#cecece] dark:bg-[#2a2a2a]";

function getItemStyle(depth: number): CSSProperties {
  return {
    ...containerStyle,
    paddingLeft: depth > 0 ? `${depth * 16 + 8}px` : "8px",
  };
}

export interface CharacterSidebarRowProps extends CharacterSidebarContentProps {
  viewport: HTMLDivElement | null;
  scrollToPathRef?: React.MutableRefObject<((path: string) => void) | null>;
  onVisibleRowsChange?: (rows: { path: string; group: FolderGroup }[]) => void;
}

export function CharacterSidebarRow({
  viewport,
  scrollToPathRef,
  onVisibleRowsChange,
  groups,
  itemRefs,
  onItemClick,
  onItemDrop,
  canAcceptDrop,
  searchTerm,
  sortKey,
  sortDirection,
  hideEmptyGroups,
  onCreateFolder,
  onDeleteFolder,
  onManualSubGroupChange,
  showSkeleton,
  previewCacheKey,
  showWuwaFixer,
  onOpenWuwaFixer,
}: CharacterSidebarRowProps) {
  const setExpandedGroup = useModStore((s) => s.setExpandedGroup);
  const expandedGroups = useModStore((s) => s.expandedGroups);
  const persistentGroups = useModStore((s) => s.persistentGroups);
  const selectedGroupPath = useModStore((s) => s.selectedGroup?.path);
  const lastScrolledPathRef = useRef<string | null>(null);
  const skipNextScrollRef = useRef(false);
  const { data: vSettings } = useVirtualizationSettings();
  const isSearching = searchTerm.trim().length > 0;

  const rows = useCharacterSidebarVisibleRows(
    groups,
    searchTerm,
    sortKey,
    sortDirection,
    hideEmptyGroups,
  );

  useEffect(() => {
    onVisibleRowsChange?.(rows.map((row) => ({ path: row.group.path, group: row.group })));
  }, [onVisibleRowsChange, rows]);

  const isVirtualizationEnabled =
    (vSettings?.enabled ?? true) && rows.length >= (vSettings?.threshold ?? 30);

  const rowVirtualizer = useVirtualizer({
    count: isVirtualizationEnabled ? rows.length : 0,
    getItemKey: (index) => rows[index]?.group.path ?? index,
    getScrollElement: () => viewport,
    estimateSize: useCallback(() => ROW_HEIGHT, []),
    overscan: 8,
  });

  const pathToIndex = useMemo(() => {
    const map = new Map<string, number>();
    for (const [index, row] of rows.entries()) {
      map.set(row.group.path, index);
    }
    return map;
  }, [rows]);

  const scrollToPath = useCallback(
    (path: string) => {
      const index = pathToIndex.get(path);
      if (index == null) {
        return false;
      }

      if (isVirtualizationEnabled) {
        rowVirtualizer.scrollToIndex(index, { align: "center" });
        return true;
      }

      const element = itemRefs.current.get(path)?.element;
      if (!element) {
        return false;
      }
      element.scrollIntoView({ behavior: "auto", block: "center" });
      return true;
    },
    [isVirtualizationEnabled, itemRefs, pathToIndex, rowVirtualizer],
  );

  useEffect(() => {
    if (!scrollToPathRef) {
      return;
    }
    scrollToPathRef.current = (path: string) => {
      if (scrollToPath(path)) {
        lastScrolledPathRef.current = path;
      }
    };
    return () => {
      scrollToPathRef.current = null;
    };
  }, [scrollToPath, scrollToPathRef]);

  useEffect(() => {
    if (!selectedGroupPath || lastScrolledPathRef.current === selectedGroupPath) {
      return;
    }
    if (skipNextScrollRef.current) {
      skipNextScrollRef.current = false;
      lastScrolledPathRef.current = selectedGroupPath;
      return;
    }
    const frame = requestAnimationFrame(() => {
      if (scrollToPath(selectedGroupPath)) {
        lastScrolledPathRef.current = selectedGroupPath;
      }
    });
    return () => cancelAnimationFrame(frame);
  }, [pathToIndex, scrollToPath, selectedGroupPath]);

  const handleItemClick = useCallback(
    (group: FolderGroup, e: React.MouseEvent, collapseGroupPath?: string) => {
      if (collapseGroupPath) {
        const isExpanded = expandedGroups.has(collapseGroupPath);
        const showSubGroups =
          isExpanded || (isSearching && persistentGroups.has(collapseGroupPath));
        if (showSubGroups && !isExpanded) {
          setExpandedGroup(collapseGroupPath, true);
        }
      }

      skipNextScrollRef.current = true;
      onItemClick(group, e);
    },
    [expandedGroups, isSearching, onItemClick, persistentGroups, setExpandedGroup],
  );

  if (showSkeleton) {
    return (
      <div className="flex flex-col">
        {Array.from({ length: 8 }).map((_, index) => (
          <CharacterSidebarItemSkeleton key={index.toString()} layout="row" />
        ))}
      </div>
    );
  }

  if (!isVirtualizationEnabled) {
    return (
      <div className="flex flex-col">
        {rows.map((row) => (
          <CharacterSidebarItem
            key={row.group.path}
            itemRefs={itemRefs}
            group={row.group}
            onClick={(group, e) => handleItemClick(group, e, row.collapseGroupPath)}
            collapseGroupPath={row.collapseGroupPath}
            onDrop={onItemDrop}
            canAcceptDrop={canAcceptDrop}
            onCreateFolder={onCreateFolder}
            onDeleteFolder={onDeleteFolder}
            onManualSubGroupChange={onManualSubGroupChange}
            depth={row.depth}
            previewCacheKey={previewCacheKey}
            layout="row"
            parentGroupName={row.parentGroupName}
            itemClassName={itemClassName}
            selectedItemClassName={selectedItemClassName}
            itemStyle={getItemStyle(row.depth)}
            showWuwaFixer={showWuwaFixer}
            onOpenWuwaFixer={onOpenWuwaFixer}
            forceSelectOnClick={isSearching}
            autoScrollOnSelect={false}
          />
        ))}
      </div>
    );
  }

  return (
    <div
      style={{
        height: `${rowVirtualizer.getTotalSize()}px`,
        width: "100%",
        position: "relative",
      }}
    >
      {rowVirtualizer.getVirtualItems().map((virtualRow) => {
        const row = rows[virtualRow.index];
        if (!row) {
          return null;
        }

        return (
          <div
            key={virtualRow.key}
            data-index={virtualRow.index}
            style={{
              position: "absolute",
              top: 0,
              left: 0,
              width: "100%",
              height: `${virtualRow.size}px`,
              transform: `translateY(${virtualRow.start}px)`,
            }}
          >
            <CharacterSidebarItem
              itemRefs={itemRefs}
              group={row.group}
              onClick={(group, e) => handleItemClick(group, e, row.collapseGroupPath)}
              collapseGroupPath={row.collapseGroupPath}
              onDrop={onItemDrop}
              canAcceptDrop={canAcceptDrop}
              onCreateFolder={onCreateFolder}
              onDeleteFolder={onDeleteFolder}
              onManualSubGroupChange={onManualSubGroupChange}
              depth={row.depth}
              previewCacheKey={previewCacheKey}
              layout="row"
              parentGroupName={row.parentGroupName}
              itemClassName={itemClassName}
              selectedItemClassName={selectedItemClassName}
              itemStyle={getItemStyle(row.depth)}
              showWuwaFixer={showWuwaFixer}
              onOpenWuwaFixer={onOpenWuwaFixer}
              forceSelectOnClick={isSearching}
              autoScrollOnSelect={false}
            />
          </div>
        );
      })}
    </div>
  );
}
