import { Badge } from "@renderer/components/ui/badge";
import { Button } from "@renderer/components/ui/button";
import {
  Popover,
  PopoverClose,
  PopoverContent,
  PopoverDescription,
  PopoverTitle,
  PopoverTrigger,
} from "@renderer/components/ui/popover";
import { Tooltip, TooltipContent, TooltipTrigger } from "@renderer/components/ui/tooltip";
import { useSetting } from "@renderer/hooks/use-settings";
import { cn } from "@renderer/lib/utils";
import {
  listTitlebarActivities,
  useTitlebarActivityStore,
} from "@renderer/store/titlebar-activity";
import { useNavigate } from "@tanstack/react-router";
import { WrenchIcon, XIcon } from "lucide-react";
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

        const isInteractive =
          !activity.popover && clickNavigate && (!!activity.onClick || !!activity.href);
        const Icon = activity.icon;
        const badge = (
          <Badge
            variant="secondary"
            className={cn(
              "h-5 max-w-56 gap-1 truncate border-border/60 bg-muted/70 px-1.5 text-[11px] font-medium text-muted-foreground",
              (isInteractive || !!activity.popover) &&
                "no-drag cursor-pointer hover:bg-muted hover:text-foreground",
            )}
            render={isInteractive ? <button type="button" onClick={activate} /> : undefined}
          >
            <Icon className={cn("size-3!", activity.status === "paused" && "opacity-60")} />
            <span className="truncate">
              {activity.label}
              {activity.detail ? ` · ${activity.detail}` : ""}
            </span>
          </Badge>
        );

        if (activity.popover) {
          const popover = activity.popover;
          return (
            <Popover key={activity.id} defaultOpen={popover.defaultOpen ?? true}>
              <PopoverTrigger
                render={
                  <button
                    type="button"
                    className="no-drag flex min-w-0 shrink-0 cursor-pointer items-center outline-hidden"
                  />
                }
              >
                {badge}
              </PopoverTrigger>
              <PopoverContent align="start" side="bottom" className="w-80 gap-2.5 p-3 select-none">
                <div className="flex items-start justify-between gap-2">
                  <div className="flex min-w-0 flex-col gap-1">
                    <PopoverTitle className="text-sm leading-tight font-semibold break-words text-foreground">
                      {popover.title}
                    </PopoverTitle>
                    {popover.description && (
                      <PopoverDescription className="text-xs break-words text-muted-foreground">
                        {popover.description}
                      </PopoverDescription>
                    )}
                  </div>
                  <PopoverClose
                    className="no-drag inline-flex size-5 shrink-0 cursor-pointer items-center justify-center rounded-sm text-muted-foreground transition-colors hover:text-foreground"
                    title={t("g.close", "닫기")}
                  >
                    <XIcon className="size-3.5" />
                  </PopoverClose>
                </div>
                <div className="flex items-center justify-end gap-1.5 pt-1">
                  <PopoverClose
                    render={
                      <Button
                        size="xs"
                        variant="ghost"
                        className="h-6 px-2 text-xs text-muted-foreground hover:text-foreground"
                        onClick={() => popover.onDismiss?.()}
                      />
                    }
                  >
                    {popover.dismissLabel ?? t("titlebar.activity.dismiss", t("g.close", "닫기"))}
                  </PopoverClose>
                  {popover.actionLabel && (
                    <Button
                      size="xs"
                      className="h-6 px-2.5 text-xs font-medium"
                      onClick={() => {
                        popover.onAction?.();
                      }}
                    >
                      <WrenchIcon className="size-3" />
                      {popover.actionLabel}
                    </Button>
                  )}
                </div>
              </PopoverContent>
            </Popover>
          );
        }

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
