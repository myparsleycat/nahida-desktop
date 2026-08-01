import { CharacterSidebarModCountBadge } from "./character-sidebar-mod-count-badge";
import { Preview } from "./preview";

interface CharacterSidebarItemRowProps {
  group: {
    name: string;
    preview?: string;
    modCount?: number;
    enabledModCount?: number;
    mods: { isEnabled: boolean }[];
  };
  depth: number;
  previewCacheKey?: number;
}

export function CharacterSidebarItemRow({
  group,
  depth,
  previewCacheKey,
}: CharacterSidebarItemRowProps) {
  return (
    <>
      {depth > 0 && (
        <div
          className="absolute top-0 bottom-0 left-0 w-px bg-border/50"
          style={{ left: `${(depth - 1) * 16 + 16}px` }}
        />
      )}

      <div className="flex h-10 w-10 shrink-0 items-center justify-center overflow-hidden rounded-full bg-muted">
        <Preview
          path={group.preview}
          alt={group.name}
          cacheKey={previewCacheKey}
          objectFit="cover"
          fallback={<span className="text-center font-bold">?</span>}
          allowPlay={true}
        />
      </div>

      <span className="min-w-0 truncate text-left text-sm text-foreground">{group.name}</span>
      <CharacterSidebarModCountBadge group={group} />
    </>
  );
}
