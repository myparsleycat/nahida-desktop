import { Backup, type Target } from "@bindings/backup";
import { Dialog } from "@bindings/platform";
import { Badge } from "@renderer/components/ui/badge";
import { Button } from "@renderer/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@renderer/components/ui/card";
import { Switch } from "@renderer/components/ui/switch";
import { useQueryClient } from "@tanstack/react-query";
import { FolderIcon, FolderPlusIcon, GamepadIcon, TrashIcon } from "lucide-react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

import { backupErrorMessage } from "./errors";
import { backupOverviewKey } from "./queries";

export function BackupTargetsCard({ targets }: { targets: Target[] }) {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const refresh = () => queryClient.invalidateQueries({ queryKey: backupOverviewKey });

  const attempt = async (action: () => Promise<unknown>) => {
    try {
      await action();
      await refresh();
    } catch (error) {
      toast.error(backupErrorMessage(t, error));
    }
  };

  const addFolder = async () => {
    const result = await Dialog.SelectDirectory();
    const path = result.filePath;
    if (result.canceled || !path) return;
    await attempt(() => Backup.AddCustomPath(path));
  };

  const games = targets.filter((target) => target.kind === "game");
  const custom = targets.filter((target) => target.kind === "custom");

  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between gap-4">
        <div className="space-y-0.5">
          <CardTitle className="text-sm font-medium">{t("page.backup.targets.title")}</CardTitle>
          <p className="text-xs text-muted-foreground">{t("page.backup.targets.description")}</p>
        </div>
        <Button variant="outline" size="sm" onClick={() => void addFolder()}>
          <FolderPlusIcon />
          {t("page.backup.targets.add_folder")}
        </Button>
      </CardHeader>
      <CardContent className="space-y-1">
        {games.length === 0 && custom.length === 0 && (
          <p className="py-6 text-center text-sm text-muted-foreground">
            {t("page.backup.targets.empty")}
          </p>
        )}
        {games.map((target) => (
          <TargetRow key={target.id} target={target} icon={<GamepadIcon className="size-4" />}>
            <Switch
              checked={!target.excluded}
              aria-label={t("page.backup.targets.include", { name: target.label })}
              onCheckedChange={(included) =>
                void attempt(() => Backup.SetGameExcluded(target.game ?? target.label, !included))
              }
            />
          </TargetRow>
        ))}
        {custom.map((target) => (
          <TargetRow key={target.id} target={target} icon={<FolderIcon className="size-4" />}>
            <Button
              variant="ghost"
              size="icon-sm"
              aria-label={t("page.backup.targets.remove", { name: target.label })}
              onClick={() => void attempt(() => Backup.RemoveCustomPath(target.id))}
            >
              <TrashIcon />
            </Button>
          </TargetRow>
        ))}
      </CardContent>
    </Card>
  );
}

function TargetRow({
  target,
  icon,
  children,
}: {
  target: Target;
  icon: React.ReactNode;
  children: React.ReactNode;
}) {
  const { t } = useTranslation();
  return (
    <div className="flex items-center gap-3 rounded-md px-2 py-2 hover:bg-muted/50">
      <span className="text-muted-foreground">{icon}</span>
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2">
          <span className="truncate text-sm font-medium">{target.label}</span>
          {target.missing && (
            <Badge variant="destructive">{t("page.backup.targets.missing")}</Badge>
          )}
          {target.excluded && (
            <Badge variant="secondary">{t("page.backup.targets.excluded")}</Badge>
          )}
        </div>
        <p className="truncate text-xs text-muted-foreground" title={target.path}>
          {target.path}
        </p>
      </div>
      {children}
    </div>
  );
}
