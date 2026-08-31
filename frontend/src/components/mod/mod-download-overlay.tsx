import { Progress } from "@renderer/components/ui/progress";
import { cn } from "@renderer/lib/utils";
import type { TransferWithoutData } from "@shared/types";
import { formatSize } from "@shared/utils";
import { LoaderCircleIcon, PauseIcon } from "lucide-react";
import { useTranslation } from "react-i18next";

export interface ModDownloadDisplay {
  status: "queued" | "preparing" | "downloading" | "finalizing" | "paused";
  progress: number | null;
  speed: number | null;
  pulse: boolean;
}

export function getModDownloadDisplay(transfer: TransferWithoutData): ModDownloadDisplay {
  const progress = transfer.totalSize > 0 ? Math.max(0, Math.min(100, transfer.progress)) : null;
  if (transfer.status === "pending") {
    return { status: "queued", progress, speed: null, pulse: true };
  }
  if (transfer.status === "preparing") {
    return { status: "preparing", progress, speed: null, pulse: true };
  }
  if (transfer.status === "paused") {
    return { status: "paused", progress, speed: null, pulse: false };
  }
  if (progress !== null && progress >= 100) {
    return { status: "finalizing", progress, speed: null, pulse: false };
  }
  return {
    status: "downloading",
    progress,
    speed: transfer.speed > 0 ? transfer.speed : null,
    pulse: progress === null,
  };
}

export function ModDownloadOverlay({
  transfer,
  variant,
}: {
  transfer: TransferWithoutData;
  variant: "card" | "row";
}) {
  const { t } = useTranslation();
  const display = getModDownloadDisplay(transfer);
  const isPaused = display.status === "paused";

  return (
    <div
      className={cn(
        "absolute inset-0 z-30 flex cursor-wait items-center justify-center bg-muted/88 px-4 backdrop-blur-[2px]",
        variant === "row" && "cursor-default py-2",
      )}
    >
      <div
        className={cn(
          "flex w-full max-w-sm flex-col gap-2",
          variant === "row" && "max-w-xl gap-1.5",
        )}
      >
        <div className="flex items-center justify-between gap-3 text-sm font-medium">
          <span className="flex min-w-0 items-center gap-2">
            {isPaused ? (
              <PauseIcon className="size-4 shrink-0" />
            ) : (
              <LoaderCircleIcon className="size-4 shrink-0 animate-spin" />
            )}
            <span className="truncate">{t(`page.mod.download_overlay.${display.status}`)}</span>
          </span>
          <span className="flex shrink-0 items-center gap-3 text-xs text-muted-foreground tabular-nums">
            {display.speed !== null && <span>{formatSize(display.speed)}/s</span>}
            <span>{display.progress === null ? "--%" : `${Math.round(display.progress)}%`}</span>
          </span>
        </div>
        <Progress
          value={display.progress ?? 0}
          className={cn("w-full", display.pulse && "animate-pulse")}
        />
      </div>
    </div>
  );
}
