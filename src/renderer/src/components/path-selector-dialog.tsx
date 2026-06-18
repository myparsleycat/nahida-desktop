import { Button } from "@renderer/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@renderer/components/ui/dialog";
import { useModStore } from "@renderer/store/mod";
import { useNavigate } from "@tanstack/react-router";
import { FolderOpen, Grid3x3 } from "lucide-react";
import { useRef } from "react";
import { useTranslation } from "react-i18next";

interface PathSelectorDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  selectionId: string;
  suggestedName?: string;
  downloadTargetName?: string;
}

export function PathSelectorDialog({
  open,
  onOpenChange,
  selectionId,
  suggestedName,
  downloadTargetName,
}: PathSelectorDialogProps) {
  const { t } = useTranslation();
  const navi = useNavigate();
  const setDownloadMode = useModStore((s) => s.setDownloadMode);
  const skipCancelOnCloseRef = useRef(false);

  const closeWithoutCancel = () => {
    skipCancelOnCloseRef.current = true;
    onOpenChange(false);
  };

  const cancelSelection = async () => {
    await window.api.invoke("pathSelector:cancel", selectionId);
  };

  const handleOpenChange = (nextOpen: boolean) => {
    if (nextOpen) {
      onOpenChange(true);
      return;
    }

    if (skipCancelOnCloseRef.current) {
      skipCancelOnCloseRef.current = false;
      onOpenChange(false);
      return;
    }

    void cancelSelection().finally(() => onOpenChange(false));
  };

  const handleFolderSelect = async () => {
    await window.api.invoke("pathSelector:selectFolderPath", selectionId);
    closeWithoutCancel();
  };

  const handleModManagerSelect = () => {
    // Navigate to mod page with download mode set
    setDownloadMode({ downloadId: selectionId, suggestedName, downloadTargetName });
    void navi({ to: "/mod" });
    closeWithoutCancel();
  };

  const handleCancel = async () => {
    await cancelSelection();
    closeWithoutCancel();
  };

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogContent onOpenAutoFocus={(e) => e.preventDefault()} showCloseButton={false}>
        <DialogHeader>
          <DialogTitle>{t("components.path-selector-dialog.title")}</DialogTitle>
          <DialogDescription>
            {t("components.path-selector-dialog.description", { name: suggestedName })}
          </DialogDescription>
        </DialogHeader>

        <div className="grid grid-cols-2 gap-3 py-4">
          <Button
            variant="outline"
            className="h-auto p-3 flex flex-col items-center gap-2 whitespace-normal"
            onClick={handleModManagerSelect}
          >
            <Grid3x3 className="size-8" />
            <div className="flex flex-col items-center text-center">
              <span className="font-semibold">
                {t("components.path-selector-dialog.mod_manager.title")}
              </span>
              <div className="flex flex-col items-center text-center text-xs text-muted-foreground">
                <span>{t("components.path-selector-dialog.mod_manager.description.0")}</span>
                <span>{t("components.path-selector-dialog.mod_manager.description.1")}</span>
              </div>
            </div>
          </Button>

          <Button
            variant="outline"
            className="h-auto p-3 flex flex-col items-center gap-2 whitespace-normal"
            onClick={handleFolderSelect}
          >
            <FolderOpen className="size-8" />
            <div className="flex flex-col items-center text-center">
              <span className="font-semibold">
                {t("components.path-selector-dialog.explorer.title")}
              </span>
              <div className="flex flex-col items-center text-center text-xs text-muted-foreground">
                <span>{t("components.path-selector-dialog.explorer.description.0")}</span>
                <span>{t("components.path-selector-dialog.explorer.description.1")}</span>
              </div>
            </div>
          </Button>
        </div>

        <DialogFooter>
          <Button variant="ghost" onClick={handleCancel}>
            {t("g.cancel")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
