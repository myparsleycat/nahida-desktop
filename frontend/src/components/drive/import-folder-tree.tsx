import { Checkbox } from "@renderer/components/ui/checkbox";
import { cn } from "@renderer/lib/utils";
import { formatSize } from "@shared/utils";
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
  loadingIds: Set<string>;
  descendantMap: Map<string, Set<string>>;
  onToggle: (id: string) => void;
  onExpand: (id: string) => void;
  onCollapse: (id: string) => void;
};

function getCheckState(id: string, selected: Set<string>, descendantMap: Map<string, Set<string>>) {
  if (selected.has(id)) return true as const;
  const desc = descendantMap.get(id);
  if (!desc || desc.size === 0) return false as const;
  for (const d of desc) if (selected.has(d)) return "indeterminate" as const;
  return false as const;
}

export function ImportFolderTree({
  visibleNodes,
  expanded,
  selected,
  loadingIds,
  descendantMap,
  onToggle,
  onExpand,
  onCollapse,
}: Props) {
  if (visibleNodes.length === 0) {
    return (
      <div className="flex h-full items-center justify-center p-8 text-sm text-muted-foreground">
        표시할 폴더가 없습니다
      </div>
    );
  }
  return (
    <div className="flex flex-col">
      {visibleNodes.map((node) => {
        const isFolder = node.isDir;
        const isExpanded = expanded.has(node.id);
        const isLoading = loadingIds.has(node.id);
        const checkState = isFolder ? getCheckState(node.id, selected, descendantMap) : false;
        const isChecked = checkState === true;
        const isIndeterminate = checkState === "indeterminate";

        return (
          <div
            key={node.id}
            className={cn(
              "flex items-center gap-2 px-2 py-1.5 hover:bg-accent/50",
              isChecked && "bg-accent/30",
            )}
            style={{ paddingLeft: `${node.depth * 16 + 8}px` }}
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
