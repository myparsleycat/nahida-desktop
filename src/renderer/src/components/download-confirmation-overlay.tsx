import { Button } from "@renderer/components/ui/button";
import { Checkbox } from "@renderer/components/ui/checkbox";
import { Input } from "@renderer/components/ui/input";
import { useSetting } from "@renderer/hooks/use-settings";
import { Logger } from "@renderer/lib/logger";
import { setSetting } from "@renderer/lib/settings";
import { useModStore } from "@renderer/store/mod";
import { useNavigate } from "@tanstack/react-router";
import { Download } from "lucide-react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

export function DownloadConfirmationOverlay() {
  const { t } = useTranslation();
  const navigate = useNavigate();

  const downloadMode = useModStore((s) => s.downloadMode);
  const setDownloadMode = useModStore((s) => s.setDownloadMode);
  const resetUserSelectedDuringDownload = useModStore((s) => s.resetUserSelectedDuringDownload);
  const selectedGroup = useModStore((s) => s.selectedGroup);

  const { data: returnToGamebanana = false } = useSetting("mod.returnToGamebananaAfterDownload");

  const selectedPath = selectedGroup?.path || null;
  const selectedGroupName = selectedGroup?.name;
  const suggestedName = downloadMode?.suggestedName;

  const [fileName, setFileName] = useState(suggestedName || "");
  const [shouldReturnToGamebanana, setShouldReturnToGamebanana] = useState(returnToGamebanana);

  useEffect(() => {
    setFileName(suggestedName || "");
  }, [suggestedName]);

  useEffect(() => {
    setShouldReturnToGamebanana(returnToGamebanana);
  }, [returnToGamebanana]);

  const handleConfirm = async () => {
    if (!downloadMode || !selectedGroup) return;

    try {
      await window.api.invoke(
        "pathSelector:selectModManagerPath",
        downloadMode.downloadId,
        selectedGroup.path,
        suggestedName ? fileName.trim() : undefined,
      );

      setDownloadMode(null);
      resetUserSelectedDuringDownload();
      if (downloadMode.downloadSource === "gamebanana" && shouldReturnToGamebanana) {
        void navigate({ to: "/gamebanana" });
      }
    } catch (error) {
      toast.error(t("components.download-confirmation-overlay.select_path_failed"));
      Logger.error(error, "DownloadConfirmationOverlay:handleConfirm");
    }
  };

  const handleCancel = async () => {
    if (!downloadMode) return;

    try {
      await window.api.invoke("pathSelector:cancel", downloadMode.downloadId);
      setDownloadMode(null);
      resetUserSelectedDuringDownload();
    } catch (error) {
      Logger.error(error, "DownloadConfirmationOverlay:handleCancel");
    }
  };

  if (!downloadMode) return null;

  return (
    <div
      className="absolute inset-0 z-50 flex items-center justify-center bg-black/20"
      onClick={(e) => {
        e.stopPropagation();
      }}
    >
      <div
        className="mx-4 w-full max-w-md rounded-lg bg-background/75 p-4 shadow-lg outline backdrop-blur-lg"
        onClick={(e) => {
          e.stopPropagation();
        }}
      >
        <div className="space-y-4">
          <div className="flex items-start justify-between">
            <div>
              <h3 className="text-lg font-semibold">
                {t("components.download-confirmation-overlay.title")}
              </h3>
              <p className="mt-1 text-sm text-muted-foreground">
                {t("components.download-confirmation-overlay.description")}
              </p>
            </div>
          </div>

          {suggestedName && (
            <div className="space-y-1">
              <p className="text-sm font-medium">
                {t("components.download-confirmation-overlay.file_name")}
              </p>
              <Input
                value={fileName}
                onChange={(e) => setFileName(e.target.value)}
                placeholder={t("components.download-confirmation-overlay.file_name_placeholder")}
                transparentBackground
                className="w-full"
              />
            </div>
          )}

          <div className="space-y-1">
            <p className="text-sm font-medium">
              {t("components.download-confirmation-overlay.download_location")}
            </p>
            <Input
              value={
                selectedGroupName
                  ? selectedGroupName
                  : t("components.download-confirmation-overlay.need_select_character_folder")
              }
              className="w-full"
              hideFocusRing
              transparentBackground
              readOnly
            />
          </div>

          {downloadMode.downloadSource === "gamebanana" && (
            <label className="flex cursor-pointer items-center gap-2 text-sm">
              <Checkbox
                checked={shouldReturnToGamebanana}
                onCheckedChange={(checked) => {
                  setShouldReturnToGamebanana(checked);
                  void setSetting("mod.returnToGamebananaAfterDownload", checked);
                }}
              />
              {t("components.download-confirmation-overlay.return_to_gamebanana")}
            </label>
          )}

          <div className="flex gap-2 pt-2">
            <Button variant="outline" onClick={handleCancel} className="flex-1">
              {t("g.cancel")}
            </Button>
            <Button
              onClick={handleConfirm}
              disabled={!selectedPath || (suggestedName ? !fileName.trim() : false)}
              className="flex-1"
            >
              <Download className="mr-2 size-4" />
              {t("g.select")}
            </Button>
          </div>
        </div>
      </div>
    </div>
  );
}
