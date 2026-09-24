import { BackupStatusCard } from "@renderer/components/backup/backup-status-card";
import { backupErrorMessage } from "@renderer/components/backup/errors";
import { useBackupOverview } from "@renderer/components/backup/use-backup-overview";
import { createFileRoute } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";

export const Route = createFileRoute("/backup/status")({
  component: RouteComponent,
});

function RouteComponent() {
  const { t } = useTranslation();
  const overview = useBackupOverview();

  return (
    <div className="space-y-6 p-4">
      <p className="text-sm text-muted-foreground">{t("page.backup.description")}</p>
      {overview.isError ? (
        <p className="text-sm text-destructive">{backupErrorMessage(t, overview.error)}</p>
      ) : overview.isPending ? (
        <p className="text-sm text-muted-foreground">{t("page.backup.loading")}</p>
      ) : (
        <BackupStatusCard overview={overview.data} />
      )}
    </div>
  );
}
