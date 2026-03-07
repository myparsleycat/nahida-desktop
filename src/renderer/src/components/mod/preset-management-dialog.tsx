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
import { useTranslation } from "react-i18next";

export function PresetManagementDialog() {
  const { t } = useTranslation();
  const isOpen = useModStore((s) => s.isSelectedPresetDialogOpen);
  const setIsOpen = useModStore((s) => s.setIsSelectedPresetDialogOpen);
  const selectedPreset = useModStore((s) => s.selectedPreset);

  const { applyPresetMutation, deletePresetMutation } = usePresetMutations();

  return (
    <Dialog open={isOpen} onOpenChange={setIsOpen}>
      <DialogContent className="w-100">
        <DialogHeader>
          <DialogTitle>{selectedPreset?.name}</DialogTitle>
        </DialogHeader>
        <div className="space-y-2">
          {selectedPreset?.description && (
            <p className="text-sm text-muted-foreground whitespace-pre-wrap">
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
            onClick={() => selectedPreset && deletePresetMutation.mutate(selectedPreset.id)}
          >
            {t("g.delete")}
          </Button>
          <div className="flex gap-2">
            <DialogClose asChild>
              <Button variant="outline">{t("g.cancel")}</Button>
            </DialogClose>
            <Button
              disabled={selectedPreset?.isLegacy}
              onClick={() => selectedPreset && applyPresetMutation.mutate(selectedPreset.id)}
            >
              {t("g.apply")}
            </Button>
          </div>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
