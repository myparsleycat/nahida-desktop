import { Progress } from "@renderer/components/ui/progress";
import type { NteBootstrapProgress } from "@shared/types";
import { Events } from "@wailsio/runtime";
import { CheckCircle2Icon, Loader2Icon, XCircleIcon } from "lucide-react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";

interface NteBootstrapProgressViewProps {
  active: boolean;
}

export function NteBootstrapProgressView({ active }: NteBootstrapProgressViewProps) {
  const { t } = useTranslation();
  const [progress, setProgress] = useState<NteBootstrapProgress | null>(null);
  const [prevActive, setPrevActive] = useState(active);

  if (active !== prevActive) {
    setPrevActive(active);
    if (active) {
      setProgress(null);
    }
  }

  useEffect(() => {
    return Events.On("mod:nte-bootstrap-progress", (event) => {
      setProgress(event.data);
    });
  }, []);

  const showPanel = active || progress?.phase === "failed";
  if (!showPanel) return null;

  const displayProgress =
    progress ?? ({ phase: "checking", progress: 0 } satisfies NteBootstrapProgress);
  const progressValue = displayProgress.progress ?? 15;
  const isFailed = displayProgress.phase === "failed";
  const isCompleted = displayProgress.phase === "completed";

  return (
    <div className="space-y-2 rounded-md border bg-muted/30 px-3 py-2">
      <div className="flex min-w-0 items-center justify-between gap-3 text-xs text-muted-foreground">
        <div className="flex min-w-0 items-center gap-2">
          {isFailed ? (
            <XCircleIcon className="size-4 shrink-0 text-destructive" />
          ) : isCompleted ? (
            <CheckCircle2Icon className="size-4 shrink-0 text-primary" />
          ) : (
            <Loader2Icon className="size-4 shrink-0 animate-spin" />
          )}
          <span className="truncate">
            {t(`page.mod.dialog.add-game.nte_bootstrap_progress.${displayProgress.phase}`, {
              archiveName: displayProgress.archiveName ?? "",
            })}
          </span>
        </div>
        <span className="shrink-0 tabular-nums">
          {displayProgress.progress === null
            ? t("page.mod.dialog.add-game.nte_bootstrap_progress.working")
            : t("page.mod.dialog.add-game.nte_bootstrap_progress.percent", {
                value: Math.round(displayProgress.progress),
              })}
        </span>
      </div>
      <Progress
        value={progressValue}
        className={displayProgress.progress === null ? "animate-pulse" : undefined}
      />
      {displayProgress.message ? (
        <p className="text-xs break-all text-muted-foreground">{displayProgress.message}</p>
      ) : null}
    </div>
  );
}
