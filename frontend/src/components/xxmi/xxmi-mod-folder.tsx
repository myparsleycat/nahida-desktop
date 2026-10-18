import { Dialog } from "@bindings/platform";
import { XXMI } from "@bindings/xxmi";
import { Alert, AlertDescription } from "@renderer/components/ui/alert";
import {
  AlertDialog,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@renderer/components/ui/alert-dialog";
import { Badge } from "@renderer/components/ui/badge";
import { Button } from "@renderer/components/ui/button";
import { Input } from "@renderer/components/ui/input";
import { toErrorMessage } from "@shared/utils";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { FolderOpenIcon, LinkIcon, TriangleAlertIcon, UnlinkIcon } from "lucide-react";
import { useId, useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

const modFolderErrorCodes = [
  "XXMI_MOD_FOLDER_TARGET_NOT_EMPTY",
  "XXMI_MOD_FOLDER_INVALID_TARGET",
  "XXMI_IMPORT_NO_SPACE",
  "XXMI_GAME_RUNNING",
  "XXMI_BUSY",
] as const;

export function modFolderQueryKey(importer: string) {
  return ["xxmi:mod-folder", importer] as const;
}

// XXMI only reads <importer folder>\Mods, so a folder elsewhere is shown as the target of a junction there.
export function XXMIModFolderField({
  importer,
  disabledReason,
}: {
  importer: string;
  disabledReason?: string;
}) {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const inputId = useId();
  const descriptionId = useId();
  const { data: folder } = useQuery({
    queryKey: modFolderQueryKey(importer),
    queryFn: () => XXMI.GetImporterModFolder(importer),
  });
  // An empty target unlinks Mods.
  const [pendingTarget, setPendingTarget] = useState<string | null>(null);
  const [isApplying, setIsApplying] = useState(false);

  if (!folder) return null;

  const linked = !!folder.linkTarget;
  const unlinking = pendingTarget === "";
  const label = t("page.setting.xxmi.builtin.modFolder");

  const apply = async (target: string) => {
    setIsApplying(true);
    try {
      const next = await XXMI.SetImporterModFolder(importer, target);
      queryClient.setQueryData(modFolderQueryKey(importer), next);
      setPendingTarget(null);
      toast.success(
        t(
          target
            ? "page.setting.xxmi.builtin.modFolderChanged"
            : "page.setting.xxmi.builtin.modFolderUnlinked",
        ),
      );
      next.warnings?.forEach((warning) => toast.warning(warning));
      // The game keeps the Mods path while what it holds changes, so its listings are refetched.
      void queryClient.invalidateQueries({ queryKey: ["games"] });
      void queryClient.invalidateQueries({ queryKey: ["characters"] });
      void queryClient.invalidateQueries({ queryKey: ["modGroup"] });
    } catch (error) {
      const message = toErrorMessage(error);
      const code = modFolderErrorCodes.find((code) => message.includes(code));
      toast.error(code ? t(`page.setting.xxmi.builtin.modFolderErrors.${code}`) : message);
    } finally {
      setIsApplying(false);
    }
  };

  return (
    <div className="space-y-1.5">
      <div className="min-w-0 space-y-0.5">
        <div className="flex items-center gap-2">
          <label htmlFor={inputId} className="text-sm font-medium">
            {label}
          </label>
          {linked && (
            <Badge variant={folder.targetMissing ? "destructive" : "secondary"}>
              <LinkIcon />
              {t("page.setting.xxmi.builtin.modFolderLinked")}
            </Badge>
          )}
        </div>
        <p id={descriptionId} className="text-xs break-all text-muted-foreground">
          {linked
            ? t("page.setting.xxmi.builtin.modFolderLinkedDescription", { path: folder.path })
            : t("page.setting.xxmi.builtin.modFolderDescription")}
        </p>
      </div>
      <div className="flex gap-2">
        <Input
          id={inputId}
          aria-describedby={descriptionId}
          readOnly
          hideFocusRing
          spellCheck={false}
          value={linked ? folder.linkTarget : folder.path}
        />
        <Button
          variant="outline"
          size="icon"
          aria-label={t("page.setting.xxmi.builtin.modFolderChange")}
          title={disabledReason ?? t("page.setting.xxmi.builtin.modFolderChange")}
          disabled={!!disabledReason}
          onClickPromise={async () => {
            try {
              const selected = await Dialog.ShowOpenDialog({
                title: label,
                defaultPath: linked ? folder.linkTarget : folder.path,
                filters: [],
                properties: ["openDirectory"],
              });
              const path = selected.filePaths?.[0];
              if (!selected.canceled && path) setPendingTarget(path);
            } catch (error) {
              toast.error(toErrorMessage(error));
            }
          }}
        >
          <FolderOpenIcon />
        </Button>
        {linked && (
          <Button
            variant="outline"
            size="icon"
            aria-label={t("page.setting.xxmi.builtin.modFolderUnlink")}
            title={disabledReason ?? t("page.setting.xxmi.builtin.modFolderUnlink")}
            disabled={!!disabledReason}
            onClick={() => setPendingTarget("")}
          >
            <UnlinkIcon />
          </Button>
        )}
      </div>
      {folder.targetMissing && (
        <Alert variant="destructive">
          <TriangleAlertIcon />
          <AlertDescription>
            {t("page.setting.xxmi.builtin.modFolderTargetMissing")}
          </AlertDescription>
        </Alert>
      )}

      <AlertDialog
        open={pendingTarget !== null}
        onOpenChange={(open) => !open && !isApplying && setPendingTarget(null)}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {unlinking
                ? t("page.setting.xxmi.builtin.modFolderUnlinkTitle")
                : t("page.setting.xxmi.builtin.modFolderChangeTitle")}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {unlinking
                ? t("page.setting.xxmi.builtin.modFolderUnlinkDescription")
                : linked
                  ? t("page.setting.xxmi.builtin.modFolderRelinkDescription")
                  : t("page.setting.xxmi.builtin.modFolderMoveDescription")}
            </AlertDialogDescription>
          </AlertDialogHeader>
          {!unlinking && pendingTarget && (
            <p className="rounded-md bg-muted/50 px-3 py-2 text-xs break-all">{pendingTarget}</p>
          )}
          <AlertDialogFooter>
            <AlertDialogCancel disabled={isApplying}>{t("g.cancel")}</AlertDialogCancel>
            <Button disabled={isApplying} onClickPromise={() => apply(pendingTarget ?? "")}>
              {unlinking
                ? t("page.setting.xxmi.builtin.modFolderUnlink")
                : t("page.setting.xxmi.builtin.modFolderChange")}
            </Button>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
