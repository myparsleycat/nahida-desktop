import { BackupTargetsCard } from "@renderer/components/backup/backup-targets-card";
import { useBackupOverview } from "@renderer/components/backup/use-backup-overview";
import { createFileRoute } from "@tanstack/react-router";

export const Route = createFileRoute("/backup/targets")({
  component: RouteComponent,
});

function RouteComponent() {
  const overview = useBackupOverview();

  return (
    <div className="space-y-6 p-4">
      <BackupTargetsCard targets={overview.data?.targets ?? []} />
    </div>
  );
}
