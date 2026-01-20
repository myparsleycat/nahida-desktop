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
      <DialogContent className="w-112.5">
        <DialogHeader>
          <DialogTitle>경로 선택</DialogTitle>
          <DialogDescription>
            {suggestedName && `"${suggestedName}" `}
            경로를 선택하는 방법을 선택하세요.
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
              <span className="font-semibold">매니저에서 선택</span>
              <div className="flex flex-col items-center text-center text-xs text-muted-foreground">
                <span>모드 매니저에서</span>
                <span>캐릭터 폴더를 선택합니다</span>
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
              <span className="font-semibold">경로 선택</span>
              <div className="flex flex-col items-center text-center text-xs text-muted-foreground">
                <span>파일 탐색기에서</span>
                <span>폴더를 선택합니다</span>
              </div>
            </div>
          </Button>
        </div>

        <DialogFooter>
          <Button variant="ghost" onClick={handleCancel}>
            취소
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
