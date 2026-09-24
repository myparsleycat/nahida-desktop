import { BackupScheduleCard } from "@renderer/components/backup/backup-schedule-card";
import { useBackupOverview } from "@renderer/components/backup/use-backup-overview";
import { createFileRoute } from "@tanstack/react-router";

export const Route = createFileRoute("/backup/schedule")({
  component: RouteComponent,
});

function RouteComponent() {
  const overview = useBackupOverview();

  return (
    <div className="space-y-6 p-4">
      <BackupScheduleCard deviceName={overview.data?.deviceName ?? ""} />
    </div>
  );
}
