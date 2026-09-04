import { Checkbox } from "@renderer/components/ui/checkbox";
import { cn } from "@renderer/lib/utils";
import { formatSize } from "@shared/utils";
import { useVirtualizer } from "@tanstack/react-virtual";
import { ChevronDownIcon, ChevronRightIcon, FileIcon, FolderIcon, Loader2Icon } from "lucide-react";

type TreeNode = {
  id: string;
  name: string;
  isDir: boolean;
  size: number | null;
  depth: number;
};

type Props = {
  visibleNodes: TreeNode[];
  expanded: Set<string>;
  selected: Set<string>;
  selectedAncestorIds: Set<string>;
  loadingIds: Set<string>;
  scrollElement: HTMLDivElement | null;
  onToggle: (id: string) => void;
  onExpand: (id: string) => void;
  onCollapse: (id: string) => void;
};

function getCheckState(id: string, selected: Set<string>, selectedAncestorIds: Set<string>) {
  if (selected.has(id)) return true as const;
  if (selectedAncestorIds.has(id)) return "indeterminate" as const;
  return false as const;
}

export function ImportFolderTree({
  visibleNodes,
  expanded,
  selected,
  selectedAncestorIds,
  loadingIds,
  scrollElement,
  onToggle,
  onExpand,
  onCollapse,
}: Props) {
  const virtualizer = useVirtualizer({
    count: visibleNodes.length,
    getScrollElement: () => scrollElement,
    estimateSize: () => 32,
    overscan: 8,
    getItemKey: (index) => visibleNodes[index]?.id ?? index,
    initialRect: scrollElement
      ? { width: scrollElement.clientWidth, height: scrollElement.clientHeight }
      : undefined,
  });

  if (visibleNodes.length === 0) {
    return (
      <div className="flex h-full items-center justify-center p-8 text-sm text-muted-foreground">
        표시할 폴더가 없습니다
      </div>
    );
  }
  return (
    <div className="relative w-full" style={{ height: `${virtualizer.getTotalSize()}px` }}>
      {virtualizer.getVirtualItems().map((virtualRow) => {
        const node = visibleNodes[virtualRow.index];
        if (!node) return null;
        const isFolder = node.isDir;
        const isExpanded = expanded.has(node.id);
        const isLoading = loadingIds.has(node.id);
        const checkState = isFolder ? getCheckState(node.id, selected, selectedAncestorIds) : false;
        const isChecked = checkState === true;
        const isIndeterminate = checkState === "indeterminate";

        return (
          <div
            key={node.id}
            data-index={virtualRow.index}
            className={cn(
              "absolute top-0 left-0 flex w-full items-center gap-2 px-2 py-1.5 hover:bg-accent/50",
              isChecked && "bg-accent/30",
            )}
            style={{
              paddingLeft: `${node.depth * 16 + 8}px`,
              transform: `translateY(${virtualRow.start}px)`,
            }}
          >
            <button
              type="button"
              className={cn(
                "flex size-5 shrink-0 items-center justify-center rounded hover:bg-accent",
                !isFolder && "invisible",
              )}
              disabled={!isFolder}
              onClick={() => (isExpanded ? onCollapse(node.id) : onExpand(node.id))}
            >
              {isLoading ? (
                <Loader2Icon className="size-3 animate-spin" />
              ) : isFolder ? (
                isExpanded ? (
                  <ChevronDownIcon className="size-3" />
                ) : (
                  <ChevronRightIcon className="size-3" />
                )
              ) : null}
            </button>

            {isFolder ? (
              <Checkbox
                checked={isChecked}
                indeterminate={isIndeterminate}
                onCheckedChange={() => onToggle(node.id)}
              />
            ) : (
              <Checkbox checked={false} disabled />
            )}

            <div className="flex min-w-0 flex-1 items-center gap-2">
              {isFolder ? (
                <FolderIcon className="size-4 shrink-0 text-yellow-400" />
              ) : (
                <FileIcon className="size-4 shrink-0 text-muted-foreground" />
              )}
              <span
                className={cn("truncate text-sm", !isFolder && "text-muted-foreground")}
                title={node.name}
              >
                {node.name}
              </span>
            </div>
            <span className="shrink-0 text-xs text-muted-foreground">
              {node.isDir ? "" : formatSize(node.size ?? 0)}
            </span>
          </div>
        );
      })}
    </div>
  );
}

export type { TreeNode };
