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

interface DownloadModeDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  downloadId: string;
  suggestedName?: string;
}

export function DownloadModeDialog({
  open,
  onOpenChange,
  downloadId,
  suggestedName,
}: DownloadModeDialogProps) {
  const navi = useNavigate();

  const handleFolderSelect = async () => {
    await window.api.invoke("drive:fn:selectPathForDownload", downloadId);
    onOpenChange(false);
  };

  const handleModManagerSelect = () => {
    navi({ to: "/mod" });
    sessionStorage.setItem("pendingDownload", JSON.stringify({ downloadId, suggestedName }));
    onOpenChange(false);
  };

  const handleCancel = async () => {
    await window.api.invoke("drive:fn:cancelPendingDownload", downloadId);
    onOpenChange(false);
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="w-112.5">
        <DialogHeader>
          <DialogTitle>다운로드 경로 선택</DialogTitle>
          <DialogDescription>
            {suggestedName && `"${suggestedName}" `}
            다운로드 경로를 선택하는 방법을 선택하세요.
          </DialogDescription>
        </DialogHeader>

        <div className="grid gap-3 py-4">
          <Button
            variant="outline"
            className="h-auto py-6 px-6 flex flex-col items-center gap-2"
            onClick={handleFolderSelect}
          >
            <FolderOpen className="size-8" />
            <div className="flex flex-col items-center">
              <span className="font-semibold">경로 선택</span>
              <span className="text-xs text-muted-foreground">
                파일 탐색기에서 폴더를 선택합니다
              </span>
            </div>
          </Button>

          <Button
            variant="outline"
            className="h-auto py-6 px-6 flex flex-col items-center gap-2"
            onClick={handleModManagerSelect}
          >
            <Grid3x3 className="size-8" />
            <div className="flex flex-col items-center">
              <span className="font-semibold">매니저에서 선택</span>
              <span className="text-xs text-muted-foreground">
                모드 매니저에서 캐릭터 폴더를 선택합니다
              </span>
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
