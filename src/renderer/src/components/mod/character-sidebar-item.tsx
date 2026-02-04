import { cn } from "@renderer/lib/utils";
import { Skeleton } from "@renderer/components/ui/skeleton";
import type { FolderGroup } from "@renderer/types/mod";
import { useState, memo, useRef, useEffect } from "react";

import { Preview } from "./preview";
import { useTranslation } from "react-i18next";

interface CharacterSidebarItemProps {
  group: FolderGroup;
  isSelected: boolean;
  onClick: (group: FolderGroup) => void;
  onDrop: (group: FolderGroup, files: File[]) => void;
  itemRefs: React.MutableRefObject<Map<string, HTMLButtonElement>>;
}

export const CharacterSidebarItem = memo(
  ({ group, isSelected, onClick, onDrop, itemRefs }: CharacterSidebarItemProps) => {
    const { t } = useTranslation();
    const [isDragOver, setIsDragOver] = useState(false);
    const ref = useRef<HTMLButtonElement>(null);

    useEffect(() => {
      if (ref.current) {
        itemRefs.current.set(group.name, ref.current);
      }
      return () => {
        itemRefs.current.delete(group.name);
      };
    }, [group.name, itemRefs]);

    const handleDragEnter = (e: React.DragEvent) => {
      if (e.dataTransfer?.types.includes("Files")) {
        e.preventDefault();
        e.stopPropagation();
        setIsDragOver(true);
      }
    };

    const handleDragLeave = (e: React.DragEvent) => {
      if (e.dataTransfer?.types.includes("Files")) {
        e.preventDefault();
        e.stopPropagation();

        const rect = e.currentTarget.getBoundingClientRect();
        const x = e.clientX;
        const y = e.clientY;

        if (x <= rect.left || x >= rect.right || y <= rect.top || y >= rect.bottom) {
          setIsDragOver(false);
        }
      }
    };

    const handleDragOver = (e: React.DragEvent) => {
      if (e.dataTransfer?.types.includes("Files")) {
        e.preventDefault();
        e.stopPropagation();
        setIsDragOver(true);
      }
    };

    const handleDrop = (e: React.DragEvent) => {
      if (e.dataTransfer?.types.includes("Files")) {
        e.preventDefault();
        e.stopPropagation();
        setIsDragOver(false);
        const files = Array.from(e.dataTransfer.files);
        if (files.length > 0) {
          onDrop(group, files);
        }
      }
    };

    return (
      <button
        ref={ref}
        onClick={() => onClick(group)}
        onDragEnter={handleDragEnter}
        onDragLeave={handleDragLeave}
        onDragOver={handleDragOver}
        onDrop={handleDrop}
        className={cn(
          "relative w-full grid grid-columns-[auto_1fr_auto] items-center gap-3 pl-2 pr-4 py-2 hover:bg-[#cecece] dark:hover:bg-[#2a2a2a] overflow-hidden h-14",
          isSelected && "dark:bg-[#2a2a2a] bg-[#cecece]",
        )}
        style={{ gridTemplateColumns: "auto 1fr auto" }}
      >
        {isDragOver && (
          <div className="absolute inset-0 z-50 flex items-center justify-center bg-background/80 backdrop-blur-sm border-2 border-dashed border-primary pointer-events-none">
            <span className="text-sm font-bold">
              {t("page.mod.character-sidebar.add-to-character", { name: group.name })}
            </span>
          </div>
        )}
        <div className="w-10 h-10 rounded-full flex items-center justify-center shrink-0 overflow-hidden bg-muted">
          <Preview
            path={group.preview}
            alt={group.name}
            objectFit="cover"
            fallback={<span className="text-lg font-bold text-center">?</span>}
            allowPlay={true}
          />
        </div>
        <span className="text-left text-sm text-foreground truncate min-w-0">{group.name}</span>
        <span className="text-sm text-muted-foreground shrink-0">
          {group.modCount ?? group.mods.length}
        </span>
      </button>
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
