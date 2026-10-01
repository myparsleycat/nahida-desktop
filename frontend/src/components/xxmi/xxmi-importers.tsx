import { XXMI } from "@bindings/xxmi";
import { GameIcon } from "@renderer/components/game-icon";
import { Checkbox } from "@renderer/components/ui/checkbox";
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuTrigger,
} from "@renderer/components/ui/context-menu";
import { useLaunchGuard } from "@renderer/hooks/use-launch-guard";
import { cn } from "@renderer/lib/utils";
import type { XXMIData } from "@renderer/routes/xxmi/index";
import { toErrorMessage } from "@shared/utils";
import { useQueryClient } from "@tanstack/react-query";
import { EyeIcon, EyeOffIcon, Loader2Icon } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

export function XXMIImporters({ xxmiData }: { xxmiData?: XXMIData }) {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const [processingKey, setProcessingKey] = useState<string | null>(null);
  const [showDisabled, setShowDisabled] = useState(false);
  const { startImporter, launchGuardDialog } = useLaunchGuard();

  if (!xxmiData?.xxmiPath) {
    return null;
  }

  const handleStartGame = async (key: string) => {
    if (processingKey !== null) return;

    setProcessingKey(key);
    try {
      await startImporter(key);
    } catch (error) {
      toast.error(toErrorMessage(error));
    } finally {
      setProcessingKey(null);
    }
  };

  const setImporterEnabled = async (key: string, enabled: boolean) => {
    try {
      await XXMI.SetExternalImporterEnabled(key, enabled);
      // The importer list feeds the mod, tool, and game dialogs as well as this page.
      await queryClient.invalidateQueries();
    } catch (error) {
      toast.error(toErrorMessage(error));
    }
  };

  const isAnyProcessing = processingKey !== null;
  const disabledImporters = xxmiData.disabledImporters ?? [];
  const importers = [
    ...(xxmiData.enabledImporters ?? []).map((importer) => ({ importer, enabled: true })),
    ...(showDisabled ? disabledImporters.map((importer) => ({ importer, enabled: false })) : []),
  ];

  return (
    <div>
      <div className="flex items-center justify-between gap-3">
        <span className="text-sm font-medium">{t("page.setting.xxmi.activeImporter")}</span>
        {disabledImporters.length > 0 && (
          <label className="flex items-center gap-2 text-xs text-muted-foreground">
            <Checkbox
              checked={showDisabled}
              onCheckedChange={(checked) => setShowDisabled(checked)}
            />
            {t("page.setting.xxmi.showDisabledImporters")}
          </label>
        )}
      </div>
      <div className="flex flex-row justify-evenly space-x-2 pt-2">
        {importers.map(({ importer, enabled }) => {
          const isThisProcessing = processingKey === importer.key;

          return (
            <ContextMenu key={importer.key}>
              <ContextMenuTrigger
                render={
                  // A disabled importer stays a live element so its context menu can still open.
                  <button
                    className={cn(
                      "group relative flex flex-col space-y-1",
                      isAnyProcessing && !isThisProcessing && "cursor-not-allowed opacity-50",
                      !enabled && "cursor-default opacity-50 grayscale",
                    )}
                    onClick={enabled ? () => handleStartGame(importer.key) : undefined}
                    disabled={isAnyProcessing}
                    aria-disabled={!enabled}
                  />
                }
              >
                <div className="relative inline-block">
                  <GameIcon gameName={importer.key} className="size-16" />

                  {isThisProcessing && (
                    <div className="absolute inset-0 flex items-center justify-center rounded-md bg-black/60 transition-opacity">
                      <Loader2Icon className="size-8 animate-spin text-white" />
                    </div>
                  )}
                </div>

                <span className="text-center text-xs">{importer.key}</span>
                <span className="text-center text-xs">
                  {importer.installedVersion ?? t("page.setting.xxmi.packageVersionUnknown")}
                </span>
              </ContextMenuTrigger>

              <ContextMenuContent>
                <ContextMenuItem
                  disabled={isAnyProcessing}
                  onClick={() => void setImporterEnabled(importer.key, !enabled)}
                >
                  {enabled ? <EyeOffIcon /> : <EyeIcon />}
                  {t(
                    enabled
                      ? "page.setting.xxmi.disableImporter"
                      : "page.setting.xxmi.enableImporter",
                  )}
                </ContextMenuItem>
              </ContextMenuContent>
            </ContextMenu>
          );
        })}
      </div>
      {launchGuardDialog}
    </div>
  );
}
