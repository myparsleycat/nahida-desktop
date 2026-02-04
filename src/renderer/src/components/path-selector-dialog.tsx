import { Button } from "@renderer/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@renderer/components/ui/dialog";
import { FolderOpen, Grid3x3 } from "lucide-react";
import { useNavigate } from "@tanstack/react-router";
import { useModStore } from "@renderer/store/mod";
import { useTranslation } from "react-i18next";

interface PathSelectorDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  selectionId: string;
  suggestedName?: string;
}

export function PathSelectorDialog({
  open,
  onOpenChange,
  selectionId,
  suggestedName,
}: PathSelectorDialogProps) {
  const { t } = useTranslation();
  const navi = useNavigate();
  const setDownloadMode = useModStore((s) => s.setDownloadMode);

  const handleFolderSelect = async () => {
    await window.api.invoke("pathSelector:selectFolderPath", selectionId);
    onOpenChange(false);
  };

  const handleModManagerSelect = () => {
    // Navigate to mod page with download mode set
    setDownloadMode({ downloadId: selectionId, suggestedName });
    navi({ to: "/mod" });
    onOpenChange(false);
  };

  const handleCancel = async () => {
    await window.api.invoke("pathSelector:cancel", selectionId);
    onOpenChange(false);
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
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
