import { Backup, type ManifestTarget, type Snapshot } from "@bindings/backup";
import { Dialog as PlatformDialog } from "@bindings/platform";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@renderer/components/ui/alert-dialog";
import { Badge } from "@renderer/components/ui/badge";
import { Button } from "@renderer/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@renderer/components/ui/card";
import { Checkbox } from "@renderer/components/ui/checkbox";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@renderer/components/ui/dialog";
import { formatSize } from "@shared/utils";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { ArchiveRestoreIcon, FolderOpenIcon, RefreshCwIcon, TrashIcon } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

import { backupErrorMessage } from "./errors";
import { backupSnapshotsKey } from "./queries";

export function BackupSnapshotsCard({ busy }: { busy: boolean }) {
  const { t, i18n } = useTranslation();
  const queryClient = useQueryClient();
  const snapshots = useQuery({
    queryKey: backupSnapshotsKey,
    queryFn: () => Backup.ListSnapshots(),
  });
  const [restoring, setRestoring] = useState<Snapshot | null>(null);
  const [deleting, setDeleting] = useState<Snapshot | null>(null);
  const dateFormat = new Intl.DateTimeFormat(i18n.language, {
    dateStyle: "medium",
    timeStyle: "short",
  });

  const snapshotDate = (snapshot: Snapshot) =>
    dateFormat.format(new Date(snapshot.completedAt ?? snapshot.createdAt));

  const remove = async (snapshot: Snapshot) => {
    setDeleting(null);
    try {
      await Backup.DeleteSnapshot(snapshot.id);
      await queryClient.invalidateQueries({ queryKey: backupSnapshotsKey });
    } catch (error) {
      toast.error(backupErrorMessage(t, error));
    }
  };

  const rows = snapshots.data ?? [];

  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between gap-4">
        <div className="space-y-0.5">
          <CardTitle className="text-sm font-medium">{t("page.backup.snapshots.title")}</CardTitle>
          <p className="text-xs text-muted-foreground">{t("page.backup.snapshots.description")}</p>
        </div>
        <Button
          variant="ghost"
          size="icon-sm"
          aria-label={t("page.backup.snapshots.refresh")}
          onClick={() => void snapshots.refetch()}
        >
          <RefreshCwIcon className={snapshots.isFetching ? "animate-spin" : undefined} />
        </Button>
      </CardHeader>
      <CardContent className="space-y-1">
        {snapshots.isError && (
          <p className="py-6 text-center text-sm text-destructive">
            {backupErrorMessage(t, snapshots.error)}
          </p>
        )}
        {!snapshots.isError && rows.length === 0 && (
          <p className="py-6 text-center text-sm text-muted-foreground">
            {snapshots.isLoading
              ? t("page.backup.snapshots.loading")
              : t("page.backup.snapshots.empty")}
          </p>
        )}
        {rows.map((snapshot) => (
          <div
            key={snapshot.id}
            className="flex items-center gap-3 rounded-md px-2 py-2 hover:bg-muted/50"
          >
            <div className="min-w-0 flex-1">
              <div className="flex items-center gap-2">
                <span className="text-sm font-medium tabular-nums">{snapshotDate(snapshot)}</span>
                <Badge variant="outline">{t(`page.backup.trigger.${snapshot.trigger}`)}</Badge>
                {snapshot.state === "PENDING" && (
                  <Badge variant="secondary">{t("page.backup.snapshots.pending")}</Badge>
                )}
              </div>
              <p className="text-xs text-muted-foreground tabular-nums">
                {t("page.backup.status.files", { count: snapshot.fileCount })} ·{" "}
                {formatSize(snapshot.totalSize)}
                {skippedTotal(snapshot) > 0 &&
                  ` · ${t("page.backup.snapshots.skipped", { count: skippedTotal(snapshot) })}`}
              </p>
            </div>
            <Button
              variant="outline"
              size="sm"
              disabled={busy || snapshot.state !== "COMPLETED"}
              onClick={() => setRestoring(snapshot)}
            >
              <ArchiveRestoreIcon />
              {t("page.backup.snapshots.restore")}
            </Button>
            <Button
              variant="ghost"
              size="icon-sm"
              aria-label={t("page.backup.snapshots.delete")}
              disabled={busy}
              onClick={() => setDeleting(snapshot)}
            >
              <TrashIcon />
            </Button>
          </div>
        ))}
      </CardContent>
      <RestoreDialog snapshot={restoring} onClose={() => setRestoring(null)} />
      <AlertDialog open={!!deleting} onOpenChange={(open) => !open && setDeleting(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t("page.backup.snapshots.delete_confirm.title")}</AlertDialogTitle>
            <AlertDialogDescription>
              {deleting &&
                t("page.backup.snapshots.delete_confirm.description", {
                  date: snapshotDate(deleting),
                })}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t("g.cancel")}</AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              onClick={() => deleting && void remove(deleting)}
            >
              {t("page.backup.snapshots.delete")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </Card>
  );
}

function skippedTotal(snapshot: Snapshot) {
  return Object.values(snapshot.skipped ?? {}).reduce(
    (sum: number, count) => sum + (count ?? 0),
    0,
  );
}

function RestoreDialog({ snapshot, onClose }: { snapshot: Snapshot | null; onClose: () => void }) {
  const { t } = useTranslation();
  const [selected, setSelected] = useState<string[] | null>(null);
  const [destination, setDestination] = useState("");
  const targets = useQuery({
    queryKey: ["backup", "snapshot-targets", snapshot?.id],
    queryFn: () => Backup.GetSnapshotTargets(snapshot?.id ?? ""),
    enabled: !!snapshot,
  });
  const available: ManifestTarget[] = targets.data ?? [];
  const chosen = selected ?? available.map((target) => target.id);

  const close = () => {
    setSelected(null);
    setDestination("");
    onClose();
  };

  const pickDestination = async () => {
    try {
      const result = await PlatformDialog.SelectDirectory();
      if (!result.canceled && result.filePath) setDestination(result.filePath);
    } catch (error) {
      toast.error(backupErrorMessage(t, error));
    }
  };

  const start = async () => {
    if (!snapshot) return;
    try {
      await Backup.Restore(snapshot.id, chosen, destination);
      toast(t("page.backup.restore.started"));
      close();
    } catch (error) {
      toast.error(backupErrorMessage(t, error));
    }
  };

  return (
    <Dialog open={!!snapshot} onOpenChange={(open) => !open && close()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t("page.backup.restore.title")}</DialogTitle>
          <DialogDescription>{t("page.backup.restore.description")}</DialogDescription>
        </DialogHeader>
        <div className="space-y-4">
          <div className="space-y-2">
            {available.map((target) => (
              <label key={target.id} className="flex items-center gap-2 text-sm">
                <Checkbox
                  checked={chosen.includes(target.id)}
                  onCheckedChange={(checked) =>
                    setSelected(
                      checked ? [...chosen, target.id] : chosen.filter((id) => id !== target.id),
                    )
                  }
                />
                <span className="font-medium">{target.label}</span>
                <span className="truncate text-xs text-muted-foreground">{target.sourcePath}</span>
              </label>
            ))}
          </div>
          <div className="flex items-center gap-2">
            <Button variant="outline" size="sm" onClick={() => void pickDestination()}>
              <FolderOpenIcon />
              {t("page.backup.restore.choose_folder")}
            </Button>
            <span className="truncate text-xs text-muted-foreground" title={destination}>
              {destination || t("page.backup.restore.no_folder")}
            </span>
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={close}>
            {t("g.cancel")}
          </Button>
          <Button disabled={!destination || chosen.length === 0} onClick={() => void start()}>
            {t("page.backup.restore.start")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
