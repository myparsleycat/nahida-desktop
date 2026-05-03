import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuSeparator,
  ContextMenuTrigger,
} from "@renderer/components/ui/context-menu";
import { Skeleton } from "@renderer/components/ui/skeleton";
import { cn } from "@renderer/lib/utils";
import { useModStore } from "@renderer/store/mod";
import type { FolderGroup } from "@renderer/types/mod";
import type { SidebarLayoutMode } from "@shared/mod";
import {
  ChevronDown,
  ChevronRight,
  FolderIcon,
  FolderMinus,
  FolderPlus,
  FolderTree,
  TrashIcon,
} from "lucide-react";
import { memo, useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { buttonVariants } from "../ui/button";
import { CharacterSidebarItemGrid } from "./character-sidebar-item-grid";
import { CharacterSidebarItemRow } from "./character-sidebar-item-row";

interface CharacterSidebarItemProps {
  group: FolderGroup;
  isSelected: boolean;
  onClick: (group: FolderGroup, e: React.MouseEvent) => void;
  onDrop?: (group: FolderGroup, files: File[]) => void;
  onCreateFolder?: (group: FolderGroup) => void;
  onDeleteFolder?: (group: FolderGroup) => void;
  itemRefs: React.MutableRefObject<Map<string, { element: HTMLElement; group: FolderGroup }>>;
  depth?: number;
  layout?: SidebarLayoutMode;
  parentGroupName?: string;
  itemClassName?: string;
  selectedItemClassName?: string;
  nestedItemClassName?: string;
  itemStyle?: React.CSSProperties;
}

export const CharacterSidebarItem = memo(function CharacterSidebarItem({
  group,
  isSelected,
  onClick,
  onDrop,
  onCreateFolder,
  onDeleteFolder,
  itemRefs,
  depth = 0,
  layout = "row",
  parentGroupName,
  itemClassName,
  selectedItemClassName,
  nestedItemClassName,
  itemStyle,
}: CharacterSidebarItemProps) {
  const { t } = useTranslation();
  const expandedGroups = useModStore((s) => s.expandedGroups);
  const persistentGroups = useModStore((s) => s.persistentGroups);
  const toggleExpandedGroup = useModStore((s) => s.toggleExpandedGroup);
  const togglePersistentGroup = useModStore((s) => s.togglePersistentGroup);

  const isExpanded = expandedGroups.has(group.path);
  const isPersistent = persistentGroups.has(group.path);
  const isGridLayout = layout === "grid";

  const ref = useRef<HTMLButtonElement>(null);
  const isInitialMount = useRef(true);

  useEffect(() => {
    if (isSelected && ref.current && isInitialMount.current) {
      requestAnimationFrame(() => {
        ref.current?.scrollIntoView({ behavior: "auto", block: "center" });
      });
    }
    isInitialMount.current = false;
  }, [isSelected]);

  useEffect(() => {
    if (ref.current) {
      itemRefs.current.set(group.path, { element: ref.current, group });
    }

    return () => {
      itemRefs.current.delete(group.path);
    };
  }, [group, group.path, itemRefs]);

  const [isDragOver, setIsDragOver] = useState(false);

  const setDragOverIfChanged = (next: boolean) => {
    setIsDragOver((prev) => (prev === next ? prev : next));
  };

  const hasFiles = (e: React.DragEvent) => e.dataTransfer?.types.includes("Files");

  const handleDragEnter = (e: React.DragEvent) => {
    if (!hasFiles(e)) return;
    e.preventDefault();
    e.stopPropagation();
    setDragOverIfChanged(true);
  };

  const handleDragLeave = (e: React.DragEvent) => {
    if (!hasFiles(e)) return;
    e.preventDefault();
    e.stopPropagation();

    const rect = e.currentTarget.getBoundingClientRect();
    const x = e.clientX;
    const y = e.clientY;

    if (x <= rect.left || x >= rect.right || y <= rect.top || y >= rect.bottom) {
      setDragOverIfChanged(false);
    }
  };

  const handleDragOver = (e: React.DragEvent) => {
    if (!hasFiles(e)) return;
    e.preventDefault();
    e.stopPropagation();
    setDragOverIfChanged(true);
  };

  const handleDrop = (e: React.DragEvent) => {
    if (!hasFiles(e)) return;
    e.preventDefault();
    e.stopPropagation();
    setDragOverIfChanged(false);

    const files = Array.from(e.dataTransfer.files);
    if (files.length > 0) {
      onDrop?.(group, files);
    }
  };

  const handlePrimaryClick = (e: React.MouseEvent) => {
    if (isPersistent || isExpanded) {
      toggleExpandedGroup(group.path);
      return;
    }

    onClick(group, e);
  };

  return (
    <ContextMenu>
      <ContextMenuTrigger asChild>
        <div
          onDragEnter={handleDragEnter}
          onDragLeave={handleDragLeave}
          onDragOver={handleDragOver}
          onDrop={handleDrop}
          className={cn(
            "relative",
            isGridLayout ? "min-w-0 overflow-visible" : "w-full overflow-hidden",
          )}
        >
          {isDragOver && (
            <div className="pointer-events-none absolute inset-0 z-50 flex items-center justify-center border-2 border-dashed border-primary bg-background/80 backdrop-blur-sm">
              <span className="text-sm font-bold">
                {t("page.mod.character-sidebar.add-to-character", { name: group.name })}
              </span>
            </div>
          )}

          <button
            ref={ref}
            type="button"
            onClick={handlePrimaryClick}
            className={cn(
              "w-full text-left",
              itemClassName,
              depth > 0 && nestedItemClassName,
              isSelected && selectedItemClassName,
            )}
            style={itemStyle}
          >
            {isGridLayout ? (
              <CharacterSidebarItemGrid
                group={group}
                depth={depth}
                parentGroupName={parentGroupName}
              />
            ) : (
              <CharacterSidebarItemRow group={group} depth={depth} />
            )}
          </button>

          {isGridLayout && (
            <span
              className={cn(
                "absolute right-1 top-1 z-10 h-7 w-7 rounded-full pointer-events-none",
                buttonVariants({ variant: "ghost", size: "icon" }),
              )}
            >
              {isExpanded ? (
                <ChevronDown className="size-4" />
              ) : (
                <ChevronRight className="size-4" />
              )}
            </span>
          )}
        </div>
      </ContextMenuTrigger>

      <ContextMenuContent className="w-56">
        <ContextMenuItem onClick={() => window.api.invoke("util:openPath", group.path)}>
          <FolderIcon className="mr-2 h-4 w-4" />
          {t("page.mod.character-sidebar.open-in-explorer")}
        </ContextMenuItem>

        <ContextMenuItem onClick={() => onCreateFolder?.(group)}>
          <FolderPlus className="mr-2 h-4 w-4" />
          {t("page.mod.character-sidebar.create-folder")}
        </ContextMenuItem>

        <ContextMenuItem variant="destructive" onClick={() => onDeleteFolder?.(group)}>
          <TrashIcon className="mr-2 h-4 w-4" />
          {t("page.mod.character-sidebar.delete-folder")}
        </ContextMenuItem>

        <ContextMenuSeparator />

        {!isPersistent && (
          <ContextMenuItem onClick={() => toggleExpandedGroup(group.path)}>
            {isExpanded ? (
              <>
                <FolderMinus className="mr-2 h-4 w-4" />
                {t("page.mod.character-sidebar.collapse-subgroups")}
              </>
            ) : (
              <>
                <FolderTree className="mr-2 h-4 w-4" />
                {t("page.mod.character-sidebar.expand-subgroups")}
              </>
            )}
          </ContextMenuItem>
        )}

        <ContextMenuItem onClick={() => togglePersistentGroup(group.path)}>
          {isPersistent ? (
            <>
              <FolderMinus className="mr-2 h-4 w-4 text-destructive" />
              {t("page.mod.character-sidebar.unpersist-subgroups")}
            </>
          ) : (
            <>
              <FolderTree className="mr-2 h-4 w-4 text-primary" />
              {t("page.mod.character-sidebar.persist-subgroups")}
            </>
          )}
        </ContextMenuItem>
      </ContextMenuContent>
    </ContextMenu>
  );
});

CharacterSidebarItem.displayName = "CharacterSidebarItem";

export function CharacterSidebarItemSkeleton({ layout = "row" }: { layout?: SidebarLayoutMode }) {
  if (layout === "grid") {
    return (
      <div className="rounded-xl border bg-card p-2">
        <Skeleton className="aspect-square w-full rounded-lg" />
        <div className="mt-2 space-y-2">
          <Skeleton className="h-4 w-full" />
          <Skeleton className="h-3 w-10" />
        </div>
      </div>
    );
  }

  return (
    <div
      className="grid h-14 w-full items-center gap-3 py-2 pl-2 pr-4"
      style={{ gridTemplateColumns: "auto 1fr auto" }}
    >
      <Skeleton className="h-10 w-10 rounded-full" />
      <Skeleton className="h-4 w-full" />
      <Skeleton className="h-4 w-8" />
    </div>
  );
}
