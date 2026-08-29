import { Button } from "@renderer/components/ui/button";
import { Progress } from "@renderer/components/ui/progress";
import type { DriveCopyProgress } from "@shared/types";
import { Loader2Icon, SquareIcon } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";

type CollectionCopyToastProps = {
  progress: DriveCopyProgress;
  onCancel: () => Promise<unknown>;
};

export function CollectionCopyToast({ progress, onCancel }: CollectionCopyToastProps) {
  const { t } = useTranslation();
  const [isCanceling, setIsCanceling] = useState(false);
  const isFinished =
    progress.phase === "completed" || progress.phase === "canceled" || progress.phase === "error";

  const cancel = async () => {
    setIsCanceling(true);
    try {
      await onCancel();
    } finally {
      setIsCanceling(false);
    }
  };

  return (
    <div className="w-[min(22rem,calc(100vw-2rem))] space-y-2 rounded-md border bg-popover p-3 text-popover-foreground shadow-lg">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <p className="text-sm font-medium">{t("page.drive.import.title")}</p>
          <p className="truncate text-xs text-muted-foreground">
            {progress.itemName ?? t(`page.drive.import.progress.${progress.phase}`)}
          </p>
        </div>
        {!isFinished && (
          <Button
            aria-label={t("page.drive.import.progress.cancel")}
            className="shrink-0"
            disabled={isCanceling}
            size="icon-sm"
            title={t("page.drive.import.progress.cancel")}
            type="button"
            variant="ghost"
            onClick={() => void cancel()}
          >
            {isCanceling ? <Loader2Icon className="animate-spin" /> : <SquareIcon />}
          </Button>
        )}
      </div>
      <div className="flex items-center justify-between gap-3 text-xs text-muted-foreground">
        <span>{t(`page.drive.import.progress.${progress.phase}`)}</span>
        <span className="shrink-0 tabular-nums">
          {progress.current}/{progress.total}
        </span>
      </div>
      <Progress
        value={
          progress.phase === "downloading"
            ? null
            : progress.total > 0
              ? (progress.current / progress.total) * 100
              : null
        }
      />
    </div>
  );
}
