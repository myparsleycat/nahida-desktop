import { cn } from "@renderer/lib/utils";
import { Skeleton } from "@renderer/components/ui/skeleton";
import type { FolderGroup } from "@renderer/types/mod";
import { forwardRef, useState } from "react";

interface CharacterSidebarItemProps {
  group: FolderGroup;
  isSelected: boolean;
  onClick: () => void;
  onDrop: (files: File[]) => void;
}

export const CharacterSidebarItem = forwardRef<HTMLButtonElement, CharacterSidebarItemProps>(
  ({ group, isSelected, onClick, onDrop }, ref) => {
    const [isDragOver, setIsDragOver] = useState(false);

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
          onDrop(files);
        }
      }
    };

    return (
      <button
        ref={ref}
        onClick={onClick}
        onDragEnter={handleDragEnter}
        onDragLeave={handleDragLeave}
        onDragOver={handleDragOver}
        onDrop={handleDrop}
        className={cn(
          "relative w-full grid grid-columns-[auto_1fr_auto] items-center gap-3 pl-2 pr-4 py-2 hover:bg-[#cecece] dark:hover:bg-[#2a2a2a] overflow-hidden",
          isSelected && "dark:bg-[#2a2a2a] bg-[#cecece]",
        )}
        style={{ gridTemplateColumns: "auto 1fr auto" }}
      >
        {isDragOver && (
          <div className="absolute inset-0 z-50 flex items-center justify-center bg-background/80 backdrop-blur-sm border-2 border-dashed border-primary pointer-events-none">
            <span className="text-sm font-bold">이 캐릭터에 추가</span>
          </div>
        )}
        <div className="w-10 h-10 rounded-full flex items-center justify-center shrink-0 overflow-hidden">
          {group.preview ? (
            group.preview.toLowerCase().match(/\.(mp4|webm|avi|mkv|mov)$/) ? (
              <video
                src={`local://${group.preview}`}
                className="w-full h-full object-cover"
                autoPlay
                loop
                muted
                playsInline
              />
            ) : (
              <img
                src={`local://${group.preview}`}
                alt={group.name}
                className="w-full h-full object-cover"
                decoding="async"
                loading="lazy"
              />
            )
          ) : (
            <span className="text-lg font-bold text-center">?</span>
          )}
        </div>
        <span className="text-left text-sm text-foreground truncate min-w-0">{group.name}</span>
        <span className="text-sm text-muted-foreground shrink-0">{group.mods.length}</span>
      </button>
    );
  },
);

CharacterSidebarItem.displayName = "CharacterSidebarItem";

export function CharacterSidebarItemSkeleton() {
  return (
    <div
      className="w-full grid items-center gap-3 pl-2 pr-4 py-2"
      style={{ gridTemplateColumns: "auto 1fr auto" }}
    >
      <Skeleton className="w-10 h-10 rounded-full" />
      <Skeleton className="h-4 w-full" />
      <Skeleton className="h-4 w-8" />
    </div>
  );
}
