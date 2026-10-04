import { cn } from "@renderer/lib/utils";
import { ChevronDown, ChevronRight } from "lucide-react";
import { useTranslation } from "react-i18next";

interface CharacterSidebarSectionHeaderProps {
  name: string | null;
  count: number;
  collapsed: boolean;
  onToggle: () => void;
  className?: string;
  style?: React.CSSProperties;
}

export function CharacterSidebarSectionHeader({
  name,
  count,
  collapsed,
  onToggle,
  className,
  style,
}: CharacterSidebarSectionHeaderProps) {
  const { t } = useTranslation();

  return (
    <button
      type="button"
      aria-expanded={!collapsed}
      onClick={onToggle}
      className={cn(
        "flex h-8 w-full items-center gap-1 px-2 text-left text-xs font-semibold text-muted-foreground hover:text-foreground",
        className,
      )}
      style={style}
    >
      {collapsed ? (
        <ChevronRight className="size-4 shrink-0" />
      ) : (
        <ChevronDown className="size-4 shrink-0" />
      )}
      <span className="min-w-0 flex-1 truncate">
        {name ?? t("page.mod.character-sidebar.classification.unassigned")}
      </span>
      <span className="shrink-0 tabular-nums">{count}</span>
    </button>
  );
}
