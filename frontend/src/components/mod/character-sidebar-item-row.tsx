import { cn } from "@renderer/lib/utils";

import { CharacterSidebarModCountBadge } from "./character-sidebar-mod-count-badge";
import { Preview } from "./preview";

interface CharacterSidebarItemRowProps {
  group: {
    name: string;
    isEnabled?: boolean;
    preview?: string;
    modCount?: number;
    enabledModCount?: number;
    mods: { isEnabled: boolean }[];
  };
  depth: number;
  parentGroupName?: string;
  previewCacheKey?: number;
}

export function CharacterSidebarItemRow({
  group,
  depth,
  parentGroupName,
  previewCacheKey,
}: CharacterSidebarItemRowProps) {
  // A sub folder listed in a classification section sits at depth 0, so its parent is named instead of indented.
  const listedParentName = depth === 0 ? parentGroupName : undefined;
  const disabled = group.isEnabled === false;

  return (
    <>
      {depth > 0 && (
        <div
          className="absolute top-0 bottom-0 left-0 w-px bg-border/50"
          style={{ left: `${(depth - 1) * 16 + 16}px` }}
        />
      )}

      <div
        className={cn(
          "flex h-10 w-10 shrink-0 items-center justify-center overflow-hidden rounded-full bg-muted",
          disabled && "opacity-50",
        )}
      >
        <Preview
          path={group.preview}
          alt={group.name}
          cacheKey={previewCacheKey}
          objectFit="cover"
          fallback={<span className="text-center font-bold">?</span>}
          allowPlay={true}
        />
      </div>

      {listedParentName ? (
        <span className="flex min-w-0 flex-col text-left">
          <span
            className={cn(
              "truncate text-sm",
              disabled ? "text-muted-foreground" : "text-foreground",
            )}
          >
            {group.name}
          </span>
          <span className="truncate text-xs text-muted-foreground">{listedParentName}</span>
        </span>
      ) : (
        <span
          className={cn(
            "min-w-0 truncate text-left text-sm",
            disabled ? "text-muted-foreground" : "text-foreground",
          )}
        >
          {group.name}
        </span>
      )}
      <CharacterSidebarModCountBadge group={group} />
    </>
  );
}
