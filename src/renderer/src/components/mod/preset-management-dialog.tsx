import { Button } from "@renderer/components/ui/button";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@renderer/components/ui/dialog";
import type { Preset } from "@shared/types";

interface PresetManagementDialogProps {
  isOpen: boolean;
  onOpenChange: (open: boolean) => void;
  selectedPreset: Preset | null;
  onApplyPreset: (presetId: string) => void;
  onDeletePreset: (presetId: string) => void;
}

export function PresetManagementDialog({
  isOpen,
  onOpenChange,
  selectedPreset,
  onApplyPreset,
  onDeletePreset,
}: PresetManagementDialogProps) {
  return (
    <Dialog open={isOpen} onOpenChange={onOpenChange}>
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
            onClick={() => selectedPreset && onDeletePreset(selectedPreset.id)}
          >
            삭제
          </Button>
          <div className="flex gap-2">
            <DialogClose asChild>
              <Button variant="outline">취소</Button>
            </DialogClose>
            <Button onClick={() => selectedPreset && onApplyPreset(selectedPreset.id)}>적용</Button>
          </div>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
