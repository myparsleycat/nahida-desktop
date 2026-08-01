import { useTranslation } from "react-i18next";

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
  const { t } = useTranslation();
  const total = group.modCount ?? group.mods.length;
  const enabled = group.enabledModCount ?? group.mods.filter((mod) => mod.isEnabled).length;

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
      <span
        className="flex shrink-0 items-center gap-1.5 text-xs text-muted-foreground"
        title={t("page.mod.character-sidebar.active-mod-count", { enabled, total })}
      >
        <span className="size-1.5 rounded-full bg-emerald-500" />
        {enabled} / {total}
      </span>
    </>
  );
}
