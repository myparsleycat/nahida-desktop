import { Dialog } from "@bindings/platform";
import { XXMI } from "@bindings/xxmi";
import type { ImporterIconChange } from "@bindings/xxmi/models";
import { GameIcon } from "@renderer/components/game-icon";
import { Button } from "@renderer/components/ui/button";
import { importerIconsQueryKey, useImporterIcons } from "@renderer/hooks/use-importer-icons";
import { toErrorMessage } from "@shared/utils";
import { useQueryClient } from "@tanstack/react-query";
import { ImageUpIcon, RotateCcwIcon } from "lucide-react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

// Keep in sync with convertImporterIcon in internal/xxmi/importer_icon_windows.go.
const invalidIconCode = "XXMI_SHORTCUT_ICON_INVALID";

export function XXMIImporterIconField({ importer }: { importer: string }) {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const { data: icons, isError } = useImporterIcons();
  const icon = icons?.find((entry) => entry.key === importer);
  const unavailable = isError || icon?.unavailable;

  const apply = async (change: Promise<ImporterIconChange>) => {
    try {
      const result = await change;
      toast.success(
        t(
          result.shortcutUpdated
            ? "page.setting.xxmi.builtin.importerIconShortcutUpdated"
            : "page.setting.xxmi.builtin.importerIconChanged",
        ),
      );
    } catch (error) {
      const message = toErrorMessage(error);
      toast.error(
        message.includes(invalidIconCode)
          ? t("page.setting.xxmi.builtin.importerIconInvalid")
          : message,
      );
    } finally {
      await queryClient.invalidateQueries({ queryKey: importerIconsQueryKey });
    }
  };

  return (
    <div className="flex items-center justify-between gap-6">
      <div className="flex min-w-0 items-center gap-3">
        <GameIcon gameName={importer} className="size-10 shrink-0 rounded-md object-contain" />
        <div className="min-w-0 space-y-0.5">
          <span className="text-sm font-medium">{t("page.setting.xxmi.builtin.importerIcon")}</span>
          <p className="text-xs text-muted-foreground">
            {t("page.setting.xxmi.builtin.importerIconDescription")}
          </p>
          {unavailable && (
            <p className="text-xs text-destructive">
              {t("page.setting.xxmi.builtin.importerIconLoadFailed")}
            </p>
          )}
        </div>
      </div>
      <div className="flex shrink-0 gap-2">
        {(icon?.custom || isError) && (
          <Button
            variant="outline"
            size="sm"
            onClickPromise={() => apply(XXMI.ResetImporterIcon(importer))}
          >
            <RotateCcwIcon />
            {t("page.setting.xxmi.builtin.importerIconReset")}
          </Button>
        )}
        <Button
          variant="outline"
          size="sm"
          onClickPromise={async () => {
            const picked = await Dialog.ShowOpenDialog({
              title: t("page.setting.xxmi.builtin.importerIconChange"),
              defaultPath: "",
              filters: [
                {
                  name: t("page.setting.xxmi.builtin.importerIconFilter"),
                  extensions: ["png", "jpg", "jpeg", "webp", "avif"],
                },
              ],
              properties: ["openFile"],
              role: "xxmi-importer-icon",
            }).catch((error: unknown) => {
              toast.error(toErrorMessage(error));
              return undefined;
            });
            if (!picked || picked.canceled || !picked.filePaths?.[0]) return;
            await apply(XXMI.SetImporterIcon(importer, picked.filePaths[0]));
          }}
        >
          <ImageUpIcon />
          {t("page.setting.xxmi.builtin.importerIconChange")}
        </Button>
      </div>
    </div>
  );
}
