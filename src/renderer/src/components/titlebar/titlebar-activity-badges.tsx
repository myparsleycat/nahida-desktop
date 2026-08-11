import { Badge } from "@renderer/components/ui/badge";
import { Tooltip, TooltipContent, TooltipTrigger } from "@renderer/components/ui/tooltip";
import { useSetting } from "@renderer/hooks/use-settings";
import { cn } from "@renderer/lib/utils";
import {
  listTitlebarActivities,
  useTitlebarActivityStore,
} from "@renderer/store/titlebar-activity";
import { useNavigate } from "@tanstack/react-router";
import { Loader2Icon, PauseIcon } from "lucide-react";
import { useMemo } from "react";
import { useTranslation } from "react-i18next";

const MAX_VISIBLE_ACTIVITIES = 3;

export function TitlebarActivityBadges() {
  const navigate = useNavigate();
  const { t } = useTranslation();
  const { data: clickNavigate = true } = useSetting("general.titlebarActivityBadgeClickNavigate");
  const activityMap = useTitlebarActivityStore((state) => state.activities);
  const activities = useMemo(() => listTitlebarActivities(activityMap), [activityMap]);

  if (activities.length === 0) return null;

  const visibleActivities = activities.slice(0, MAX_VISIBLE_ACTIVITIES);
  const hiddenCount = activities.length - visibleActivities.length;

  return (
    <div className="flex min-w-0 items-center gap-1.5 overflow-hidden pr-2">
      {visibleActivities.map((activity) => {
        const activate = () => {
          if (activity.onClick) {
            activity.onClick();
            return;
          }
          if (activity.href) {
            void navigate({ to: activity.href });
          }
        };

        const isInteractive = clickNavigate && (!!activity.onClick || !!activity.href);
        const badge = (
          <Badge
            variant="secondary"
            className={cn(
              "h-5 max-w-56 gap-1 truncate border-border/60 bg-muted/70 px-1.5 text-[11px] font-medium text-muted-foreground",
              isInteractive && "no-drag cursor-pointer hover:bg-muted hover:text-foreground",
            )}
            render={isInteractive ? <button type="button" onClick={activate} /> : undefined}
          >
            {activity.status === "paused" ? (
              <PauseIcon className="size-3!" />
            ) : (
              <Loader2Icon className="size-3! animate-spin" />
            )}
            <span className="truncate">
              {activity.label}
              {activity.detail ? ` · ${activity.detail}` : ""}
            </span>
          </Badge>
        );

        if (!activity.tooltip) {
          return (
            <div key={activity.id} className={cn("shrink-0", isInteractive && "no-drag")}>
              {badge}
            </div>
          );
        }

        return (
          <Tooltip key={activity.id}>
            <TooltipTrigger
              render={<div className={cn("min-w-0 shrink-0", isInteractive && "no-drag")} />}
            >
              {badge}
            </TooltipTrigger>
            <TooltipContent side="bottom">{activity.tooltip}</TooltipContent>
          </Tooltip>
        );
      })}
      {hiddenCount > 0 && (
        <Badge variant="outline" className="h-5 shrink-0 px-1.5 text-[11px] text-muted-foreground">
          {t("titlebar.activity.more", { count: hiddenCount })}
        </Badge>
      )}
    </div>
  );
}
