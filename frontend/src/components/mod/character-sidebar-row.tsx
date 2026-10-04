import { useModStore } from "@renderer/store/mod";
import type { FolderGroup } from "@renderer/types/mod";
import { useVirtualizer } from "@tanstack/react-virtual";
import { type CSSProperties, useCallback, useEffect, useRef } from "react";

import type { CharacterSidebarContentProps } from "./character-sidebar-content";

import { CharacterSidebarItem, CharacterSidebarItemSkeleton } from "./character-sidebar-item";
import { CharacterSidebarSectionHeader } from "./character-sidebar-section-header";
import { useCharacterSidebarVisibleRows } from "./use-character-sidebar-visible-rows";

const containerStyle: CSSProperties = {
  gridTemplateColumns: "auto 1fr auto",
};

const itemClassName =
  "relative grid h-14 items-center gap-3 overflow-hidden py-2 pr-4 hover:bg-[#cecece] dark:hover:bg-[#2a2a2a]";
const selectedItemClassName = "bg-[#cecece] dark:bg-[#2a2a2a]";
const rowHeight = 56;
const sectionHeight = 32;
const itemStyles = new Map<number, CSSProperties>();

function getItemStyle(depth: number): CSSProperties {
  const cachedStyle = itemStyles.get(depth);
  if (cachedStyle) {
    return cachedStyle;
  }

  const style = {
    ...containerStyle,
    paddingLeft: depth > 0 ? `${depth * 16 + 8}px` : "8px",
  };
  itemStyles.set(depth, style);
  return style;
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
  searchTerm,
  sortKey,
  sortDirection,
  hideEmptyGroups,
  onCreateFolder,
  onDeleteFolder,
  onManualSubGroupChange,
  showSkeleton,
  previewCacheKey,
  modFixer,
  onOpenModFixer,
  classification,
}: CharacterSidebarRowProps) {
  const setExpandedGroup = useModStore((s) => s.setExpandedGroup);
  const toggleCollapsedSection = useModStore((s) => s.toggleCollapsedSection);
  const expandedGroups = useModStore((s) => s.expandedGroups);
  const persistentGroups = useModStore((s) => s.persistentGroups);
  const selectedGroupPath = useModStore((s) => s.selectedGroup?.path);
  const lastScrolledPathRef = useRef<string | null>(null);
  const skipNextScrollRef = useRef(false);
  const isSearching = searchTerm.trim().length > 0;

  const rows = useCharacterSidebarVisibleRows(
    groups,
    searchTerm,
    sortKey,
    sortDirection,
    hideEmptyGroups,
    classification,
  );
  const rowVirtualizer = useVirtualizer({
    count: rows.length,
    getScrollElement: () => viewport,
    estimateSize: (index) => (rows[index]?.kind === "section" ? sectionHeight : rowHeight),
    getItemKey: (index) => {
      const row = rows[index];
      if (!row) {
        return index;
      }
      return row.kind === "section" ? `section:${row.key}` : row.group.path;
    },
    overscan: 4,
    initialRect: viewport
      ? { width: viewport.clientWidth, height: viewport.clientHeight }
      : undefined,
  });

  useEffect(() => {
    onVisibleRowsChange?.(
      rows.flatMap((row) =>
        row.kind === "group" ? [{ path: row.group.path, group: row.group }] : [],
      ),
    );
  }, [onVisibleRowsChange, rows]);

  const scrollToPath = useCallback(
    (path: string) => {
      const index = rows.findIndex((row) => row.kind === "group" && row.group.path === path);
      if (!viewport || index < 0) {
        return false;
      }

      rowVirtualizer.scrollToIndex(index, { align: "center" });
      return true;
    },
    [rowVirtualizer, rows, viewport],
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
  }, [rows, scrollToPath, selectedGroupPath]);

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

  return (
    <div className="relative w-full" style={{ height: `${rowVirtualizer.getTotalSize()}px` }}>
      {rowVirtualizer.getVirtualItems().map((virtualRow) => {
        const row = rows[virtualRow.index];
        if (!row) {
          return null;
        }

        return (
          <div
            key={virtualRow.key}
            data-index={virtualRow.index}
            className="absolute top-0 left-0 w-full"
            style={{ transform: `translateY(${virtualRow.start}px)` }}
          >
            {row.kind === "section" ? (
              <CharacterSidebarSectionHeader
                name={row.name}
                count={row.count}
                collapsed={row.collapsed}
                onToggle={() => toggleCollapsedSection(row.key)}
              />
            ) : (
              <CharacterSidebarItem
                itemRefs={itemRefs}
                group={row.group}
                onClick={handleItemClick}
                collapseGroupPath={row.collapseGroupPath}
                onDrop={onItemDrop}
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
                modFixer={modFixer}
                onOpenModFixer={onOpenModFixer}
                forceSelectOnClick={isSearching}
                autoScrollOnSelect={false}
              />
            )}
          </div>
        );
      })}
    </div>
  );
}
