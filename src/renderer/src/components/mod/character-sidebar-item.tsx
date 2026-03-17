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
import { FolderIcon, FolderMinus, FolderTree } from "lucide-react";
import { memo, useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { Preview } from "./preview";

interface CharacterSidebarItemProps {
  group: FolderGroup;
  isSelected: boolean;
  onClick: (group: FolderGroup, e: React.MouseEvent) => void;
  onDrop?: (group: FolderGroup, files: File[]) => void;
  itemRefs: React.MutableRefObject<Map<string, { element: HTMLButtonElement; group: FolderGroup }>>;
  depth?: number;
}

export const CharacterSidebarItem = memo(
  ({ group, isSelected, onClick, onDrop, itemRefs, depth = 0 }: CharacterSidebarItemProps) => {
    const { t } = useTranslation();
    const expandedGroups = useModStore((s) => s.expandedGroups);
    const persistentGroups = useModStore((s) => s.persistentGroups);
    const toggleExpandedGroup = useModStore((s) => s.toggleExpandedGroup);
    const togglePersistentGroup = useModStore((s) => s.togglePersistentGroup);

    const isExpanded = expandedGroups.has(group.path);
    const isPersistent = persistentGroups.has(group.path);

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
    }, [group.path, itemRefs, group]);

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

    return (
      <ContextMenu>
        <ContextMenuTrigger asChild>
          <button
            ref={ref}
            onClick={(e) => {
              if (isPersistent || isExpanded) {
                toggleExpandedGroup(group.path);
              } else {
                onClick(group, e);
              }
            }}
            onDragEnter={handleDragEnter}
            onDragLeave={handleDragLeave}
            onDragOver={handleDragOver}
            onDrop={handleDrop}
            className={cn(
              "relative w-full grid items-center gap-3 pr-4 py-2 hover:bg-[#cecece] dark:hover:bg-[#2a2a2a] overflow-hidden h-14",
              isSelected && "dark:bg-[#2a2a2a] bg-[#cecece]",
            )}
            style={{
              gridTemplateColumns: "auto 1fr auto",
              paddingLeft: depth > 0 ? `${depth * 16 + 8}px` : "8px",
            }}
          >
            {isDragOver && (
              <div className="absolute inset-0 z-50 flex items-center justify-center bg-background/80 backdrop-blur-sm border-2 border-dashed border-primary pointer-events-none">
                <span className="text-sm font-bold">
                  {t("page.mod.character-sidebar.add-to-character", { name: group.name })}
                </span>
              </div>
            )}

            {depth > 0 && (
              <div
                className="absolute left-0 top-0 bottom-0 w-px bg-border/50"
                style={{ left: `${(depth - 1) * 16 + 16}px` }}
              />
            )}

            <div
              className={cn(
                "flex items-center justify-center shrink-0 overflow-hidden bg-muted rounded-full",
                "w-10 h-10",
              )}
            >
              <Preview
                path={group.preview}
                alt={group.name}
                objectFit="cover"
                fallback={<span className={cn("font-bold text-center")}>?</span>}
                allowPlay={true}
              />
            </div>

            <span className="text-left text-sm text-foreground truncate min-w-0">{group.name}</span>
            <span className="text-sm text-muted-foreground shrink-0">
              {group.modCount ?? group.mods.length}
            </span>
          </button>
        </ContextMenuTrigger>

        <ContextMenuContent className="w-56">
          <ContextMenuItem onClick={() => window.api.invoke("util:openPath", group.path)}>
            <FolderIcon className="mr-2 h-4 w-4" />
            {t("page.mod.character-sidebar.open-in-explorer")}
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
  },
);

CharacterSidebarItem.displayName = "CharacterSidebarItem";

export function CharacterSidebarItemSkeleton() {
  return (
    <div
      className="w-full grid items-center gap-3 pl-2 pr-4 py-2 h-14"
      style={{ gridTemplateColumns: "auto 1fr auto" }}
    >
      <Skeleton className="w-10 h-10 rounded-full" />
      <Skeleton className="h-4 w-full" />
      <Skeleton className="h-4 w-8" />
    </div>
  );
}
