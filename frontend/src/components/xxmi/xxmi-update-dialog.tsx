import { XXMI } from "@bindings/xxmi";
import { Button } from "@renderer/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@renderer/components/ui/dialog";
import { useQuery } from "@tanstack/react-query";
import { DownloadIcon } from "lucide-react";
import { useTranslation } from "react-i18next";

export type UpdateStatus = NonNullable<Awaited<ReturnType<typeof XXMI.CheckUpdates>>>[number];

// The dialog stays open while an importer is set and lists that importer's updates before installing them.
export function XXMIUpdateDialog({
  importer,
  updates,
  confirmLabel,
  onConfirm,
  onClose,
}: {
  importer: string | null;
  updates: UpdateStatus[];
  confirmLabel: string;
  onConfirm: () => Promise<void>;
  onClose: () => void;
}) {
  const { t } = useTranslation();

  return (
    <Dialog open={importer !== null} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>
            {t("page.setting.xxmi.builtin.updateDialogTitle", { importer })}
          </DialogTitle>
          <DialogDescription>
            {t("page.setting.xxmi.builtin.updateDialogDescription")}
          </DialogDescription>
        </DialogHeader>
        <div className="max-h-96 space-y-3 overflow-y-auto">
          {updates.map((entry) => (
            <UpdateEntry key={entry.package} entry={entry} />
          ))}
        </div>
        <div className="flex justify-end gap-2">
          <Button variant="outline" onClick={onClose}>
            {t("g.cancel")}
          </Button>
          <Button onClickPromise={onConfirm}>
            <DownloadIcon />
            {confirmLabel}
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}

function UpdateEntry({ entry }: { entry: UpdateStatus }) {
  const { t } = useTranslation();
  const { data: releases } = useQuery({
    queryKey: ["xxmi:package-releases", entry.package],
    queryFn: () => XXMI.ListReleases(entry.package),
    staleTime: 60 * 60 * 1000,
    retry: false,
  });
  const notes = releases?.find((release) => release.version === entry.latestVersion)?.notes;

  return (
    <div className="space-y-2 rounded-lg bg-muted/50 p-3">
      <p className="font-mono text-xs break-all">
        {entry.package}: {entry.installed || t("page.setting.xxmi.builtin.notInstalled")} →{" "}
        {entry.latestVersion}
      </p>
      {notes && (
        <p className="max-h-48 overflow-y-auto text-xs whitespace-pre-wrap text-muted-foreground">
          {notes}
        </p>
      )}
    </div>
  );
}
