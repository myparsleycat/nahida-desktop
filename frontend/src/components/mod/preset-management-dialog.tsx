import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@renderer/components/ui/alert-dialog";
import { Button } from "@renderer/components/ui/button";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@renderer/components/ui/dialog";
import { usePresetMutations } from "@renderer/hooks/use-mod-mutations";
import { useModStore } from "@renderer/store/mod";
import { useState } from "react";
import { useTranslation } from "react-i18next";

export function PresetManagementDialog() {
  const { t } = useTranslation();
  const isOpen = useModStore((s) => s.isSelectedPresetDialogOpen);
  const setIsOpen = useModStore((s) => s.setIsSelectedPresetDialogOpen);
  const selectedPreset = useModStore((s) => s.selectedPreset);
  const [isDeleteConfirmOpen, setIsDeleteConfirmOpen] = useState(false);

  const { applyPresetMutation, deletePresetMutation } = usePresetMutations();
  const isBusy = applyPresetMutation.isPending || deletePresetMutation.isPending;

  return (
    <Dialog open={isOpen} onOpenChange={setIsOpen}>
      <DialogContent className="w-100">
        <DialogHeader>
          <DialogTitle>{selectedPreset?.name}</DialogTitle>
        </DialogHeader>
        <div className="space-y-2">
          {selectedPreset?.description && (
            <p className="text-sm whitespace-pre-wrap text-muted-foreground">
              {selectedPreset.description}
            </p>
          )}
          {selectedPreset?.isLegacy && (
            <p className="text-sm text-destructive">
              {t("page.mod.dialog.preset-management.legacy-description")}
            </p>
          )}
        </div>
        <DialogFooter className="flex justify-between">
          <Button
            variant="destructive"
            disabled={!selectedPreset || isBusy}
            onClick={() => setIsDeleteConfirmOpen(true)}
          >
            {t("g.delete")}
          </Button>
          <div className="flex gap-2">
            <DialogClose render={<Button variant="outline" />}>{t("g.cancel")}</DialogClose>
            <Button
              disabled={!selectedPreset || selectedPreset.isLegacy || isBusy}
              onClick={() => selectedPreset && applyPresetMutation.mutate(selectedPreset.id)}
            >
              {t("g.apply")}
            </Button>
          </div>
        </DialogFooter>
      </DialogContent>

      <AlertDialog open={isDeleteConfirmOpen} onOpenChange={setIsDeleteConfirmOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {t("page.mod.dialog.preset-management.delete-title")}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {t("page.mod.dialog.preset-management.delete-description", {
                name: selectedPreset?.name ?? "",
              })}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t("g.cancel")}</AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              disabled={isBusy}
              onClick={() => {
                setIsDeleteConfirmOpen(false);
                if (selectedPreset) {
                  deletePresetMutation.mutate(selectedPreset.id);
                }
              }}
            >
              {t("g.delete")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </Dialog>
  );
}
