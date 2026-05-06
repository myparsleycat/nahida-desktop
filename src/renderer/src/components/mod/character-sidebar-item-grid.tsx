// oxlint-disable no-unused-vars
import { Preview } from "./preview";

interface CharacterSidebarItemGridProps {
  group: {
    name: string;
    preview?: string;
    modCount?: number;
    mods: unknown[];
  };
  depth: number;
  parentGroupName?: string;
  previewCacheKey?: number;
}

export function CharacterSidebarItemGrid({
  group,
  depth,
  parentGroupName,
  previewCacheKey,
}: CharacterSidebarItemGridProps) {
  const isNestedGridItem = depth > 0;

  return (
    <>
      {/* {isNestedGridItem && parentGroupName && (
        <div className="pointer-events-none absolute left-3 top-5 z-10 -translate-y-1/2">
          <div className={"flex size-6 items-center justify-center rounded-full bg-primary"}>
            <GitBranchIcon className="size-3.5 text-primary-foreground" />
          </div>
        </div>
      )} */}

      <div className="flex flex-col gap-0.5 p-1">
        <div className="relative aspect-square overflow-hidden rounded-lg bg-muted">
          <Preview
            path={group.preview}
            alt={group.name}
            cacheKey={previewCacheKey}
            objectFit="cover"
            fallback={
              <span className="flex h-full w-full items-center justify-center text-center font-bold">
                ?
              </span>
            }
            allowPlay={true}
          />
        </div>
        <div className="flex min-w-0 items-center justify-center">
          <div className="truncate text-center text-xs font-medium text-foreground">
            {group.name}
          </div>
        </div>
      </div>
    </>
  );
}
