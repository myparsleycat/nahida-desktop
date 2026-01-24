import { Button } from "@renderer/components/ui/button";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@renderer/components/ui/dialog";
import { useModStore } from "@renderer/store/mod";
import { usePresetMutations } from "@renderer/hooks/use-mod-mutations";

export function PresetManagementDialog() {
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
          <p className="text-sm text-muted-foreground">
            이 프리셋에는 {selectedPreset?.mods.length || 0}개의 모드가 저장되어 있습니다.
          </p>
        </div>
        <DialogFooter className="flex justify-between">
          <Button
            variant="destructive"
            onClick={() => selectedPreset && deletePresetMutation.mutate(selectedPreset.id)}
          >
            삭제
          </Button>
          <div className="flex gap-2">
            <DialogClose asChild>
              <Button variant="outline">취소</Button>
            </DialogClose>
            <Button onClick={() => selectedPreset && applyPresetMutation.mutate(selectedPreset.id)}>
              적용
            </Button>
          </div>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
