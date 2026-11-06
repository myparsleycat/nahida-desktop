import { Dialog } from "@bindings/platform";
import { XXMI } from "@bindings/xxmi";
import type { ShortcutIconChange } from "@bindings/xxmi/models";
import { GameIcon } from "@renderer/components/game-icon";
import { Button } from "@renderer/components/ui/button";
import { localFileSrc } from "@renderer/lib/local-file";
import { toErrorMessage } from "@shared/utils";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { ImageUpIcon, RotateCcwIcon } from "lucide-react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

// Keep in sync with convertShortcutIcon in internal/xxmi/shortcut_icon_windows.go.
const invalidIconCode = "XXMI_SHORTCUT_ICON_INVALID";

export function XXMIShortcutIconField({ importer }: { importer: string }) {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const queryKey = ["xxmi:shortcut-icon", importer];
  const { data: icon, isError } = useQuery({
    queryKey,
    queryFn: () => XXMI.GetShortcutIcon(importer),
  });

  const apply = async (change: Promise<ShortcutIconChange>) => {
    try {
      const result = await change;
      toast.success(
        t(
          result.shortcutUpdated
            ? "page.setting.xxmi.builtin.shortcutIconShortcutUpdated"
            : "page.setting.xxmi.builtin.shortcutIconChanged",
        ),
      );
    } catch (error) {
      const message = toErrorMessage(error);
      toast.error(
        message.includes(invalidIconCode)
          ? t("page.setting.xxmi.builtin.shortcutIconInvalid")
          : message,
      );
    } finally {
      await queryClient.invalidateQueries({ queryKey });
    }
  };

  return (
    <div className="flex items-center justify-between gap-6">
      <div className="flex min-w-0 items-center gap-3">
        {icon?.custom ? (
          <img
            src={localFileSrc(icon.path)}
            alt={importer}
            className="size-10 shrink-0 rounded-md object-contain"
          />
        ) : (
          <GameIcon gameName={importer} className="size-10 shrink-0 rounded-md object-contain" />
        )}
        <div className="min-w-0 space-y-0.5">
          <span className="text-sm font-medium">{t("page.setting.xxmi.builtin.shortcutIcon")}</span>
          <p className="text-xs text-muted-foreground">
            {t("page.setting.xxmi.builtin.shortcutIconDescription")}
          </p>
          {isError && (
            <p className="text-xs text-destructive">
              {t("page.setting.xxmi.builtin.shortcutIconLoadFailed")}
            </p>
          )}
        </div>
      </div>
      <div className="flex shrink-0 gap-2">
        {(icon?.custom || isError) && (
          <Button
            variant="outline"
            size="sm"
            onClickPromise={() => apply(XXMI.ResetShortcutIcon(importer))}
          >
            <RotateCcwIcon />
            {t("page.setting.xxmi.builtin.shortcutIconReset")}
          </Button>
        )}
        <Button
          variant="outline"
          size="sm"
          onClickPromise={async () => {
            const picked = await Dialog.ShowOpenDialog({
              title: t("page.setting.xxmi.builtin.shortcutIconChange"),
              defaultPath: "",
              filters: [
                {
                  name: t("page.setting.xxmi.builtin.shortcutIconFilter"),
                  extensions: ["png", "jpg", "jpeg", "webp"],
                },
              ],
              properties: ["openFile"],
              role: "xxmi-shortcut-icon",
            }).catch((error: unknown) => {
              toast.error(toErrorMessage(error));
              return undefined;
            });
            if (!picked || picked.canceled || !picked.filePaths?.[0]) return;
            await apply(XXMI.SetShortcutIcon(importer, picked.filePaths[0]));
          }}
        >
          <ImageUpIcon />
          {t("page.setting.xxmi.builtin.shortcutIconChange")}
        </Button>
      </div>
    </div>
  );
}
