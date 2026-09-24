import { BackupSnapshotsCard } from "@renderer/components/backup/backup-snapshots-card";
import { useBackupOverview } from "@renderer/components/backup/use-backup-overview";
import { createFileRoute } from "@tanstack/react-router";

export const Route = createFileRoute("/backup/snapshots")({
  component: RouteComponent,
});

function RouteComponent() {
  const overview = useBackupOverview();

  return (
    <div className="space-y-6 p-4">
      <BackupSnapshotsCard busy={(overview.data?.status.state ?? "idle") !== "idle"} />
    </div>
  );
}
