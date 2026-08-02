import { cn } from "@renderer/lib/utils";
import { useTranslation } from "react-i18next";

interface CharacterSidebarModCountBadgeProps {
  group: {
    enabledModCount?: number;
    modCount?: number;
    mods: { isEnabled: boolean }[];
  };
  className?: string;
}

export function CharacterSidebarModCountBadge({
  group,
  className,
}: CharacterSidebarModCountBadgeProps) {
  const { t } = useTranslation();
  const total = group.modCount ?? group.mods.length;
  const enabled = group.enabledModCount ?? group.mods.filter((mod) => mod.isEnabled).length;

  return (
    <span
      className={cn("flex shrink-0 items-center text-xs text-muted-foreground", className)}
      title={t("page.mod.character-sidebar.active-mod-count", { enabled, total })}
    >
      {enabled} / {total}
    </span>
  );
}
