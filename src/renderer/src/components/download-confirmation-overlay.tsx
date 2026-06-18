import { Button } from "@renderer/components/ui/button";
import { Input } from "@renderer/components/ui/input";
import { Logger } from "@renderer/lib/logger";
import { useModStore } from "@renderer/store/mod";
import { Download } from "lucide-react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

export function DownloadConfirmationOverlay() {
  const { t } = useTranslation();

  const downloadMode = useModStore((s) => s.downloadMode);
  const setDownloadMode = useModStore((s) => s.setDownloadMode);
  const selectedGroup = useModStore((s) => s.selectedGroup);

  const selectedPath = selectedGroup?.path || null;
  const selectedGroupName = selectedGroup?.name;
  const suggestedName = downloadMode?.suggestedName;

  const [fileName, setFileName] = useState(suggestedName || "");

  useEffect(() => {
    setFileName(suggestedName || "");
  }, [suggestedName]);

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
        className="bg-background/75 backdrop-blur-lg rounded-lg p-4 max-w-md w-full mx-4 shadow-lg outline"
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
              <p className="text-sm text-muted-foreground mt-1">
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

          <div className="flex gap-2 pt-2">
            <Button variant="outline" onClick={handleCancel} className="flex-1">
              {t("g.cancel")}
            </Button>
            <Button
              onClick={handleConfirm}
              disabled={!selectedPath || (suggestedName ? !fileName.trim() : false)}
              className="flex-1"
            >
              <Download className="size-4 mr-2" />
              {t("g.select")}
            </Button>
          </div>
        </div>
      </div>
    </div>
  );
}
