import { Backup, type Overview } from "@bindings/backup";
import { Badge } from "@renderer/components/ui/badge";
import { Button } from "@renderer/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@renderer/components/ui/card";
import { Progress } from "@renderer/components/ui/progress";
import { useSetting } from "@renderer/hooks/use-settings";
import { formatSize } from "@shared/utils";
import { CloudUploadIcon, LoaderIcon, XIcon } from "lucide-react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

import { backupErrorMessage } from "./errors";

export function BackupStatusCard({ overview }: { overview: Overview | undefined }) {
  const { t, i18n } = useTranslation();
  const enabled = useSetting("backup.enabled");
  const status = overview?.status;
  const state = status?.state ?? "idle";
  const running = state !== "idle";
  const lastRun = overview?.lastRun;
  const dateFormat = new Intl.DateTimeFormat(i18n.language, {
    dateStyle: "medium",
    timeStyle: "short",
  });

  const progressValue = (() => {
    if (!status || !running) return null;
    if ((state === "uploading" || state === "restoring") && status.totalBytes > 0) {
      return Math.min(100, (status.bytes / status.totalBytes) * 100);
    }
    if (status.total > 0) return Math.min(100, (status.processed / status.total) * 100);
    return null;
  })();

  const runNow = async () => {
    try {
      await Backup.RunNow();
    } catch (error) {
      toast.error(backupErrorMessage(t, error));
    }
  };

  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between gap-4">
        <div className="flex items-center gap-2">
          <CardTitle className="text-sm font-medium">{t("page.backup.status.title")}</CardTitle>
          <Badge variant={enabled.data ? "default" : "secondary"}>
            {enabled.data ? t("page.backup.status.auto_on") : t("page.backup.status.auto_off")}
          </Badge>
        </div>
        {running ? (
          <Button variant="outline" size="sm" onClick={() => void Backup.Cancel()}>
            <XIcon />
            {t("g.cancel")}
          </Button>
        ) : (
          <Button size="sm" onClick={() => void runNow()} disabled={!overview}>
            <CloudUploadIcon />
            {t("page.backup.status.run_now")}
          </Button>
        )}
      </CardHeader>
      <CardContent className="space-y-4">
        {running && status && (
          <div className="space-y-2">
            <div className="flex items-center gap-2 text-sm">
              <LoaderIcon className="size-4 animate-spin text-muted-foreground" />
              <span>{t(`page.backup.state.${state}`)}</span>
              <span className="ml-auto text-xs text-muted-foreground tabular-nums">
                {state === "uploading" || state === "restoring"
                  ? `${formatSize(status.bytes)} / ${formatSize(status.totalBytes)}`
                  : status.total > 0
                    ? `${status.processed} / ${status.total}`
                    : ""}
              </span>
            </div>
            {progressValue !== null && <Progress value={progressValue} />}
          </div>
        )}

        <dl className="grid grid-cols-[auto_1fr] gap-x-6 gap-y-2 text-sm">
          <dt className="text-muted-foreground">{t("page.backup.status.last_run")}</dt>
          <dd>
            {lastRun ? (
              <span className="flex flex-wrap items-center gap-2">
                <span>{dateFormat.format(new Date(lastRun.at))}</span>
                <Badge variant={lastRun.outcome === "failed" ? "destructive" : "outline"}>
                  {t(`page.backup.outcome.${lastRun.outcome}`)}
                </Badge>
                {lastRun.outcome === "completed" && (
                  <span className="text-xs text-muted-foreground tabular-nums">
                    {t("page.backup.status.files", { count: lastRun.fileCount })} ·{" "}
                    {formatSize(lastRun.totalSize)}
                  </span>
                )}
              </span>
            ) : (
              <span className="text-muted-foreground">{t("page.backup.status.never")}</span>
            )}
          </dd>
          {lastRun?.outcome === "failed" && lastRun.error && (
            <>
              <dt className="text-muted-foreground">{t("page.backup.status.error")}</dt>
              <dd className="text-destructive">{backupErrorMessage(t, lastRun.error)}</dd>
            </>
          )}
          <dt className="text-muted-foreground">{t("page.backup.status.next_run")}</dt>
          <dd>
            {overview?.nextRunAt
              ? dateFormat.format(new Date(overview.nextRunAt))
              : t("page.backup.status.not_scheduled")}
          </dd>
        </dl>
      </CardContent>
    </Card>
  );
}
